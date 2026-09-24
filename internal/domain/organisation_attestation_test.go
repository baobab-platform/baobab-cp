package domain

import (
	"errors"
	"testing"
	"time"
)

func TestAttestOrganisation(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	past, future := at.Add(-time.Hour), at.Add(time.Hour)
	org := CanonicalEntity{ID: "org-1", EntityType: EntityTypeOrganisation, Status: "ACTIVE", OwnerTenantID: "tn_owner"}
	buyer := CanonicalEntity{ID: "org-2", EntityType: EntityTypeBuyerOrganisation, Status: "ACTIVE", OwnerTenantID: "tn_owner"}
	mapping := func(tenant, org, status string, from time.Time, to *time.Time) TenantOrganisationMapping {
		return TenantOrganisationMapping{TenantID: tenant, OrganisationID: org, Status: status, EffectiveFrom: from, EffectiveTo: to}
	}
	cases := []struct {
		name     string
		entity   CanonicalEntity
		tenant   string
		mappings []TenantOrganisationMapping
		want     error
	}{
		{"active mapping", org, "tn_a", []TenantOrganisationMapping{mapping("tn_a", "org-1", RelationshipStatusActive, past, nil)}, nil},
		{"owner of a generic organisation without a mapping", org, "tn_owner", nil, ErrOrganisationNotMappedToTenant},
		{"mapping of another organisation", org, "tn_a", []TenantOrganisationMapping{mapping("tn_a", "org-9", RelationshipStatusActive, past, nil)}, ErrOrganisationNotMappedToTenant},
		{"another tenant's mapping", org, "tn_a", []TenantOrganisationMapping{mapping("tn_b", "org-1", RelationshipStatusActive, past, nil)}, ErrOrganisationNotMappedToTenant},
		{"pending mapping", org, "tn_a", []TenantOrganisationMapping{mapping("tn_a", "org-1", RelationshipStatusPending, past, nil)}, ErrOrganisationNotMappedToTenant},
		{"future mapping", org, "tn_a", []TenantOrganisationMapping{mapping("tn_a", "org-1", RelationshipStatusActive, future, nil)}, ErrOrganisationNotMappedToTenant},
		{"expired mapping", org, "tn_a", []TenantOrganisationMapping{mapping("tn_a", "org-1", RelationshipStatusActive, past.Add(-time.Hour), &past)}, ErrOrganisationNotMappedToTenant},
		{"owner of a buyer record", buyer, "tn_owner", nil, nil},
		{"non-owner of a buyer record", buyer, "tn_a", nil, ErrOrganisationNotMappedToTenant},
		{"mapped buyer record", buyer, "tn_a", []TenantOrganisationMapping{mapping("tn_a", "org-2", RelationshipStatusActive, past, nil)}, nil},
		{"inactive organisation", CanonicalEntity{ID: "org-1", EntityType: EntityTypeOrganisation, Status: "SUSPENDED"}, "tn_a",
			[]TenantOrganisationMapping{mapping("tn_a", "org-1", RelationshipStatusActive, past, nil)}, ErrOrganisationNotActive},
		{"not an organisation", CanonicalEntity{ID: "org-1", EntityType: EntityTypeProduct, Status: "ACTIVE"}, "tn_a",
			[]TenantOrganisationMapping{mapping("tn_a", "org-1", RelationshipStatusActive, past, nil)}, ErrOrganisationNotOrganisationKind},
		{"unknown id", CanonicalEntity{}, "tn_a", nil, ErrOrganisationNotOrganisationKind},
	}
	for _, c := range cases {
		if got := AttestOrganisation(c.entity, c.tenant, c.mappings, at); !errors.Is(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
