// Target path: internal/provisioning/context_resolver_test.go
package provisioning

import (
	"context"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
)

type contextAuthorityFake struct {
	tenant       domain.Tenant
	market       domain.Market
	assignment   domain.MarketAssignment
	estate       domain.DigitalEstate
	profile      domain.IsolationProfile
	organisation domain.CanonicalEntity
	mappings     []domain.TenantOrganisationMapping
}

func (f contextAuthorityFake) GetTenant(context.Context, string) (domain.Tenant, error) {
	return f.tenant, nil
}
func (f contextAuthorityFake) GetMarket(context.Context, string) (domain.Market, error) {
	return f.market, nil
}
func (f contextAuthorityFake) ListMarketAssignmentsForTenant(context.Context, string) ([]domain.MarketAssignment, error) {
	return []domain.MarketAssignment{f.assignment}, nil
}
func (f contextAuthorityFake) GetDigitalEstate(context.Context, string) (domain.DigitalEstate, error) {
	return f.estate, nil
}
func (f contextAuthorityFake) GetIsolationProfile(context.Context, string) (domain.IsolationProfile, error) {
	return f.profile, nil
}
func (f contextAuthorityFake) GetCanonicalEntity(context.Context, string) (domain.CanonicalEntity, error) {
	return f.organisation, nil
}
func (f contextAuthorityFake) ListTenantOrganisationMappings(context.Context, string, time.Time) ([]domain.TenantOrganisationMapping, error) {
	return f.mappings, nil
}

func TestAuthoritativeContextResolution(t *testing.T) {
	now := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	f := contextAuthorityFake{
		tenant: domain.Tenant{TenantID: "tn_zuri", LegalEntityID: "ZURIBEANS"},
		market: domain.Market{ID: "market-ug", Code: "UG", Currency: "UGX", IsActive: true},
		assignment: domain.MarketAssignment{
			TenantID: "tn_zuri", LegalEntityID: "ZURIBEANS", MarketID: "market-ug",
			EffectiveFrom: now.Add(-time.Hour),
		},
		estate:  domain.DigitalEstate{ID: "estate-zuri", TenantID: "tn_zuri"},
		profile: domain.IsolationProfile{ID: "iso-zuri"},
		organisation: domain.CanonicalEntity{
			ID: "org-zuri", EntityType: domain.EntityTypeBuyerOrganisation,
			Status: "ACTIVE", OwnerTenantID: "tn_zuri",
		},
	}
	r := NewAuthoritativeContextResolver(f)
	r.now = func() time.Time { return now }

	got, err := r.Resolve(context.Background(), ContextResolutionRequest{
		PrincipalID: "principal-1", TenantID: "tn_zuri", LegalEntityID: "ZURIBEANS",
		OrganisationID: "org-zuri",
		MarketID:       "market-ug", DigitalEstateID: "estate-zuri",
		IsolationProfileID: "iso-zuri", DeploymentRegion: "africa-south1",
		Environment: "production", CorrelationID: "corr-1", TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tn_zuri" || got.MarketID != "market-ug" || got.CurrencyCode != "UGX" {
		t.Fatalf("unexpected context: %+v", got)
	}
	if got.OrganisationID != "org-zuri" {
		t.Fatalf("expected organisation_id to be resolved, got %+v", got)
	}
	if got.ExpiresAt == nil {
		t.Fatal("expected bounded context lifetime")
	}
}
