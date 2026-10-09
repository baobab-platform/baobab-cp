package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/workloadtoken"
)

type Config struct {
	HTTPAddress       string
	DatabaseURL       string
	AdminOIDCIssuer   string
	AdminOIDCAudience string
	// AdminOIDCClientID is the IAM workforce client whose resource_access
	// roles back scope entitlements such as onboarding-requester.
	AdminOIDCClientID    string
	WorkloadOIDCIssuer   string
	WorkloadOIDCAudience string
	// PlatformContextTTL bounds how long a Context persisted by
	// POST /v1/platform-context/resolve remains redeemable (ADR-BCP-004
	// §72). Unlike resolver.ResolutionEvidence.TTL's zero-means-unbounded
	// default (safe there because nothing calls it in production), this
	// value backs a route wired live into cmd/controlplane/main.go, so
	// Load defaults it to a bounded value rather than leaving rows to
	// accumulate forever when the operator sets nothing.
	PlatformContextTTL time.Duration
	// Environment names the deployment (BAOBAB_ENVIRONMENT). Anything other
	// than development, test, integration or sandbox -- including unset --
	// is production for engine registration: providers not permitted in
	// production are refused (ADR-SHARED-011).
	Environment string
	// Exact registered CP runtime instance for private federation evidence.
	// Empty leaves the source fail-closed; never infer an instance from tenancy.
	FederationSourceEngineInstanceID string
	// BillingEngineURL, when set, enables the billing projection of
	// classified ProductSubscriptions into baobab-subscriptions (ADR-BCP-018
	// gate ORG-11). BillingWorkloadTokenFile is the platform-projected
	// workload token for the engine's audience; there is no static secret.
	BillingEngineURL         string
	BillingWorkloadTokenFile string
	BillingSyncInterval      time.Duration
	// ERPProvisioningURL, when set, enables provider provisioning of ERP from
	// the APPLY phase (Shared erp/v1 requestErpProvisioning), called as the
	// dedicated provisioner workload. ERPProvisionerTokenFile is its
	// platform-projected token for audience baobab-erp (no static secret);
	// ERPProvisionerIssuer and ERPProvisionerSubject are that token's iss and
	// sub, which name the canonical principal that owns the provisioning
	// Context. Unset, the pipeline is unchanged.
	ERPProvisioningURL      string
	ERPProvisionerTokenFile string
	// ERPProvisionerAssertionFile (ERP_PROVISIONER_ASSERTION_FILE) selects the federated mode: the platform-projected assertion that
	// is exchanged at ERPProvisionerTokenURL (ERP_PROVISIONER_TOKEN_URL, the provider's public token endpoint) as the public client
	// ERPProvisionerClientID (ERP_PROVISIONER_CLIENT_ID) for ERPProvisionerScope (ERP_PROVISIONER_SCOPE, default erp:provision), with
	// the optional ERPProvisionerAudience (ERP_PROVISIONER_AUDIENCE). Exactly one of this and ERPProvisionerTokenFile is set.
	ERPProvisionerAssertionFile string
	ERPProvisionerTokenURL      string
	ERPProvisionerClientID      string
	ERPProvisionerScope         string
	ERPProvisionerAudience      string
	ERPProvisionerIssuer        string
	ERPProvisionerSubject       string
	ERPProvisioningContextTTL   time.Duration
	// EventDeliveryKeysFile (EVENT_DELIVERY_KEYS_FILE) is the registry of delivery keys for signed engine event delivery (Shared
	// control-plane/v1 receiveEngineEvent): a JSON array of {key_id, sender, secret_b64, revoked}. Unset, the route is not served.
	// It is a mounted secret, never a value in the environment, and is re-read when it changes so keys rotate without a restart.
	// EventProcessingInterval is how often recorded events are applied (they are also applied as soon as one is recorded).
	EventDeliveryKeysFile   string
	EventProcessingInterval time.Duration
	// ERPRecoveryInterval (ERP_RECOVERY_INTERVAL, default 1m) is how often the recovery sweep looks for open ERP operations that have
	// gone quiet; ERPRecoveryGrace (ERP_RECOVERY_GRACE, default 5m) is how long an open operation may be quiet before it is read.
	ERPRecoveryInterval time.Duration
	ERPRecoveryGrace    time.Duration
	// GroupDerivationInterval is how often due CorporateGroup derivations
	// are processed; GroupReconciliationInterval how often every derivable
	// group is re-derived to repair drift (ADR-BCP-018 gate ORG-05).
	GroupDerivationInterval     time.Duration
	GroupReconciliationInterval time.Duration
	// TenantBootstrapRegistration enables the migration-only bootstrap
	// registration route (ADR-BCP-017 sections 22-24). Off by default.
	TenantBootstrapRegistration bool
	// LA-04C privileged, nonactivating mandate proposal/review only.
	// This must never override PostgreSQL's ACTIVE activation guard.
	LegalActorMandateCommandsEnabled bool
	// LA-04D allows only explicitly enabled nonproduction lifecycle operations.
	LegalActorMandateLifecycleEnabled bool
	LegalActorAssessmentEnabled bool
	// EnforcementRollback (ADMINISTRATIVE_ENFORCEMENT_ROLLBACK, comma-
	// separated permission keys, or "*") returns permissions to the role
	// decision at once, without a release. It can only return authority to
	// roles; the enforcement policy in Shared is the only way to enforce.
	EnforcementRollback []string
	// WorkloadRegistryFile is a local snapshot of Shared's
	// contracts/identity/v1/workload-registry.yaml (WORKLOAD_REGISTRY_FILE).
	// Shared owns workload identity and lifecycle, so a production Control
	// Plane requires it and starts only with a valid one: a workload that is
	// not ACTIVE has no runtime authority, and an absent registry never
	// means "everyone is ACTIVE". Outside production it is optional and,
	// when absent, workload lifecycle is not enforced (docs/adr ADR-0007 §45).
	WorkloadRegistryFile string
}

// nonProductionEnvironments are the environments that are not production;
// anything else, including unset, is production (see Environment).
var nonProductionEnvironments = []string{"development", "test", "integration", "sandbox"}

// Production reports whether the deployment is production: any environment
// not named as non-production, including unset.
func (c Config) Production() bool {
	for _, e := range nonProductionEnvironments {
		if c.Environment == e {
			return false
		}
	}
	return true
}

func Load() (Config, error) {
	c := Config{
		HTTPAddress:          env("HTTP_ADDRESS", ":8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		AdminOIDCIssuer:      os.Getenv("ADMIN_OIDC_ISSUER"),
		AdminOIDCAudience:    env("ADMIN_OIDC_AUDIENCE", "baobab-control-plane"),
		AdminOIDCClientID:    env("ADMIN_OIDC_CLIENT_ID", "baobab-control-plane-admin"),
		WorkloadOIDCIssuer:   os.Getenv("WORKLOAD_OIDC_ISSUER"),
		WorkloadOIDCAudience: env("WORKLOAD_OIDC_AUDIENCE", "baobab-control-plane"),
	}
	if c.DatabaseURL == "" || c.AdminOIDCIssuer == "" || c.AdminOIDCAudience == "" || c.WorkloadOIDCIssuer == "" || c.WorkloadOIDCAudience == "" {
		return Config{}, errors.New("DATABASE_URL, ADMIN_OIDC_ISSUER, ADMIN_OIDC_AUDIENCE, WORKLOAD_OIDC_ISSUER and WORKLOAD_OIDC_AUDIENCE are required")
	}
	ttl, err := time.ParseDuration(env("PLATFORM_CONTEXT_TTL", "15m"))
	if err != nil || ttl <= 0 {
		return Config{}, errors.New("PLATFORM_CONTEXT_TTL must be a positive Go duration (e.g. \"15m\")")
	}
	c.PlatformContextTTL = ttl
	c.Environment = strings.ToLower(strings.TrimSpace(os.Getenv("BAOBAB_ENVIRONMENT")))
	c.LegalActorMandateCommandsEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv("LEGAL_ACTOR_MANDATE_COMMANDS_ENABLED")), "true")
	c.LegalActorMandateLifecycleEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv("LEGAL_ACTOR_MANDATE_LIFECYCLE_ENABLED")), "true")
	c.LegalActorAssessmentEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv("LEGAL_ACTOR_ASSESSMENT_ENABLED")), "true")
	c.FederationSourceEngineInstanceID = os.Getenv("FEDERATION_SOURCE_ENGINE_INSTANCE_ID")
	c.WorkloadRegistryFile = strings.TrimSpace(os.Getenv("WORKLOAD_REGISTRY_FILE"))
	if c.FederationSourceEngineInstanceID != "" {
		if !domain.ValidEngineInstanceID(c.FederationSourceEngineInstanceID) || c.WorkloadOIDCAudience != "baobab-control-plane" || c.WorkloadRegistryFile == "" {
			return Config{}, errors.New("federation source requires a canonical CP engine instance, baobab-control-plane workload audience and WORKLOAD_REGISTRY_FILE")
		}
		switch c.Environment {
		case "local", "development", "staging", "production":
		default:
			return Config{}, errors.New("federation source requires an explicit canonical reference environment")
		}
	}
	for _, key := range strings.Split(os.Getenv("ADMINISTRATIVE_ENFORCEMENT_ROLLBACK"), ",") {
		if key = strings.TrimSpace(key); key != "" {
			c.EnforcementRollback = append(c.EnforcementRollback, key)
		}
	}
	if c.Production() && c.WorkloadRegistryFile == "" {
		return Config{}, errors.New("WORKLOAD_REGISTRY_FILE is required in production: the Control Plane enforces the canonical workload registry and fails closed without it")
	}
	if raw := strings.TrimSpace(os.Getenv("BILLING_ENGINE_URL")); raw != "" {
		engine, err := url.Parse(raw)
		if err != nil || engine.Host == "" || (engine.Scheme != "https" && !localIssuer(engine)) {
			return Config{}, errors.New("BILLING_ENGINE_URL must use HTTPS (HTTP is allowed only for localhost development)")
		}
		c.BillingEngineURL = raw
		c.BillingWorkloadTokenFile = os.Getenv("BILLING_WORKLOAD_TOKEN_FILE")
		if c.BillingWorkloadTokenFile == "" {
			return Config{}, errors.New("BILLING_WORKLOAD_TOKEN_FILE is required when BILLING_ENGINE_URL is set")
		}
		interval, err := time.ParseDuration(env("BILLING_SYNC_INTERVAL", "30s"))
		if err != nil || interval < time.Second {
			return Config{}, errors.New("BILLING_SYNC_INTERVAL must be a Go duration of at least 1s")
		}
		c.BillingSyncInterval = interval
	}
	if raw := strings.TrimSpace(os.Getenv("ERP_PROVISIONING_URL")); raw != "" {
		engine, err := url.Parse(raw)
		if err != nil || engine.Host == "" || (engine.Scheme != "https" && !localIssuer(engine)) {
			return Config{}, errors.New("ERP_PROVISIONING_URL must use HTTPS (HTTP is allowed only for localhost development)")
		}
		c.ERPProvisioningURL = raw
		c.ERPProvisionerTokenFile = os.Getenv("ERP_PROVISIONER_TOKEN_FILE")
		c.ERPProvisionerAssertionFile = os.Getenv("ERP_PROVISIONER_ASSERTION_FILE")
		c.ERPProvisionerTokenURL = strings.TrimSpace(os.Getenv("ERP_PROVISIONER_TOKEN_URL"))
		c.ERPProvisionerClientID = strings.TrimSpace(os.Getenv("ERP_PROVISIONER_CLIENT_ID"))
		c.ERPProvisionerScope = env("ERP_PROVISIONER_SCOPE", "erp:provision")
		c.ERPProvisionerAudience = strings.TrimSpace(os.Getenv("ERP_PROVISIONER_AUDIENCE"))
		c.ERPProvisionerIssuer = strings.TrimSpace(os.Getenv("ERP_PROVISIONER_ISSUER"))
		// Under the federated exchange the access token's subject is the assertion's subject, not the logical client id, so it has
		// no safe default there: it must be set to the exact subject the provider's trust grant binds.
		defaultSubject := "baobab-cp-provisioning-workload"
		if c.ERPProvisionerAssertionFile != "" {
			defaultSubject = ""
		}
		c.ERPProvisionerSubject = env("ERP_PROVISIONER_SUBJECT", defaultSubject)
		if (c.ERPProvisionerTokenFile == "") == (c.ERPProvisionerAssertionFile == "") {
			return Config{}, errors.New("exactly one of ERP_PROVISIONER_TOKEN_FILE and ERP_PROVISIONER_ASSERTION_FILE is required when ERP_PROVISIONING_URL is set")
		}
		if c.ERPProvisionerIssuer == "" {
			return Config{}, errors.New("ERP_PROVISIONER_ISSUER is required when ERP_PROVISIONING_URL is set")
		}
		if c.ERPProvisionerAssertionFile != "" {
			if c.ERPProvisionerTokenURL == "" || c.ERPProvisionerClientID == "" || c.ERPProvisionerSubject == "" {
				return Config{}, errors.New("ERP_PROVISIONER_TOKEN_URL, ERP_PROVISIONER_CLIENT_ID and ERP_PROVISIONER_SUBJECT are required with ERP_PROVISIONER_ASSERTION_FILE")
			}
			exchange := &workloadtoken.JWTBearer{TokenURL: c.ERPProvisionerTokenURL, ClientID: c.ERPProvisionerClientID,
				AssertionFile: c.ERPProvisionerAssertionFile, Scope: strings.Fields(c.ERPProvisionerScope)}
			if err := exchange.Validate(); err != nil {
				return Config{}, fmt.Errorf("ERP provisioner token exchange: %w", err)
			}
		} else if c.ERPProvisionerTokenURL != "" || c.ERPProvisionerClientID != "" || c.ERPProvisionerAudience != "" {
			return Config{}, errors.New("ERP_PROVISIONER_TOKEN_URL, ERP_PROVISIONER_CLIENT_ID and ERP_PROVISIONER_AUDIENCE apply only with ERP_PROVISIONER_ASSERTION_FILE")
		}
		// A provisioning Context lives at most 15 minutes (Shared
		// control-plane/v1 1.34.0); the default leaves room for a slow ERP.
		ttl, err := time.ParseDuration(env("ERP_PROVISIONING_CONTEXT_TTL", "10m"))
		if err != nil || ttl < time.Minute || ttl > 15*time.Minute {
			return Config{}, errors.New("ERP_PROVISIONING_CONTEXT_TTL must be a Go duration between 1m and 15m")
		}
		c.ERPProvisioningContextTTL = ttl
	}
	if raw := strings.TrimSpace(os.Getenv("EVENT_DELIVERY_KEYS_FILE")); raw != "" {
		c.EventDeliveryKeysFile = raw
	}
	if c.EventProcessingInterval, err = time.ParseDuration(env("EVENT_PROCESSING_INTERVAL", "10s")); err != nil || c.EventProcessingInterval < time.Second {
		return Config{}, errors.New("EVENT_PROCESSING_INTERVAL must be a Go duration of at least 1s")
	}
	if c.ERPRecoveryInterval, err = time.ParseDuration(env("ERP_RECOVERY_INTERVAL", "1m")); err != nil || c.ERPRecoveryInterval < 10*time.Second {
		return Config{}, errors.New("ERP_RECOVERY_INTERVAL must be a Go duration of at least 10s")
	}
	if c.ERPRecoveryGrace, err = time.ParseDuration(env("ERP_RECOVERY_GRACE", "5m")); err != nil || c.ERPRecoveryGrace < time.Minute {
		return Config{}, errors.New("ERP_RECOVERY_GRACE must be a Go duration of at least 1m")
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TENANT_BOOTSTRAP_REGISTRATION"))) {
	case "", "disabled":
	case "enabled":
		c.TenantBootstrapRegistration = true
	default:
		return Config{}, errors.New("TENANT_BOOTSTRAP_REGISTRATION must be \"enabled\" or \"disabled\"")
	}
	if c.GroupDerivationInterval, err = time.ParseDuration(env("GROUP_DERIVATION_INTERVAL", "30s")); err != nil || c.GroupDerivationInterval < time.Second {
		return Config{}, errors.New("GROUP_DERIVATION_INTERVAL must be a Go duration of at least 1s")
	}
	if c.GroupReconciliationInterval, err = time.ParseDuration(env("GROUP_RECONCILIATION_INTERVAL", "1h")); err != nil || c.GroupReconciliationInterval < time.Minute {
		return Config{}, errors.New("GROUP_RECONCILIATION_INTERVAL must be a Go duration of at least 1m")
	}
	for name, rawIssuer := range map[string]string{"ADMIN_OIDC_ISSUER": c.AdminOIDCIssuer, "WORKLOAD_OIDC_ISSUER": c.WorkloadOIDCIssuer} {
		issuer, err := url.Parse(rawIssuer)
		if err != nil || issuer.Host == "" || (issuer.Scheme != "https" && !localIssuer(issuer)) {
			return Config{}, fmt.Errorf("%s must use HTTPS (HTTP is allowed only for localhost development)", name)
		}
	}
	return c, nil
}

func localIssuer(issuer *url.URL) bool {
	host := strings.ToLower(issuer.Hostname())
	return issuer.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
