package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baobab-platform/baobab-cp/internal/verification"
)

// ApplicantClaims are an applicant's claims on their own client application
// (ADR-BCP-023 sections 7, 9, 191-192). They live in the application's
// ORGANISATION_ADMISSION VerificationCase, whose subject is the application,
// and start SELF_ASSERTED: only a reviewer's result changes their standing.
// Callers establish that the application is the applicant's own and still
// editable; these methods never take a case id from an applicant.
type ApplicantClaims interface {
	// AddApplicantClaim records the claim in the application's case,
	// opening that case (with newCaseID) on the first claim. A concluded
	// case takes no claims.
	// key, when set, makes it replayable: requestHash is compared on replay
	// by the caller; a concurrent create with the same key is
	// ErrVerificationIdempotency.
	AddApplicantClaim(ctx context.Context, applicationID string, claim verification.ApplicantClaim, claimID, newCaseID, asserter, key, requestHash string, now time.Time, actor AuditActor) (verification.Claim, error)
	// ApplicantClaimByIdempotencyKey is the claim asserter created with key,
	// and its request hash, or ErrVerificationNotFound.
	ApplicantClaimByIdempotencyKey(ctx context.Context, asserter, key string) (verification.Claim, string, error)
	// ListApplicationClaims lists the claims of the application's cases,
	// oldest first.
	ListApplicationClaims(ctx context.Context, applicationID string) ([]verification.Claim, error)
	// WithdrawApplicantClaim withdraws a claim asserter made on the
	// application. Anyone else's claim, or one on another application, is
	// ErrVerificationNotFound.
	WithdrawApplicantClaim(ctx context.Context, applicationID, claimID string, expectedVersion int64, asserter string, now time.Time, actor AuditActor) (verification.Claim, error)
}

const (
	subjectApplication = "APPLICATION"
	admissionPurpose   = "ORGANISATION_ADMISSION"
)

func (r *PostgresRepository) AddApplicantClaim(ctx context.Context, applicationID string, claim verification.ApplicantClaim, claimID, newCaseID, asserter, key, requestHash string, now time.Time, actor AuditActor) (verification.Claim, error) {
	if err := validateActor(actor); err != nil {
		return verification.Claim{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return verification.Claim{}, err
	}
	defer tx.Rollback(ctx)
	// One application has one open admission case: concurrent first claims
	// serialise here instead of opening two.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('application-case:' || $1, 0))`, applicationID); err != nil {
		return verification.Claim{}, err
	}
	subject := verification.Subject{SubjectType: subjectApplication, SubjectID: applicationID}
	var caseID string
	err = tx.QueryRow(ctx, `SELECT case_id FROM evidence.verification_case
		WHERE subject_type = $1 AND subject_id = $2 AND document->>'purpose' = $3
		ORDER BY opened_at DESC, case_id DESC LIMIT 1`, subjectApplication, applicationID, admissionPurpose).Scan(&caseID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		c := verification.Case{CaseID: newCaseID, Subject: subject, ApplicationID: applicationID, Purpose: admissionPurpose,
			Status: "DRAFT", ClaimIDs: []string{}, OpenedAt: now, CorrelationID: actor.CorrelationID, Version: 1}
		doc, _ := json.Marshal(c)
		if _, err := tx.Exec(ctx, `INSERT INTO evidence.verification_case (case_id, subject_type, subject_id, status, opened_by, opened_at, document, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`, c.CaseID, subjectApplication, applicationID, c.Status, asserter, now, doc, c.Version); err != nil {
			return verification.Claim{}, fmt.Errorf("open the application's verification case: %w", err)
		}
		if err := insertProvisioningAudit(ctx, tx, actor, "", "verification_case.opened", "verification-case/"+c.CaseID, map[string]any{
			"case_id": c.CaseID, "purpose": c.Purpose, "subject": c.Subject, "opened_for": "applicant_claim"}); err != nil {
			return verification.Claim{}, err
		}
		caseID = c.CaseID
	case err != nil:
		return verification.Claim{}, err
	}
	c, err := lockCase(ctx, tx, caseID, -1)
	if err != nil {
		return verification.Claim{}, err
	}
	if !verification.CaseOpen(c.Status) {
		return verification.Claim{}, fmt.Errorf("%w: the application's %s verification case takes no new claims", ErrVerificationState, c.Status)
	}
	cl := verification.Claim{ClaimID: claimID, Subject: subject, ClaimType: claim.ClaimType, Jurisdiction: claim.Jurisdiction,
		ClaimedValue: claim.ClaimedValue, AssertedBy: asserter, AssertedVia: originApplicant, AssertedAt: now, Purpose: admissionPurpose,
		Status: verification.InitialClaimStatus(originApplicant), EvidenceIDs: []string{}, SourceAppID: applicationID, Version: 1}
	if err := insertClaim(ctx, tx, c, cl, now, actor); err != nil {
		return cl, err
	}
	if key != "" {
		_, err := tx.Exec(ctx, `UPDATE evidence.claim SET create_idempotency_key = $2, create_request_hash = $3 WHERE claim_id = $1`,
			cl.ClaimID, key, requestHash)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return cl, ErrVerificationIdempotency
		}
		if err != nil {
			return cl, err
		}
	}
	return cl, tx.Commit(ctx)
}

func (r *PostgresRepository) ApplicantClaimByIdempotencyKey(ctx context.Context, asserter, key string) (verification.Claim, string, error) {
	var doc []byte
	var hash string
	if err := r.pool.QueryRow(ctx, `SELECT document, create_request_hash FROM evidence.claim
		WHERE asserted_by = $1 AND create_idempotency_key = $2`, asserter, key).Scan(&doc, &hash); err != nil {
		return verification.Claim{}, "", notFound(err)
	}
	cl, err := decodeDocument[verification.Claim](doc)
	return cl, hash, err
}

func (r *PostgresRepository) ListApplicationClaims(ctx context.Context, applicationID string) ([]verification.Claim, error) {
	return listDocuments[verification.Claim](ctx, r.pool, `SELECT cl.document FROM evidence.claim cl
		JOIN evidence.verification_case c ON c.case_id = cl.case_id
		WHERE c.subject_type = $1 AND c.subject_id = $2
		ORDER BY cl.created_at, cl.claim_id`, subjectApplication, applicationID)
}

func (r *PostgresRepository) WithdrawApplicantClaim(ctx context.Context, applicationID, claimID string, expectedVersion int64, asserter string, now time.Time, actor AuditActor) (verification.Claim, error) {
	var caseID string
	err := r.pool.QueryRow(ctx, `SELECT cl.case_id FROM evidence.claim cl
		JOIN evidence.verification_case c ON c.case_id = cl.case_id
		WHERE cl.claim_id = $1 AND cl.asserted_by = $2 AND c.subject_type = $3 AND c.subject_id = $4`,
		claimID, asserter, subjectApplication, applicationID).Scan(&caseID)
	if err != nil {
		return verification.Claim{}, notFound(err)
	}
	return r.transitionClaim(ctx, caseID, claimID, expectedVersion, "withdraw", verification.ActorApplicant, asserter, now, actor)
}

func (r *PostgresRepository) OpenClaimVerification(ctx context.Context, caseID, claimID string, expectedVersion int64, now time.Time, actor AuditActor) (verification.Claim, error) {
	return r.transitionClaim(ctx, caseID, claimID, expectedVersion, "open_verification", verification.ActorReviewer, "", now, actor)
}

// transitionClaim applies one command of the evidence_claim lifecycle for
// actorKind at the claim's expected version. An applicant (asserter set)
// acts only on their own claim; a reviewer only while the case is open.
// A command that sets a standing is RecordVerificationResult's, never this.
func (r *PostgresRepository) transitionClaim(ctx context.Context, caseID, claimID string, expectedVersion int64, command, actorKind, asserter string, now time.Time, actor AuditActor) (verification.Claim, error) {
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
	cl, err := lockClaim(ctx, tx, caseID, claimID)
	if errors.Is(err, verification.ErrUnsupported) {
		return verification.Claim{}, ErrVerificationNotFound
	}
	if err != nil {
		return verification.Claim{}, err
	}
	if asserter != "" && cl.AssertedBy != asserter {
		return verification.Claim{}, ErrVerificationNotFound
	}
	if cl.Version != expectedVersion {
		return verification.Claim{}, ErrVerificationVersion
	}
	if actorKind == verification.ActorReviewer && !verification.CaseOpen(c.Status) {
		return verification.Claim{}, fmt.Errorf("%w: a %s case takes no further verification", ErrVerificationState, c.Status)
	}
	next, err := verification.Next(verification.MachineClaim, cl.Status, command, actorKind)
	if err != nil {
		return verification.Claim{}, fmt.Errorf("%w: %v", ErrVerificationState, err)
	}
	from := cl.Status
	cl.Status = next
	cl.Version++
	doc, _ := json.Marshal(cl)
	if _, err := tx.Exec(ctx, `UPDATE evidence.claim SET status = $2, document = $3::jsonb, version = $4 WHERE claim_id = $1`,
		cl.ClaimID, cl.Status, doc, cl.Version); err != nil {
		return cl, err
	}
	if err := insertProvisioningAudit(ctx, tx, actor, "", "verification_claim."+command, "verification-case/"+caseID, map[string]any{
		"case_id": caseID, "claim_id": cl.ClaimID, "from": from, "to": cl.Status}); err != nil {
		return cl, err
	}
	return cl, tx.Commit(ctx)
}
