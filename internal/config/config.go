package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddress          string
	DatabaseURL          string
	AdminOIDCIssuer      string
	AdminOIDCAudience    string
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
	// BillingEngineURL, when set, enables the billing projection of
	// classified ProductSubscriptions into baobab-subscriptions (ADR-BCP-018
	// gate ORG-11). BillingWorkloadTokenFile is the platform-projected
	// workload token for the engine's audience; there is no static secret.
	BillingEngineURL         string
	BillingWorkloadTokenFile string
	BillingSyncInterval      time.Duration
}

func Load() (Config, error) {
	c := Config{
		HTTPAddress:          env("HTTP_ADDRESS", ":8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		AdminOIDCIssuer:      os.Getenv("ADMIN_OIDC_ISSUER"),
		AdminOIDCAudience:    env("ADMIN_OIDC_AUDIENCE", "baobab-control-plane"),
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
