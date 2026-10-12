package scannerruntime

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	verificationservice "github.com/baobab-platform/baobab-cp/internal/service/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type CPConfig struct {
	CustodyBackend                string `json:"custody_backend"`
	DurableCustodyConfigFile      string `json:"durable_custody_config_file"`
	TrustFile                     string `json:"trust_file"`
	CustodyDirectory              string `json:"custody_directory"`
	CustodyKeyFile                string `json:"custody_key_file"`
	IntrospectionURL              string `json:"introspection_url"`
	IntrospectionClientIDFile     string `json:"introspection_client_id_file"`
	IntrospectionClientSecretFile string `json:"introspection_client_secret_file"`
}

// ComposeCP contains ONLY the verifying public trust, custody and current IAM
// authority. It cannot construct an antivirus signer or load a private scan
// key. Unknown/production environments and incomplete registry/config deny.
func ComposeCP(ctx context.Context, environment, configFile, registryFile string, jwt auth.TokenVerifier, repo *repository.PostgresRepository) (verificationservice.ScannedArtifactIntake, ScannerVerifier, error) {
	var empty verificationservice.ScannedArtifactIntake
	if ctx == nil || ctx.Err() != nil || repo == nil || jwt == nil || (environment != "staging" && environment != "integration") {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	var cfg CPConfig
	if privateJSON(configFile, &cfg) != nil {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	u, parseErr := url.Parse(cfg.IntrospectionURL)
	if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	var vault verificationservice.SealedScannedArtifactWriter
	var err error
	switch cfg.CustodyBackend {
	case "local-integration":
		if environment != "integration" || !filepath.IsAbs(cfg.CustodyDirectory) || cfg.DurableCustodyConfigFile != "" {
			return empty, ScannerVerifier{}, ErrConfiguration
		}
		key, e := privateFile(cfg.CustodyKeyFile, 32)
		if e != nil || len(key) != 32 {
			return empty, ScannerVerifier{}, ErrConfiguration
		}
		vault = authority.ScannedDocumentVault{Directory: cfg.CustodyDirectory, Key: key}
	case "s3-kms":
		if cfg.CustodyDirectory != "" || cfg.CustodyKeyFile != "" {
			return empty, ScannerVerifier{}, ErrConfiguration
		}
		durable, deployment, e := ComposeCustody(ctx, cfg.DurableCustodyConfigFile, repo)
		if e != nil || deployment.Workload != "SCANNER_INTAKE" {
			return empty, ScannerVerifier{}, ErrConfiguration
		}
		vault = durable
	default:
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	verifier := ScannerVerifier{JWT: jwt, RegistryFile: registryFile, Environment: environment,
		Endpoint: cfg.IntrospectionURL, ClientIDFile: cfg.IntrospectionClientIDFile, ClientSecretFile: cfg.IntrospectionClientSecretFile,
		HTTPClient: &http.Client{Timeout: 4 * time.Second}, Now: time.Now}
	if verifier.registered(ctx) != nil {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	if _, err = secret(cfg.IntrospectionClientIDFile); err != nil {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	if _, err = secret(cfg.IntrospectionClientSecretFile); err != nil {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	// Trust is read again at intake/commit, not snapshotted here. No fallback
	// scanner key, fixture certificate, signer identity or ACTIVE state exists.
	trust := MountedScannerTrust{Path: cfg.TrustFile, Now: time.Now}
	if trust.Validate(ctx) != nil {
		return empty, ScannerVerifier{}, ErrConfiguration
	}
	return verificationservice.ScannedArtifactIntake{Repository: repo,
		Vault: vault,
		Trust: trust, Now: time.Now}, verifier, nil
}
