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
