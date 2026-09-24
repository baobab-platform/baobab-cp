package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
	basestore "github.com/nabhold/baobab-cp/internal/store"
)

// defaultEventSource is the stable, absolute producer URI used for every
// event this Store emits, per contracts/events/v1/envelope.schema.json's
// "source" field. Store.EventSource overrides it when set.
const defaultEventSource = "urn:baobab-platform:service:baobab-cp"

// provisioningStartedDataSchema is the immutable payload schema URI for the
// tenant-provisioning-started event, per ADR-0004 ("every domain event
// requires an immutable payload schema URI").
const provisioningStartedDataSchema = "https://contracts.baobab-platform.com/control-plane/v1/provisioning-started.schema.json"

type Store struct {
	pool *pgxpool.Pool
	// EventSource overrides defaultEventSource when set; production callers
	// normally leave this at its zero value.
	EventSource string
}

func (s *Store) eventSource() string {
	if s.EventSource != "" {
		return s.EventSource
	}
	return defaultEventSource
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}
func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *Store) ApplyMigrations(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("migration database pool is nil")
	}
	return ApplyMigrations(ctx, s.pool)
}
func (s *Store) GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	var tenant domain.Tenant
	var metadata map[string]string
	if err := s.pool.QueryRow(ctx, `SELECT tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region, metadata, desired_state, observed_state, revision FROM tenants WHERE tenant_id=$1`, tenantID).Scan(&tenant.TenantID, &tenant.LegalEntityID, &tenant.DisplayName, &tenant.IsolationStrategy, &tenant.ResidencyRegion, &metadata, &tenant.DesiredState, &tenant.ObservedState, &tenant.Revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Tenant{}, domain.NotFoundError("tenant not found")
		}
		return domain.Tenant{}, err
	}
	tenant.Metadata = metadata
	return tenant, nil
}
func (s *Store) GetEntitlement(ctx context.Context, tenantID, productID string) (domain.Entitlement, error) {
	var e domain.Entitlement
	// product.product_subscription (migration 000033) has no tier column,
	// same as the table it replaced - the previous query's
	// `coalesce(tier, 'standard')` referenced a column that has never
	// existed in any migration and would fail against a real database the
	// first time this path was hit. Tier is not part of the new
	// contracts/product/v1 canonical model either, so it is fixed to
	// "standard" here rather than queried.
	e.Tier = "standard"
	if err := s.pool.QueryRow(ctx, `SELECT tenant_id, product_id, status FROM product.product_subscription WHERE tenant_id=$1 AND product_id=$2`, tenantID, productID).Scan(&e.TenantID, &e.ProductID, &e.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Entitlement{}, domain.NotFoundError("product entitlement not found")
		}
		return domain.Entitlement{}, err
	}
	return e, nil
}
func (s *Store) UpdateTenantLifecycle(ctx context.Context, tenantID string, next domain.LifecycleStatus) error {
	if !next.Valid() {
		return domain.NotFoundError("invalid tenant lifecycle status")
	}
	res, err := s.pool.Exec(ctx, `UPDATE tenants SET desired_state=$2, observed_state=$2, revision=revision+1 WHERE tenant_id=$1`, tenantID, string(next))
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return domain.NotFoundError("tenant not found")
	}
	return nil
}
func (s *Store) RegisterTenant(ctx context.Context, key string, metadata basestore.RequestMetadata, c domain.RegisterTenant) (domain.Operation, error) {
	payload, _ := json.Marshal(c)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	auditPayload, _ := json.Marshal(map[string]any{
		"legal_entity_id":    c.LegalEntityID,
		"requested_products": c.RequestedProducts,
		"isolation_strategy": c.IsolationStrategy,
		"residency_region":   c.ResidencyRegion,
	})
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Operation{}, err
	}
	defer tx.Rollback(ctx)
	var op domain.Operation
	var prior string
	err = tx.QueryRow(ctx, `SELECT operation_id::text,tenant_id,state,revision,created_at,updated_at,request_hash FROM provisioning_operations WHERE idempotency_key=$1`, key).Scan(&op.OperationID, &op.TenantID, &op.State, &op.Revision, &op.CreatedAt, &op.UpdatedAt, &prior)
	if err == nil {
		if prior != hash {
			if err = insertAudit(ctx, tx, op.TenantID, metadata, key, "tenant.registration.requested", "tenant:"+op.TenantID, "denied", "idempotency_conflict", auditPayload); err != nil {
				return domain.Operation{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return domain.Operation{}, err
			}
			return domain.Operation{}, basestore.ErrIdempotencyConflict
		}
		if err = insertAudit(ctx, tx, op.TenantID, metadata, key, "tenant.registration.requested", "tenant:"+op.TenantID, "accepted", "idempotent_replay", auditPayload); err != nil {
			return domain.Operation{}, err
		}
		return op, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id)VALUES($1)ON CONFLICT DO NOTHING`, c.LegalEntityID); err != nil {
		return domain.Operation{}, err
	}
	// c.Metadata is a nil map on every request that omits "metadata" (it has
	// no default in domain.RegisterTenant), and pgx's jsonb codec sends a nil
	// map as SQL NULL rather than "{}" - COALESCE keeps that from tripping the
	// NOT NULL DEFAULT '{}' constraint on tenants.metadata.
	if _, err = tx.Exec(ctx, `INSERT INTO tenants(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region,metadata)VALUES($1,$2,$3,$4,$5,COALESCE($6,'{}'::jsonb))`, c.TenantID, c.LegalEntityID, c.DisplayName, c.IsolationStrategy, c.ResidencyRegion, c.Metadata); err != nil {
		return domain.Operation{}, err
	}
	// ADR-BCP-018: ensure organisation canonical entity, profile, and default
	// tenant_legal_entity_mapping in the same registration transaction.
	if err = s.insertOrganisationOnRegister(ctx, tx, c, metadata, key); err != nil {
		return domain.Operation{}, err
	}
	for _, product := range c.RequestedProducts {
		// product_version_id is left unset (NULL): RegisterTenant's command
		// surface only carries a product_id, with no version-selection
		// workflow yet (Programme Gate P4) to resolve one - see migration
		// 000033's comment. source='ONBOARDING' needs no source_reference
		// per product.product_subscription's own CHECK constraint.
		if _, err = tx.Exec(ctx, `INSERT INTO product.product_subscription(tenant_id,product_id,status,source)VALUES($1,$2,'PENDING','ONBOARDING')`, c.TenantID, product); err != nil {
			return domain.Operation{}, err
		}
	}
	if err = tx.QueryRow(ctx, `INSERT INTO provisioning_operations(tenant_id,idempotency_key,request_hash)VALUES($1,$2,$3)RETURNING operation_id::text,tenant_id,state,revision,created_at,updated_at`, c.TenantID, key, hash).Scan(&op.OperationID, &op.TenantID, &op.State, &op.Revision, &op.CreatedAt, &op.UpdatedAt); err != nil {
		return domain.Operation{}, err
	}
	// Written as the ADR-0004 CloudEvents envelope into the canonical,
	// schema-qualified outbox (migration 000015, widened to accept a
	// tn_-prefixed aggregate/tenant identity by migration 000023) rather
	// than the legacy outbox_events table and its retired snake_case shape
	// - see docs/reconciliation/shared-control-plane-audit.md §12.
	env, err := events.New(events.Params{
		Type:           "com.baobab-platform.control-plane.tenant.provisioning-started.v1",
		Source:         s.eventSource(),
		Subject:        c.TenantID,
		DataSchema:     provisioningStartedDataSchema,
		CorrelationID:  metadata.CorrelationID,
		TenantID:       c.TenantID,
		IdempotencyKey: key,
		Data: map[string]any{
			"operation_id":       op.OperationID,
			"tenant_id":          c.TenantID,
			"isolation_strategy": c.IsolationStrategy,
			"revision":           op.Revision,
		},
	})
	if err != nil {
		return domain.Operation{}, fmt.Errorf("construct tenant-provisioning-started event: %w", err)
	}
	eventPayload, err := json.Marshal(env)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("marshal tenant-provisioning-started event: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO messaging.outbox(aggregate_type,aggregate_id,aggregate_version,event_type,tenant_id,correlation_id,payload) VALUES('tenant',$1,$2,$3,$4,$5,$6)`, c.TenantID, op.Revision, env.Type, c.TenantID, metadata.CorrelationID, eventPayload); err != nil {
		return domain.Operation{}, err
	}
	if err = insertAudit(ctx, tx, c.TenantID, metadata, key, "tenant.registration.requested", "tenant:"+c.TenantID, "accepted", "scope_allowed", auditPayload); err != nil {
		return domain.Operation{}, err
	}
	return op, tx.Commit(ctx)
}

func (s *Store) ResolveContext(ctx context.Context, metadata basestore.RequestMetadata, tenantID, productID string) (domain.ResolvedContext, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ResolvedContext{}, err
	}
	defer tx.Rollback(ctx)

	var legalEntityID, desiredState, observedState, subscriptionStatus string
	err = tx.QueryRow(ctx, `
		SELECT t.legal_entity_id,t.desired_state,t.observed_state,COALESCE(ps.status,'')
		FROM tenants t
		LEFT JOIN product.product_subscription ps ON ps.tenant_id=t.tenant_id AND ps.product_id=$2
		WHERE t.tenant_id=$1`, tenantID, productID).Scan(&legalEntityID, &desiredState, &observedState, &subscriptionStatus)

	found := true
	if errors.Is(err, pgx.ErrNoRows) {
		found = false
	} else if err != nil {
		return domain.ResolvedContext{}, err
	}
	result, policyDecision := contextPolicyDecision(found, desiredState, observedState, subscriptionStatus)

	auditPayload, marshalErr := json.Marshal(map[string]string{
		"product_id":          productID,
		"desired_state":       desiredState,
		"observed_state":      observedState,
		"subscription_status": subscriptionStatus,
	})
	if marshalErr != nil {
		return domain.ResolvedContext{}, fmt.Errorf("marshal context audit: %w", marshalErr)
	}
	target := "tenant:" + tenantID + "/product:" + productID
	if err = insertAudit(ctx, tx, tenantID, metadata, "", "context.resolve", target, result, policyDecision, auditPayload); err != nil {
		return domain.ResolvedContext{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ResolvedContext{}, err
	}
	if result == "denied" {
		return domain.ResolvedContext{}, basestore.ErrContextDenied
	}

	return domain.ResolvedContext{
		TenantID:        tenantID,
		EntityID:        legalEntityID,
		LifecycleStatus: "active",
		ProductID:       productID,
		Entitled:        true,
		CacheTTLSeconds: 15,
		ResolvedAt:      time.Now().UTC(),
		CorrelationID:   metadata.CorrelationID,
	}, nil
}

func contextPolicyDecision(found bool, desiredState, observedState, subscriptionStatus string) (string, string) {
	if !found {
		return "denied", "tenant_unknown"
	}
	if desiredState != "active" || observedState != "active" {
		return "denied", "tenant_not_active"
	}
	// product.product_subscription.status (migration 000033) uses the
	// uppercase enum contracts/product/v1/domain.schema.json#/$defs/subscriptionStatus
	// defines (DRAFT/PENDING/ACTIVE/SUSPENDED/CANCELLED/EXPIRED), unlike
	// tenants.desired_state/observed_state above, which remain the
	// lowercase values migration 000019 already shipped and this migration
	// does not touch.
	if subscriptionStatus != "ACTIVE" {
		return "denied", "product_not_entitled"
	}
	return "allowed", "context_allowed"
}

func insertAudit(ctx context.Context, tx pgx.Tx, tenantID string, metadata basestore.RequestMetadata, idempotencyKey, action, target, result, decision string, payload []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,actor_type,client_id,token_id,correlation_id,idempotency_key,action,target,result,policy_decision,payload) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12)`, tenantID, metadata.ActorID, metadata.ActorType, metadata.ClientID, metadata.TokenID, metadata.CorrelationID, idempotencyKey, action, target, result, decision, payload)
	return err
}
