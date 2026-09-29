package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type fakeMarketParticipations []domain.MarketParticipation

func (f fakeMarketParticipations) ActiveMarketParticipations(context.Context, string, time.Time) ([]domain.MarketParticipation, error) {
	return f, nil
}

// TestContextResolutionFillsMarketFromParticipation: the context's
// country, primary registry market and currency come from the tenant's
// ACTIVE market participation, its only one or the one selected; several
// with none selected, or a selection outside them, fail closed, and a
// tenant that participates nowhere keeps a context without a market.
func TestContextResolutionFillsMarketFromParticipation(t *testing.T) {
	ug := domain.MarketParticipation{CountryCode: "UG", RegistryMarketID: "mkt_ug", CurrencyCode: "UGX"}
	za := domain.MarketParticipation{CountryCode: "ZA", RegistryMarketID: "mkt_za", CurrencyCode: "ZAR"}
	resolve := func(markets MarketParticipationReader, country string) (domain.Context, error) {
		svc := ContextResolutionService{
			Identity: identityServiceFor(repository.NewInMemoryRepository()),
			Tenants:  &fakeTenantStore{tenant: activeTenant()},
			Markets:  markets,
		}
		_, resolved, err := svc.WithCountry(country).Resolve(context.Background(), workloadPrincipalForContext(), "tenant-123", "", "correlation-123", time.Now())
		return resolved, err
	}
	for name, tc := range map[string]struct {
		markets MarketParticipationReader
		country string
		want    domain.MarketParticipation
		err     error
	}{
		"single participation":         {markets: fakeMarketParticipations{ug}, want: ug},
		"selected participation":       {markets: fakeMarketParticipations{ug, za}, country: "ZA", want: za},
		"several and none selected":    {markets: fakeMarketParticipations{ug, za}, err: ErrMarketContextAmbiguous},
		"selection outside the tenant": {markets: fakeMarketParticipations{ug}, country: "KE", err: ErrMarketContextNotParticipating},
		"no participation":             {markets: fakeMarketParticipations{}},
		"no reader, no selection":      {},
		"no reader, a selection":       {country: "UG", err: ErrMarketContextNotParticipating},
	} {
		t.Run(name, func(t *testing.T) {
			resolved, err := resolve(tc.markets, tc.country)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			got := domain.MarketParticipation{CountryCode: resolved.CountryCode, RegistryMarketID: resolved.MarketID, CurrencyCode: resolved.CurrencyCode}
			if got != tc.want {
				t.Fatalf("market = %+v, want %+v", got, tc.want)
			}
			if tc.want.CountryCode != "" && resolved.Provenance["market_id"].Evidence != tc.want.RegistryMarketID {
				t.Fatalf("market provenance = %+v", resolved.Provenance["market_id"])
			}
		})
	}
}
