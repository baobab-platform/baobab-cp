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

// TestWorkloadRegistryLoaderFailsClosed: a snapshot that is empty, not YAML,
// missing, or names a status outside the lifecycle is refused at startup,
// never loaded as "nobody is ACTIVE" or "nothing is enforced".
func TestWorkloadRegistryLoaderFailsClosed(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, path := range map[string]string{
		"missing":        filepath.Join(dir, "absent.yaml"),
		"not yaml":       write("garbage.yaml", "workloads: [unterminated"),
		"empty":          write("empty.yaml", "workloads: {}\n"),
		"no section":     write("none.yaml", "schema: {name: x}\n"),
		"unknown status": write("status.yaml", "workloads:\n  a:\n    status: ENABLED\n"),
		"no status":      write("nostatus.yaml", "workloads:\n  a:\n    environment: production\n"),
	} {
		if registry, err := LoadWorkloadRegistryFile(path); err == nil {
			t.Fatalf("%s: loaded %+v", name, registry)
		}
	}
}

// TestSharedWorkloadRegistryLifecycle loads the canonical registry from the
// Shared checkout under test and checks what production would enforce: the
// six ACTIVE entries are active, the two deliberately PROVISIONED identities
// and the new production reporter are not, and loading the registry promotes
// nothing. Skipped without SHARED_CONTRACTS_DIR.
func TestSharedWorkloadRegistryLifecycle(t *testing.T) {
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("SHARED_CONTRACTS_DIR not set; skipping baobab-platform/shared contract-compatibility test")
	}
	registry, err := LoadWorkloadRegistryFile(filepath.Join(dir, "contracts", "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"baobab-cms-workload", "baobab-erp-workload", "baobab-pulse-workload", "baobab-trade-workload", "thamani-backend", "zuribeans-backend"} {
		if !registry.IsActive(id) {
			t.Errorf("%s is ACTIVE in Shared and must be accepted", id)
		}
	}
	for _, id := range []string{"baobab-cp-workload", "baobab-subscriptions-workload", "unknown-workload"} {
		if registry.IsActive(id) {
			t.Errorf("%s must have no runtime authority", id)
		}
	}
	// The production reporter, if registered, is not a reporter until ACTIVE.
	if _, ok := registry.Reporter("baobab-deployment-controller-production"); ok && !registry.IsActive("baobab-deployment-controller-production") {
		t.Error("a non-ACTIVE deployment controller must not be a reporter")
	}
}

// TestSharedPulseValidatorAuthority proves the canonical P-CAP-07 registry
// makes Pulse a validator only for callers addressed to the baobab-pulse
// resource-server audience. The validator credential itself is still a
// tenant-neutral baobab-control-plane workload token.
func TestSharedPulseValidatorAuthority(t *testing.T) {
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		t.Skip("SHARED_CONTRACTS_DIR not set; skipping baobab-platform/shared contract-compatibility test")
	}
	registry, err := LoadWorkloadRegistryFile(filepath.Join(dir, "contracts", "identity", "v1", "workload-registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	const clientID = "baobab-pulse-workload"
	if !registry.IsActive(clientID) {
		t.Fatal("Pulse validator workload must be ACTIVE")
	}
	if !registry.AllowsScope(clientID, ContextValidateScope) {
		t.Fatal("Pulse validator workload must be allowed context:validate")
	}
	got := registry.ValidatesAudiences(clientID)
	if len(got) != 1 || got[0] != "baobab-pulse" {
		t.Fatalf("Pulse validator audiences = %v, want [baobab-pulse]", got)
	}
	for _, forbidden := range []string{"baobab-erp", "baobab-control-plane", "baobab-trade"} {
		for _, audience := range got {
			if audience == forbidden {
				t.Fatalf("Pulse validator must not validate %q", forbidden)
			}
		}
	}
}

// A validator needs all three: ACTIVE, the context:validate scope, and a
// registered audience. Any one missing validates nothing.
func TestValidatesAudiencesRequiresStatusScopeAndAudience(t *testing.T) {
	registry, err := LoadWorkloadRegistryFile(writeTestRegistry(t, `
workloads:
  erp-ok:
    status: ACTIVE
    allowed_scopes: ["context:validate", "context:resolve"]
    validates_audiences: ["baobab-erp", "baobab-erp-batch"]
  erp-no-scope:
    status: ACTIVE
    allowed_scopes: ["context:resolve"]
    validates_audiences: ["baobab-erp"]
  erp-no-audience:
    status: ACTIVE
    allowed_scopes: ["context:validate"]
  erp-suspended:
    status: SUSPENDED
    allowed_scopes: ["context:validate"]
    validates_audiences: ["baobab-erp"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.ValidatesAudiences("erp-ok"); len(got) != 2 || got[0] != "baobab-erp" || got[1] != "baobab-erp-batch" {
		t.Fatalf("erp-ok: %v", got)
	}
	for _, client := range []string{"erp-no-scope", "erp-no-audience", "erp-suspended", "unknown-client", ""} {
		if got := registry.ValidatesAudiences(client); len(got) != 0 {
			t.Fatalf("%q must validate nothing, got %v", client, got)
		}
	}
	// The caller cannot widen the registered list through the returned slice.
	registry.ValidatesAudiences("erp-ok")[0] = "baobab-control-plane"
	if registry.ValidatesAudiences("erp-ok")[0] != "baobab-erp" {
		t.Fatal("the registered audiences were mutated through a returned slice")
	}
	var nilRegistry *StaticWorkloadRegistry
	if got := nilRegistry.ValidatesAudiences("erp-ok"); got != nil {
		t.Fatalf("nil registry: %v", got)
	}
}

// A shared API audience does not force independently revocable components
// to share one workload identity (ADR-0007).
func TestValidatorsCanShareAnExplicitlyRegisteredAudience(t *testing.T) {
	registry, err := LoadWorkloadRegistryFile(writeTestRegistry(t, `
workloads:
  erp-api:
    status: ACTIVE
    allowed_scopes: ["context:validate"]
    validates_audiences: ["baobab-erp"]
  erp-worker:
    status: ACTIVE
    allowed_scopes: ["context:validate"]
    validates_audiences: ["baobab-erp"]
  erp-revoked:
    status: REVOKED
    allowed_scopes: ["context:validate"]
    validates_audiences: ["baobab-erp"]
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"erp-api", "erp-worker"} {
		if got := registry.ValidatesAudiences(client); len(got) != 1 || got[0] != "baobab-erp" {
			t.Fatalf("%s: %v", client, got)
		}
	}
	if got := registry.ValidatesAudiences("erp-revoked"); len(got) != 0 {
		t.Fatalf("revoked validator: %v", got)
	}
}
