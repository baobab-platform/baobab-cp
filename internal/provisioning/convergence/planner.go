package convergence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Product is a product's current ACTIVE version, reduced to what planning
// needs.
type Product struct {
	ProductID      string
	CompositionKey string
}

// Member is one capability of a composition and how critical it is to it.
type Member struct {
	CapabilityKey string
	Criticality   string
}

// Candidate is one engine instance a provider supporting a capability runs
// on, in ADR-SHARED-012 identifiers.
type Candidate struct {
	ProviderKey         string
	EngineID            string
	EngineInstanceID    string
	Region              string
	Environment         string
	ProductionPermitted bool
}

// Registry is the authoritative state planning reads. found is false when a
// thing does not exist or is not ACTIVE, which the planner reports as a
// blocker or warning; an error means the state could not be read.
type Registry interface {
	PlanningProduct(ctx context.Context, productID string) (product Product, found bool, err error)
	PlanningComposition(ctx context.Context, compositionKey string) (members []Member, found bool, err error)
	PlanningMarket(ctx context.Context, code string) (found bool, err error)
	PlanningCapability(ctx context.Context, capabilityKey string) (resolvable bool, err error)
	PlanningCandidates(ctx context.Context, capabilityKey string) ([]Candidate, error)
}

// Criticality of a composition member (capability/v1
// capabilityMembershipCriticality).
const (
	CriticalityMandatory = "MANDATORY"
	CriticalityImportant = "IMPORTANT"
	CriticalityOptional  = "OPTIONAL"
)

// Plan validity windows (ADR-BCP-021 section 28): higher risk, shorter.
const (
	highRiskValidity   = 24 * time.Hour
	mediumRiskValidity = 72 * time.Hour
)

// nonProductionEnvironments are the deployments that are not production
// for provider eligibility, as for engine registration (ADR-SHARED-011).
var nonProductionEnvironments = []string{"development", "test", "integration", "sandbox"}

// Production reports whether environment is a production deployment. Unset
// is production: eligibility fails closed.
func Production(environment string) bool {
	return !slices.Contains(nonProductionEnvironments, strings.ToLower(strings.TrimSpace(environment)))
}

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

// Planner derives a Plan from desired state and authoritative state.
type Planner struct {
	Registry    Registry
	Environment string
}

// Input identifies the plan to generate.
type Input struct {
	Desired              DesiredState
	TenantProvisioningID string
	PlanID               string
	PlanVersion          int
	BaseRevision         int64
	Now                  time.Time
}

// Plan generates the plan for in. It is deterministic: the same desired
// state against the same authoritative state yields the same Material
// (Technical Specification section 27). It writes nothing.
func (p Planner) Plan(ctx context.Context, in Input) (Plan, error) {
	if p.Registry == nil {
		return Plan{}, fmt.Errorf("planning registry is required")
	}
	d := in.Desired
	tenant := d.Tenant.TenantID
	production := Production(p.Environment)
	plan := Plan{
		PlanID: in.PlanID, PlanVersion: in.PlanVersion, BaseRevision: in.BaseRevision,
		TenantProvisioningID: in.TenantProvisioningID, TenantID: tenant,
		DesiredStateVersion: d.Provenance.DesiredStateVersion, DesiredStateDigest: d.DesiredStateDigest,
		GeneratedAt: in.Now.UTC(), Steps: []Step{}, Blockers: []Finding{}, Warnings: []Finding{},
	}

	for _, market := range d.MarketParticipation {
		if len(market.Activities) == 0 {
			plan.Blockers = append(plan.Blockers, Finding{Code: "MARKET_ACTIVITIES_UNDECLARED",
				Message: fmt.Sprintf("The onboarding request declared no participation capability for market %s.", market.Market)})
			continue
		}
		found, err := p.Registry.PlanningMarket(ctx, market.Market)
		if err != nil {
			return Plan{}, fmt.Errorf("read market %s: %w", market.Market, err)
		}
		if !found {
			plan.Blockers = append(plan.Blockers, Finding{Code: "MARKET_NOT_AVAILABLE",
				Message: fmt.Sprintf("Market %s is not an ACTIVE market.", market.Market)})
			continue
		}
		plan.Steps = append(plan.Steps, Step{StepID: stepID("market", strings.ToLower(market.Market)),
			Operation: OpCreateMarketParticipation, DependsOn: []string{},
			Resources: StepResources{TenantID: tenant, Market: market.Market}})
	}

	// Each capability once, at its strongest criticality, attributed to the
	// first product that requires it.
	type need struct {
		criticality string
		productID   string
	}
	needs := map[string]need{}
	for _, product := range d.Products {
		current, found, err := p.Registry.PlanningProduct(ctx, product.ProductID)
		if err != nil {
			return Plan{}, fmt.Errorf("read product %s: %w", product.ProductID, err)
		}
		if !found {
			plan.Blockers = append(plan.Blockers, Finding{Code: "PRODUCT_NOT_AVAILABLE",
				Message: fmt.Sprintf("Product %s has no ACTIVE version.", product.ProductID)})
			continue
		}
		members, found, err := p.Registry.PlanningComposition(ctx, current.CompositionKey)
		if err != nil {
			return Plan{}, fmt.Errorf("read composition %s: %w", current.CompositionKey, err)
		}
		if !found {
			plan.Blockers = append(plan.Blockers, Finding{Code: "COMPOSITION_NOT_AVAILABLE",
				Message: fmt.Sprintf("Product %s packages composition %s, which is not ACTIVE.", product.ProductID, current.CompositionKey)})
			continue
		}
		for _, m := range members {
			existing, seen := needs[m.CapabilityKey]
			if !seen {
				needs[m.CapabilityKey] = need{criticality: m.Criticality, productID: product.ProductID}
			} else if rank(m.Criticality) > rank(existing.criticality) {
				needs[m.CapabilityKey] = need{criticality: m.Criticality, productID: existing.productID}
			}
		}
	}
	keys := make([]string, 0, len(needs))
	for key := range needs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var bindings []string
	for _, key := range keys {
		n := needs[key]
		resolvable, err := p.Registry.PlanningCapability(ctx, key)
		if err != nil {
			return Plan{}, fmt.Errorf("read capability %s: %w", key, err)
		}
		if !resolvable {
			p.report(&plan, n.criticality, Finding{Code: "CAPABILITY_NOT_AVAILABLE",
				Message: fmt.Sprintf("Capability %s is not ACTIVE.", key)})
			continue
		}
		grant := stepID("grant", key)
		plan.Steps = append(plan.Steps, Step{StepID: grant, Operation: OpCreateCapabilityGrant, DependsOn: []string{},
			Resources: StepResources{TenantID: tenant, CapabilityKey: key, ProductID: n.productID}})

		candidates, err := p.Registry.PlanningCandidates(ctx, key)
		if err != nil {
			return Plan{}, fmt.Errorf("read providers of %s: %w", key, err)
		}
		selected, reason := p.choose(candidates, d.ResidencyRequirement, production)
		if reason != "" {
			p.report(&plan, n.criticality, Finding{Code: reason, StepID: grant,
				Message: fmt.Sprintf("No eligible provider instance for %s in %s: %s.", key, d.ResidencyRequirement, describe(reason))})
			continue
		}
		bind := stepID("bind", key)
		resources := StepResources{CapabilityKey: key, ProviderKey: selected.ProviderKey, BindingMode: "PRIMARY",
			EngineID: selected.EngineID, EngineInstanceID: selected.EngineInstanceID}
		if _, namespace, ok := strings.Cut(selected.ProviderKey, "."); ok && namespacePattern.MatchString(namespace) {
			resources.SystemNamespace = namespace
		}
		plan.Steps = append(plan.Steps, Step{StepID: bind, Operation: OpCreateCapabilityBinding,
			DependsOn: []string{grant}, Resources: resources})
		bindings = append(bindings, bind)
	}

	if bindings == nil {
		bindings = []string{}
	}
	plan.Steps = append(plan.Steps,
		Step{StepID: "verify-security", Operation: OpVerifySecurity, DependsOn: bindings, Resources: StepResources{TenantID: tenant}},
		Step{StepID: "verify-readiness", Operation: OpVerifyReadiness, DependsOn: []string{"verify-security"}, Resources: StepResources{TenantID: tenant}})

	plan.SecurityChecks = []Check{
		{Check: "RESIDENCY_COMPATIBLE", Description: fmt.Sprintf("Every bound engine instance runs in %s.", d.ResidencyRequirement)},
		{Check: "ISOLATION_PROFILE", Description: fmt.Sprintf("The tenant is isolated by %s.", d.IsolationRequirement)},
	}
	if production {
		plan.SecurityChecks = append(plan.SecurityChecks, Check{Check: "PRODUCTION_PERMITTED",
			Description: "Every bound provider is permitted in production and runs in a production environment."})
	}
	plan.ReadinessRequirements = []Check{{Check: "MANDATORY_CAPABILITIES_BOUND",
		Description: "Every mandatory capability of the tenant's products has an active binding."}}

	// Risk is derived, never chosen (ADR-BCP-021 sections 43-46): activating
	// a new production tenant is high risk.
	plan.RiskClass, plan.ExpiresAt = "MEDIUM", plan.GeneratedAt.Add(mediumRiskValidity)
	if production {
		plan.RiskClass, plan.ExpiresAt = "HIGH", plan.GeneratedAt.Add(highRiskValidity)
	}

	created := 0
	for _, s := range plan.Steps {
		if strings.HasPrefix(s.Operation, "CREATE_") {
			created++
		}
	}
	plan.ImpactAnalysis = ImpactAnalysis{
		Summary: fmt.Sprintf("Provisions %s in %d market(s) with %d capability grant(s) and %d binding(s).",
			d.Tenant.DisplayName, len(d.MarketParticipation), len(keys), len(bindings)),
		ResourcesCreated: created,
		ResidencyImpact:  fmt.Sprintf("Bindings are restricted to engine instances in %s.", d.ResidencyRequirement),
		IsolationImpact:  fmt.Sprintf("The tenant is provisioned with %s isolation.", d.IsolationRequirement),
	}
	plan.VerificationStrategy = "Reconcile desired against observed state, then require every mandatory capability to resolve for the tenant before it is READY."
	plan.CompensationStrategy = "Nothing irreversible is planned. Created grants and bindings are retired in reverse order."
	plan.PlanDigest = PlanDigest(plan)
	return plan, nil
}

// report records a finding as a blocker when the capability is mandatory,
// and as a warning otherwise: an optional capability that cannot be served
// does not prevent provisioning.
func (p Planner) report(plan *Plan, criticality string, f Finding) {
	if criticality == CriticalityMandatory {
		plan.Blockers = append(plan.Blockers, f)
		return
	}
	plan.Warnings = append(plan.Warnings, f)
}

// choose picks the eligible candidate: in the residency region and, in
// production, a production instance of a provider permitted there. Ties are
// broken by provider key, then instance, so selection is deterministic. The
// reason names why none is eligible.
func (p Planner) choose(candidates []Candidate, residency string, production bool) (Candidate, string) {
	if len(candidates) == 0 {
		return Candidate{}, "NO_PROVIDER"
	}
	var inRegion, eligible []Candidate
	for _, c := range candidates {
		if c.Region != residency {
			continue
		}
		inRegion = append(inRegion, c)
		if production && (!c.ProductionPermitted || c.Environment != "production") {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(inRegion) == 0 {
		return Candidate{}, "NO_RESIDENCY_COMPLIANT_PROVIDER"
	}
	if len(eligible) == 0 {
		return Candidate{}, "NO_PRODUCTION_PERMITTED_PROVIDER"
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].ProviderKey != eligible[j].ProviderKey {
			return eligible[i].ProviderKey < eligible[j].ProviderKey
		}
		return eligible[i].EngineInstanceID < eligible[j].EngineInstanceID
	})
	return eligible[0], ""
}

func describe(reason string) string {
	switch reason {
	case "NO_PROVIDER":
		return "no ACTIVE provider supports it"
	case "NO_RESIDENCY_COMPLIANT_PROVIDER":
		return "no provider instance runs in the residency region"
	default:
		return "no provider instance in the region is permitted in production"
	}
}

func rank(criticality string) int {
	switch criticality {
	case CriticalityMandatory:
		return 3
	case CriticalityImportant:
		return 2
	default:
		return 1
	}
}

// stepID is a stable step id ("grant-commerce-order-manage"). One longer
// than the contract allows keeps a readable prefix and a hash of the rest.
func stepID(kind, key string) string {
	id := kind + "-" + strings.NewReplacer(".", "-", "_", "-").Replace(key)
	if len(id) <= 64 {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return strings.TrimRight(id[:51], "-") + "-" + hex.EncodeToString(sum[:])[:12]
}
