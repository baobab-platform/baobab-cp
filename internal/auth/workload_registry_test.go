package auth

import (
	"os"
	"path/filepath"
	"testing"
)

const testRegistryYAML = `
workloads:
  baobab-trade-workload:
    status: ACTIVE
  baobab-erp-workload:
    status: REVOKED
  thamani-backend:
    status: SUSPENDED
`

func writeTestRegistry(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "workload-registry.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test registry: %v", err)
	}
	return path
}

func TestLoadWorkloadRegistryFileParsesStatuses(t *testing.T) {
	registry, err := LoadWorkloadRegistryFile(writeTestRegistry(t, testRegistryYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !registry.IsActive("baobab-trade-workload") {
		t.Fatal("expected baobab-trade-workload to be active")
	}
	if registry.IsActive("baobab-erp-workload") {
		t.Fatal("expected baobab-erp-workload (REVOKED) to be inactive")
	}
	if registry.IsActive("thamani-backend") {
		t.Fatal("expected thamani-backend (SUSPENDED) to be inactive")
	}
}

func TestWorkloadRegistryUnknownClientIsNotActive(t *testing.T) {
	registry, err := LoadWorkloadRegistryFile(writeTestRegistry(t, testRegistryYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An unregistered client_id must never default-allow (ADR-0007 §44).
	if registry.IsActive("never-registered-workload") {
		t.Fatal("expected an unregistered client_id to be inactive")
	}
}

func TestLoadWorkloadRegistryFileMissingFile(t *testing.T) {
	if _, err := LoadWorkloadRegistryFile(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestLoadWorkloadRegistryFileMalformedYAML(t *testing.T) {
	if _, err := LoadWorkloadRegistryFile(writeTestRegistry(t, "not: [valid yaml")); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

// TestWorkloadRegistryReporters: a workload is a reporter (ADR-BCP-025
// section 2.9) only when it is ACTIVE, may hold deployment:observe and lists
// regions; it may report only for its environment and those regions.
func TestWorkloadRegistryReporters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte(`
workloads:
  controller:
    environment: staging
    allowed_scopes: ["deployment:observe"]
    deployment_regions: [af-south-1, eu-west-1]
    status: ACTIVE
  suspended:
    environment: staging
    allowed_scopes: ["deployment:observe"]
    deployment_regions: [af-south-1]
    status: SUSPENDED
  no-regions:
    environment: staging
    allowed_scopes: ["deployment:observe"]
    status: ACTIVE
  no-scope:
    environment: staging
    allowed_scopes: ["context:resolve"]
    deployment_regions: [af-south-1]
    status: ACTIVE
`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := LoadWorkloadRegistryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"suspended", "no-regions", "no-scope", "unknown"} {
		if _, ok := registry.Reporter(client); ok {
			t.Fatalf("%s must not be a reporter", client)
		}
	}
	scope, ok := registry.Reporter("controller")
	if !ok {
		t.Fatal("controller must be a reporter")
	}
	for _, c := range []struct {
		environment, region string
		want                bool
	}{
		{"staging", "af-south-1", true}, {"staging", "eu-west-1", true},
		{"staging", "us-east-1", false}, {"production", "af-south-1", false}, {"", "af-south-1", false},
	} {
		if got := scope.Allows(c.environment, c.region); got != c.want {
			t.Fatalf("Allows(%q, %q) = %v, want %v", c.environment, c.region, got, c.want)
		}
	}
}
