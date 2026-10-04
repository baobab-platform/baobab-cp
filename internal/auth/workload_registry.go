package auth

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// WorkloadRegistry answers whether a workload client_id is currently
// ACTIVE per baobab-platform/shared's contracts/identity/v1/workload-registry.yaml
// (ADR-0007 §45's PROVISIONED/ACTIVE/SUSPENDED/REVOKED/RETIRED lifecycle).
//
// This closes the gap docs/reconciliation/gate-zb03-authority-contract-freeze.md
// §7 named: WorkloadVerifier only ever checked token signature/issuer/
// audience/actor_type, never the caller's lifecycle status, so a REVOKED
// workload's still-unexpired token would pass verification. baobab-iam's
// own tests/integration/run.sh §9 (Gate IAM-18) already enforces the
// registration half of this same gap (a Keycloak client's enabled flag
// must agree with its registry status); this is the request-time half, in
// the repository ADR-0007 §99 names as the one that enforces "at request
// time" against the registry both repos already treat as authoritative.
type WorkloadRegistry interface {
	// IsActive reports whether clientID is both known to the registry and
	// currently ACTIVE. An unknown clientID (never registered, or a typo)
	// and a known-but-non-ACTIVE clientID are both !active -- per ADR-0007
	// §44 ("an orphaned IAM client is a security defect"), the registry is
	// the sole source of truth here; there is no default-allow case.
	IsActive(clientID string) (active bool)
}

// StaticWorkloadRegistry is a WorkloadRegistry backed by an in-memory
// snapshot, loaded once at process startup rather than fetched over the
// network on every request (or even periodically) -- this repository has
// no existing runtime mechanism for consuming baobab-platform/shared contracts
// live (contracttest's SHARED_CONTRACTS_DIR is a test-only, local-checkout
// pattern; see internal/contracttest's own doc comment), and inventing one
// here -- polling cadence, staleness policy, fail-open-vs-fail-closed on a
// fetch error -- is a real design decision this slice deliberately does
// not make unilaterally. An operator who wants this enforced in production
// supplies a local snapshot file (LoadWorkloadRegistryFile); nil (the
// default -- see api.Dependencies.WorkloadRegistry) disables the check
// entirely, preserving exactly today's behavior until that snapshot exists.
type StaticWorkloadRegistry struct {
	active           map[string]bool
	allowedScopes    map[string]map[string]bool
	reporters        map[string]ReporterScope
	runtimeObservers map[string]ReporterScope
	validators       map[string][]string
}

// Canonical workload scopes consumed by the Control Plane.
const (
	ContextValidateScope       = "context:validate"
	ObserveScope               = "deployment:observe"
	IdentityRuntimeObserveScope = "identity-runtime:observe"
)

// WorkloadScopeRegistry answers whether Shared currently permits a workload to
// hold a scope. Token possession and registry permission are both required.
type WorkloadScopeRegistry interface {
	AllowsScope(clientID, scope string) bool
}

// AllowsScope implements WorkloadScopeRegistry.
func (r *StaticWorkloadRegistry) AllowsScope(clientID, scope string) bool {
	if r == nil || !r.active[clientID] {
		return false
	}
	return r.allowedScopes[clientID][scope]
}

// ValidatorRegistry answers which subject-token audiences a workload is
// registered to validate contexts for (workload-registry.yaml
// validates_audiences). It is the only source of that relationship: the
// request cannot name an audience, and no audience list the validator merely
// appears in is consulted.
type ValidatorRegistry interface {
	// ValidatesAudiences returns the audiences clientID may validate, or nil
	// when it is unknown, not ACTIVE, not allowed context:validate, or
	// declares none.
	ValidatesAudiences(clientID string) []string
}

// ValidatesAudiences implements ValidatorRegistry.
func (r *StaticWorkloadRegistry) ValidatesAudiences(clientID string) []string {
	if r == nil {
		return nil
	}
	return slices.Clone(r.validators[clientID])
}

// ReporterScope is where a registered deployment-observation reporter may
// report (ADR-BCP-025 section 2.9): its workload-registry `environment` and
// `deployment_regions`.
type ReporterScope struct {
	Environment string
	Regions     []string
}

// Allows reports whether an observation of environment and region is inside
// the reporter's registration.
func (s ReporterScope) Allows(environment, region string) bool {
	if s.Environment == "" || environment != s.Environment {
		return false
	}
	for _, r := range s.Regions {
		if r == region {
			return true
		}
	}
	return false
}

// ReporterRegistry answers where a workload is registered to report
// deployment observations. A workload that is unknown, not ACTIVE, not
// allowed deployment:observe, or lists no region has no scope: an
// observation from it is refused, never stored.
type ReporterRegistry interface {
	Reporter(clientID string) (ReporterScope, bool)
}

// IdentityRuntimeObserverRegistry is the separate authority for secret-free
// identity-runtime profile publication. deployment:observe does not imply it.
type IdentityRuntimeObserverRegistry interface {
	IdentityRuntimeObserver(clientID string) (ReporterScope, bool)
}

// Reporter implements ReporterRegistry.
func (r *StaticWorkloadRegistry) Reporter(clientID string) (ReporterScope, bool) {
	scope, ok := r.reporters[clientID]
	if !ok {
		return ReporterScope{}, false
	}
	scope.Regions = slices.Clone(scope.Regions)
	return scope, true
}

// IdentityRuntimeObserver implements IdentityRuntimeObserverRegistry.
func (r *StaticWorkloadRegistry) IdentityRuntimeObserver(clientID string) (ReporterScope, bool) {
	scope, ok := r.runtimeObservers[clientID]
	if !ok {
		return ReporterScope{}, false
	}
	scope.Regions = slices.Clone(scope.Regions)
	return scope, true
}

func (r *StaticWorkloadRegistry) IsActive(clientID string) bool {
	return r.active[clientID]
}

// workloadRegistryFile mirrors the subset of baobab-platform/shared's
// contracts/identity/v1/workload-registry.yaml this repository actually
// needs for request-time admission: lifecycle, allowed scopes, reporter
// environment/regions and validator audiences. Provider credential mechanics
// remain baobab-iam's concern.
type workloadRegistryFile struct {
	Workloads map[string]struct {
		Status             string   `yaml:"status"`
		Environment        string   `yaml:"environment"`
		AllowedScopes      []string `yaml:"allowed_scopes"`
		DeploymentRegions  []string `yaml:"deployment_regions"`
		ValidatesAudiences []string `yaml:"validates_audiences"`
	} `yaml:"workloads"`
}

// LoadWorkloadRegistryFile parses a local snapshot of baobab-platform/shared's
// workload-registry.yaml (fetched and pinned by whatever process an
// operator's deployment tooling uses -- this repository does not fetch it
// itself; see StaticWorkloadRegistry's doc comment for why). The map key
// in the source file's "workloads" section is itself a registry entry name
// (e.g. "baobab-trade-workload"), not necessarily the OAuth client_id a
// token's azp claim carries -- but every current entry's name IS its
// client_id (baobab-iam/tests/integration/run.sh §9 asserts this
// correspondence already), so this loader keys directly off that name
// rather than re-deriving client_id from a field the source schema does
// not separately provide.
func LoadWorkloadRegistryFile(path string) (*StaticWorkloadRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workload registry file: %w", err)
	}
	var parsed workloadRegistryFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse workload registry file: %w", err)
	}
	// An empty registry would reject every workload, and an unreadable
	// status would silently mean "not ACTIVE": either is a broken snapshot,
	// refused at startup rather than served.
	if len(parsed.Workloads) == 0 {
		return nil, errors.New("workload registry file declares no workloads")
	}
	for clientID, entry := range parsed.Workloads {
		switch entry.Status {
		case "PROVISIONED", "ACTIVE", "SUSPENDED", "REVOKED", "RETIRED":
		default:
			return nil, fmt.Errorf("workload registry: %s has unknown status %q", clientID, entry.Status)
		}
	}
	active := make(map[string]bool, len(parsed.Workloads))
	allowedScopes := make(map[string]map[string]bool, len(parsed.Workloads))
	reporters := map[string]ReporterScope{}
	runtimeObservers := map[string]ReporterScope{}
	validators := map[string][]string{}
	for clientID, entry := range parsed.Workloads {
		active[clientID] = entry.Status == "ACTIVE"
		if active[clientID] {
			allowedScopes[clientID] = make(map[string]bool, len(entry.AllowedScopes))
			for _, scope := range entry.AllowedScopes {
				allowedScopes[clientID][scope] = true
			}
		}
		// Both halves are required, as the registry validator requires them:
		// the scope without a registered audience, or an audience without the
		// scope, validates nothing.
		if active[clientID] && slices.Contains(entry.AllowedScopes, ContextValidateScope) && len(entry.ValidatesAudiences) > 0 {
			validators[clientID] = slices.Clone(entry.ValidatesAudiences)
		}
		if active[clientID] && slices.Contains(entry.AllowedScopes, ObserveScope) && entry.Environment != "" && len(entry.DeploymentRegions) > 0 {
			reporters[clientID] = ReporterScope{Environment: entry.Environment, Regions: slices.Clone(entry.DeploymentRegions)}
		}
		if active[clientID] && slices.Contains(entry.AllowedScopes, IdentityRuntimeObserveScope) && entry.Environment != "" && len(entry.DeploymentRegions) > 0 {
			runtimeObservers[clientID] = ReporterScope{Environment: entry.Environment, Regions: slices.Clone(entry.DeploymentRegions)}
		}
	}
	return &StaticWorkloadRegistry{active: active, allowedScopes: allowedScopes, reporters: reporters, runtimeObservers: runtimeObservers, validators: validators}, nil
}
