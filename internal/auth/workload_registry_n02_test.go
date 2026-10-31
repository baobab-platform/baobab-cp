package auth

import (
	"path/filepath"
	"testing"
)

// IAM-N02 is a DEVELOPMENT-ONLY candidate projection of the reviewed Shared
// workload entry. Registration is not activation, issuer credentials, CP
// Principal creation or purchasing authority. The default CP source pin
// remains independently review-gated while Shared #421 is a draft.
func TestN02ProposedEstatePrincipalReaderCannotCreateAuthority(t *testing.T) {
	path := filepath.Join("..", "contracts", "candidates", "n02", "identity", "v1", "workload-registry.yaml")
	registry, err := LoadWorkloadRegistryFile(path)
	if err != nil {
		t.Fatal("cannot parse candidate Shared workload source", err)
	}
	const candidate = "baobab-dev-zuribeans-cp-principal-validator"
	if registry.IsActive(candidate) {
		t.Fatal("PROVISIONED estate CP reader became ACTIVE")
	}
	if registry.AllowsScope(candidate, PrincipalResolveScope) ||
		registry.AllowsScope(candidate, PrincipalRegisterBuyerScope) ||
		registry.AllowsScope(candidate, ContextValidateScope) {
		t.Fatal("unprovisioned estate caller gained CP workload scope")
	}
	if registry.AllowsEnvironment(candidate, "development") ||
		registry.AllowsEnvironment(candidate, "staging") ||
		registry.AllowsEnvironment(candidate, "production") {
		t.Fatal("PROVISIONED caller passed workload/environment admission")
	}
	if audience := registry.SubjectPrincipalAudiences(candidate); len(audience) != 0 {
		t.Fatalf("PROVISIONED caller gained human subject delegation: %v", audience)
	}
	// The independently owned Next commerce BFF is Trade-audience only and
	// cannot inherit the new CP reader's identity:principal:resolve scope.
	const existing = "baobab-dev-zuribeans-bff"
	if !registry.IsActive(existing) ||
		registry.AllowsScope(existing, PrincipalResolveScope) ||
		registry.AllowsScope(existing, PrincipalRegisterBuyerScope) ||
		len(registry.SubjectPrincipalAudiences(existing)) != 0 {
		t.Fatal("existing Next commerce BFF inherited new native CP privilege")
	}
	// N02's actual authorization-code browser app is not a WORKLOAD entry,
	// and cannot use this registry as a substitute for real Hydra registration.
	const browser = "baobab-dev-zuribeans-native-browser-bff"
	if registry.IsActive(browser) || registry.AllowsScope(browser, PrincipalResolveScope) {
		t.Fatal("human OAuth browser client was treated as an ACTIVE CP workload")
	}
}

func TestN02CandidateNotAnAcceptedRuntimeRegistry(t *testing.T) {
    // Until CP contracts.lock and every owning CP contract consumer are
    // independently reviewed, runtime loads only the already pinned Shared
    // registry. The proposed synthetic Principal reader cannot be admitted
    // merely by placing its candidate YAML next to the accepted source.
    accepted := filepath.Join("..", "contracts", "shared", "identity", "v1", "workload-registry.yaml")
    runtime, err := LoadWorkloadRegistryFile(accepted)
    if err != nil { t.Fatal(err) }
    for _, name := range []string{
        "baobab-dev-zuribeans-cp-principal-validator",
        "baobab-dev-zuribeans-native-browser-bff",
    } {
        if runtime.IsActive(name) || runtime.AllowsScope(name, PrincipalResolveScope) ||
           len(runtime.SubjectPrincipalAudiences(name)) != 0 {
            t.Fatalf("draft N02 client %s leaked into accepted CP runtime", name)
        }
    }
}
