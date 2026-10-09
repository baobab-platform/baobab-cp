// LA-04B: trusted, read-only adapter from CP persistence to the canonical
// scoped legal-actor policy. Not an API and not a grant of provider authority.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
)

// ResolveOperatingLegalActor is for CP-internal trusted context/PEP code only.
// It never takes a caller-selected legal actor or mandate identifier. It also
// refuses to use an unattested, expired or absent PRIMARY Organisation.
func (s *Store) ResolveOperatingLegalActor(ctx context.Context, request legalactor.Request, evaluatedAt time.Time) (legalactor.Resolution, error) {
	// Invalid requests must never reach PostgreSQL's UUID cast. They have
	// the same non-authorising outcome as an absent mandate.
	denied := legalactor.Resolve(request, nil, evaluatedAt)
	if request.TenantID == "" || request.OperatingOrganisationID == "" ||
		request.EffectiveAt.IsZero() || evaluatedAt.IsZero() {
		return denied, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.mandate_id::text, m.tenant_id,
		       m.operating_organisation_id::text,
		       m.responsible_legal_entity_id,
		       m.roles, m.activity_scope, m.market_scope, m.capability_scope,
		       m.status, m.authority_basis_reference, m.evidence_references,
		       COALESCE(m.legal_actor_verification_reference, ''),
		       m.effective_from, m.effective_to, m.created_by::text,
		       COALESCE(m.approved_by::text, ''), m.approved_at,
		       (
		          lp.verification_state = 'VERIFIED'
		          AND lp.source_authority NOT IN ('shared-governance', 'control-plane-registration')
		          AND lp.legal_status = 'ACTIVE'
		          AND lp.verified_at IS NOT NULL
		          AND lp.verified_at <= $3
		          AND jsonb_array_length(lp.evidence_references) > 0
		          AND lp.effective_from <= $3
		          AND (lp.effective_to IS NULL OR lp.effective_to > $3)
		          AND NOT EXISTS (
		              SELECT 1 FROM registry.first_party_organisation_identity fp
		              WHERE fp.organisation_id = lp.organisation_id
		                AND fp.identity_class = 'OPERATING_BUSINESS'
		                AND fp.incorporation_claim = 'NOT_INCORPORATED'
		          )
		       ) AS actor_verified
		  FROM registry.operating_legal_actor_mandate m
		  JOIN registry.legal_entity_profile lp
		    ON lp.legal_entity_id = m.responsible_legal_entity_id
		 WHERE m.tenant_id = $1
		   AND m.operating_organisation_id = $2::uuid
		   AND m.status IN ('ACTIVE', 'REVOKED', 'EXPIRED', 'SUSPENDED')
		   AND EXISTS (
		       SELECT 1 FROM registry.tenant_organisation_mapping om
		       WHERE om.tenant_id = m.tenant_id
		         AND om.organisation_id = m.operating_organisation_id
		         AND om.mapping_role = 'PRIMARY_ORGANISATION'
		         AND om.status = 'ACTIVE'
		         AND om.effective_from <= $3
		         AND (om.effective_to IS NULL OR om.effective_to > $3)
		   )
	`, request.TenantID, request.OperatingOrganisationID, evaluatedAt.UTC())
	if err != nil {
		// Database failure is not a positive legal-actor decision.
		return denied, fmt.Errorf("load governed legal-actor mandates: %w", err)
	}
	defer rows.Close()

	candidates := make([]legalactor.Candidate, 0)
	for rows.Next() {
		var c legalactor.Candidate
		if err := rows.Scan(
			&c.MandateID, &c.TenantID, &c.OperatingOrganisationID,
			&c.ResponsibleLegalEntityID, &c.Roles, &c.ActivityScope,
			&c.MarketScope, &c.CapabilityScope, &c.Status,
			&c.AuthorityBasisReference, &c.EvidenceReferences,
			&c.LegalActorVerificationReference, &c.EffectiveFrom,
			&c.EffectiveTo, &c.CreatedBy, &c.ApprovedBy,
			&c.ApprovedAt, &c.ActorVerified,
		); err != nil {
			return denied, fmt.Errorf("decode governed legal-actor mandate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return denied, fmt.Errorf("read governed legal-actor mandate candidates: %w", err)
	}
	return legalactor.Resolve(request, candidates, evaluatedAt), nil
}
