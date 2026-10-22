package repository

// INT-03C narrowly governed Principal deactivation. A request is an immutable
// digest-bound plan; a distinct CP-authenticated ACTIVE human checker commits
// the Principal, linked issuer identity and audit record in ONE transaction.
// This never provisions an identity or alters a real tenant's memberships.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInt03CNotPermitted = errors.New("int03c principal change refused")

var (
	int03cUUID        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	int03cHex         = regexp.MustCompile(`^[0-9a-f]{32}$`)
	int03cSHA         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	int03cProofDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const int03CIssuer = "https://hydra.int03.test"

func validInt03CSubject(subject string) bool {
	return subject == "baobab-int03c-issuer-owner" || subject == "baobab-int03c-issuer-verifier"
}

type Int03CDeactivation struct {
	RequestID   string    `json:"request_id"`
	PrincipalID string    `json:"principal_id"`
	PlanDigest  string    `json:"plan_digest"`
	Decision    string    `json:"decision"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// The digest binds every security-relevant source to a single immutable
// request. The database refuses changing the source, owner or target after
// creation, including through a raw request replay.
func int03CDigest(principal, issuer, subject, run, sha string) string {
	material := strings.Join([]string{"INT03C-DEACTIVATE-v1", principal, issuer, subject, run, sha}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The approved request digest includes actor, idempotency and an absolute
// expiration. A copied digest cannot authorise a different human maker or
// extend the decision window. The approved terminal state is always DISABLED.
func int03CPlanDigest(principal, issuer, subject, run, sha, maker, key string, expires time.Time) string {
	material := strings.Join([]string{"INT03C-DEACTIVATE-v2", "DISABLED",
		principal, issuer, subject, run, sha, maker, key, expires.UTC().Format(time.RFC3339Nano)}, "\\x00")
	sum := sha256.Sum256([]byte(material))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *PostgresRepository) ProposeInt03C(ctx context.Context, principal, issuer, subject, run, sourceSHA, maker, key string) (Int03CDeactivation, error) {
	if r == nil || r.pool == nil || !int03cUUID.MatchString(principal) || !int03cUUID.MatchString(maker) ||
		!int03cUUID.MatchString(key) || principal == maker || issuer != int03CIssuer ||
		!validInt03CSubject(subject) || !int03cHex.MatchString(run) || !int03cSHA.MatchString(sourceSHA) {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Int03CDeactivation{}, err
	}
	defer tx.Rollback(ctx)
	var makerActive bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal
       WHERE principal_id=$1::uuid AND actor_type='human' AND status='ACTIVE')`, maker).Scan(&makerActive); err != nil {
		return Int03CDeactivation{}, err
	}
	if !makerActive {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(
       SELECT 1 FROM identity.principal p JOIN identity.external_identity e
        ON e.principal_id=p.principal_id
       WHERE p.principal_id=$1::uuid AND p.actor_type='workload'
         AND p.status='ACTIVE' AND e.status='ACTIVE'
         AND e.issuer=$2 AND e.subject=$3
       )`, principal, issuer, subject).Scan(&active); err != nil {
		return Int03CDeactivation{}, err
	}
	if !active {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	// A client name and caller-supplied run/source are NOT provenance.
	// A separate CP human checker must have approved this exact, still-live
	// external identity and source revision before deactivation planning.
	var provenance bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM identity.int03c_principal_provenance p
        JOIN identity.int03c_provenance_approval a ON a.provenance_id=p.provenance_id
        WHERE p.principal_id=$1::uuid AND p.issuer=$2 AND p.subject=$3
          AND p.run_id=$4 AND p.source_sha=$5 AND p.expires_at>clock_timestamp()
          AND p.maker_id<>a.checker_id
          AND EXISTS(SELECT 1 FROM identity.principal m WHERE m.principal_id=p.maker_id
                     AND m.actor_type='human' AND m.status='ACTIVE')
          AND EXISTS(SELECT 1 FROM identity.principal h WHERE h.principal_id=a.checker_id
                     AND h.actor_type='human' AND h.status='ACTIVE'))`,
		principal, issuer, subject, run, sourceSHA).Scan(&provenance)
	if err != nil {
		return Int03CDeactivation{}, err
	}
	if !provenance {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	// PostgreSQL timestamptz stores microsecond precision. Bind the exact
	// representable deadline into the immutable digest before INSERT/RETURNING;
	// otherwise nanosecond rounding changes the apparent approved request.
	expires := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Microsecond)
	digest := int03CPlanDigest(principal, issuer, subject, run, sourceSHA, maker, key, expires)
	var out Int03CDeactivation
	err = tx.QueryRow(ctx, `INSERT INTO identity.int03c_deactivation_request
        (principal_id,issuer,subject,run_id,source_sha,plan_digest,idempotency_key,maker_id,expires_at)
      VALUES ($1::uuid,$2,$3,$4,$5,$6,$7::uuid,$8::uuid,$9::timestamptz)
      ON CONFLICT DO NOTHING
      RETURNING request_id::text,principal_id::text,plan_digest,decision,expires_at`,
		principal, issuer, subject, run, sourceSHA, digest, key, maker, expires).Scan(&out.RequestID, &out.PrincipalID, &out.PlanDigest, &out.Decision, &out.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Replays may only read their own exact request, never mutate immutable
		// evidence, re-extend approval time, or overwrite another plan digest.
		err = tx.QueryRow(ctx, `SELECT request_id::text,principal_id::text,plan_digest,decision,expires_at
     FROM identity.int03c_deactivation_request
     WHERE maker_id=$1::uuid AND idempotency_key=$2::uuid
     FOR UPDATE`, maker, key).Scan(&out.RequestID, &out.PrincipalID, &out.PlanDigest, &out.Decision, &out.ExpiresAt)
	}
	if err != nil {
		return Int03CDeactivation{}, err
	}
	if out.PlanDigest != int03CPlanDigest(principal, issuer, subject, run, sourceSHA, maker, key, out.ExpiresAt) || out.PrincipalID != principal || out.Decision != "PENDING" || !out.ExpiresAt.After(time.Now().UTC()) {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	if err = tx.Commit(ctx); err != nil {
		return Int03CDeactivation{}, err
	}
	return out, nil
}

func (r *PostgresRepository) ApproveInt03C(ctx context.Context, requestID, digest, checker string) (Int03CDeactivation, error) {
	if r == nil || r.pool == nil || !int03cUUID.MatchString(requestID) || !int03cUUID.MatchString(checker) ||
		len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Int03CDeactivation{}, err
	}
	defer tx.Rollback(ctx)
	var out Int03CDeactivation
	var maker, issuer, subject, run, sourceSHA string
	err = tx.QueryRow(ctx, `SELECT request_id::text,principal_id::text,plan_digest,decision,expires_at,maker_id::text,issuer,subject,run_id,source_sha
       FROM identity.int03c_deactivation_request WHERE request_id=$1::uuid FOR UPDATE`, requestID).
		Scan(&out.RequestID, &out.PrincipalID, &out.PlanDigest, &out.Decision, &out.ExpiresAt, &maker, &issuer, &subject, &run, &sourceSHA)
	if err != nil {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	if out.Decision != "PENDING" || out.PlanDigest != digest || out.ExpiresAt.Before(time.Now().UTC()) ||
		checker == maker || issuer != int03CIssuer || !validInt03CSubject(subject) {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	var checkerActive bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal
    WHERE principal_id=$1::uuid AND actor_type='human' AND status='ACTIVE')`, checker).Scan(&checkerActive); err != nil {
		return Int03CDeactivation{}, err
	}
	if !checkerActive {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	// Revalidate exactly the same CP-issued, independently approved synthetic
	// provenance within the commit transaction: no stale run or replaced source.
	var currentProvenance bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
       SELECT 1 FROM identity.int03c_principal_provenance p
       JOIN identity.int03c_provenance_approval a ON a.provenance_id=p.provenance_id
       WHERE p.principal_id=$1::uuid AND p.issuer=$2 AND p.subject=$3
         AND p.run_id=$4 AND p.source_sha=$5 AND p.expires_at>clock_timestamp()
         AND p.maker_id<>a.checker_id
       AND EXISTS(SELECT 1 FROM identity.principal m
                  WHERE m.principal_id=p.maker_id
                    AND m.actor_type='human' AND m.status='ACTIVE')
       AND EXISTS(SELECT 1 FROM identity.principal h
                  WHERE h.principal_id=a.checker_id
                    AND h.actor_type='human' AND h.status='ACTIVE'))`, out.PrincipalID, issuer, subject, run, sourceSHA).
		Scan(&currentProvenance)
	if err != nil {
		return Int03CDeactivation{}, err
	}
	if !currentProvenance {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	// Compare the current Principal and exact issuer mapping under row locks;
	// a different linked identity or any concurrent deactivation is fatal.
	var current string
	err = tx.QueryRow(ctx, `SELECT status FROM identity.principal
       WHERE principal_id=$1::uuid AND actor_type='workload' FOR UPDATE`, out.PrincipalID).Scan(&current)
	if err != nil || current != "ACTIVE" {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	var externalID string
	err = tx.QueryRow(ctx, `SELECT external_identity_id::text FROM identity.external_identity
       WHERE principal_id=$1::uuid AND issuer=$2 AND subject=$3 AND status='ACTIVE'
       FOR UPDATE`, out.PrincipalID, issuer, subject).Scan(&externalID)
	if err != nil {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	changed, err := tx.Exec(ctx, `UPDATE identity.principal SET status='DISABLED',updated_at=clock_timestamp()
       WHERE principal_id=$1::uuid AND status='ACTIVE'`, out.PrincipalID)
	if err != nil || changed.RowsAffected() != 1 {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	changed, err = tx.Exec(ctx, `UPDATE identity.external_identity SET status='DISABLED'
       WHERE external_identity_id=$1::uuid AND status='ACTIVE'`, externalID)
	if err != nil || changed.RowsAffected() != 1 {
		return Int03CDeactivation{}, ErrInt03CNotPermitted
	}
	// No secondary identity link may retain authority after deactivation.
	_, err = tx.Exec(ctx, `UPDATE identity.external_identity SET status='DISABLED'
       WHERE principal_id=$1::uuid AND status='ACTIVE'`, out.PrincipalID)
	if err != nil {
		return Int03CDeactivation{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity.int03c_deactivation_audit
        (request_id,principal_id,maker_id,checker_id,plan_digest,original_status,final_status)
       VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,'ACTIVE','DISABLED')`,
		out.RequestID, out.PrincipalID, maker, checker, out.PlanDigest)
	if err != nil {
		return Int03CDeactivation{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE identity.int03c_deactivation_request SET
       decision='COMPLETED', checker_id=$2::uuid,completed_at=clock_timestamp()
       WHERE request_id=$1::uuid AND decision='PENDING'`, out.RequestID, checker)
	if err != nil {
		return Int03CDeactivation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Int03CDeactivation{}, err
	}
	out.Decision = "COMPLETED"
	return out, nil
}

func (r *PostgresRepository) ReadInt03CPrincipal(ctx context.Context, principal string) (string, error) {
	if r == nil || r.pool == nil || !int03cUUID.MatchString(principal) {
		return "", ErrInt03CNotPermitted
	}
	var status string
	err := r.pool.QueryRow(ctx, `SELECT p.status FROM identity.principal p
    JOIN identity.external_identity e ON e.principal_id=p.principal_id
    WHERE p.principal_id=$1::uuid AND p.actor_type='workload'
      AND e.issuer=$2 AND e.subject IN ($3,$4) LIMIT 1`, principal, int03CIssuer,
		"baobab-int03c-issuer-owner", "baobab-int03c-issuer-verifier").Scan(&status)
	if err != nil {
		return "", fmt.Errorf("read registered INT03C Principal: %w", err)
	}
	return status, nil
}
