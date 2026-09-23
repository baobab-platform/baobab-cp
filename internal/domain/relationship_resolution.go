// Target path: baobab-platform/baobab-cp/internal/domain/relationship_resolution.go
//
// ADR-BCP-018 — Fail-closed relationship resolution helpers.
//
// INTERNAL subscription eligibility (ADR-BCP-017) and related-party treatment
// may consult verified CorporateRelationship and PlatformRelationship graphs.
// Missing or conflicted evidence yields no eligibility — never a permissive
// default.
//
// These helpers do not perform authorization. Tenant isolation remains the
// sole data-access boundary.

package domain

import "time"

// InternalEligibilityEvidence is the minimum evidence set required to treat
// an organisation as INTERNAL for subscription eligibility purposes.
// All fields that are present must be consequential (verified + active + in force).
type InternalEligibilityEvidence struct {
	OrganisationID          string
	PlatformRelationship    *PlatformRelationship
	CorporateRelationships  []CorporateRelationship
	EvaluatedAt             time.Time
}

// DeriveInternalEligibility returns true only when authoritative evidence
// supports INTERNAL treatment. Fail closed on any gap.
//
// Policy (ADR-BCP-018 / ADR-BCP-017):
//   - PLATFORM_OWNER or PLATFORM_OPERATOR with verified platform relationship, OR
//   - PLATFORM_GROUP_AFFILIATE with verified platform relationship AND at least
//     one consequential corporate relationship to a PLATFORM_OWNER organisation
//     (caller supplies the owner set), OR
//   - explicit server-side grant (not modelled here)
//
// This function does not grant tenant data access or administration rights.
func DeriveInternalEligibility(ev InternalEligibilityEvidence, platformOwnerOrgIDs map[string]struct{}) bool {
	if ev.OrganisationID == "" {
		return false
	}
	if ev.PlatformRelationship == nil {
		return false
	}
	pr := ev.PlatformRelationship
	if pr.OrganisationID != ev.OrganisationID {
		return false
	}
	if !pr.IsConsequential(ev.EvaluatedAt) {
		return false
	}

	switch pr.RelationshipType {
	case PlatformRelOwner, PlatformRelOperator:
		return true
	case PlatformRelGroupAffiliate:
		// Require a consequential corporate edge to a known platform owner.
		for _, cr := range ev.CorporateRelationships {
			if !cr.IsConsequential(ev.EvaluatedAt) {
				continue
			}
			// Edge from owner -> this org, or this org -> owner (symmetric check
			// of participation). Exact direction policy may be tightened later.
			if _, ok := platformOwnerOrgIDs[cr.SourceOrganisationID]; ok && cr.TargetOrganisationID == ev.OrganisationID {
				return true
			}
			if _, ok := platformOwnerOrgIDs[cr.TargetOrganisationID]; ok && cr.SourceOrganisationID == ev.OrganisationID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// FilterConsequentialCorporateRelationships returns only relationships that
// pass IsConsequential at the given time.
func FilterConsequentialCorporateRelationships(rels []CorporateRelationship, at time.Time) []CorporateRelationship {
	out := make([]CorporateRelationship, 0, len(rels))
	for _, r := range rels {
		if r.IsConsequential(at) {
			out = append(out, r)
		}
	}
	return out
}
