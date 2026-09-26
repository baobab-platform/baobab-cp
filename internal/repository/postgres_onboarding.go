// ADR-BCP-017 sections 22-24, 39, 41, 44, 46 — TenantOnboardingRequest
// persistence. Contract: baobab-platform/shared
// contracts/admission/v1/onboarding.schema.json.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
)

// ErrOnboardingRequestNotFound: no such TenantOnboardingRequest.
var ErrOnboardingRequestNotFound = errors.New("tenant onboarding request not found")

// OnboardingTenantFacts is what fulfilment checks against the desired state.
type OnboardingTenantFacts struct {
	TenantID          string
	IsolationStrategy string
	ResidencyRegion   string
	// OnboardedBy is the request that already produced this tenant, if any.
	OnboardedBy string
}

// OnboardingChange is the audit action and event a transition records.
type OnboardingChange struct {
	AuditAction  string
	EventType    string
	EventData    map[string]any
	AuditPayload map[string]any
}

// OnboardingDecision decides, under the request's row lock, the next state
// of current and what to record. tenant is set when the transition names a
// tenant (fulfilment), nil otherwise.
type OnboardingDecision func(current domain.TenantOnboardingRequest, tenant *OnboardingTenantFacts) (domain.TenantOnboardingRequest, OnboardingChange, error)

// TenantOnboardingRepository keeps TenantOnboardingRequests.
type TenantOnboardingRepository interface {
	// CreateTenantOnboardingRequest records a REQUESTED request. When the
	// decision already has a live request it returns that one, created=false.
	CreateTenantOnboardingRequest(ctx context.Context, req domain.TenantOnboardingRequest, actor AuditActor) (domain.TenantOnboardingRequest, bool, error)
	GetTenantOnboardingRequest(ctx context.Context, id string) (domain.TenantOnboardingRequest, error)
	// ListTenantOnboardingRequests returns requests, newest first, in status
	// when it is set.
	ListTenantOnboardingRequests(ctx context.Context, status string, limit int) ([]domain.TenantOnboardingRequest, error)
	// TransitionTenantOnboardingRequest locks the request, loads tenantID's
	// facts when it is set, applies decide and records its change.
	TransitionTenantOnboardingRequest(ctx context.Context, id, tenantID string, decide OnboardingDecision, actor AuditActor) (domain.TenantOnboardingRequest, error)
	// TransitionTenantOnboardingRequestTx does the same inside tx.
	TransitionTenantOnboardingRequestTx(ctx context.Context, tx pgx.Tx, id, tenantID string, decide OnboardingDecision, actor AuditActor) (domain.TenantOnboardingRequest, error)
}

var _ TenantOnboardingRepository = (*PostgresRepository)(nil)

const onboardingColumns = `tenant_onboarding_request_id::text, client_application_id::text, admission_decision_id::text, status,
	display_name, residency_region, isolation_strategy, subscription_type, market_scope, product_requirements, reason,
	correlation_id::text, requested_by::text, requested_at, COALESCE(authorised_by::text, ''), authorised_at,
	COALESCE(tenant_id, ''), fulfilled_at, COALESCE(cancelled_by::text, ''), cancelled_at, COALESCE(cancellation_reason, '')`

func scanOnboarding(row pgx.Row) (domain.TenantOnboardingRequest, error) {
	var (
		r                           domain.TenantOnboardingRequest
		rowID, application, decided string
		subscription                string
	)
	err := row.Scan(&rowID, &application, &decided, &r.Status, &r.DesiredState.DisplayName, &r.DesiredState.ResidencyRegion,
		&r.DesiredState.IsolationStrategy, &subscription, &r.DesiredState.MarketScope, &r.DesiredState.ProductRequirements,
		&r.Reason, &r.CorrelationID, &r.RequestedBy, &r.RequestedAt, &r.AuthorisedBy, &r.AuthorisedAt, &r.TenantID,
		&r.FulfilledAt, &r.CancelledBy, &r.CancelledAt, &r.CancellationReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrOnboardingRequestNotFound
	}
	if err != nil {
		return r, err
	}
	r.DesiredState.SubscriptionType = domain.SubscriptionType(subscription)
	if r.DesiredState.ProductRequirements == nil {
		r.DesiredState.ProductRequirements = []string{}
	}
	if r.ID, err = domain.FormatResourceID(domain.TenantOnboardingRequestIDPrefix, rowID); err != nil {
		return r, err
	}
	if r.ClientApplicationID, err = domain.FormatResourceID(domain.ClientApplicationIDPrefix, application); err != nil {
		return r, err
	}
	if r.AdmissionDecisionID, err = domain.FormatResourceID(domain.AdmissionDecisionIDPrefix, decided); err != nil {
		return r, err
	}
	r.RequestedAt = r.RequestedAt.UTC()
	for _, t := range []**time.Time{&r.AuthorisedAt, &r.FulfilledAt, &r.CancelledAt} {
		if *t != nil {
			u := (*t).UTC()
			*t = &u
		}
	}
	return r, nil
}

func onboardingRows(r domain.TenantOnboardingRequest) (id, application, decision string, err error) {
	if id, err = domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, r.ID); err != nil {
		return
	}
	if application, err = domain.ParseResourceID(domain.ClientApplicationIDPrefix, r.ClientApplicationID); err != nil {
		return
	}
	decision, err = domain.ParseResourceID(domain.AdmissionDecisionIDPrefix, r.AdmissionDecisionID)
	return
}

func (r *PostgresRepository) CreateTenantOnboardingRequest(ctx context.Context, req domain.TenantOnboardingRequest, actor AuditActor) (domain.TenantOnboardingRequest, bool, error) {
	id, application, decision, err := onboardingRows(req)
	if err != nil {
		return domain.TenantOnboardingRequest{}, false, err
	}
	var (
		out     domain.TenantOnboardingRequest
		created bool
	)
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO admission.tenant_onboarding_request (tenant_onboarding_request_id, client_application_id,
				admission_decision_id, status, display_name, residency_region, isolation_strategy, subscription_type,
				market_scope, product_requirements, reason, correlation_id, requested_by, requested_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'REQUESTED', $4, $5, $6, $7, $8, $9, $10, $11::uuid, $12::uuid, $13)
			ON CONFLICT (admission_decision_id) WHERE status IN ('REQUESTED','AUTHORISED','FULFILLED') DO NOTHING`,
			id, application, decision, req.DesiredState.DisplayName, req.DesiredState.ResidencyRegion,
			req.DesiredState.IsolationStrategy, string(req.DesiredState.SubscriptionType), req.DesiredState.MarketScope,
			emptyIfNil(req.DesiredState.ProductRequirements), req.Reason, req.CorrelationID, req.RequestedBy, req.RequestedAt)
		if err != nil {
			return fmt.Errorf("record tenant onboarding request: %w", err)
		}
		if tag.RowsAffected() == 0 {
			out, err = scanOnboarding(tx.QueryRow(ctx, `SELECT `+onboardingColumns+` FROM admission.tenant_onboarding_request
				WHERE admission_decision_id = $1::uuid AND status IN ('REQUESTED','AUTHORISED','FULFILLED')`, decision))
			return err
		}
		out, created = req, true
		out.Status = domain.OnboardingRequested
		return r.recordOnboardingChange(ctx, tx, actor, out, OnboardingChange{
			AuditAction: "tenant_onboarding.requested", EventType: events.TenantOnboardingRequested,
			EventData:    map[string]any{"requested_at": events.Timestamp(req.RequestedAt)},
			AuditPayload: map[string]any{"reason": req.Reason, "desired_state": req.DesiredState},
		})
	})
	return out, created, err
}

func (r *PostgresRepository) GetTenantOnboardingRequest(ctx context.Context, id string) (domain.TenantOnboardingRequest, error) {
	rowID, err := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, id)
	if err != nil {
		return domain.TenantOnboardingRequest{}, ErrOnboardingRequestNotFound
	}
	return scanOnboarding(r.pool.QueryRow(ctx, `SELECT `+onboardingColumns+` FROM admission.tenant_onboarding_request
		WHERE tenant_onboarding_request_id = $1::uuid`, rowID))
}

func (r *PostgresRepository) ListTenantOnboardingRequests(ctx context.Context, status string, limit int) ([]domain.TenantOnboardingRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT `+onboardingColumns+` FROM admission.tenant_onboarding_request
		WHERE $1 = '' OR status = $1 ORDER BY requested_at DESC, tenant_onboarding_request_id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TenantOnboardingRequest{}
	for rows.Next() {
		req, err := scanOnboarding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) TransitionTenantOnboardingRequest(ctx context.Context, id, tenantID string, decide OnboardingDecision, actor AuditActor) (domain.TenantOnboardingRequest, error) {
	var out domain.TenantOnboardingRequest
	err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var err error
		out, err = r.TransitionTenantOnboardingRequestTx(ctx, tx, id, tenantID, decide, actor)
		return err
	})
	return out, err
}

// TransitionTenantOnboardingRequestTx is TransitionTenantOnboardingRequest
// inside the caller's transaction. Tenant registration uses it to fulfil
// the request in the same transaction that registers the tenant.
func (r *PostgresRepository) TransitionTenantOnboardingRequestTx(ctx context.Context, tx pgx.Tx, id, tenantID string, decide OnboardingDecision, actor AuditActor) (domain.TenantOnboardingRequest, error) {
	if err := validateActor(actor); err != nil {
		return domain.TenantOnboardingRequest{}, err
	}
	rowID, err := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, id)
	if err != nil {
		return domain.TenantOnboardingRequest{}, ErrOnboardingRequestNotFound
	}
	var out domain.TenantOnboardingRequest
	err = func() error {
		current, err := scanOnboarding(tx.QueryRow(ctx, `SELECT `+onboardingColumns+` FROM admission.tenant_onboarding_request
			WHERE tenant_onboarding_request_id = $1::uuid FOR UPDATE`, rowID))
		if err != nil {
			return err
		}
		var facts *OnboardingTenantFacts
		if tenantID != "" {
			f := OnboardingTenantFacts{TenantID: tenantID}
			err := tx.QueryRow(ctx, `SELECT isolation_strategy, residency_region FROM tenants WHERE tenant_id = $1 FOR SHARE`,
				tenantID).Scan(&f.IsolationStrategy, &f.ResidencyRegion)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrTenantNotRegistered
			}
			if err != nil {
				return err
			}
			var other string
			err = tx.QueryRow(ctx, `SELECT tenant_onboarding_request_id::text FROM admission.tenant_onboarding_request
				WHERE tenant_id = $1`, tenantID).Scan(&other)
			switch {
			case err == nil:
				if f.OnboardedBy, err = domain.FormatResourceID(domain.TenantOnboardingRequestIDPrefix, other); err != nil {
					return err
				}
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
			facts = &f
		}
		next, change, err := decide(current, facts)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE admission.tenant_onboarding_request SET status = $2, authorised_by = NULLIF($3, '')::uuid, authorised_at = $4,
				tenant_id = NULLIF($5, ''), fulfilled_at = $6, cancelled_by = NULLIF($7, '')::uuid, cancelled_at = $8,
				cancellation_reason = NULLIF($9, ''), updated_at = now()
			WHERE tenant_onboarding_request_id = $1::uuid`,
			rowID, next.Status, next.AuthorisedBy, next.AuthorisedAt, next.TenantID, next.FulfilledAt, next.CancelledBy,
			next.CancelledAt, next.CancellationReason); err != nil {
			return fmt.Errorf("transition tenant onboarding request: %w", err)
		}
		out = next
		return r.recordOnboardingChange(ctx, tx, actor, next, change)
	}()
	return out, err
}

// recordOnboardingChange writes the audit record and the outbox event. The
// event carries the request's own correlation id, so the admission-to-
// activation journey stays traceable (section 41).
func (r *PostgresRepository) recordOnboardingChange(ctx context.Context, tx pgx.Tx, actor AuditActor, req domain.TenantOnboardingRequest, change OnboardingChange) error {
	payload, err := json.Marshal(change.AuditPayload)
	if err != nil {
		return err
	}
	target := "tenant-onboarding-request/" + req.ID
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_id, actor_type, client_id, token_id, correlation_id, action, target, result, payload)
		VALUES ($1, $2, $3, $4, $5, $6::uuid, $7, $8, 'accepted', $9::jsonb)`,
		nullable(req.TenantID), actor.ActorID, actor.ActorType, nullable(actor.ClientID), nullable(actor.TokenID),
		actor.CorrelationID, change.AuditAction, target, payload); err != nil {
		return fmt.Errorf("audit %s: %w", change.AuditAction, err)
	}
	data := map[string]any{"tenant_onboarding_request_id": req.ID, "client_application_id": req.ClientApplicationID,
		"admission_decision_id": req.AdmissionDecisionID}
	for k, v := range change.EventData {
		data[k] = v
	}
	env, err := events.NewAdmissionEnvelope(change.EventType, target, data, r.eventSource(), req.CorrelationID)
	if err != nil {
		return fmt.Errorf("build %s event: %w", change.EventType, err)
	}
	rowID, _ := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, req.ID)
	return r.insertOutboxEvent(ctx, tx, "tenant_onboarding_request", rowID, time.Now().UnixMicro(), env)
}
