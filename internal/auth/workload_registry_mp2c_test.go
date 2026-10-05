package auth

import "testing"

func TestMP2CWorkloadScopeAndRuntimeObserverRegistration(t *testing.T) {
	registry, err := LoadWorkloadRegistryFile(writeTestRegistry(t, `
workloads:
  staging-observer:
    status: ACTIVE
    environment: staging
    allowed_scopes: ["deployment:observe", "identity-runtime:observe"]
    deployment_regions: [af-south-1]
  token-only:
    status: ACTIVE
    environment: staging
    allowed_scopes: ["context:resolve"]
    deployment_regions: [af-south-1]
  suspended-observer:
    status: SUSPENDED
    environment: staging
    allowed_scopes: ["identity-runtime:observe"]
    deployment_regions: [af-south-1]
`))
	if err != nil {
		t.Fatal(err)
	}

	if !registry.AllowsScope("staging-observer", IdentityRuntimeObserveScope) {
		t.Fatal("registered ACTIVE observer lost identity-runtime:observe")
	}
	if registry.AllowsScope("token-only", IdentityRuntimeObserveScope) {
		t.Fatal("scope absent from registry was allowed")
	}
	if registry.AllowsScope("suspended-observer", IdentityRuntimeObserveScope) {
		t.Fatal("suspended workload retained scope authority")
	}
	if registry.AllowsScope("unknown", IdentityRuntimeObserveScope) {
		t.Fatal("unknown workload default-allowed")
	}

	scope, ok := registry.IdentityRuntimeObserver("staging-observer")
	if !ok || !scope.Allows("staging", "af-south-1") {
		t.Fatal("staging observer scope was not derived from its registry entry")
	}
	if scope.Allows("production", "af-south-1") || scope.Allows("staging", "eu-west-1") {
		t.Fatal("runtime observer escaped its registered environment or region")
	}
	for _, client := range []string{"token-only", "suspended-observer", "unknown"} {
		if _, ok := registry.IdentityRuntimeObserver(client); ok {
			t.Fatalf("%s must not be an identity runtime observer", client)
		}
	}

	// Returned regions are defensive copies.
	scope.Regions[0] = "eu-west-1"
	fresh, ok := registry.IdentityRuntimeObserver("staging-observer")
	if !ok || !fresh.Allows("staging", "af-south-1") {
		t.Fatal("caller mutated the registered runtime observer scope")
	}
}
