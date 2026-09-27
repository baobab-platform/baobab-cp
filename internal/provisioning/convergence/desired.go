package convergence

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// Errors freezing desired state. Each names why an onboarding request cannot
// be the source of this tenant's provisioning.
var (
	ErrOnboardingNotAuthorised = errors.New("the onboarding request was not authorised for this tenant")
)

// FreezeDesiredState captures the business intent of an onboarding request
// that was AUTHORISED and fulfilled by registering tenantID. It copies the
// intent only: the market participation the request declared, and no legal
// entities, estates or profiles, which it does not carry. A market without
// declared activities keeps none; planning blocks it rather than invent one.
func FreezeDesiredState(request domain.TenantOnboardingRequest, tenantID string, version int64, now time.Time) (DesiredState, error) {
	if request.Status != domain.OnboardingFulfilled || request.AuthorisedBy == "" || request.TenantID != tenantID {
		return DesiredState{}, ErrOnboardingNotAuthorised
	}
	intent := request.DesiredState
	products := make([]DesiredProduct, 0, len(intent.ProductRequirements))
	for _, id := range dedupe(intent.ProductRequirements) {
		products = append(products, DesiredProduct{ProductID: id, Profiles: []string{}})
	}
	markets := make([]DesiredMarket, 0, len(intent.MarketScope))
	for _, code := range dedupe(intent.MarketScope) {
		market := DesiredMarket{Market: strings.ToUpper(code), Activities: []string{}}
		for _, p := range intent.MarketParticipation {
			if strings.EqualFold(p.Market, code) {
				market.Activities = dedupe(p.Activities)
			}
		}
		markets = append(markets, market)
	}
	d := DesiredState{
		Tenant:               DesiredTenant{TenantID: tenantID, DisplayName: intent.DisplayName},
		LegalEntities:        []string{},
		Products:             products,
		MarketParticipation:  markets,
		DigitalEstates:       []DesiredEstate{},
		IsolationRequirement: intent.IsolationStrategy,
		ResidencyRequirement: intent.ResidencyRegion,
		Provenance: DesiredStateProvenance{
			TenantOnboardingRequestID: request.ID,
			AdmissionDecisionID:       request.AdmissionDecisionID,
			DesiredStateVersion:       version,
		},
		FrozenAt: now.UTC(),
	}
	d.DesiredStateDigest = DesiredStateDigest(d)
	return d, nil
}

// dedupe keeps the first occurrence of each value, in order.
func dedupe(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
