package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

// ErrAuthoritativeSourceConflict means a write would falsify or rebind
// source-provenance evidence. It must never be treated as permission to skip
// verification, retry with untrusted content, or activate onboarding.
var ErrAuthoritativeSourceConflict = errors.New("authoritative source observation is not admissible")

// RecordAuthoritativeSourceObservation stores only metadata; source response
// bodies, secrets and applicant tax PINs must be kept out of this ledger.
//
// The caller MUST be a registered/authenticated CP workload or authorised
// reviewer and MUST supply a pre-registered AVAILABLE EvidenceRecord whose
// source, subject and purpose exactly match this existing verification case.
// This is a repository command, NOT a public applicant-submission endpoint.
func (r *PostgresRepository) RecordAuthoritativeSourceObservation(
	ctx context.Context, caseID string, obs verification.SourceObservation,
	evidenceID, key string, now time.Time, actor AuditActor, allowSynthetic bool,
) (string, error) {
	if err := validateActor(actor); err != nil {
		return "", err
	}
	if !strings.HasPrefix(evidenceID, "evr_") || len(evidenceID) > 63 ||
		len(key) < 16 || len(key) > 128 ||
		obs.CaseID != caseID || obs.Outcome != "MATCHED" ||
		obs.Synthetic && (!allowSynthetic || r.Environment == "production") {
		return "", ErrAuthoritativeSourceConflict
	}
	digestBody, err := json.Marshal(struct {
		Observation verification.SourceObservation
		EvidenceID  string
	}{obs, evidenceID})
	if err != nil {
		return "", ErrAuthoritativeSourceConflict
	}
	hash := sha256.Sum256(digestBody)
	digest := hex.EncodeToString(hash[:])
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// Serialize idempotent attempts even when two workers try the same key.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"peo03c:"+actor.ActorID+":"+key); err != nil {
		return "", err
	}
	var existingID, existingDigest string
	err = tx.QueryRow(ctx, `SELECT observation_id,request_digest
		FROM evidence.authoritative_source_observation
		WHERE recorded_by=$1 AND idempotency_key=$2`, actor.ActorID, key).Scan(&existingID, &existingDigest)
	if err == nil {
		if existingDigest != digest {
			return "", ErrAuthoritativeSourceConflict
		}
		return existingID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	recordCase, err := lockCase(ctx, tx, caseID, -1)
	if err != nil {
		return "", err
	}
	if recordCase.Status != verification.CaseVerifying {
		return "", ErrAuthoritativeSourceConflict
	}
	claim, err := lockClaim(ctx, tx, caseID, obs.ClaimID)
	if err != nil {
		return "", err
	}
	if err = verification.ValidateAuthoritativeObservation(claim, caseID, obs, now, allowSynthetic); err != nil {
		return "", ErrAuthoritativeSourceConflict
	}
	proofs, err := evidenceByID(ctx, tx, []string{evidenceID})
	if err != nil {
		return "", err
	}
	ev, exists := proofs[evidenceID]
	if !exists || ev.Status != verification.EvidenceAvail ||
		ev.SourceID != obs.SourceID || ev.Subject != claim.Subject ||
		ev.Purpose != claim.Purpose || ev.SourceRecordReference != obs.SourceReference {
		return "", ErrAuthoritativeSourceConflict
	}
	// Only an independently produced provenance record may be ingested.
	// Matching a claim is NOT itself evidence that an official API was used.
	id := domain.NewResourceID("aobs")
	var adapterID, witness, checker any
	if obs.Method == "SOURCE_API_LOOKUP" {
		adapterID = obs.AdapterID
	} else {
		witness, checker = obs.WitnessPrincipal, obs.CheckerPrincipal
	}
	_, err = tx.Exec(ctx, `INSERT INTO evidence.authoritative_source_observation
		(observation_id,case_id,claim_id,evidence_id,source_id,method,outcome,
		 source_record_reference,provenance_reference,response_digest,
		 source_observed_at,freshness_until,recorded_at,recorded_by,adapter_id,
		 witness_principal,checker_principal,is_synthetic,idempotency_key,request_digest)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		id,caseID,obs.ClaimID,evidenceID,obs.SourceID,obs.Method,obs.Outcome,
		obs.SourceReference,obs.ProvenanceRef,obs.ResponseDigest,
		obs.ObservedAt,obs.FreshnessUntil,now,actor.ActorID,adapterID,
		witness,checker,obs.Synthetic,key,digest)
	if err != nil {
		return "", fmt.Errorf("persist authoritative observation: %w", err)
	}
	if err = insertProvisioningAudit(ctx, tx, actor, "", "verification.source_observation.recorded",
		"verification-case/"+caseID, map[string]any{
			"observation_id": id, "case_id": caseID, "claim_id": obs.ClaimID,
			"source_id": obs.SourceID, "evidence_id": evidenceID,
			"synthetic": obs.Synthetic, "response_digest": obs.ResponseDigest,
		}); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}
