// PEO-02: governed forward-only suspension and revocation of founding grants.
// These commands never approve sponsorship, reinstate a grant, change expiry,
// establish legal-actor authority, or provision a tenant.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/jackc/pgx/v5"
)

type FoundingLifecycleInput struct {
	Reason            string `json:"reason"`
	EvidenceReference string `json:"evidence_reference"`
	ExpectedStatus    string `json:"expected_status"`
}

type FoundingLifecycleReceipt struct {
	TargetID string `json:"target_id"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
}

func validateFoundingLifecycle(kind, action string, in FoundingLifecycleInput) error {
	if (kind != "SPONSORSHIP" && kind != "DOCUMENTARY_DEFERRAL") ||
		(action != "SUSPEND" && action != "REVOKE") ||
		(kind == "DOCUMENTARY_DEFERRAL" && action != "REVOKE") ||
		len(strings.TrimSpace(in.Reason)) < 10 || len(in.Reason) > 2000 ||
		len(strings.TrimSpace(in.EvidenceReference)) < 3 || len(in.EvidenceReference) > 500 ||
		(in.ExpectedStatus != "ACTIVE" && in.ExpectedStatus != "SUSPENDED") ||
		(in.ExpectedStatus == "SUSPENDED" && action != "REVOKE") {
		return ErrFoundingAuthority
	}
	return nil
}

// TransitionFoundingGovernance is an independent, audited, idempotent human
// decision. A suspension or revocation immediately removes grant eligibility
// from the active database view (which also filters time-bound expiry).
// Any later expiry is governed by current-time reads, not a batch job.
func (s *Store) TransitionFoundingGovernance(
	ctx context.Context, key string, meta basestore.RequestMetadata,
	checkerID, kind, targetID, action string, in FoundingLifecycleInput,
) (FoundingLifecycleReceipt, error) {
	var empty FoundingLifecycleReceipt
	if requireProgressiveHuman(meta, checkerID) != nil ||
		!domain.IsUUID(targetID) || len(key) < 16 || len(key) > 128 ||
		validateFoundingLifecycle(kind, action, in) != nil {
		return empty, ErrFoundingAuthority
	}
	// The command identity includes the exact object, expected state and
	// evidentiary decision. Reusing a key with different intent is denied.
	request := struct {
		TargetID string                 `json:"target_id"`
		Kind     string                 `json:"kind"`
		Action   string                 `json:"action"`
		Input    FoundingLifecycleInput `json:"input"`
	}{targetID, kind, action, in}
	raw, err := json.Marshal(request)
	if err != nil {
		return empty, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"peo02:lifecycle:"+checkerID+":"+key); err != nil {
		return empty, err
	}
	var originalDigest string
	var receiptBytes []byte
	err = tx.QueryRow(ctx, `SELECT request_digest,receipt
 FROM admission.founding_lifecycle_command WHERE actor_id=$1::uuid AND idempotency_key=$2`,
		checkerID, key).Scan(&originalDigest, &receiptBytes)
	if err == nil {
		if originalDigest != digest {
			return empty, ErrFoundingAuthority
		}
		var previous FoundingLifecycleReceipt
		if err = json.Unmarshal(receiptBytes, &previous); err != nil {
			return empty, err
		}
		return previous, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var human bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.principal
 WHERE principal_id=$1::uuid AND actor_type='human')`, checkerID).Scan(&human)
	if err != nil {
		return empty, err
	}
	if !human {
		return empty, ErrFoundingAuthority
	}

	var prior, proposedBy, organisationID string
	switch kind {
	case "SPONSORSHIP":
		err = tx.QueryRow(ctx, `SELECT status,proposed_by::text,operating_organisation_id::text
 FROM admission.founding_group_sponsorship WHERE sponsorship_id=$1::uuid FOR UPDATE`, targetID).
			Scan(&prior, &proposedBy, &organisationID)
	case "DOCUMENTARY_DEFERRAL":
		err = tx.QueryRow(ctx, `SELECT status,proposed_by::text,organisation_id::text
 FROM admission.founding_documentary_deferral WHERE deferral_id=$1::uuid FOR UPDATE`, targetID).
			Scan(&prior, &proposedBy, &organisationID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrFoundingAuthority
	}
	if err != nil {
		return empty, err
	}
	if checkerID == proposedBy || prior != in.ExpectedStatus ||
		(prior == "SUSPENDED" && kind != "SPONSORSHIP") {
		return empty, ErrFoundingAuthority
	}

	now := time.Now().UTC()
	next := "SUSPENDED"
	if action == "REVOKE" {
		next = "REVOKED"
	}
	switch kind {
	case "SPONSORSHIP":
		_, err = tx.Exec(ctx, `UPDATE admission.founding_group_sponsorship
 SET status=$2, revoked_at=CASE WHEN $2='REVOKED' THEN $3 ELSE revoked_at END
 WHERE sponsorship_id=$1::uuid`, targetID, next, now)
	case "DOCUMENTARY_DEFERRAL":
		_, err = tx.Exec(ctx, `UPDATE admission.founding_documentary_deferral
 SET status='REVOKED' WHERE deferral_id=$1::uuid`, targetID)
	}
	if err != nil {
		return empty, err
	}
	if err = foundingAudit(ctx, tx, meta, "lifecycle_"+strings.ToLower(action), targetID, kind, map[string]any{
		"previous_status": prior, "new_status": next, "reason": in.Reason,
		"evidence_reference": in.EvidenceReference, "checker_id": checkerID,
	}); err != nil {
		return empty, err
	}
	if err = publishFoundingLifecycle(ctx, tx, meta, kind, targetID, organisationID, "", next); err != nil {
		return empty, err
	}
	receipt := FoundingLifecycleReceipt{TargetID: targetID, Kind: kind, Status: next}
	value, err := json.Marshal(receipt)
	if err != nil {
		return empty, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admission.founding_lifecycle_command
 (actor_id,idempotency_key,target_kind,target_id,action,request_digest,receipt)
 VALUES($1::uuid,$2,$3,$4::uuid,$5,$6,$7::jsonb)`,
		checkerID, key, kind, targetID, action, digest, value)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return receipt, nil
}
