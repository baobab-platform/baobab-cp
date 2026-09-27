package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/health"
)

// SourceBinding is one live binding of the source provider for a migrated
// capability, with the context its scope names.
type SourceBinding struct {
	BindingID       string
	CapabilityKey   string
	ContractVersion int
	TenantID        string
	LegalEntityID   string
	EstateID        string
	// Markets are the ISO 3166-1 codes the scope names (market or
	// jurisdiction).
	Markets     []string
	Region      string
	Environment string
}

// Provider is what planning needs of a registered provider.
type Provider struct {
	Status string
	// Support is the contract major versions the provider supports for
	// each capability it declares.
	Support map[string][]int
}

// Instance is one engine instance a provider serves from, with its health
// for each migrated capability.
type Instance struct {
	EngineInstanceID string
	Region           string
	Environment      string
	Status           string
	Health           map[string]health.Levels
}

// Facts reads the authoritative state a plan is derived from. Planning
// never writes through it.
type Facts interface {
	SourceBindings(ctx context.Context, providerKey string, capabilityKeys []string) ([]SourceBinding, error)
	// Provider reports found=false for an unregistered provider.
	Provider(ctx context.Context, providerKey string) (Provider, bool, error)
	ProviderInstances(ctx context.Context, providerKey string, capabilityKeys []string) ([]Instance, error)
	// OpenMigrations names the migrations that have not ended and move
	// any of the capabilities away from the source provider.
	OpenMigrations(ctx context.Context, sourceProviderKey string, capabilityKeys []string) ([]string, error)
}

// ErrInvalid wraps a request the schema accepts but whose meaning is
// contradictory (422).
var ErrInvalid = errors.New("invalid provider migration request")

// Validate applies the rules the contract states but its schema cannot
// express.
func Validate(r Request) error {
	switch {
	case r.SourceProviderKey == r.TargetProviderKey:
		return fmt.Errorf("%w: the source and target provider are the same; moving a provider's own instances is an engine upgrade or instance replacement", ErrInvalid)
	case r.CutoverWindow != nil && !r.CutoverWindow.EndsAt.After(r.CutoverWindow.StartsAt):
		return fmt.Errorf("%w: the cutover window ends before it starts", ErrInvalid)
	}
	seen := map[string]bool{}
	for i, c := range r.Cohorts {
		if seen[c.CohortKey] {
			return fmt.Errorf("%w: cohort %s is named twice", ErrInvalid, c.CohortKey)
		}
		seen[c.CohortKey] = true
		if c.Selector == nil && i != len(r.Cohorts)-1 {
			return fmt.Errorf("%w: only the last cohort may omit its selector", ErrInvalid)
		}
	}
	capabilities := map[string]bool{}
	for _, c := range r.Capabilities {
		if capabilities[c.CapabilityKey] {
			return fmt.Errorf("%w: capability %s is named twice", ErrInvalid, c.CapabilityKey)
		}
		capabilities[c.CapabilityKey] = true
	}
	return nil
}

// Planner derives a provider migration plan from a request and
// authoritative state.
type Planner struct {
	Facts  Facts
	Policy *health.Policy
	// PlanTTL bounds how long a plan stays approvable (ADR-BCP-021
	// section 28).
	PlanTTL time.Duration
}

// Input identifies the plan to generate.
type Input struct {
	Request             Request
	ProviderMigrationID string
	PlanID              string
	PlanVersion         int
	BaseRevision        int64
	Now                 time.Time
}

// Plan generates the plan for in. It is deterministic: the same request
// against the same authoritative state yields the same Material. It writes
// nothing.
func (p Planner) Plan(ctx context.Context, in Input) (Plan, error) {
	if p.Facts == nil || p.Policy == nil {
		return Plan{}, errors.New("migration planning needs facts and a health policy")
	}
	r := in.Request
	r.Normalize()
	if err := Validate(r); err != nil {
		return Plan{}, err
	}
	ttl := p.PlanTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	plan := Plan{
		PlanID: in.PlanID, PlanVersion: in.PlanVersion, BaseRevision: in.BaseRevision,
		GeneratedAt: in.Now.UTC(), ExpiresAt: in.Now.UTC().Add(ttl),
		ProviderMigrationID: in.ProviderMigrationID, Request: r,
		SourceProviderKey: r.SourceProviderKey, TargetProviderKey: r.TargetProviderKey, MigrationMode: r.MigrationMode,
		Steps: []Step{}, SecurityChecks: []Check{}, ReadinessRequirements: []Check{}, Blockers: []Finding{}, Warnings: []Finding{},
	}
	keys := make([]string, 0, len(r.Capabilities))
	for _, c := range r.Capabilities {
		keys = append(keys, c.CapabilityKey)
	}

	bindings, err := p.Facts.SourceBindings(ctx, r.SourceProviderKey, keys)
	if err != nil {
		return Plan{}, fmt.Errorf("discover source bindings: %w", err)
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].BindingID < bindings[j].BindingID })
	byCapability := map[string][]SourceBinding{}
	for _, b := range bindings {
		byCapability[b.CapabilityKey] = append(byCapability[b.CapabilityKey], b)
	}
	for _, c := range r.Capabilities {
		if len(byCapability[c.CapabilityKey]) == 0 {
			plan.block(BlockSourceNotBound, fmt.Sprintf("%s serves %s through no active binding.", r.SourceProviderKey, c.CapabilityKey))
		}
	}

	open, err := p.Facts.OpenMigrations(ctx, r.SourceProviderKey, keys)
	if err != nil {
		return Plan{}, fmt.Errorf("read open migrations: %w", err)
	}
	for _, id := range open {
		if id != in.ProviderMigrationID {
			plan.block(BlockAlreadyInProgress, fmt.Sprintf("Migration %s already moves one of these capabilities away from %s.", id, r.SourceProviderKey))
		}
	}

	// Section 48: the target is registered, supports each capability at
	// the required contract version, and has a healthy instance able to
	// serve each affected context.
	target, found, err := p.Facts.Provider(ctx, r.TargetProviderKey)
	if err != nil {
		return Plan{}, fmt.Errorf("read target provider: %w", err)
	}
	chosen := map[string]string{} // binding id -> target engine instance
	switch {
	case !found || !strings.EqualFold(target.Status, "ACTIVE"):
		plan.block(BlockTargetNotRegistered, fmt.Sprintf("%s is not a registered, ACTIVE provider.", r.TargetProviderKey))
	default:
		for _, c := range r.Capabilities {
			versions, supported := target.Support[c.CapabilityKey]
			switch {
			case !supported:
				plan.block(BlockTargetNotSupported, fmt.Sprintf("%s does not declare support for %s.", r.TargetProviderKey, c.CapabilityKey))
			case !slices.Contains(versions, c.ContractVersion):
				plan.block(BlockTargetContractIncompatible, fmt.Sprintf("%s does not support %s contract version %d.", r.TargetProviderKey, c.CapabilityKey, c.ContractVersion))
			}
		}
		instances, err := p.Facts.ProviderInstances(ctx, r.TargetProviderKey, keys)
		if err != nil {
			return Plan{}, fmt.Errorf("read target instances: %w", err)
		}
		sort.Slice(instances, func(i, j int) bool { return instances[i].EngineInstanceID < instances[j].EngineInstanceID })
		unhealthy, ineligible := map[string]bool{}, map[string]bool{}
		for _, b := range bindings {
			instance, reason := p.serve(b, instances, in.Now)
			switch reason {
			case "":
				chosen[b.BindingID] = instance
			case BlockTargetUnhealthy:
				unhealthy[b.CapabilityKey] = true
			default:
				ineligible[b.CapabilityKey] = true
			}
		}
		for _, c := range r.Capabilities {
			if ineligible[c.CapabilityKey] {
				plan.block(BlockTargetNotEligible, fmt.Sprintf("No %s instance serves every %s context's region and environment.", r.TargetProviderKey, c.CapabilityKey))
			}
			if unhealthy[c.CapabilityKey] {
				plan.block(BlockTargetUnhealthy, fmt.Sprintf("No %s instance able to serve every %s context has a current HEALTHY observation.", r.TargetProviderKey, c.CapabilityKey))
			}
		}
	}

	if r.Shadow {
		// No capability or provider declares shadow-safe operations yet,
		// so shadowing is refused rather than guessed (section 49).
		plan.block(BlockShadowUnsafe, "Shadow was requested, but no migrated capability is declared shadow-safe.")
	}

	// Every affected context migrates in exactly one cohort.
	members := make([][]SourceBinding, len(r.Cohorts))
	for _, b := range bindings {
		matched := []int{}
		for i, c := range r.Cohorts {
			if c.Selector != nil && c.Selector.matches(b) {
				matched = append(matched, i)
			}
		}
		last := len(r.Cohorts) - 1
		switch {
		case len(matched) > 1:
			plan.block(BlockCohortOverlap, fmt.Sprintf("Binding %s matches cohorts %s and %s.", b.BindingID, r.Cohorts[matched[0]].CohortKey, r.Cohorts[matched[1]].CohortKey))
		case len(matched) == 1:
			members[matched[0]] = append(members[matched[0]], b)
		case last >= 0 && r.Cohorts[last].Selector == nil:
			members[last] = append(members[last], b)
		default:
			plan.block(BlockContextUnassigned, fmt.Sprintf("Binding %s (tenant %s) belongs to no cohort.", b.BindingID, b.TenantID))
		}
	}

	tenants, estates, markets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, b := range bindings {
		tenants[b.TenantID] = true
		if b.EstateID != "" {
			estates[b.EstateID] = true
		}
		for _, m := range b.Markets {
			markets[m] = true
		}
	}
	plan.Discovery = Discovery{BindingCount: len(bindings), TenantIDs: sortedKeys(tenants), EstateIDs: sortedKeys(estates), Markets: sortedKeys(markets)}
	for i, c := range r.Cohorts {
		cohortTenants := map[string]bool{}
		for _, b := range members[i] {
			cohortTenants[b.TenantID] = true
		}
		plan.Discovery.Cohorts = append(plan.Discovery.Cohorts, CohortCount{CohortKey: c.CohortKey, BindingCount: len(members[i]), TenantCount: len(cohortTenants)})
		if len(members[i]) == 0 {
			plan.Warnings = append(plan.Warnings, Finding{Code: WarnCohortEmpty, Message: fmt.Sprintf("Cohort %s matches no context today.", c.CohortKey)})
		}
	}
	if r.RollbackStrategy == RollbackForwardFixOnly {
		plan.Warnings = append(plan.Warnings, Finding{Code: WarnNotReversible, Message: "A shifted cohort cannot return to the source provider."})
	}

	plan.steps(r, byCapability, members, chosen)
	plan.RiskClass = risk(r)
	plan.SecurityChecks = []Check{{Check: "TARGET_ISOLATION_COMPATIBLE", Description: "Every target instance serves each cohort's region and environment."}}
	plan.ReadinessRequirements = []Check{{Check: "TARGET_HEALTHY", Description: "Every target instance a cohort moves to has a current HEALTHY observation."}}
	for _, c := range r.ValidationChecks {
		plan.ReadinessRequirements = append(plan.ReadinessRequirements, Check(c))
	}
	plan.ImpactAnalysis = ImpactAnalysis{
		Summary: fmt.Sprintf("Moves %d binding(s) of %d capabilit%s for %d tenant(s) from %s to %s in %d cohort(s).",
			len(bindings), len(r.Capabilities), plural(len(r.Capabilities), "y", "ies"), len(tenants), r.SourceProviderKey, r.TargetProviderKey, len(r.Cohorts)),
		ResourcesCreated: len(bindings), ResourcesChanged: len(bindings), ResourcesRemoved: len(bindings),
	}
	if r.MigrationMode == ModeStatefulCutover {
		plan.ImpactAnalysis.AvailabilityImpact = "Writes are frozen for each cohort during its cutover."
		plan.VerificationStrategy = "Each cohort's data is reconciled and validated before the next cohort shifts."
	} else {
		plan.VerificationStrategy = "Each cohort is validated before the next cohort shifts."
	}
	plan.CompensationStrategy = compensation(r.RollbackStrategy)
	plan.PlanDigest = PlanDigest(plan)
	return plan, nil
}

// serve picks the target instance for a binding: ACTIVE, in the binding's
// environment and region where its scope names them, and healthy for a
// CRITICAL capability (section 48). It returns the first such instance
// by id, or why none qualifies.
func (p Planner) serve(b SourceBinding, instances []Instance, now time.Time) (string, string) {
	located := false
	for _, in := range instances {
		if !strings.EqualFold(in.Status, "ACTIVE") ||
			(b.Environment != "" && in.Environment != b.Environment) || (b.Region != "" && in.Region != b.Region) {
			continue
		}
		located = true
		if p.Policy.Evaluate(health.CriticalityCritical, in.Health[b.CapabilityKey], now).Eligible {
			return in.EngineInstanceID, ""
		}
	}
	if located {
		return "", BlockTargetUnhealthy
	}
	return "", BlockTargetNotEligible
}

func (s Selector) matches(b SourceBinding) bool {
	in := func(values []string, v string) bool { return len(values) == 0 || slices.Contains(values, v) }
	anyMarket := len(s.Markets) == 0
	for _, m := range b.Markets {
		anyMarket = anyMarket || slices.Contains(s.Markets, m)
	}
	return in(s.TenantIDs, b.TenantID) && in(s.LegalEntityIDs, b.LegalEntityID) && in(s.EstateIDs, b.EstateID) &&
		in(s.Regions, b.Region) && anyMarket
}

func (plan *Plan) block(code, message string) {
	for _, f := range plan.Blockers {
		if f.Code == code && f.Message == message {
			return
		}
	}
	plan.Blockers = append(plan.Blockers, Finding{Code: code, Message: message})
}

// steps lays the plan out as one chain: verify, bind the target in
// MIGRATION mode, optionally shadow, move each cohort in order, then retire
// the source bindings.
func (plan *Plan) steps(r Request, byCapability map[string][]SourceBinding, members [][]SourceBinding, chosen map[string]string) {
	previous := ""
	add := func(id, op string, res StepResources, irreversible bool) {
		deps := []string{}
		if previous != "" {
			deps = []string{previous}
		}
		plan.Steps = append(plan.Steps, Step{StepID: id, Operation: op, DependsOn: deps, Resources: res, Irreversible: irreversible})
		previous = id
	}
	count := func(n int) *int { return &n }
	add("verify-target", OpVerifyTargetReadiness, StepResources{ProviderKey: r.TargetProviderKey}, false)
	for _, c := range r.Capabilities {
		res := StepResources{CapabilityKey: c.CapabilityKey, ProviderKey: r.TargetProviderKey, BindingMode: "MIGRATION",
			BindingCount: count(len(byCapability[c.CapabilityKey]))}
		if instance := single(byCapability[c.CapabilityKey], chosen); instance != "" {
			res.EngineInstanceID = instance
		}
		add(stepID("bind", c.CapabilityKey), OpCreateMigrationBinding, res, false)
	}
	if r.Shadow {
		add("start-shadow", OpStartShadow, StepResources{ProviderKey: r.TargetProviderKey}, false)
		add("stop-shadow", OpStopShadow, StepResources{ProviderKey: r.TargetProviderKey}, false)
	}
	irreversible := r.RollbackStrategy == RollbackForwardFixOnly
	for i, c := range r.Cohorts {
		res := StepResources{CohortKey: c.CohortKey, ProviderKey: r.TargetProviderKey, BindingCount: count(len(members[i]))}
		ops := []string{OpShiftCohort}
		if r.MigrationMode == ModeStatefulCutover {
			ops = statefulSequence
		}
		for _, op := range append(slices.Clone(ops), OpValidateCohort) {
			add(stepID(c.CohortKey, op), op, res, irreversible && op == OpShiftCohort)
		}
	}
	for _, c := range r.Capabilities {
		add(stepID("retire", c.CapabilityKey), OpRetireSourceBinding, StepResources{CapabilityKey: c.CapabilityKey,
			ProviderKey: r.SourceProviderKey, BindingCount: count(len(byCapability[c.CapabilityKey]))}, false)
	}
}

// single is the one target instance every binding moves to, if there is one.
func single(bindings []SourceBinding, chosen map[string]string) string {
	instance := ""
	for _, b := range bindings {
		switch c := chosen[b.BindingID]; {
		case c == "":
			return ""
		case instance == "":
			instance = c
		case instance != c:
			return ""
		}
	}
	return instance
}

// risk is derived, never chosen (ADR-BCP-021 sections 43-46): moving
// authoritative state is CRITICAL, an irreversible move at least HIGH, and
// a stateless rebind HIGH, since every affected context changes provider.
func risk(r Request) string {
	if r.MigrationMode == ModeStatefulCutover {
		return "CRITICAL"
	}
	return "HIGH"
}

func compensation(strategy string) string {
	switch strategy {
	case "REBIND_SOURCE":
		return "Rebind each shifted cohort to the source provider, which still holds authoritative state."
	case "RESTORE_AND_REBIND_SOURCE":
		return "Restore state written to the target after cutover to the source, then rebind the cohort to the source."
	}
	return "None: a shifted cohort is fixed forward on the target provider."
}

// Material is the digest of the plan's execution semantics: what it would
// do and on what it depends, never its identity, timing or wording
// (ADR-BCP-021 section 23).
func Material(p Plan) string {
	type material struct {
		ProviderMigrationID string
		Request             Request
		SourceProviderKey   string
		TargetProviderKey   string
		MigrationMode       string
		RiskClass           string
		Discovery           Discovery
		Steps               []Step
		Readiness           []Check
		Blockers            []string
	}
	codes := make([]string, 0, len(p.Blockers))
	for _, b := range p.Blockers {
		codes = append(codes, b.Code+":"+b.Message)
	}
	return digestOf(material{p.ProviderMigrationID, p.Request, p.SourceProviderKey, p.TargetProviderKey, p.MigrationMode, p.RiskClass,
		p.Discovery, p.Steps, p.ReadinessRequirements, codes})
}

// PlanDigest binds one plan version; an approval names it.
func PlanDigest(p Plan) string {
	type bound struct {
		PlanID       string
		PlanVersion  int
		BaseRevision int64
		Material     string
	}
	return digestOf(bound{p.PlanID, p.PlanVersion, p.BaseRevision, Material(p)})
}

func digestOf(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic("migration: digest input is not encodable: " + err.Error())
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stepID joins a prefix and a key into a contract step id, at most 64
// characters: a longer id keeps its start and ends with a short digest of
// the whole, so ids stay unique and deterministic.
func stepID(prefix, key string) string {
	id := prefix + "-" + strings.NewReplacer(".", "-", "_", "-").Replace(strings.ToLower(key))
	if len(id) <= 64 {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return strings.TrimRight(id[:51], "-") + "-" + hex.EncodeToString(sum[:])[:12]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
