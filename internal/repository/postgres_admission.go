// ADR-BCP-017 — ClientApplication and AdmissionDecision persistence
// (migration 000050). Contract: baobab-platform/shared contracts/admission/v1.
//
// The repository stores and reads; the admission service owns every rule.
// Each change runs in one transaction: the row is locked, the service's
// mutation decides the new state (and validates it against the contract),
// and the row, the decision if any, the audit record and the lifecycle
// event commit together.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
)

var (
	ErrClientApplicationNotFound = errors.New("client application not found")
	// ErrApplicationIdempotencyConflict: the Idempotency-Key was already used
	// by this applicant for a different request.
	ErrApplicationIdempotencyConflict = errors.New("idempotency key was used for a different request")
	// ErrInvalidApplicationCursor: a page cursor that is not an application id.
	ErrInvalidApplicationCursor = errors.New("the page cursor is not a client application id")
)

// ApplicationChange is what one command did: the audit record it always
// writes, the lifecycle event it publishes when EventType is set, and the
// AdmissionDecision it records, if any.
type ApplicationChange struct {
	AuditAction  string
	AuditPayload map[string]any
	EventType    string
	EventData    map[string]any
	Decision     *domain.AdmissionDecision
	// ClosureReason is recorded when the change closes the application
	// (withdrawal, cancellation, expiry).
	ClosureReason string
}

// ApplicationMutation changes app in place and reports the change, or
// returns a nil change to leave the application untouched. It runs while
// the row is locked, so it must not block on anything slow.
type ApplicationMutation func(app *domain.ClientApplication) (*ApplicationChange, error)

// ClientApplicationQuery selects applications, newest first.
type ClientApplicationQuery struct {
	// ApplicantPrincipalID restricts to one applicant's applications.
	ApplicantPrincipalID string
	// Statuses restricts to these statuses; empty means any.
	Statuses []domain.ApplicationStatus
	Limit    int
	// Before is the client_application_id cursor of the previous page.
	Before string
}

type AdmissionRepository interface {
	// CreateClientApplication mints an application's identifiers, lets build
	// complete it and records it. A repeated Idempotency-Key from the same
	// applicant with the same request hash returns the original (replayed
	// true); with a different hash it fails with
	// ErrApplicationIdempotencyConflict.
	CreateClientApplication(ctx context.Context, applicantPrincipalID, idempotencyKey, requestHash string, actor AuditActor,
		build func(id, reference string) (domain.ClientApplication, ApplicationChange, error)) (domain.ClientApplication, bool, error)
	GetClientApplication(ctx context.Context, id string) (*domain.ClientApplication, error)
	ListClientApplications(ctx context.Context, q ClientApplicationQuery) ([]domain.ClientApplication, error)
	UpdateClientApplication(ctx context.Context, id string, actor AuditActor, apply ApplicationMutation) (domain.ClientApplication, error)
	GetAdmissionDecision(ctx context.Context, clientApplicationID string) (*domain.AdmissionDecision, error)
}

const clientApplicationColumns = `a.client_application_id::text, a.reference, a.status, a.application_channel, a.version,
	a.applicant_principal_id::text, COALESCE(a.assigned_reviewer::text, ''), a.organisation_profile, a.requirements,
	a.requested_markets, a.evidence, a.information_requests, a.created_at, a.updated_at, a.submitted_at, a.closed_at,
	d.admission_decision_id::text, d.decision, d.decided_at, d.reason`

const clientApplicationFrom = `admission.client_application a
	LEFT JOIN admission.admission_decision d ON d.client_application_id = a.client_application_id`

func scanClientApplication(row pgx.Row) (domain.ClientApplication, error) {
	var (
		a                                     domain.ClientApplication
		rowID                                 string
		markets, evidence, requests           []byte
		decisionRow, decision, decisionReason *string
		decidedAt                             *time.Time
	)
	if err := row.Scan(&rowID, &a.Reference, &a.Status, &a.Channel, &a.Version, &a.ApplicantPrincipalID,
		&a.AssignedReviewer, &a.OrganisationProfile, &a.Requirements, &markets, &evidence, &requests,
		&a.CreatedAt, &a.UpdatedAt, &a.SubmittedAt, &a.ClosedAt, &decisionRow, &decision, &decidedAt, &decisionReason); err != nil {
		return a, err
	}
	var err error
	if a.ID, err = domain.FormatResourceID(domain.ClientApplicationIDPrefix, rowID); err != nil {
		return a, err
	}
	if decisionRow != nil {
		id, err := domain.FormatResourceID(domain.AdmissionDecisionIDPrefix, *decisionRow)
		if err != nil {
			return a, err
		}
		a.Decision = &domain.DecisionSummary{AdmissionDecisionID: id, Decision: domain.AdmissionDecisionValue(*decision),
			DecidedAt: decidedAt.UTC(), Reason: *decisionReason}
	}
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	a.SubmittedAt, a.ClosedAt = utcPtr(a.SubmittedAt), utcPtr(a.ClosedAt)
	return a, errors.Join(json.Unmarshal(markets, &a.RequestedMarkets), json.Unmarshal(evidence, &a.Evidence),
		json.Unmarshal(requests, &a.InformationRequests))
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (r *PostgresRepository) CreateClientApplication(ctx context.Context, applicantPrincipalID, idempotencyKey, requestHash string, actor AuditActor,
	build func(id, reference string) (domain.ClientApplication, ApplicationChange, error)) (domain.ClientApplication, bool, error) {
	if (idempotencyKey == "") != (requestHash == "") {
		return domain.ClientApplication{}, false, errors.New("an idempotency key needs its request hash")
	}
	if idempotencyKey != "" {
		if existing, err := r.clientApplicationByIdempotencyKey(ctx, applicantPrincipalID, idempotencyKey, requestHash); err == nil || !errors.Is(err, ErrClientApplicationNotFound) {
			return existing, err == nil, err
		}
	}
	var created domain.ClientApplication
	err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var seq int64
		if err := tx.QueryRow(ctx, `SELECT nextval('admission.client_application_reference_seq')`).Scan(&seq); err != nil {
			return err
		}
		rowID := domain.NewUUIDv7()
		id, err := domain.FormatResourceID(domain.ClientApplicationIDPrefix, rowID)
		if err != nil {
			return err
		}
		reference := fmt.Sprintf("APP-%04d-%06d", time.Now().UTC().Year(), seq)
		app, change, err := build(id, reference)
		if err != nil {
			return err
		}
		markets, evidence, requests, err := applicationDocuments(app)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO admission.client_application (client_application_id, reference, status, application_channel, version,
				applicant_principal_id, organisation_profile, requirements, requested_markets, evidence, information_requests,
				create_idempotency_key, create_request_hash, created_at, updated_at)
			VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7::jsonb, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12, $13, $14, $14)`,
			rowID, reference, app.Status, app.Channel, app.Version, app.ApplicantPrincipalID, jsonDoc(app.OrganisationProfile, "{}"),
			jsonDoc(app.Requirements, "{}"), markets, evidence, requests, nullable(idempotencyKey), nullable(requestHash), app.CreatedAt); err != nil {
			return err
		}
		created = app
		return r.recordApplicationChange(ctx, tx, actor, rowID, app, change)
	})
	var pgErr *pgconn.PgError
	if idempotencyKey != "" && errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// A concurrent create with the same key won; converge on it.
		existing, getErr := r.clientApplicationByIdempotencyKey(ctx, applicantPrincipalID, idempotencyKey, requestHash)
		return existing, getErr == nil, getErr
	}
	return created, false, err
}

func (r *PostgresRepository) clientApplicationByIdempotencyKey(ctx context.Context, applicantPrincipalID, key, requestHash string) (domain.ClientApplication, error) {
	var hash string
	var rowID string
	err := r.pool.QueryRow(ctx, `
		SELECT client_application_id::text, create_request_hash FROM admission.client_application
		WHERE applicant_principal_id = $1::uuid AND create_idempotency_key = $2`, applicantPrincipalID, key).Scan(&rowID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ClientApplication{}, ErrClientApplicationNotFound
	}
	if err != nil {
		return domain.ClientApplication{}, err
	}
	if hash != requestHash {
		return domain.ClientApplication{}, ErrApplicationIdempotencyConflict
	}
	app, err := scanClientApplication(r.pool.QueryRow(ctx, `SELECT `+clientApplicationColumns+` FROM `+clientApplicationFrom+`
		WHERE a.client_application_id = $1::uuid`, rowID))
	return app, err
}

func (r *PostgresRepository) GetClientApplication(ctx context.Context, id string) (*domain.ClientApplication, error) {
	rowID, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, id)
	if err != nil {
		return nil, ErrClientApplicationNotFound
	}
	app, err := scanClientApplication(r.pool.QueryRow(ctx, `SELECT `+clientApplicationColumns+` FROM `+clientApplicationFrom+`
		WHERE a.client_application_id = $1::uuid`, rowID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClientApplicationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *PostgresRepository) ListClientApplications(ctx context.Context, q ClientApplicationQuery) ([]domain.ClientApplication, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		return nil, errors.New("limit must be between 1 and 500")
	}
	sql := `SELECT ` + clientApplicationColumns + ` FROM ` + clientApplicationFrom + ` WHERE true`
	var args []any
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if q.ApplicantPrincipalID != "" {
		sql += ` AND a.applicant_principal_id = ` + arg(q.ApplicantPrincipalID) + `::uuid`
	}
	if len(q.Statuses) > 0 {
		statuses := make([]string, len(q.Statuses))
		for i, s := range q.Statuses {
			statuses[i] = string(s)
		}
		sql += ` AND a.status = ANY(` + arg(statuses) + `::text[])`
	}
	if q.Before != "" {
		before, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, q.Before)
		if err != nil {
			return nil, ErrInvalidApplicationCursor
		}
		// Keyset on (created_at, id), both descending.
		sql += ` AND (a.created_at, a.client_application_id) < (SELECT created_at, client_application_id
			FROM admission.client_application WHERE client_application_id = ` + arg(before) + `::uuid)`
	}
	sql += ` ORDER BY a.created_at DESC, a.client_application_id DESC LIMIT ` + arg(q.Limit)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ClientApplication{}
	for rows.Next() {
		app, err := scanClientApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpdateClientApplication(ctx context.Context, id string, actor AuditActor, apply ApplicationMutation) (domain.ClientApplication, error) {
	rowID, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, id)
	if err != nil {
		return domain.ClientApplication{}, ErrClientApplicationNotFound
	}
	var out domain.ClientApplication
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		app, err := scanClientApplication(tx.QueryRow(ctx, `SELECT `+clientApplicationColumns+` FROM `+clientApplicationFrom+`
			WHERE a.client_application_id = $1::uuid FOR UPDATE OF a`, rowID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClientApplicationNotFound
		}
		if err != nil {
			return err
		}
		before := app.Version
		change, err := apply(&app)
		if err != nil {
			return err
		}
		if change == nil {
			out = app
			return nil
		}
		if app.Version != before+1 {
			return fmt.Errorf("an application change must advance the version by one (from %d to %d)", before, app.Version)
		}
		markets, evidence, requests, err := applicationDocuments(app)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE admission.client_application SET status = $2, version = $3, assigned_reviewer = $4::uuid,
				organisation_profile = $5::jsonb, requirements = $6::jsonb, requested_markets = $7::jsonb, evidence = $8::jsonb,
				information_requests = $9::jsonb, updated_at = $10, submitted_at = $11, closed_at = $12,
				closure_reason = COALESCE($13, closure_reason)
			WHERE client_application_id = $1::uuid`,
			rowID, app.Status, app.Version, nullable(app.AssignedReviewer), jsonDoc(app.OrganisationProfile, "{}"),
			jsonDoc(app.Requirements, "{}"), markets, evidence, requests, app.UpdatedAt, app.SubmittedAt, app.ClosedAt,
			nullable(change.ClosureReason)); err != nil {
			return err
		}
		if d := change.Decision; d != nil {
			if err := insertAdmissionDecision(ctx, tx, rowID, *d); err != nil {
				return err
			}
			summary := d.Summary()
			app.Decision = &summary
		}
		out = app
		return r.recordApplicationChange(ctx, tx, actor, rowID, app, *change)
	})
	return out, err
}

func insertAdmissionDecision(ctx context.Context, tx pgx.Tx, applicationRow string, d domain.AdmissionDecision) error {
	decisionRow, err := domain.ParseResourceID(domain.AdmissionDecisionIDPrefix, d.ID)
	if err != nil {
		return err
	}
	var eligibility []byte
	if d.InternalEligibility != nil {
		if eligibility, err = json.Marshal(d.InternalEligibility); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO admission.admission_decision (admission_decision_id, client_application_id, decision, reason, decided_by,
			decided_at, approved_subscription_type, internal_eligibility, approved_market_scope, approved_product_requirements,
			approved_isolation_requirements, conditions, evidence_references)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7, $8::jsonb, $9, $10, $11, $12, $13)`,
		decisionRow, applicationRow, d.Decision, d.Reason, d.DecidedBy, d.DecidedAt, nullable(string(d.ApprovedSubscriptionType)),
		eligibility, nilIfEmpty(d.ApprovedMarketScope), nilIfEmpty(d.ApprovedProductRequirements),
		nullable(d.ApprovedIsolationRequirements), nilIfEmpty(d.Conditions), nilIfEmpty(d.EvidenceReferences))
	return err
}

func (r *PostgresRepository) GetAdmissionDecision(ctx context.Context, clientApplicationID string) (*domain.AdmissionDecision, error) {
	rowID, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, clientApplicationID)
	if err != nil {
		return nil, ErrClientApplicationNotFound
	}
	var (
		d                                    domain.AdmissionDecision
		decisionRow, subscription, isolation *string
		eligibility                          []byte
	)
	err = r.pool.QueryRow(ctx, `
		SELECT admission_decision_id::text, decision, reason, decided_by::text, decided_at, approved_subscription_type,
			internal_eligibility, COALESCE(approved_market_scope, '{}'), COALESCE(approved_product_requirements, '{}'),
			approved_isolation_requirements, COALESCE(conditions, '{}'), COALESCE(evidence_references, '{}')
		FROM admission.admission_decision WHERE client_application_id = $1::uuid`, rowID).Scan(
		&decisionRow, &d.Decision, &d.Reason, &d.DecidedBy, &d.DecidedAt, &subscription, &eligibility,
		&d.ApprovedMarketScope, &d.ApprovedProductRequirements, &isolation, &d.Conditions, &d.EvidenceReferences)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if d.ID, err = domain.FormatResourceID(domain.AdmissionDecisionIDPrefix, *decisionRow); err != nil {
		return nil, err
	}
	d.ClientApplicationID = clientApplicationID
	d.DecidedAt = d.DecidedAt.UTC()
	if subscription != nil {
		d.ApprovedSubscriptionType = domain.SubscriptionType(*subscription)
	}
	if isolation != nil {
		d.ApprovedIsolationRequirements = *isolation
	}
	if eligibility != nil {
		d.InternalEligibility = &domain.InternalEligibilityRecord{}
		if err := json.Unmarshal(eligibility, d.InternalEligibility); err != nil {
			return nil, err
		}
		d.InternalEligibility.EvaluatedAt = d.InternalEligibility.EvaluatedAt.UTC()
	}
	return &d, nil
}

// recordApplicationChange writes the audit record and, when the change has
// a section 40 event, the outbox event, on tx. The aggregate version is the
// application's version, so consumers can order an application's events.
func (r *PostgresRepository) recordApplicationChange(ctx context.Context, tx pgx.Tx, actor AuditActor, rowID string, app domain.ClientApplication, change ApplicationChange) error {
	if change.AuditAction == "" {
		return errors.New("an application change needs an audit action")
	}
	payload, err := json.Marshal(change.AuditPayload)
	if err != nil {
		return fmt.Errorf("marshal audit payload for %s: %w", change.AuditAction, err)
	}
	target := "client-application/" + app.ID
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (actor_id, actor_type, client_id, token_id, correlation_id, action, target, result, payload)
		VALUES ($1, $2, $3, $4, $5::uuid, $6, $7, 'accepted', $8::jsonb)`,
		actor.ActorID, actor.ActorType, nullable(actor.ClientID), nullable(actor.TokenID), actor.CorrelationID,
		change.AuditAction, target, payload); err != nil {
		return fmt.Errorf("audit %s: %w", change.AuditAction, err)
	}
	if change.EventType == "" {
		return nil
	}
	env, err := events.NewAdmissionEnvelope(change.EventType, target, change.EventData, r.eventSource(), actor.CorrelationID)
	if err != nil {
		return fmt.Errorf("build %s event: %w", change.EventType, err)
	}
	return r.insertOutboxEvent(ctx, tx, "client_application", rowID, app.Version, env)
}

func applicationDocuments(app domain.ClientApplication) (markets, evidence, requests []byte, err error) {
	if markets, err = json.Marshal(emptyIfNil(app.RequestedMarkets)); err != nil {
		return
	}
	if evidence, err = json.Marshal(emptyIfNil(app.Evidence)); err != nil {
		return
	}
	requests, err = json.Marshal(emptyIfNil(app.InformationRequests))
	return
}

func emptyIfNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func nilIfEmpty(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	return v
}

func jsonDoc(raw json.RawMessage, empty string) string {
	if len(raw) == 0 {
		return empty
	}
	return string(raw)
}
