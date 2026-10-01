package administration

import (
	"slices"
	"strings"
)

// Relations is the canonical relation data authority is judged against: for
// each tenant, the organisations that have an effective
// TenantOrganisationMapping to it (ADR-BCP-018 section 50; "effective" is
// domain.TenantOrganisationMapping.InEffect: ACTIVE and inside its window).
//
// It is the only organisation-to-tenant relation authority follows. Corporate
// ownership, naming, common parentage and PlatformAccount membership are not
// inputs (ADR-BCP-018 sections 29, 41, 60, 89). Coverage (Resource) and
// containment (Contains) read the same relation, so they cannot disagree.
// The zero value knows no mapping: organisation scopes then reach only the
// resources that name their organisation, and containment across levels is
// not provable, which fails closed.
type Relations struct {
	// TenantOrganisations maps a tenant id to its mapped organisations.
	TenantOrganisations map[string][]string
}

func normaliseEntityID(id string) string { return strings.ToLower(id) }

// OrganisationsOf returns the organisations mapped to the tenant.
func (r Relations) OrganisationsOf(tenantID string) []string { return r.TenantOrganisations[tenantID] }

// Maps reports whether the organisation has an effective mapping to the tenant.
func (r Relations) Maps(tenantID, organisationID string) bool {
	if tenantID == "" || organisationID == "" {
		return false
	}
	want := normaliseEntityID(organisationID)
	return slices.ContainsFunc(r.TenantOrganisations[tenantID], func(o string) bool { return normaliseEntityID(o) == want })
}

// TenantsOf lists the tenants the scopes name, for loading their mappings.
func TenantsOf(scopes ...Scope) []string {
	var out []string
	for _, s := range scopes {
		if s.TenantID != "" && !slices.Contains(out, s.TenantID) {
			out = append(out, s.TenantID)
		}
	}
	return out
}

// ResolveResource sets what the relations say about a resource: the
// organisations mapped to its tenant. A resource that names a tenant is
// then resolved even when none are mapped, so an organisation grant's
// failure to cover it is evidence rather than ignorance.
func (r Relations) ResolveResource(res Resource) Resource {
	if res.TenantID == "" {
		return res
	}
	res.MappedOrganisationIDs = slices.Clone(r.OrganisationsOf(res.TenantID))
	res.TenantOrganisationsResolved = true
	return res
}
