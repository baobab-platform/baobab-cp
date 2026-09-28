package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/baobab-platform/baobab-cp/internal/topology/migration"
)

// ErrProviderMigrationNotFound: no provider migration has that id or
// idempotency key.
var ErrProviderMigrationNotFound = errors.New("provider migration not found")

// ErrProviderMigrationIdempotencyConflict: another create committed the same
// idempotency key first.
var ErrProviderMigrationIdempotencyConflict = errors.New("provider migration idempotency key already used")

// ProviderMigrationRepository persists provider migrations and reads the
// authoritative state they are planned from (ADR-BCP-006 Gate 8).
type ProviderMigrationRepository interface {
	migration.Facts
	// CreateProviderMigration records a migration and its first plan in one
	// transaction. Creates for the same source provider are serialised, and
	// build runs under that lock, so a plan always sees every migration
	// created before it and the in-progress blocker cannot be raced.
	CreateProviderMigration(ctx context.Context, sourceProviderKey, idempotencyKey, requestHash string,
		build func(ctx context.Context) (migration.Migration, migration.Plan, error), actor AuditActor) (migration.Migration, error)
	GetProviderMigration(ctx context.Context, id string) (migration.Migration, error)
	// GetProviderMigrationByIdempotencyKey returns the migration and the
	// hash of the request that created it.
	GetProviderMigrationByIdempotencyKey(ctx context.Context, key string) (migration.Migration, string, error)
	CurrentProviderMigrationPlan(ctx context.Context, id string) (migration.Plan, error)
}

var _ ProviderMigrationRepository = (*PostgresRepository)(nil)

// SourceBindings implements migration.Facts: the source provider's ACTIVE,
// currently effective PRIMARY and FALLBACK bindings of the capabilities,
// with the context their scope names. SHADOW, MIGRATION and DISABLED
// bindings carry no authoritative traffic to move.
func (r *PostgresRepository) SourceBindings(ctx context.Context, providerKey string, capabilityKeys []string) ([]migration.SourceBinding, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("repository is not initialized")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT cb.id::text, cap.code, cb.contract_version, s.tenant_id, COALESCE(s.legal_entity_id, ''),
			COALESCE(s.digital_estate_id, ''), COALESCE(s.market_id, ''), COALESCE(s.jurisdiction, ''),
			COALESCE(s.deployment_region, ''), COALESCE(s.environment, '')
		FROM capability.capability_binding cb
		JOIN capability.capability cap ON cap.capability_id = cb.capability_id
		JOIN capability.capability_provider p ON p.provider_id = cb.provider_id
		JOIN capability.capability_scope s ON s.scope_id = cb.scope_id
		WHERE p.provider_key = $1 AND cap.code = ANY($2)
			AND UPPER(cb.status) = 'ACTIVE' AND cb.binding_mode IN ('PRIMARY', 'FALLBACK')
			AND cb.valid_period @> now()
		ORDER BY cb.id`, providerKey, capabilityKeys)
	if err != nil {
		return nil, fmt.Errorf("load source bindings: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (migration.SourceBinding, error) {
		var b migration.SourceBinding
		var contract, market, jurisdiction string
		err := row.Scan(&b.BindingID, &b.CapabilityKey, &contract, &b.TenantID, &b.LegalEntityID, &b.EstateID,
			&market, &jurisdiction, &b.Region, &b.Environment)
		if err != nil {
			return b, err
		}
		b.ContractVersion = contractMajor(contract)
		if b.BindingID, err = domain.FormatResourceID("bind", b.BindingID); err != nil {
			return b, err
		}
		for _, m := range []string{market, jurisdiction} {
			if len(m) == 2 && m == strings.ToUpper(m) && !containsString(b.Markets, m) {
				b.Markets = append(b.Markets, m)
			}
		}
		return b, nil
	})
}

// Provider implements migration.Facts.
func (r *PostgresRepository) Provider(ctx context.Context, providerKey string) (migration.Provider, bool, error) {
	if r == nil || r.pool == nil {
		return migration.Provider{}, false, errors.New("repository is not initialized")
	}
	var p migration.Provider
	err := r.pool.QueryRow(ctx, `SELECT status FROM capability.capability_provider WHERE provider_key = $1`, providerKey).Scan(&p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return migration.Provider{}, false, nil
	}
	if err != nil {
		return migration.Provider{}, false, fmt.Errorf("load provider %s: %w", providerKey, err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT cap.code, s.contract_versions
		FROM capability.provider_capability_support s
		JOIN capability.capability_provider p ON p.provider_id = s.provider_id
		JOIN capability.capability cap ON cap.capability_id = s.capability_id
		WHERE p.provider_key = $1 AND s.status = 'ACTIVE'
			AND s.effective_from <= now() AND (s.effective_to IS NULL OR s.effective_to > now())`, providerKey)
	if err != nil {
		return migration.Provider{}, false, fmt.Errorf("load provider %s support: %w", providerKey, err)
	}
	defer rows.Close()
	p.Support = map[string][]int{}
	for rows.Next() {
		var key string
		var versions []int32
		if err := rows.Scan(&key, &versions); err != nil {
			return migration.Provider{}, false, err
		}
		for _, v := range versions {
			p.Support[key] = append(p.Support[key], int(v))
		}
	}
	return p, true, rows.Err()
}

// ProviderInstances implements migration.Facts: the instances of the
// provider's engine, with their health for each capability.
func (r *PostgresRepository) ProviderInstances(ctx context.Context, providerKey string, capabilityKeys []string) ([]migration.Instance, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("repository is not initialized")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT ei.engine_instance_id::text, ei.engine_instance_key, ei.region, ei.environment, UPPER(ei.status), p.provider_id::text
		FROM capability.capability_provider p
		JOIN topology.engine_instance ei ON ei.engine_id = p.engine_id
		WHERE p.provider_key = $1
		ORDER BY ei.engine_instance_key`, providerKey)
	if err != nil {
		return nil, fmt.Errorf("load provider instances: %w", err)
	}
	type row struct {
		rowID, providerID string
		instance          migration.Instance
	}
	found, err := pgx.CollectRows(rows, func(cr pgx.CollectableRow) (row, error) {
		var x row
		err := cr.Scan(&x.rowID, &x.instance.EngineInstanceID, &x.instance.Region, &x.instance.Environment, &x.instance.Status, &x.providerID)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("load provider instances: %w", err)
	}
	out := make([]migration.Instance, 0, len(found))
	for _, x := range found {
		x.instance.Health = make(map[string]health.Levels, len(capabilityKeys))
		for _, key := range capabilityKeys {
			levels, err := r.HealthLevels(ctx, x.rowID, x.providerID, key)
			if err != nil {
				return nil, err
			}
			x.instance.Health[key] = levels
		}
		out = append(out, x.instance)
	}
	return out, nil
}

// OpenMigrations implements migration.Facts.
func (r *PostgresRepository) OpenMigrations(ctx context.Context, sourceProviderKey string, capabilityKeys []string) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("repository is not initialized")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT provider_migration_id FROM topology.provider_migration
		WHERE source_provider_key = $1 AND capability_keys && $2
			AND stage NOT IN ('COMPLETE', 'CANCELLED', 'ROLLED_BACK')
		ORDER BY provider_migration_id`, sourceProviderKey, capabilityKeys)
	if err != nil {
		return nil, fmt.Errorf("load open migrations: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// CreateProviderMigration implements ProviderMigrationRepository.
func (r *PostgresRepository) CreateProviderMigration(ctx context.Context, sourceProviderKey, idempotencyKey, requestHash string,
	build func(ctx context.Context) (migration.Migration, migration.Plan, error), actor AuditActor) (migration.Migration, error) {
	if r == nil || r.pool == nil {
		return migration.Migration{}, errors.New("repository is not initialized")
	}
	if err := validateActor(actor); err != nil {
		return migration.Migration{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return migration.Migration{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('provider-migration:' || $1))`, sourceProviderKey); err != nil {
		return migration.Migration{}, fmt.Errorf("serialise provider migrations: %w", err)
	}
	m, plan, err := build(ctx)
	if err != nil {
		return migration.Migration{}, err
	}
	request, err := json.Marshal(m.Request)
	if err != nil {
		return migration.Migration{}, err
	}
	document, err := json.Marshal(plan)
	if err != nil {
		return migration.Migration{}, err
	}
	keys := make([]string, 0, len(m.Request.Capabilities))
	for _, c := range m.Request.Capabilities {
		keys = append(keys, c.CapabilityKey)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO topology.provider_migration (provider_migration_id, source_provider_key, target_provider_key, capability_keys,
			request, stage, plan_id, plan_version, plan_digest, blocked, idempotency_key, request_hash, created_by,
			created_at, updated_at, revision)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14, $15)`,
		m.ProviderMigrationID, m.Request.SourceProviderKey, m.Request.TargetProviderKey, keys, request, m.Stage,
		m.PlanID, m.PlanVersion, m.PlanDigest, m.Blocked, idempotencyKey, requestHash, m.CreatedBy, m.CreatedAt, m.Revision); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency_key") {
			return migration.Migration{}, ErrProviderMigrationIdempotencyConflict
		}
		return migration.Migration{}, fmt.Errorf("create provider migration: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO topology.provider_migration_plan (plan_id, provider_migration_id, plan_version, plan_digest, base_revision,
			document, generated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)`,
		plan.PlanID, m.ProviderMigrationID, plan.PlanVersion, plan.PlanDigest, plan.BaseRevision, document,
		plan.GeneratedAt, plan.ExpiresAt); err != nil {
		return migration.Migration{}, fmt.Errorf("record provider migration plan: %w", err)
	}
	blockers := make([]string, 0, len(plan.Blockers))
	for _, b := range plan.Blockers {
		blockers = append(blockers, b.Code)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "provider_migration.created", m.ProviderMigrationID, map[string]any{
		"provider_migration_id": m.ProviderMigrationID, "source_provider_key": m.Request.SourceProviderKey,
		"target_provider_key": m.Request.TargetProviderKey, "capability_keys": keys, "migration_mode": m.Request.MigrationMode,
		"plan_id": plan.PlanID, "plan_digest": plan.PlanDigest, "risk_class": plan.RiskClass, "blockers": blockers,
	}); err != nil {
		return migration.Migration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return migration.Migration{}, err
	}
	return m, nil
}

const providerMigrationColumns = `provider_migration_id, request, stage, COALESCE(current_cohort_key, ''), plan_id, plan_version,
	plan_digest, blocked, COALESCE(failure_reason, ''), created_by, created_at, updated_at, started_at, completed_at, revision`

func scanProviderMigration(row pgx.Row, extra ...any) (migration.Migration, error) {
	var m migration.Migration
	var request []byte
	dest := append([]any{&m.ProviderMigrationID, &request, &m.Stage, &m.CurrentCohortKey, &m.PlanID, &m.PlanVersion,
		&m.PlanDigest, &m.Blocked, &m.FailureReason, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.StartedAt, &m.CompletedAt, &m.Revision}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return m, ErrProviderMigrationNotFound
		}
		return m, fmt.Errorf("load provider migration: %w", err)
	}
	if err := json.Unmarshal(request, &m.Request); err != nil {
		return m, fmt.Errorf("provider migration %s request: %w", m.ProviderMigrationID, err)
	}
	m.CreatedAt, m.UpdatedAt = m.CreatedAt.UTC(), m.UpdatedAt.UTC()
	for _, t := range []*time.Time{m.StartedAt, m.CompletedAt} {
		if t != nil {
			*t = t.UTC()
		}
	}
	return m, nil
}

// GetProviderMigration implements ProviderMigrationRepository.
func (r *PostgresRepository) GetProviderMigration(ctx context.Context, id string) (migration.Migration, error) {
	if r == nil || r.pool == nil {
		return migration.Migration{}, errors.New("repository is not initialized")
	}
	return scanProviderMigration(r.pool.QueryRow(ctx, `SELECT `+providerMigrationColumns+`
		FROM topology.provider_migration WHERE provider_migration_id = $1`, id))
}

// GetProviderMigrationByIdempotencyKey implements ProviderMigrationRepository.
func (r *PostgresRepository) GetProviderMigrationByIdempotencyKey(ctx context.Context, key string) (migration.Migration, string, error) {
	if r == nil || r.pool == nil {
		return migration.Migration{}, "", errors.New("repository is not initialized")
	}
	var hash string
	m, err := scanProviderMigration(r.pool.QueryRow(ctx, `SELECT `+providerMigrationColumns+`, request_hash
		FROM topology.provider_migration WHERE idempotency_key = $1`, key), &hash)
	return m, hash, err
}

// CurrentProviderMigrationPlan implements ProviderMigrationRepository.
func (r *PostgresRepository) CurrentProviderMigrationPlan(ctx context.Context, id string) (migration.Plan, error) {
	if r == nil || r.pool == nil {
		return migration.Plan{}, errors.New("repository is not initialized")
	}
	var document []byte
	err := r.pool.QueryRow(ctx, `
		SELECT p.document FROM topology.provider_migration m
		JOIN topology.provider_migration_plan p ON p.plan_id = m.plan_id
		WHERE m.provider_migration_id = $1`, id).Scan(&document)
	if errors.Is(err, pgx.ErrNoRows) {
		return migration.Plan{}, ErrProviderMigrationNotFound
	}
	if err != nil {
		return migration.Plan{}, fmt.Errorf("load provider migration plan: %w", err)
	}
	var plan migration.Plan
	if err := json.Unmarshal(document, &plan); err != nil {
		return migration.Plan{}, fmt.Errorf("provider migration %s plan: %w", id, err)
	}
	return plan, nil
}

// contractMajor reads a binding's stored contract version, "v1", "1" or
// "1.0.0", as its major version; 0 when it has none.
func contractMajor(stored string) int {
	major, _, _ := strings.Cut(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(stored)), "v"), ".")
	n, err := strconv.Atoi(major)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func containsString(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
