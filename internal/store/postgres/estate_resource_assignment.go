// ADR-SHARED-042 section 14, Decision 9 (Shared control-plane/v1
// estate-resource-assignment; CP ADR-BCP-031): CP-governed estate resource
// assignments, their four-eyes lifecycle and the immutable record of every
// resolution answer. An assignment establishes the Digital Estate of a
// non-BFF native commitment; it classifies nothing and authorises no seller,
// buyer, market or purchasing action. The database repeats every rule
// (migration 000132).
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAssignmentNotFound          = errors.New("estate resource assignment not found")
	ErrAssignmentRevisionConflict  = errors.New("estate resource assignment revision conflict")
	ErrAssignmentSelfApproval      = errors.New("estate resource assignment self-approval")
	ErrAssignmentTerminal          = errors.New("estate resource assignment is revoked")
	ErrAssignmentInvalidTransition = errors.New("estate resource assignment transition is not governed")
	ErrAssignmentOverlap           = errors.New("estate resource assignment overlaps an active assignment")
	ErrAssignmentEstateNotGoverned = errors.New("estate has no single governed tenant")
	ErrAssignmentInvalid           = errors.New("estate resource assignment command is invalid")
)

// EstateResourceAssignment is one governed assignment as CP holds it.
type EstateResourceAssignment struct {
	AssignmentID                 string     `json:"assignment_id"`
	Scope                        string     `json:"scope"`
	WorkloadClientID             string     `json:"workload_client_id,omitempty"`
	NativeConfigurationReference string     `json:"-"`
	NativeConfigurationDigest    string     `json:"-"`
	NativeResource               *NativeRef `json:"native_resource,omitempty"`
	DigitalEstateID              string     `json:"digital_estate_id"`
	TenantID                     string     `json:"tenant_id"`
	TradeEngineInstanceID        string     `json:"trade_engine_instance_id"`
	MarketScope                  []string   `json:"market_scope"`
	PermittedOperations          []string   `json:"permitted_operations"`
	Status                       string     `json:"status"`
	Revision                     int        `json:"revision"`
	EffectiveFrom                time.Time  `json:"effective_from"`
	EffectiveUntil               *time.Time `json:"effective_until,omitempty"`
	ProposedBy                   string     `json:"proposed_by"`
	ApprovedBy                   string     `json:"approved_by,omitempty"`
	ProposalReference            string     `json:"proposal_reference"`
	ApprovalReference            string     `json:"approval_reference,omitempty"`
	EvidenceReference            string     `json:"evidence_reference"`
}

// NativeRef is a Trade native seller configuration as Trade attested it under Trade-0.
type NativeRef struct {
	Reference string `json:"native_configuration_reference"`
	Digest    string `json:"native_configuration_digest"`
}

// EstateResourceAssignmentProposal is the human's proposal; the proposer and
// the Tenant are never part of it.
type EstateResourceAssignmentProposal struct {
	Scope               string     `json:"scope"`
	WorkloadClientID    string     `json:"workload_client_id,omitempty"`
	NativeResource      *NativeRef `json:"native_resource,omitempty"`
	DigitalEstateID     string     `json:"digital_estate_id"`
	TradeEngineInstance string     `json:"trade_engine_instance_id"`
	MarketScope         []string   `json:"market_scope"`
	PermittedOperations []string   `json:"permitted_operations"`
	EffectiveFrom       time.Time  `json:"effective_from"`
	EffectiveUntil      *time.Time `json:"effective_until,omitempty"`
	ProposalReference   string     `json:"proposal_reference"`
	EvidenceReference   string     `json:"evidence_reference"`
}

// EstateResourceAssignmentTransition is one governed lifecycle change.
type EstateResourceAssignmentTransition struct {
	Action            string `json:"action"`
	ExpectedRevision  int    `json:"expected_revision"`
	Reason            string `json:"reason"`
	EvidenceReference string `json:"evidence_reference"`
}

// EstateResourceAssignmentResolution is one immutable resolution answer.
type EstateResourceAssignmentResolution struct {
	AttestationID      string
	CallerClientID     string
	Scope              string
	SubjectReference   string
	DigitalEstateID    string
	EngineInstanceID   string
	MarketID           string
	Operation          string
	Outcome            string
	AssignmentID       string
	AssignmentRevision int
	TenantID           string
	CandidateCount     int
	ResolvedAt         time.Time
	ExpiresAt          *time.Time
	CorrelationID      string
}

const assignmentColumns = `assignment_id::text, scope, coalesce(workload_client_id, ''),
	coalesce(native_configuration_reference, ''), coalesce(native_configuration_digest, ''), digital_estate_id,
	tenant_id, trade_engine_instance_id, market_scope, permitted_operations, status, revision, effective_from,
	effective_until, proposed_by::text, coalesce(approved_by::text, ''), proposal_reference,
	coalesce(approval_reference, ''), evidence_reference`

func scanAssignment(row pgx.Row) (EstateResourceAssignment, error) {
	var a EstateResourceAssignment
	err := row.Scan(&a.AssignmentID, &a.Scope, &a.WorkloadClientID, &a.NativeConfigurationReference,
		&a.NativeConfigurationDigest, &a.DigitalEstateID, &a.TenantID, &a.TradeEngineInstanceID, &a.MarketScope,
		&a.PermittedOperations, &a.Status, &a.Revision, &a.EffectiveFrom, &a.EffectiveUntil, &a.ProposedBy,
		&a.ApprovedBy, &a.ProposalReference, &a.ApprovalReference, &a.EvidenceReference)
	if a.Scope == "NATIVE_RESOURCE" {
		a.NativeResource = &NativeRef{Reference: a.NativeConfigurationReference, Digest: a.NativeConfigurationDigest}
	}
	return a, err
}

func assignmentDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// mapAssignmentError turns a database guard refusal into the governed error.
func mapAssignmentError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrAssignmentOverlap
		case "23514", "23503", "22P02":
			return ErrAssignmentInvalid
		}
	}
	return err
}

// replayAssignmentCommand returns the assignment a key already produced for
// the identical command by the same actor, nil for a new key, or
// ErrIdempotencyConflict. It holds a transaction-scoped lock on the key.
func replayAssignmentCommand(ctx context.Context, tx pgx.Tx, kind, key, actorID, digest string) (*EstateResourceAssignment, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))",
		"estate-resource-assignment-command:"+kind+":"+actorID+":"+key); err != nil {
		return nil, err
	}
	var prior, assignmentID string
	err := tx.QueryRow(ctx, `SELECT request_digest, assignment_id::text FROM registry.estate_resource_assignment_command
		WHERE command_kind = $1 AND actor_principal_id = $2::uuid AND idempotency_key = $3`, kind, actorID, key).
		Scan(&prior, &assignmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if prior != digest {
		return nil, basestore.ErrIdempotencyConflict
	}
	a, err := scanAssignment(tx.QueryRow(ctx, `SELECT `+assignmentColumns+`
		FROM registry.estate_resource_assignment WHERE assignment_id = $1::uuid`, assignmentID))
	return &a, err
}

// ProposeEstateResourceAssignment records a PROPOSED assignment by the
// authenticated human. The Tenant is the estate's single governed Tenant.
func (s *Store) ProposeEstateResourceAssignment(ctx context.Context, key string, meta basestore.RequestMetadata,
	actorID string, p EstateResourceAssignmentProposal) (EstateResourceAssignment, error) {
	var empty EstateResourceAssignment
	if key == "" || !domain.IsUUID(actorID) || meta.ActorID != actorID || meta.ActorType != "human" ||
		(p.Scope == "WORKLOAD") == (p.NativeResource != nil) {
		return empty, ErrAssignmentInvalid
	}
	digest, err := assignmentDigest(struct {
		Actor    string
		Proposal EstateResourceAssignmentProposal
	}{actorID, p})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if replay, err := replayAssignmentCommand(ctx, tx, "PROPOSE", key, actorID, digest); err != nil || replay != nil {
		if replay != nil {
			return *replay, nil
		}
		return empty, err
	}
	var tenants []string
	if err := tx.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT tenant_id), '{}') FROM registry.estate_commerce_profile
		WHERE digital_estate_id = $1`, p.DigitalEstateID).Scan(&tenants); err != nil {
		return empty, err
	}
	if len(tenants) != 1 {
		return empty, ErrAssignmentEstateNotGoverned
	}
	var reference, nativeDigest any
	if p.NativeResource != nil {
		reference, nativeDigest = p.NativeResource.Reference, p.NativeResource.Digest
	}
	var workload any
	if p.WorkloadClientID != "" {
		workload = p.WorkloadClientID
	}
	a, err := scanAssignment(tx.QueryRow(ctx, `INSERT INTO registry.estate_resource_assignment (scope,
		workload_client_id, native_configuration_reference, native_configuration_digest, digital_estate_id, tenant_id,
		trade_engine_instance_id, market_scope, permitted_operations, status, revision, effective_from, effective_until,
		proposed_by, proposal_reference, evidence_reference)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'PROPOSED', 1, $10, $11, $12::uuid, $13, $14)
		RETURNING `+assignmentColumns, p.Scope, workload, reference, nativeDigest, p.DigitalEstateID, tenants[0],
		p.TradeEngineInstance, p.MarketScope, p.PermittedOperations, p.EffectiveFrom, p.EffectiveUntil, actorID,
		p.ProposalReference, p.EvidenceReference))
	if err != nil {
		return empty, mapAssignmentError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO registry.estate_resource_assignment_command (command_kind, idempotency_key,
		actor_principal_id, request_digest, assignment_id, result_revision) VALUES ('PROPOSE', $1, $2::uuid, $3, $4::uuid, 1)`,
		key, actorID, digest, a.AssignmentID); err != nil {
		return empty, mapAssignmentError(err)
	}
	return a, mapAssignmentError(tx.Commit(ctx))
}

var assignmentTransitions = map[string]struct {
	from []string
	to   string
}{
	"APPROVE":   {[]string{"PROPOSED"}, "ACTIVE"},
	"SUSPEND":   {[]string{"ACTIVE"}, "SUSPENDED"},
	"REINSTATE": {[]string{"SUSPENDED"}, "ACTIVE"},
	"REVOKE":    {[]string{"PROPOSED", "ACTIVE", "SUSPENDED"}, "REVOKED"},
}

// TransitionEstateResourceAssignment applies one governed change to the
// stated current revision, with its immutable human transition row in the
// same transaction.
func (s *Store) TransitionEstateResourceAssignment(ctx context.Context, key string, meta basestore.RequestMetadata,
	actorID, assignmentID string, t EstateResourceAssignmentTransition) (EstateResourceAssignment, error) {
	var empty EstateResourceAssignment
	rule, known := assignmentTransitions[t.Action]
	if key == "" || !known || !domain.IsUUID(actorID) || !domain.IsUUID(assignmentID) || meta.ActorID != actorID ||
		meta.ActorType != "human" {
		return empty, ErrAssignmentInvalid
	}
	digest, err := assignmentDigest(struct {
		Actor, Assignment string
		Transition        EstateResourceAssignmentTransition
	}{actorID, assignmentID, t})
	if err != nil {
		return empty, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if replay, err := replayAssignmentCommand(ctx, tx, "TRANSITION", key, actorID, digest); err != nil || replay != nil {
		if replay != nil {
			return *replay, nil
		}
		return empty, err
	}
	current, err := scanAssignment(tx.QueryRow(ctx, `SELECT `+assignmentColumns+`
		FROM registry.estate_resource_assignment WHERE assignment_id = $1::uuid FOR UPDATE`, assignmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrAssignmentNotFound
	}
	if err != nil {
		return empty, err
	}
	switch {
	case current.Revision != t.ExpectedRevision:
		return empty, ErrAssignmentRevisionConflict
	case current.Status == "REVOKED":
		return empty, ErrAssignmentTerminal
	case !slices.Contains(rule.from, current.Status):
		return empty, ErrAssignmentInvalidTransition
	case (t.Action == "APPROVE" || t.Action == "REINSTATE") && actorID == current.ProposedBy:
		return empty, ErrAssignmentSelfApproval
	}
	next := current.Revision + 1
	if _, err := tx.Exec(ctx, `INSERT INTO registry.estate_resource_assignment_transition (assignment_id, action,
		from_status, to_status, to_revision, reason, evidence_reference, performed_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::uuid)`, assignmentID, t.Action, current.Status, rule.to, next,
		t.Reason, t.EvidenceReference, actorID); err != nil {
		return empty, mapAssignmentError(err)
	}
	approvedBy, approvalReference := any(nil), any(nil)
	if t.Action == "APPROVE" {
		approvedBy, approvalReference = actorID, t.EvidenceReference
	}
	updated, err := scanAssignment(tx.QueryRow(ctx, `UPDATE registry.estate_resource_assignment
		SET status = $2, revision = $3, approved_by = coalesce($4::uuid, approved_by),
		    approval_reference = coalesce($5, approval_reference)
		WHERE assignment_id = $1::uuid RETURNING `+assignmentColumns,
		assignmentID, rule.to, next, approvedBy, approvalReference))
	if err != nil {
		return empty, mapAssignmentError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO registry.estate_resource_assignment_command (command_kind, idempotency_key,
		actor_principal_id, request_digest, assignment_id, result_revision) VALUES ('TRANSITION', $1, $2::uuid, $3, $4::uuid, $5)`,
		key, actorID, digest, assignmentID, next); err != nil {
		return empty, mapAssignmentError(err)
	}
	return updated, mapAssignmentError(tx.Commit(ctx))
}

// EstateResourceAssignment returns one assignment.
func (s *Store) EstateResourceAssignment(ctx context.Context, assignmentID string) (EstateResourceAssignment, error) {
	if !domain.IsUUID(assignmentID) {
		return EstateResourceAssignment{}, ErrAssignmentNotFound
	}
	a, err := scanAssignment(s.pool.QueryRow(ctx, `SELECT `+assignmentColumns+`
		FROM registry.estate_resource_assignment WHERE assignment_id = $1::uuid`, assignmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrAssignmentNotFound
	}
	return a, err
}

// EstateResourceAssignmentCandidates returns every assignment, in any
// status, engine instance and Market, of the exact subject and estate. The
// caller applies the resolution rules.
func (s *Store) EstateResourceAssignmentCandidates(ctx context.Context, scope, subject, estate string) (
	[]EstateResourceAssignment, error) {
	column := "workload_client_id"
	if scope == "NATIVE_RESOURCE" {
		column = "native_configuration_reference"
	}
	rows, err := s.pool.Query(ctx, `SELECT `+assignmentColumns+` FROM registry.estate_resource_assignment
		WHERE scope = $1 AND `+column+` = $2 AND digital_estate_id = $3 ORDER BY assignment_id`, scope, subject, estate)
	if err != nil {
		return nil, fmt.Errorf("read estate resource assignments: %w", err)
	}
	defer rows.Close()
	var out []EstateResourceAssignment
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan estate resource assignment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// NativeConfigurationAttested reports whether the calling Trade workload's
// latest Trade-0 attestation of the native configuration, for this engine
// instance, carries exactly this digest. A drifted, unknown or foreign
// configuration never verifies.
func (s *Store) NativeConfigurationAttested(ctx context.Context, caller, engine, reference, digest string) (bool, error) {
	var latest string
	err := s.pool.QueryRow(ctx, `SELECT native_configuration_digest FROM registry.trade_native_configuration_attestation
		WHERE client_id = $1 AND engine_instance_id = $2 AND native_configuration_reference = $3
		ORDER BY recorded_at DESC, attestation_id DESC LIMIT 1`, caller, engine, reference).Scan(&latest)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read native configuration attestation: %w", err)
	}
	return latest == digest, nil
}

// RecordEstateResourceAssignmentResolution appends one answer. An attestation
// that cannot be recorded must never be returned.
func (s *Store) RecordEstateResourceAssignmentResolution(ctx context.Context, r EstateResourceAssignmentResolution) error {
	var revision any
	if r.AssignmentRevision > 0 {
		revision = r.AssignmentRevision
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO registry.estate_resource_assignment_resolution (attestation_id,
		caller_client_id, scope, subject_reference, digital_estate_id, engine_instance_id, market_id, operation, outcome,
		assignment_id, assignment_revision, tenant_id, candidate_count, resolved_at, expires_at, correlation_id)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::uuid, $11, NULLIF($12, ''), $13,
		        $14, $15, NULLIF($16, ''))`,
		r.AttestationID, r.CallerClientID, r.Scope, r.SubjectReference, r.DigitalEstateID, r.EngineInstanceID,
		r.MarketID, r.Operation, r.Outcome, r.AssignmentID, revision, r.TenantID, r.CandidateCount, r.ResolvedAt,
		r.ExpiresAt, r.CorrelationID)
	if err != nil {
		return fmt.Errorf("record estate resource assignment resolution: %w", err)
	}
	return nil
}

// EstateAssignmentProvenance reports whether attestationID is a current
// ATTESTED assignment resolution this caller obtained for the estate, engine
// instance and Market, whose assignment is still ACTIVE at the attested
// revision. CP re-verifies the provenance from its own record.
func (s *Store) EstateAssignmentProvenance(ctx context.Context, caller, attestationID, estate, engine, market string,
	now time.Time) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM registry.estate_resource_assignment_resolution r
		JOIN registry.estate_resource_assignment a ON a.assignment_id = r.assignment_id
		WHERE r.outcome = 'ATTESTED' AND r.attestation_id::text = $2 AND r.caller_client_id = $1
		  AND r.digital_estate_id = $3 AND r.engine_instance_id = $4 AND r.market_id = $5
		  AND r.expires_at > $6 AND a.status = 'ACTIVE' AND a.revision = r.assignment_revision)`,
		caller, attestationID, estate, engine, market, now).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("read estate assignment provenance: %w", err)
	}
	return found, nil
}
