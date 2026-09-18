package auth

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WorkloadRegistry answers whether a workload client_id is currently
// ACTIVE per nabhold/shared's contracts/identity/v1/workload-registry.yaml
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
// no existing runtime mechanism for consuming nabhold/shared contracts
// live (contracttest's SHARED_CONTRACTS_DIR is a test-only, local-checkout
// pattern; see internal/contracttest's own doc comment), and inventing one
// here -- polling cadence, staleness policy, fail-open-vs-fail-closed on a
// fetch error -- is a real design decision this slice deliberately does
// not make unilaterally. An operator who wants this enforced in production
// supplies a local snapshot file (LoadWorkloadRegistryFile); nil (the
// default -- see api.Dependencies.WorkloadRegistry) disables the check
// entirely, preserving exactly today's behavior until that snapshot exists.
type StaticWorkloadRegistry struct {
	active map[string]bool
}

func (r *StaticWorkloadRegistry) IsActive(clientID string) bool {
	return r.active[clientID]
}

// workloadRegistryFile mirrors the subset of nabhold/shared's
// contracts/identity/v1/workload-registry.yaml this repository actually
// needs (client_id -> status); every other field in that contract
// (repository, owner, runtime, environment, allowed_audiences,
// allowed_scopes, credential_type, rotation_owner) is baobab-iam's and
// nabhold/shared's own concern, not re-modelled here.
type workloadRegistryFile struct {
	Workloads map[string]struct {
		Status string `yaml:"status"`
	} `yaml:"workloads"`
}

// LoadWorkloadRegistryFile parses a local snapshot of nabhold/shared's
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
	active := make(map[string]bool, len(parsed.Workloads))
	for clientID, entry := range parsed.Workloads {
		active[clientID] = entry.Status == "ACTIVE"
	}
	return &StaticWorkloadRegistry{active: active}, nil
}
