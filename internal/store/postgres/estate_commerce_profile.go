// ADR-SHARED-042 section 14, Decision 8 (Shared control-plane/v1
// estate-commerce-profile; CP ADR-BCP-030): trusted, CP-internal persistence
// for Digital Estate commerce profiles and the immutable record of every
// resolution answer. An attestation classifies the estate only; it is never
// seller, buyer, market or purchasing authority.
package postgres

import (
	"context"
	"fmt"
	"time"
)

// EstateCommerceProfile is one governed profile as CP holds it.
type EstateCommerceProfile struct {
	CommerceProfileID     string
	DigitalEstateID       string
	TenantID              string
	TradeEngineInstanceID string
	CommerceModel         string
	MarketScope           []string
	Status                string
	Revision              int
	EffectiveFrom         time.Time
	EffectiveUntil        *time.Time
	EvidenceReference     string
}

// EstateCommerceProfileResolution is one immutable resolution answer. The
// profile, revision, model, attestation and expiry are named only when the
// outcome is ATTESTED.
type EstateCommerceProfileResolution struct {
	AttestationID       string
	CallerClientID      string
	DigitalEstateID     string
	EngineInstanceID    string
	MarketID            string
	ProvenanceKind      string
	ProvenanceReference string
	Outcome             string
	CommerceProfileID   string
	ProfileRevision     int
	CommerceModel       string
	CandidateCount      int
	ResolvedAt          time.Time
	ExpiresAt           *time.Time
	CorrelationID       string
}

// EstateCommerceProfiles returns every profile of the estate, in any status,
// engine instance and Market. The caller applies the resolution rules.
func (s *Store) EstateCommerceProfiles(ctx context.Context, digitalEstateID string) ([]EstateCommerceProfile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT commerce_profile_id::text, digital_estate_id, tenant_id, trade_engine_instance_id, commerce_model,
		       market_scope, status, revision, effective_from, effective_until, evidence_reference
		FROM registry.estate_commerce_profile
		WHERE digital_estate_id = $1
		ORDER BY commerce_profile_id`, digitalEstateID)
	if err != nil {
		return nil, fmt.Errorf("read estate commerce profiles: %w", err)
	}
	defer rows.Close()
	var out []EstateCommerceProfile
	for rows.Next() {
		var p EstateCommerceProfile
		if err := rows.Scan(&p.CommerceProfileID, &p.DigitalEstateID, &p.TenantID, &p.TradeEngineInstanceID,
			&p.CommerceModel, &p.MarketScope, &p.Status, &p.Revision, &p.EffectiveFrom, &p.EffectiveUntil,
			&p.EvidenceReference); err != nil {
			return nil, fmt.Errorf("scan estate commerce profile: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read estate commerce profiles: %w", err)
	}
	return out, nil
}

// EstateBindingProvenance reports whether the caller holds a current estate
// client binding attestation (ADR-BCP-029) for the binding, estate and engine
// instance: an ATTESTED resolution it requested itself, made no earlier than
// since. CP re-verifies the provenance from its own record; the caller's
// statement is never trusted as asserted.
func (s *Store) EstateBindingProvenance(ctx context.Context, callerClientID, bindingID, digitalEstateID,
	engineInstanceID string, since time.Time) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		    FROM registry.estate_client_binding_resolution r
		    JOIN registry.estate_client_binding b ON b.binding_id = r.binding_id
		    WHERE r.outcome = 'ATTESTED' AND r.caller_client_id = $1 AND r.binding_id::text = $2
		      AND b.digital_estate_id = $3 AND b.engine_instance_id = $4 AND r.engine_instance_id = $4
		      AND b.status = 'ACTIVE' AND r.revision = b.revision AND r.resolved_at >= $5)`,
		callerClientID, bindingID, digitalEstateID, engineInstanceID, since).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("read estate binding provenance: %w", err)
	}
	return found, nil
}

// RecordEstateCommerceProfileResolution appends one answer. An attestation
// that cannot be recorded must never be returned.
func (s *Store) RecordEstateCommerceProfileResolution(ctx context.Context, r EstateCommerceProfileResolution) error {
	var revision any
	if r.ProfileRevision > 0 {
		revision = r.ProfileRevision
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO registry.estate_commerce_profile_resolution (
		    attestation_id, caller_client_id, digital_estate_id, engine_instance_id, market_id, provenance_kind,
		    provenance_reference, outcome, commerce_profile_id, profile_revision, commerce_model, candidate_count,
		    resolved_at, expires_at, correlation_id)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid, $10, NULLIF($11, ''), $12,
		        $13, $14, NULLIF($15, ''))`,
		r.AttestationID, r.CallerClientID, r.DigitalEstateID, r.EngineInstanceID, r.MarketID, r.ProvenanceKind,
		r.ProvenanceReference, r.Outcome, r.CommerceProfileID, revision, r.CommerceModel, r.CandidateCount,
		r.ResolvedAt, r.ExpiresAt, r.CorrelationID)
	if err != nil {
		return fmt.Errorf("record estate commerce profile resolution: %w", err)
	}
	return nil
}
