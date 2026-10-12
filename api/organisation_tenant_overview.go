package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

var errIncompleteTenantOverview = errors.New("tenant overview unavailable or incomplete")

type marketOverview struct {
	CountryCode  string `json:"country_code"`
	MarketID     string `json:"registry_market_id"`
	CurrencyCode string `json:"currency_code"`
}
type estateOverview struct {
	ID     string                     `json:"id"`
	Name   string                     `json:"name"`
	Domain string                     `json:"domain"`
	Status domain.DigitalEstateStatus `json:"status"`
}
type tenantOverview struct {
	TenantID             string                                  `json:"tenant_id"`
	DisplayName          string                                  `json:"display_name"`
	MappingRole          string                                  `json:"mapping_role"`
	MappingProvenance    string                                  `json:"mapping_provenance"`
	ResidencyRegion      string                                  `json:"residency_region"`
	IsolationStrategy    string                                  `json:"isolation_strategy"`
	DesiredState         string                                  `json:"desired_state"`
	ObservedState        string                                  `json:"observed_state"`
	MarketsVisible       bool                                    `json:"markets_visible"`
	SubscriptionsVisible bool                                    `json:"subscriptions_visible"`
	EstatesVisible       bool                                    `json:"estates_visible"`
	MarketParticipations []marketOverview                        `json:"market_participations"`
	Subscriptions        []repository.SubscriptionOverviewRecord `json:"subscriptions"`
	DigitalEstates       []estateOverview                        `json:"digital_estates"`
}

// The child resource permission must exist at the exact TENANT scope.
// Corporate group, legal person, parent organisation and PLATFORM grants are
// deliberately not expanded into cross-tenant read permissions.
func exactTenantGrant(authority administration.EffectiveAuthority, permission, id, environment string) bool {
	for _, grant := range authority.Grants {
		s := grant.Scope
		if grant.Permission == permission && s.Level == administration.LevelTenant &&
			(s.Mode == "" || s.Mode == administration.ModeExact) &&
			s.TenantID == id && (s.Environment == "" || s.Environment == environment) {
			return true
		}
	}
	return false
}

func (h organisationOverviewHandler) authorisedTenantSummaries(
	ctx context.Context, id string, at time.Time, authority administration.EffectiveAuthority,
) ([]tenantOverview, error) {
	if h.tenantStore == nil {
		return nil, errIncompleteTenantOverview
	}
	ids, err := h.reader.ListPrimaryTenantIDsForOrganisation(ctx, id, at)
	if err != nil || len(ids) > maxOverviewRows {
		return nil, errIncompleteTenantOverview
	}
	out := make([]tenantOverview, 0)
	seen := map[string]bool{}
	for _, tenantID := range ids {
		if seen[tenantID] || !domain.ValidTenantID(tenantID) {
			return nil, errIncompleteTenantOverview
		}
		seen[tenantID] = true
		if !exactTenantGrant(authority, "tenant.view", tenantID, h.environment) {
			continue
		}
		mappings, err := h.reader.ListTenantOrganisationMappings(ctx, tenantID, at)
		if err != nil {
			return nil, errIncompleteTenantOverview
		}
		var active *domain.TenantOrganisationMapping
		for i := range mappings {
			m := &mappings[i]
			if strings.EqualFold(m.OrganisationID, id) && m.MappingRole == "PRIMARY_ORGANISATION" && m.InEffect(at) {
				active = m
				break
			}
		}
		if active == nil {
			return nil, errIncompleteTenantOverview
		}
		tenant, err := h.tenantStore.GetTenant(ctx, tenantID)
		if err != nil || tenant.TenantID != tenantID {
			return nil, errIncompleteTenantOverview
		}
		item := tenantOverview{TenantID: tenantID, DisplayName: tenant.DisplayName,
			MappingRole: active.MappingRole, MappingProvenance: active.Provenance,
			ResidencyRegion: tenant.ResidencyRegion, IsolationStrategy: tenant.IsolationStrategy,
			DesiredState: tenant.DesiredState, ObservedState: tenant.ObservedState,
			MarketParticipations: []marketOverview{}, Subscriptions: []repository.SubscriptionOverviewRecord{},
			DigitalEstates: []estateOverview{}}
		if exactTenantGrant(authority, "market.view", tenantID, h.environment) {
			raw, err := h.reader.ActiveMarketParticipations(ctx, tenantID, at)
			if err != nil || len(raw) > maxOverviewRows {
				return nil, errIncompleteTenantOverview
			}
			for _, m := range raw {
				item.MarketParticipations = append(item.MarketParticipations, marketOverview{
					CountryCode: m.CountryCode, MarketID: m.RegistryMarketID, CurrencyCode: m.CurrencyCode})
			}
			item.MarketsVisible = true
		}
		if exactTenantGrant(authority, "subscription.view", tenantID, h.environment) {
			raw, err := h.reader.ListProductSubscriptionSummaries(ctx, tenantID)
			if err != nil || len(raw) > maxOverviewRows {
				return nil, errIncompleteTenantOverview
			}
			item.Subscriptions, item.SubscriptionsVisible = raw, true
		}
		if exactTenantGrant(authority, "digital-estate.view", tenantID, h.environment) {
			raw, err := h.reader.ListDigitalEstatesForTenant(ctx, tenantID)
			if err != nil || len(raw) > maxOverviewRows {
				return nil, errIncompleteTenantOverview
			}
			for _, estate := range raw {
				if estate.TenantID != tenantID {
					return nil, errIncompleteTenantOverview
				}
				item.DigitalEstates = append(item.DigitalEstates, estateOverview{
					ID: estate.ID, Name: estate.Name, Domain: estate.Domain, Status: estate.Status})
			}
			item.EstatesVisible = true
		}
		out = append(out, item)
	}
	return out, nil
}
