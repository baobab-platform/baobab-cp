package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// LA-05H: Shared registration is not IAM issuance, not CP activation and not
// a legal-entity mandate. Staging assessors remain PROVISIONED until a
// separate, independently accepted lifecycle promotion.
func TestLA05HStagingAssessorProfilesRemainInert(t *testing.T) {
	path := filepath.Join("..", "contracts", "shared", "identity", "v1", "workload-registry.yaml")
	registry, err := LoadWorkloadRegistryFile(path)
	if err != nil {
		t.Fatalf("load the embedded canonical workload registry: %v", err)
	}
	for _, client := range []string{
		"baobab-trade-legal-actor-assessor-staging",
		"baobab-erp-legal-actor-assessor-staging",
		"baobab-payments-legal-actor-assessor-staging",
		"baobab-trade-docs-legal-actor-assessor-staging",
	} {
		if registry.IsActive(client) {
			t.Errorf("PROVISIONED assessor %s obtained ACTIVE workload authority", client)
		}
		if registry.AllowsScope(client, "legal-actor:assess") {
			t.Errorf("PROVISIONED assessor %s received legal-actor assessment authority", client)
		}
		if registry.AllowsScope(client, "context:resolve") {
			t.Errorf("PROVISIONED assessor %s received runtime context authority", client)
		}
		if registry.AllowsContextPurpose(client, ContextPurposeRuntime) {
			t.Errorf("PROVISIONED assessor %s received runtime context purpose", client)
		}
	}
	// No production engine's pre-existing workload gets the newly registered
	// assessor privilege simply because Shared now has staging profiles.
	for _, id := range []string{
		"baobab-trade-workload",
		"baobab-erp-workload",
		"baobab-pulse-workload",
		"thamani-backend",
		"zuribeans-backend",
	} {
		if registry.AllowsScope(id, "legal-actor:assess") {
			t.Errorf("production workload %s silently gained legal-actor assessment", id)
		}
	}
}

func TestLA05HVendoredWorkloadRegistryMatchesPinnedShared(t *testing.T) {
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("external pinned Shared checkout only present in contract integration CI")
	}
	a := filepath.Join("..", "contracts", "shared", "identity", "v1", "workload-registry.yaml")
	b := filepath.Join(dir, "contracts", "identity", "v1", "workload-registry.yaml")
	embedded, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(embedded) != string(canonical) {
		t.Fatal("runtime workload registration drifted from the pinned Shared authority")
	}
}
