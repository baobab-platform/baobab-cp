package config

import (
	"testing"
	"time"
)

func validConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("ADMIN_OIDC_AUDIENCE", "baobab-control-plane")
	t.Setenv("ADMIN_OIDC_ISSUER", "http://127.0.0.1:5556")
	t.Setenv("WORKLOAD_OIDC_AUDIENCE", "baobab-control-plane")
	t.Setenv("WORKLOAD_OIDC_ISSUER", "http://127.0.0.1:5557")
	// Not production: a production Control Plane also requires
	// WORKLOAD_REGISTRY_FILE.
	t.Setenv("BAOBAB_ENVIRONMENT", "development")
}

func TestLoadDefaultsPlatformContextTTL(t *testing.T) {
	validConfigEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.PlatformContextTTL != 15*time.Minute {
		t.Fatalf("expected a 15-minute default PlatformContextTTL, got %s", cfg.PlatformContextTTL)
	}
}

func TestLoadAppliesCustomPlatformContextTTL(t *testing.T) {
	validConfigEnv(t)
	t.Setenv("PLATFORM_CONTEXT_TTL", "5m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.PlatformContextTTL != 5*time.Minute {
		t.Fatalf("expected the configured 5-minute PlatformContextTTL, got %s", cfg.PlatformContextTTL)
	}
}

func TestLoadRejectsInvalidPlatformContextTTL(t *testing.T) {
	validConfigEnv(t)
	for _, value := range []string{"not-a-duration", "0m", "-5m"} {
		t.Setenv("PLATFORM_CONTEXT_TTL", value)
		if _, err := Load(); err == nil {
			t.Fatalf("expected PLATFORM_CONTEXT_TTL=%q to be rejected", value)
		}
	}
}

func TestLoadRequiresSecureOIDCIssuer(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("ADMIN_OIDC_AUDIENCE", "baobab-control-plane")
	t.Setenv("ADMIN_OIDC_ISSUER", "http://identity.example.com")
	t.Setenv("WORKLOAD_OIDC_AUDIENCE", "baobab-control-plane")
	t.Setenv("WORKLOAD_OIDC_ISSUER", "https://workload-identity.example.com")
	t.Setenv("BAOBAB_ENVIRONMENT", "development")
	if _, err := Load(); err == nil {
		t.Fatal("insecure remote issuer was accepted")
	}
	t.Setenv("ADMIN_OIDC_ISSUER", "http://127.0.0.1:5556")
	if _, err := Load(); err != nil {
		t.Fatalf("local development issuer rejected: %v", err)
	}
	t.Setenv("WORKLOAD_OIDC_ISSUER", "http://workload-identity.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("insecure workload issuer was accepted")
	}
}

// TestProductionRequiresTheWorkloadRegistry: production is every environment
// not named non-production, including unset, and it does not start without
// the canonical workload registry. Outside production the file is optional.
func TestProductionRequiresTheWorkloadRegistry(t *testing.T) {
	for _, environment := range []string{"", "production", "staging", "Production "} {
		validConfigEnv(t)
		t.Setenv("BAOBAB_ENVIRONMENT", environment)
		if _, err := Load(); err == nil {
			t.Fatalf("environment %q started without WORKLOAD_REGISTRY_FILE", environment)
		}
		t.Setenv("WORKLOAD_REGISTRY_FILE", "/etc/baobab/workload-registry.yaml")
		cfg, err := Load()
		if err != nil || !cfg.Production() || cfg.WorkloadRegistryFile != "/etc/baobab/workload-registry.yaml" {
			t.Fatalf("environment %q with a registry file: %+v %v", environment, cfg, err)
		}
		t.Setenv("WORKLOAD_REGISTRY_FILE", "")
	}
	for _, environment := range []string{"development", "test", "integration", "sandbox"} {
		validConfigEnv(t)
		t.Setenv("BAOBAB_ENVIRONMENT", environment)
		cfg, err := Load()
		if err != nil || cfg.Production() {
			t.Fatalf("environment %q must start without a registry and not be production: %+v %v", environment, cfg, err)
		}
	}
}

func TestERPProvisioningIsOffUnlessConfigured(t *testing.T) {
	validConfigEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ERPProvisioningURL != "" {
		t.Fatal("ERP provisioning must be off by default")
	}
}

func TestERPProvisioningConfiguration(t *testing.T) {
	validConfigEnv(t)
	t.Setenv("ERP_PROVISIONING_URL", "https://erp.example.invalid/v1")
	t.Setenv("ERP_PROVISIONER_TOKEN_FILE", "/var/run/secrets/erp-token")
	t.Setenv("ERP_PROVISIONER_ISSUER", "https://iam.example.invalid/realms/baobab")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ERPProvisionerSubject != "baobab-cp-provisioning-workload" || cfg.ERPProvisioningContextTTL != 10*time.Minute {
		t.Fatalf("defaults: %q %s", cfg.ERPProvisionerSubject, cfg.ERPProvisioningContextTTL)
	}
}

func TestERPProvisioningRejectsAnUnsafeOrIncompleteConfiguration(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"plain http":      {"ERP_PROVISIONING_URL": "http://erp.example.invalid/v1"},
		"no token":        {"ERP_PROVISIONER_TOKEN_FILE": ""},
		"no issuer":       {"ERP_PROVISIONER_ISSUER": ""},
		"ttl above 15m":   {"ERP_PROVISIONING_CONTEXT_TTL": "16m"},
		"ttl below 1m":    {"ERP_PROVISIONING_CONTEXT_TTL": "30s"},
		"ttl not a value": {"ERP_PROVISIONING_CONTEXT_TTL": "soon"},
	} {
		validConfigEnv(t)
		t.Setenv("ERP_PROVISIONING_URL", "https://erp.example.invalid/v1")
		t.Setenv("ERP_PROVISIONER_TOKEN_FILE", "/var/run/secrets/erp-token")
		t.Setenv("ERP_PROVISIONER_ISSUER", "https://iam.example.invalid/realms/baobab")
		for k, v := range env {
			t.Setenv(k, v)
		}
		if _, err := Load(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
