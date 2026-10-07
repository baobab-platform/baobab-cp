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

func TestERPRecoverySettings(t *testing.T) {
	validConfigEnv(t)
	cfg, err := Load()
	if err != nil || cfg.ERPRecoveryInterval != time.Minute || cfg.ERPRecoveryGrace != 5*time.Minute {
		t.Fatalf("defaults: %v %s %s", err, cfg.ERPRecoveryInterval, cfg.ERPRecoveryGrace)
	}
	t.Setenv("ERP_RECOVERY_INTERVAL", "30s")
	t.Setenv("ERP_RECOVERY_GRACE", "10m")
	if cfg, err = Load(); err != nil || cfg.ERPRecoveryInterval != 30*time.Second || cfg.ERPRecoveryGrace != 10*time.Minute {
		t.Fatalf("configured: %v %s %s", err, cfg.ERPRecoveryInterval, cfg.ERPRecoveryGrace)
	}
	for name, value := range map[string]string{"ERP_RECOVERY_INTERVAL": "1s", "ERP_RECOVERY_GRACE": "10s"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%s must be rejected", name, value)
			}
		})
	}
}

func TestERPProvisionerTokenModes(t *testing.T) {
	base := func(t *testing.T) {
		validConfigEnv(t)
		t.Setenv("ERP_PROVISIONING_URL", "https://erp.staging.example/v1")
		t.Setenv("ERP_PROVISIONER_ISSUER", "https://issuer.staging.example")
	}
	federated := func(t *testing.T) {
		base(t)
		t.Setenv("ERP_PROVISIONER_ASSERTION_FILE", "/run/projected/assertion")
		t.Setenv("ERP_PROVISIONER_TOKEN_URL", "https://issuer.staging.example/oauth2/token")
		t.Setenv("ERP_PROVISIONER_CLIENT_ID", "baobab-cp-provisioning-evidence-workload")
		t.Setenv("ERP_PROVISIONER_SUBJECT", "provisioner-evidence")
	}

	t.Run("a ready token file keeps working and keeps its default subject", func(t *testing.T) {
		base(t)
		t.Setenv("ERP_PROVISIONER_TOKEN_FILE", "/run/token")
		cfg, err := Load()
		if err != nil || cfg.ERPProvisionerAssertionFile != "" || cfg.ERPProvisionerSubject != "baobab-cp-provisioning-workload" {
			t.Fatalf("%v %+v", err, cfg)
		}
	})
	t.Run("the federated exchange is configured completely", func(t *testing.T) {
		federated(t)
		cfg, err := Load()
		if err != nil || cfg.ERPProvisionerScope != "erp:provision" || cfg.ERPProvisionerSubject != "provisioner-evidence" || cfg.ERPProvisionerTokenFile != "" {
			t.Fatalf("%v %+v", err, cfg)
		}
	})
	t.Run("exactly one mode", func(t *testing.T) {
		base(t) // neither
		if _, err := Load(); err == nil {
			t.Fatal("neither mode accepted")
		}
		federated(t)
		t.Setenv("ERP_PROVISIONER_TOKEN_FILE", "/run/token") // both
		if _, err := Load(); err == nil {
			t.Fatal("both modes accepted")
		}
	})
	t.Run("the federated subject has no default: it must be the assertion subject, not the logical client id", func(t *testing.T) {
		federated(t)
		t.Setenv("ERP_PROVISIONER_SUBJECT", "")
		if _, err := Load(); err == nil {
			t.Fatal("federated mode accepted without an explicit subject")
		}
	})
	t.Run("an unsafe or incomplete exchange is refused at start", func(t *testing.T) {
		for name, change := range map[string]func(*testing.T){
			"plain-http": func(t *testing.T) {
				t.Setenv("ERP_PROVISIONER_TOKEN_URL", "http://issuer.staging.example/oauth2/token")
			},
			"no-token-url": func(t *testing.T) { t.Setenv("ERP_PROVISIONER_TOKEN_URL", "") },
			"no-client":    func(t *testing.T) { t.Setenv("ERP_PROVISIONER_CLIENT_ID", "") },
			"wildcard":     func(t *testing.T) { t.Setenv("ERP_PROVISIONER_SCOPE", "erp:*") },
			"no-issuer":    func(t *testing.T) { t.Setenv("ERP_PROVISIONER_ISSUER", "") },
		} {
			t.Run(name, func(t *testing.T) {
				federated(t)
				change(t)
				if _, err := Load(); err == nil {
					t.Fatal("accepted")
				}
			})
		}
	})
	t.Run("exchange settings do not apply to a token-file deployment", func(t *testing.T) {
		base(t)
		t.Setenv("ERP_PROVISIONER_TOKEN_FILE", "/run/token")
		t.Setenv("ERP_PROVISIONER_TOKEN_URL", "https://issuer.staging.example/oauth2/token")
		if _, err := Load(); err == nil {
			t.Fatal("stray exchange settings accepted")
		}
	})
	t.Run("loopback http is allowed for local development", func(t *testing.T) {
		federated(t)
		t.Setenv("ERP_PROVISIONER_TOKEN_URL", "http://127.0.0.1:4444/oauth2/token")
		if _, err := Load(); err != nil {
			t.Fatal(err)
		}
	})
}
