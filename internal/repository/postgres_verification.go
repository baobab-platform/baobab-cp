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

	"github.com/baobab-platform/baobab-cp/internal/verification"
)

// VerificationRepository persists the ADR-BCP-023 verification workflow
// (Shared evidence/v1). Every command is one transaction that locks the
// case it touches, so a case's claims, checks, results and discrepancies
// change in a serial order.
type VerificationRepository interface {
	RegisterEvidence(ctx context.Context, e verification.Evidence, key, requestHash string, actor AuditActor) (verification.Evidence, error)
	GetEvidence(ctx context.Context, id string) (verification.Evidence, error)
	GetEvidenceByIdempotencyKey(ctx context.Context, obtainedBy, key string) (verification.Evidence, string, error)
	OpenVerificationCase(ctx context.Context, c verification.Case, openedBy, key, requestHash string, actor AuditActor) (verification.Case, error)
	GetVerificationCase(ctx context.Context, id string) (verification.Case, error)
	GetVerificationCaseByIdempotencyKey(ctx context.Context, openedBy, key string) (verification.Case, string, error)
	ListVerificationCases(ctx context.Context, f VerificationCaseFilter) ([]verification.Case, string, error)
	TransitionVerificationCase(ctx context.Context, id string, expectedVersion int64, t verification.Transition, now time.Time, actor AuditActor) (verification.Case, error)
	AddVerificationClaim(ctx context.Context, caseID string, sub verification.ClaimSubmission, claimID, asserter string, now time.Time, actor AuditActor) (verification.Claim, error)
	ListVerificationClaims(ctx context.Context, caseID string) ([]verification.Claim, error)
	RecordVerificationCheck(ctx context.Context, caseID string, rec verification.CheckRecord, checkID, performer string, now time.Time, actor AuditActor) (verification.Check, error)
	ListVerificationChecks(ctx context.Context, caseID string) ([]verification.Check, error)
	RecordVerificationResult(ctx context.Context, caseID string, rec verification.ResultRecord, resultID, decider string, now time.Time, actor AuditActor) (verification.Result, error)
	ListVerificationResults(ctx context.Context, caseID string) ([]verification.Result, error)
	RecordEvidenceDiscrepancy(ctx context.Context, caseID string, rec verification.DiscrepancyRecord, discrepancyID string, now time.Time, actor AuditActor) (verification.Discrepancy, error)
	ListEvidenceDiscrepancies(ctx context.Context, caseID string) ([]verification.Discrepancy, error)
	TransitionEvidenceDiscrepancy(ctx context.Context, id string, expectedVersion int64, t verification.Transition, resolver string, now time.Time, actor AuditActor) (verification.Discrepancy, error)
	// DiscrepancyCase is the case a discrepancy belongs to.
	DiscrepancyCase(ctx context.Context, discrepancyID string) (string, error)
	// OpenClaimVerification takes a SELF_ASSERTED claim of the case under
	// verification (evidence/v1 lifecycle.yaml open_verification).
	OpenClaimVerification(ctx context.Context, caseID, claimID string, expectedVersion int64, now time.Time, actor AuditActor) (verification.Claim, error)
	ApplicantClaims
}

// VerificationCaseFilter narrows ListVerificationCases.
type VerificationCaseFilter struct {
	Status    string
	SubjectID string
	Limit     int
	PageToken string
}

var (
	ErrVerificationNotFound     = errors.New("verification record not found")
	ErrVerificationVersion      = errors.New("verification record version mismatch")
	ErrVerificationState        = errors.New("verification state conflict")
	ErrVerificationIdempotency  = errors.New("concurrent verification create with the same idempotency key")
	ErrVerificationInvalidToken = errors.New("invalid page token")
)

func decodeDocument[T any](raw []byte) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("read verification record: %w", err)
	}
	return v, nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVerificationNotFound
	}
	return err
}

// --- Evidence -------------------------------------------------------------

func (r *PostgresRepository) RegisterEvidence(ctx context.Context, e verification.Evidence, key, requestHash string, actor AuditActor) (verification.Evidence, error) {
	if err := validateActor(actor); err != nil {
		return e, err
	}
	if _, err := verification.SourceByID(e.SourceID); err != nil {
		return e, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return e, err
	}
	defer tx.Rollback(ctx)
	if e.Supersedes != "" {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM evidence.record WHERE evidence_id = $1 FOR UPDATE`, e.Supersedes).Scan(&status); err != nil {
			return e, fmt.Errorf("%w: superseded evidence %s", notFound(err), e.Supersedes)
		}
		next, err := verification.Next(verification.MachineEvidence, status, "supersede", verification.ActorPlatform)
		if err != nil {
			return e, fmt.Errorf("%w: %v", ErrVerificationState, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE evidence.record SET status = $2, version = version + 1,
			document = jsonb_set(jsonb_set(document, '{status}', to_jsonb($2::text)), '{version}', to_jsonb(version + 1))
			WHERE evidence_id = $1`, e.Supersedes, next); err != nil {
			return e, err
		}
	}
	doc, _ := json.Marshal(e)
	_, err = tx.Exec(ctx, `INSERT INTO evidence.record (evidence_id, source_id, status, supersedes, obtained_by, submitted_by, document,
		version, created_at, create_idempotency_key, create_request_hash)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7::jsonb, $8, $9, NULLIF($10, ''), NULLIF($11, ''))`,
		e.EvidenceID, e.SourceID, e.Status, e.Supersedes, e.ObtainedBy, e.SubmittedBy, doc, e.Version, e.CreatedAt, key, requestHash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency") {
		return e, ErrVerificationIdempotency
	}
	if err != nil {
		return e, fmt.Errorf("register evidence: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "evidence.registered", "evidence/"+e.EvidenceID, map[string]any{
		"evidence_id": e.EvidenceID, "evidence_type": e.EvidenceType, "source_id": e.SourceID, "supersedes": e.Supersedes}); err != nil {
		return e, err
	}
	return e, tx.Commit(ctx)
}

func (r *PostgresRepository) GetEvidence(ctx context.Context, id string) (verification.Evidence, error) {
	var doc []byte
	if err := r.pool.QueryRow(ctx, `SELECT document FROM evidence.record WHERE evidence_id = $1`, id).Scan(&doc); err != nil {
		return verification.Evidence{}, notFound(err)
	}
	return decodeDocument[verification.Evidence](doc)
}

func (r *PostgresRepository) GetEvidenceByIdempotencyKey(ctx context.Context, obtainedBy, key string) (verification.Evidence, string, error) {
	var doc []byte
	var hash string
	if err := r.pool.QueryRow(ctx, `SELECT document, create_request_hash FROM evidence.record WHERE obtained_by = $1 AND create_idempotency_key = $2`,
		obtainedBy, key).Scan(&doc, &hash); err != nil {
		return verification.Evidence{}, "", notFound(err)
	}
	e, err := decodeDocument[verification.Evidence](doc)
	return e, hash, err
}

func evidenceByID(ctx context.Context, tx pgx.Tx, ids []string) (map[string]verification.Evidence, error) {
	out := map[string]verification.Evidence{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT document FROM evidence.record WHERE evidence_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		e, err := decodeDocument[verification.Evidence](doc)
		if err != nil {
			return nil, err
		}
		out[e.EvidenceID] = e
	}
	return out, rows.Err()
}

// --- Cases ---------------------------------------------------------------

func (r *PostgresRepository) OpenVerificationCase(ctx context.Context, c verification.Case, openedBy, key, requestHash string, actor AuditActor) (verification.Case, error) {
	if err := validateActor(actor); err != nil {
		return c, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	doc, _ := json.Marshal(c)
	_, err = tx.Exec(ctx, `INSERT INTO evidence.verification_case (case_id, subject_type, subject_id, status, opened_by, opened_at, document,
		version, create_idempotency_key, create_request_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, NULLIF($9, ''), NULLIF($10, ''))`,
		c.CaseID, c.Subject.SubjectType, c.Subject.SubjectID, c.Status, openedBy, c.OpenedAt, doc, c.Version, key, requestHash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "idempotency") {
		return c, ErrVerificationIdempotency
	}
	if err != nil {
		return c, fmt.Errorf("open verification case: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "verification_case.opened", "verification-case/"+c.CaseID, map[string]any{
		"case_id": c.CaseID, "purpose": c.Purpose, "subject": c.Subject}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func (r *PostgresRepository) GetVerificationCase(ctx context.Context, id string) (verification.Case, error) {
	var doc []byte
	if err := r.pool.QueryRow(ctx, `SELECT document FROM evidence.verification_case WHERE case_id = $1`, id).Scan(&doc); err != nil {
		return verification.Case{}, notFound(err)
	}
	return decodeDocument[verification.Case](doc)
}

func (r *PostgresRepository) GetVerificationCaseByIdempotencyKey(ctx context.Context, openedBy, key string) (verification.Case, string, error) {
	var doc []byte
	var hash string
	if err := r.pool.QueryRow(ctx, `SELECT document, create_request_hash FROM evidence.verification_case WHERE opened_by = $1 AND create_idempotency_key = $2`,
		openedBy, key).Scan(&doc, &hash); err != nil {
		return verification.Case{}, "", notFound(err)
	}
	c, err := decodeDocument[verification.Case](doc)
	return c, hash, err
}

// ListVerificationCases pages cases newest first. The page token is
// "<opened_at unix nanos>.<case_id>" of the last case of the previous page.
func (r *PostgresRepository) ListVerificationCases(ctx context.Context, f VerificationCaseFilter) ([]verification.Case, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	args := []any{nullable(f.Status), nullable(f.SubjectID), limit + 1}
	where := `($1::text IS NULL OR status = $1) AND ($2::text IS NULL OR subject_id = $2)`
	if f.PageToken != "" {
		nanos, id, ok := strings.Cut(f.PageToken, ".")
		n, err := strconv.ParseInt(nanos, 10, 64)
		if !ok || err != nil || id == "" {
			return nil, "", ErrVerificationInvalidToken
		}
		args = append(args, time.Unix(0, n).UTC(), id)
		where += ` AND (opened_at, case_id) < ($4, $5)`
	}
	rows, err := r.pool.Query(ctx, `SELECT document, opened_at FROM evidence.verification_case WHERE `+where+`
		ORDER BY opened_at DESC, case_id DESC LIMIT $3`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []verification.Case
	var times []time.Time
	for rows.Next() {
		var doc []byte
		var at time.Time
		if err := rows.Scan(&doc, &at); err != nil {
			return nil, "", err
		}
		c, err := decodeDocument[verification.Case](doc)
		if err != nil {
			return nil, "", err
		}
		out, times = append(out, c), append(times, at)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = fmt.Sprintf("%d.%s", times[limit-1].UnixNano(), out[limit-1].CaseID)
	}
	return out, next, nil
}

// lockCase reads a case for update; expected < 0 skips the version check.
func lockCase(ctx context.Context, tx pgx.Tx, id string, expected int64) (verification.Case, error) {
	var doc []byte
	if err := tx.QueryRow(ctx, `SELECT document FROM evidence.verification_case WHERE case_id = $1 FOR UPDATE`, id).Scan(&doc); err != nil {
		return verification.Case{}, notFound(err)
	}
	c, err := decodeDocument[verification.Case](doc)
	if err != nil {
		return c, err
	}
	if expected >= 0 && c.Version != expected {
		return c, ErrVerificationVersion
	}
	return c, nil
}

func saveCase(ctx context.Context, tx pgx.Tx, c verification.Case) error {
	doc, _ := json.Marshal(c)
	_, err := tx.Exec(ctx, `UPDATE evidence.verification_case SET status = $2, completed_at = $3, document = $4::jsonb, version = $5
		WHERE case_id = $1`, c.CaseID, c.Status, c.CompletedAt, doc, c.Version)
	return err
}

// caseEnding are the statuses that complete a case.
func caseEnding(status string) bool {
	return status == verification.CaseVerified || verification.Terminal(verification.MachineCase, status)
}

func (r *PostgresRepository) TransitionVerificationCase(ctx context.Context, id string, expectedVersion int64, t verification.Transition, now time.Time, actor AuditActor) (verification.Case, error) {
	if err := validateActor(actor); err != nil {
		return verification.Case{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Case{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockCase(ctx, tx, id, expectedVersion)
	if err != nil {
		return c, err
	}
	next, err := verification.Next(verification.MachineCase, c.Status, t.Command, verification.ActorReviewer)
	if err != nil {
		return c, fmt.Errorf("%w: %v", ErrVerificationState, err)
	}
	// Leaving a conflict, or concluding, needs every discrepancy of the case
	// closed: only verification:decide closes one.
	if t.Command == "resume" || (caseEnding(next) && t.Command != "cancel") {
		var open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM evidence.discrepancy WHERE case_id = $1
			AND status IN ('OPEN', 'UNDER_REVIEW')`, c.CaseID).Scan(&open); err != nil {
			return c, err
		}
		if open > 0 {
			return c, fmt.Errorf("%w: %d discrepancies of the case are still open", ErrVerificationState, open)
		}
	}
	if next == verification.CaseVerified {
		claims, err := listClaims(ctx, tx, c.CaseID)
		if err != nil {
			return c, err
		}
		// A withdrawn or superseded claim no longer stands: it neither blocks
		// nor counts towards the conclusion.
		standing := 0
		for _, cl := range claims {
			if cl.Status == verification.ClaimWithdrawn || cl.Status == verification.ClaimSuperseded {
				continue
			}
			standing++
			if cl.Status != verification.ClaimVerified {
				return c, fmt.Errorf("%w: claim %s is %s, not VERIFIED", ErrVerificationState, cl.ClaimID, cl.Status)
			}
		}
		if standing == 0 {
			return c, fmt.Errorf("%w: a case without standing claims verifies nothing", ErrVerificationState)
		}
	}
	before := c.Status
	c.Status, c.Version = next, c.Version+1
	if t.Command == "submit" && c.SubmittedAt == nil {
		c.SubmittedAt = &now
	}
	if caseEnding(next) {
		c.CompletedAt = &now
	}
	if err := saveCase(ctx, tx, c); err != nil {
		return c, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "verification_case."+t.Command, "verification-case/"+c.CaseID, map[string]any{
		"case_id": c.CaseID, "from": before, "to": next, "reason": t.Reason}); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

// --- Claims --------------------------------------------------------------

func listClaims(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, caseID string) ([]verification.Claim, error) {
	return listDocuments[verification.Claim](ctx, q, `SELECT document FROM evidence.claim WHERE case_id = $1 ORDER BY created_at, claim_id`, caseID)
}

func listDocuments[T any](ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		v, err := decodeDocument[T](doc)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// subjectInCase refuses a subject a case does not concern: a case examines
// its own subject and, for an organisation, that organisation's legal
// entities (registry.legal_entity_profile).
func subjectInCase(ctx context.Context, tx pgx.Tx, c verification.Case, s verification.Subject) error {
	if s == c.Subject {
		return nil
	}
	org := c.OrganisationID
	if org == "" && c.Subject.SubjectType == "ORGANISATION" {
		org = c.Subject.SubjectID
	}
	if s.SubjectType == "LEGAL_ENTITY" && org != "" {
		var owned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM registry.legal_entity_profile
			WHERE legal_entity_id = $1 AND organisation_id::text = $2)`, s.SubjectID, org).Scan(&owned); err != nil {
			return err
		}
		if owned {
			return nil
		}
	}
	return fmt.Errorf("%w: %s %s is not this case's subject", verification.ErrUnsupported, s.SubjectType, s.SubjectID)
}

func (r *PostgresRepository) caseExists(ctx context.Context, caseID string) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM evidence.verification_case WHERE case_id = $1)`, caseID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrVerificationNotFound
	}
	return nil
}

func (r *PostgresRepository) AddVerificationClaim(ctx context.Context, caseID string, sub verification.ClaimSubmission, claimID, asserter string, now time.Time, actor AuditActor) (verification.Claim, error) {
	if err := validateActor(actor); err != nil {
		return verification.Claim{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Claim{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockCase(ctx, tx, caseID, -1)
	if err != nil {
		return verification.Claim{}, err
	}
	if !verification.CaseOpen(c.Status) {
		return verification.Claim{}, fmt.Errorf("%w: a %s case takes no new claims", ErrVerificationState, c.Status)
	}
	if sub.Purpose != c.Purpose {
		return verification.Claim{}, fmt.Errorf("%w: a %s claim does not belong to a %s case", verification.ErrUnsupported, sub.Purpose, c.Purpose)
	}
	if err := subjectInCase(ctx, tx, c, sub.Subject); err != nil {
		return verification.Claim{}, err
	}
	evidence, err := evidenceByID(ctx, tx, sub.EvidenceIDs)
	if err != nil {
		return verification.Claim{}, err
	}
	for _, id := range sub.EvidenceIDs {
		if _, ok := evidence[id]; !ok {
			return verification.Claim{}, fmt.Errorf("%w: evidence %s does not exist", verification.ErrUnsupported, id)
		}
	}
	evidenceIDs := sub.EvidenceIDs
	if evidenceIDs == nil {
		evidenceIDs = []string{}
	}
	cl := verification.Claim{ClaimID: claimID, Subject: sub.Subject, ClaimType: sub.ClaimType, Jurisdiction: sub.Jurisdiction,
		ClaimedValue: sub.ClaimedValue, AssertedBy: asserter, AssertedVia: originReviewer, AssertedAt: now, Purpose: sub.Purpose,
		Status: verification.InitialClaimStatus(originReviewer), EvidenceIDs: evidenceIDs, SourceAppID: c.ApplicationID, Version: 1}
	if err := insertClaim(ctx, tx, c, cl, now, actor); err != nil {
		return cl, err
	}
	return cl, tx.Commit(ctx)
}

// Claim origins of evidence/v1 lifecycle.yaml initial_by_origin.
const (
	originReviewer  = "REVIEWER"
	originApplicant = "APPLICANT"
)

// insertClaim adds cl to the locked case c, with its evidence links, and
// audits it.
func insertClaim(ctx context.Context, tx pgx.Tx, c verification.Case, cl verification.Claim, now time.Time, actor AuditActor) error {
	doc, _ := json.Marshal(cl)
	if _, err := tx.Exec(ctx, `INSERT INTO evidence.claim (claim_id, case_id, asserted_by, status, document, version, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, 1, $6)`, cl.ClaimID, c.CaseID, cl.AssertedBy, cl.Status, doc, now); err != nil {
		return fmt.Errorf("add claim: %w", err)
	}
	for _, id := range cl.EvidenceIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO evidence.claim_evidence (claim_id, evidence_id) VALUES ($1, $2)`, cl.ClaimID, id); err != nil {
			return err
		}
	}
	c.ClaimIDs = append(c.ClaimIDs, cl.ClaimID)
	c.Version++
	if err := saveCase(ctx, tx, c); err != nil {
		return err
	}
	return insertProvisioningAudit(ctx, tx, actor, "", "verification_claim.added", "verification-case/"+c.CaseID, map[string]any{
		"case_id": c.CaseID, "claim_id": cl.ClaimID, "claim_type": cl.ClaimType, "origin": cl.AssertedVia})
}

func (r *PostgresRepository) ListVerificationClaims(ctx context.Context, caseID string) ([]verification.Claim, error) {
	if err := r.caseExists(ctx, caseID); err != nil {
		return nil, err
	}
	return listClaims(ctx, r.pool, caseID)
}

func lockClaim(ctx context.Context, tx pgx.Tx, caseID, claimID string) (verification.Claim, error) {
	var doc []byte
	if err := tx.QueryRow(ctx, `SELECT document FROM evidence.claim WHERE claim_id = $1 AND case_id = $2 FOR UPDATE`, claimID, caseID).Scan(&doc); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return verification.Claim{}, fmt.Errorf("%w: claim %s is not part of this case", verification.ErrUnsupported, claimID)
		}
		return verification.Claim{}, err
	}
	return decodeDocument[verification.Claim](doc)
}

// --- Checks --------------------------------------------------------------

func (r *PostgresRepository) RecordVerificationCheck(ctx context.Context, caseID string, rec verification.CheckRecord, checkID, performer string, now time.Time, actor AuditActor) (verification.Check, error) {
	if err := validateActor(actor); err != nil {
		return verification.Check{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Check{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockCase(ctx, tx, caseID, -1)
	if err != nil {
		return verification.Check{}, err
	}
	if c.Status != verification.CaseVerifying {
		return verification.Check{}, fmt.Errorf("%w: checks are recorded while a case is VERIFYING, not %s", ErrVerificationState, c.Status)
	}
	claim, err := lockClaim(ctx, tx, caseID, rec.ClaimID)
	if err != nil {
		return verification.Check{}, err
	}
	if verification.Terminal(verification.MachineClaim, claim.Status) {
		return verification.Check{}, fmt.Errorf("%w: claim %s is %s", ErrVerificationState, claim.ClaimID, claim.Status)
	}
	evidence, err := evidenceByID(ctx, tx, rec.EvidenceIDs)
	if err != nil {
		return verification.Check{}, err
	}
	if err := verification.ValidateCheck(claim, performer, rec, evidence); err != nil {
		return verification.Check{}, err
	}
	reasons, evidenceIDs := rec.ReasonCodes, rec.EvidenceIDs
	if reasons == nil {
		reasons = []string{}
	}
	if evidenceIDs == nil {
		evidenceIDs = []string{}
	}
	chk := verification.Check{CheckID: checkID, CaseID: caseID, ClaimID: rec.ClaimID, Method: rec.Method, SourceID: rec.SourceID,
		Provider: rec.Provider, RequestedAt: now, PerformedAt: &now, PerformedBy: performer, SourceObservedAt: rec.SourceObservedAt,
		SourceRecordReference: rec.SourceRecordReference, Outcome: rec.Outcome, Dimensions: rec.Dimensions, ReasonCodes: reasons,
		FreshnessUntil: rec.FreshnessUntil, EvidenceIDs: evidenceIDs}
	doc, _ := json.Marshal(chk)
	if _, err := tx.Exec(ctx, `INSERT INTO evidence.check (check_id, case_id, claim_id, source_id, outcome, performed_by, performed_at, document)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, chk.CheckID, caseID, chk.ClaimID, chk.SourceID, chk.Outcome, performer, now, doc); err != nil {
		return chk, selfVerificationOr(err, "record check")
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "verification_check.recorded", "verification-case/"+caseID, map[string]any{
		"case_id": caseID, "check_id": chk.CheckID, "claim_id": chk.ClaimID, "source_id": chk.SourceID, "outcome": chk.Outcome}); err != nil {
		return chk, err
	}
	return chk, tx.Commit(ctx)
}

// selfVerificationOr maps the database's self-verification refusal to
// ErrSelfVerification.
func selfVerificationOr(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && strings.Contains(pgErr.Message, "asserter") {
		return verification.ErrSelfVerification
	}
	return fmt.Errorf("%s: %w", what, err)
}

func (r *PostgresRepository) ListVerificationChecks(ctx context.Context, caseID string) ([]verification.Check, error) {
	if err := r.caseExists(ctx, caseID); err != nil {
		return nil, err
	}
	return listDocuments[verification.Check](ctx, r.pool, `SELECT document FROM evidence.check WHERE case_id = $1 ORDER BY performed_at, check_id`, caseID)
}

// --- Results -------------------------------------------------------------

func (r *PostgresRepository) RecordVerificationResult(ctx context.Context, caseID string, rec verification.ResultRecord, resultID, decider string, now time.Time, actor AuditActor) (verification.Result, error) {
	if err := validateActor(actor); err != nil {
		return verification.Result{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Result{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockCase(ctx, tx, caseID, -1)
	if err != nil {
		return verification.Result{}, err
	}
	if c.Status != verification.CaseVerifying && c.Status != verification.CaseConflicted {
		return verification.Result{}, fmt.Errorf("%w: results are recorded while a case is VERIFYING or CONFLICTED, not %s", ErrVerificationState, c.Status)
	}
	claim, err := lockClaim(ctx, tx, caseID, rec.ClaimID)
	if err != nil {
		return verification.Result{}, err
	}
	checkList, err := listDocuments[verification.Check](ctx, tx, `SELECT document FROM evidence.check WHERE check_id = ANY($1)`, rec.CheckIDs)
	if err != nil {
		return verification.Result{}, err
	}
	checks := map[string]verification.Check{}
	for _, chk := range checkList {
		checks[chk.CheckID] = chk
	}
	discrepancyList, err := listDocuments[verification.Discrepancy](ctx, tx,
		`SELECT document FROM evidence.discrepancy WHERE discrepancy_id = ANY($1)`, nonNilStrings(rec.DiscrepancyIDs))
	if err != nil {
		return verification.Result{}, err
	}
	discrepancies := map[string]verification.Discrepancy{}
	for _, d := range discrepancyList {
		discrepancies[d.DiscrepancyID] = d
	}
	if err := verification.ValidateResult(claim, caseID, decider, rec, checks, discrepancies); err != nil {
		return verification.Result{}, err
	}
	standing := verification.ClaimStanding(rec.Outcome)
	if err := verification.NextTo(verification.MachineClaim, claim.Status, "record_result", standing, verification.ActorReviewer); err != nil {
		return verification.Result{}, fmt.Errorf("%w: %v", ErrVerificationState, err)
	}
	reasons := rec.ReasonCodes
	if reasons == nil {
		reasons = []string{}
	}
	res := verification.Result{ResultID: resultID, ClaimID: claim.ClaimID, CaseID: caseID, Outcome: rec.Outcome, CheckIDs: rec.CheckIDs,
		Dimensions: rec.Dimensions, Freshness: rec.Freshness, FreshnessUntil: rec.FreshnessUntil, DecidedBy: decider, DecidedAt: now,
		DiscrepancyIDs: rec.DiscrepancyIDs, Supersedes: claim.CurrentResultID, ReasonCodes: reasons}
	doc, _ := json.Marshal(res)
	if _, err := tx.Exec(ctx, `INSERT INTO evidence.result (result_id, case_id, claim_id, outcome, decided_by, decided_at, supersedes, document)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8::jsonb)`, res.ResultID, caseID, res.ClaimID, res.Outcome, decider, now, res.Supersedes, doc); err != nil {
		return res, selfVerificationOr(err, "record result")
	}
	claim.Status, claim.CurrentResultID, claim.Freshness = standing, res.ResultID, res.Freshness
	claim.Version++
	cdoc, _ := json.Marshal(claim)
	if _, err := tx.Exec(ctx, `UPDATE evidence.claim SET status = $2, current_result_id = $3, document = $4::jsonb, version = $5 WHERE claim_id = $1`,
		claim.ClaimID, claim.Status, claim.CurrentResultID, cdoc, claim.Version); err != nil {
		return res, err
	}
	// A conflict stops the case until a reviewer resolves it.
	if rec.Outcome == verification.CaseConflicted && c.Status == verification.CaseVerifying {
		next, err := verification.Next(verification.MachineCase, c.Status, "conflict", verification.ActorPlatform)
		if err != nil {
			return res, err
		}
		c.Status, c.Version = next, c.Version+1
		if err := saveCase(ctx, tx, c); err != nil {
			return res, err
		}
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "verification_result.recorded", "verification-case/"+caseID, map[string]any{
		"case_id": caseID, "result_id": res.ResultID, "claim_id": res.ClaimID, "outcome": res.Outcome, "check_ids": res.CheckIDs}); err != nil {
		return res, err
	}
	return res, tx.Commit(ctx)
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *PostgresRepository) ListVerificationResults(ctx context.Context, caseID string) ([]verification.Result, error) {
	if err := r.caseExists(ctx, caseID); err != nil {
		return nil, err
	}
	return listDocuments[verification.Result](ctx, r.pool, `SELECT document FROM evidence.result WHERE case_id = $1 ORDER BY decided_at, result_id`, caseID)
}

// --- Discrepancies -------------------------------------------------------

func (r *PostgresRepository) RecordEvidenceDiscrepancy(ctx context.Context, caseID string, rec verification.DiscrepancyRecord, discrepancyID string, now time.Time, actor AuditActor) (verification.Discrepancy, error) {
	if err := validateActor(actor); err != nil {
		return verification.Discrepancy{}, err
	}
	if err := verification.ValidateDiscrepancy(rec); err != nil {
		return verification.Discrepancy{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Discrepancy{}, err
	}
	defer tx.Rollback(ctx)
	c, err := lockCase(ctx, tx, caseID, -1)
	if err != nil {
		return verification.Discrepancy{}, err
	}
	if !verification.CaseOpen(c.Status) {
		return verification.Discrepancy{}, fmt.Errorf("%w: a %s case takes no new discrepancies", ErrVerificationState, c.Status)
	}
	if err := subjectInCase(ctx, tx, c, rec.Subject); err != nil {
		return verification.Discrepancy{}, err
	}
	var evidenceIDs []string
	for _, v := range rec.ConflictingValues {
		if v.EvidenceID != "" {
			evidenceIDs = append(evidenceIDs, v.EvidenceID)
		}
		if v.CheckID != "" {
			var inCase bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM evidence.check WHERE check_id = $1 AND case_id = $2)`, v.CheckID, caseID).Scan(&inCase); err != nil {
				return verification.Discrepancy{}, err
			}
			if !inCase {
				return verification.Discrepancy{}, fmt.Errorf("%w: check %s is not a check in this case", verification.ErrUnsupported, v.CheckID)
			}
		}
	}
	evidence, err := evidenceByID(ctx, tx, evidenceIDs)
	if err != nil {
		return verification.Discrepancy{}, err
	}
	for _, id := range evidenceIDs {
		if _, ok := evidence[id]; !ok {
			return verification.Discrepancy{}, fmt.Errorf("%w: evidence %s does not exist", verification.ErrUnsupported, id)
		}
	}
	d := verification.Discrepancy{DiscrepancyID: discrepancyID, Subject: rec.Subject, ClaimType: rec.ClaimType, CaseID: caseID,
		ConflictingValues: rec.ConflictingValues, Severity: rec.Severity, Status: "OPEN", DetectedAt: now, Version: 1}
	doc, _ := json.Marshal(d)
	if _, err := tx.Exec(ctx, `INSERT INTO evidence.discrepancy (discrepancy_id, case_id, status, detected_at, document, version)
		VALUES ($1, $2, $3, $4, $5::jsonb, 1)`, d.DiscrepancyID, caseID, d.Status, now, doc); err != nil {
		return d, fmt.Errorf("record discrepancy: %w", err)
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "evidence_discrepancy.recorded", "verification-case/"+caseID, map[string]any{
		"case_id": caseID, "discrepancy_id": d.DiscrepancyID, "claim_type": d.ClaimType, "severity": d.Severity}); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (r *PostgresRepository) ListEvidenceDiscrepancies(ctx context.Context, caseID string) ([]verification.Discrepancy, error) {
	if err := r.caseExists(ctx, caseID); err != nil {
		return nil, err
	}
	return listDocuments[verification.Discrepancy](ctx, r.pool, `SELECT document FROM evidence.discrepancy WHERE case_id = $1 ORDER BY detected_at, discrepancy_id`, caseID)
}

func (r *PostgresRepository) TransitionEvidenceDiscrepancy(ctx context.Context, id string, expectedVersion int64, t verification.Transition, resolver string, now time.Time, actor AuditActor) (verification.Discrepancy, error) {
	if err := validateActor(actor); err != nil {
		return verification.Discrepancy{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Discrepancy{}, err
	}
	defer tx.Rollback(ctx)
	var doc []byte
	var caseID string
	if err := tx.QueryRow(ctx, `SELECT document, case_id FROM evidence.discrepancy WHERE discrepancy_id = $1 FOR UPDATE`, id).Scan(&doc, &caseID); err != nil {
		return verification.Discrepancy{}, notFound(err)
	}
	d, err := decodeDocument[verification.Discrepancy](doc)
	if err != nil {
		return d, err
	}
	if d.Version != expectedVersion {
		return d, ErrVerificationVersion
	}
	next, err := verification.Next(verification.MachineDiscrepancy, d.Status, t.Command, verification.ActorReviewer)
	if err != nil {
		return d, fmt.Errorf("%w: %v", ErrVerificationState, err)
	}
	before := d.Status
	d.Status, d.Version = next, d.Version+1
	resolvedBy := ""
	if verification.Terminal(verification.MachineDiscrepancy, next) {
		d.Resolution, d.Reason, d.ResolvedBy, d.ResolvedAt = t.Resolution, t.Reason, resolver, &now
		resolvedBy = resolver
	}
	ndoc, _ := json.Marshal(d)
	if _, err := tx.Exec(ctx, `UPDATE evidence.discrepancy SET status = $2, resolved_by = NULLIF($3, ''), document = $4::jsonb, version = $5
		WHERE discrepancy_id = $1`, d.DiscrepancyID, d.Status, resolvedBy, ndoc, d.Version); err != nil {
		return d, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "evidence_discrepancy."+t.Command, "verification-case/"+caseID, map[string]any{
		"discrepancy_id": d.DiscrepancyID, "from": before, "to": next, "resolution": d.Resolution, "reason": t.Reason}); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (r *PostgresRepository) DiscrepancyCase(ctx context.Context, discrepancyID string) (string, error) {
	var caseID string
	if err := r.pool.QueryRow(ctx, `SELECT case_id FROM evidence.discrepancy WHERE discrepancy_id = $1`, discrepancyID).Scan(&caseID); err != nil {
		return "", notFound(err)
	}
	return caseID, nil
}
