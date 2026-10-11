// Command qualify-founding-package5 runs PEO-05 in a protected operator
// environment. It is a read-only staging qualification, never go-live.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/organisation"
	"github.com/jackc/pgx/v5/pgxpool"
)

type signerDeclaration struct {
	IssuerID       string    `json:"issuer_id"`
	SignerID       string    `json:"signer_id"`
	PublicKeyHex   string    `json:"public_key_hex"`
	ProposedBy     string    `json:"proposed_by"`
	ApprovedBy     string    `json:"approved_by"`
	ApprovalRecord string    `json:"approval_record"`
	ApprovedAt     time.Time `json:"approved_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type operatorConfig struct {
	Parent              organisation.FoundingIdentityInput `json:"nabhold"`
	Child               organisation.FoundingIdentityInput `json:"thamani"`
	Trust               []signerDeclaration                `json:"independent_control_trust"`
	Environment         string                             `json:"environment"`
	IAMIssuer           string                             `json:"iam_issuer"`
	IntrospectionURL    string                             `json:"introspection_url"`
	IntrospectionID     string                             `json:"introspection_client_id"`
	IntrospectionSecret string                             `json:"introspection_client_secret"`
	TenantTokens        map[string]string                  `json:"tenant_tokens"`
}

func readProtected(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("an absolute operator-owned configuration path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > 1<<20 {
		return nil, errors.New("operator file must be regular, <= 1 MiB and mode 0600")
	}
	return os.ReadFile(path)
}

func parseTrust(entries []signerDeclaration, now time.Time) (map[string]organisation.IndependentControlTrust, error) {
	if len(entries) == 0 || len(entries) > 16 {
		return nil, errors.New("independent control signer trust is required")
	}
	trust := make(map[string]organisation.IndependentControlTrust, len(entries))
	for _, v := range entries {
		key, err := hex.DecodeString(v.PublicKeyHex)
		if err != nil || len(key) != ed25519.PublicKeySize || v.IssuerID == "" || v.SignerID == "" ||
			v.ProposedBy == "" || v.ApprovedBy == "" || v.ProposedBy == v.ApprovedBy ||
			v.ApprovalRecord == "" || v.ApprovedAt.IsZero() || v.ApprovedAt.After(now) ||
			!v.ExpiresAt.After(now) {
			return nil, errors.New("independent control trust entry not approved")
		}
		if _, exists := trust[v.SignerID]; exists {
			return nil, errors.New("duplicate independent signer")
		}
		trust[v.SignerID] = organisation.IndependentControlTrust{
			IssuerID: v.IssuerID, SignerID: v.SignerID, PublicKey: ed25519.PublicKey(key),
			ProposedBy: v.ProposedBy, ApprovedBy: v.ApprovedBy,
			ApprovalRecord: v.ApprovalRecord, ApprovedAt: v.ApprovedAt, ExpiresAt: v.ExpiresAt,
		}
	}
	return trust, nil
}

// decodeOperatorConfig refuses concatenated or stale trailing JSON and unknown
// fields; protected credentials must form exactly one reviewed document.
func decodeOperatorConfig(raw []byte) (operatorConfig, error) {
	var cfg operatorConfig
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return operatorConfig{}, errors.New("invalid operator configuration")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return operatorConfig{}, errors.New("operator configuration must contain one JSON document")
	}
	return cfg, nil
}

func run(ctx context.Context, registryPath, configPath string) (organisation.PEO05Report, error) {
	var empty organisation.PEO05Report
	raw, err := readProtected(configPath)
	if err != nil {
		return empty, err
	}
	cfg, err := decodeOperatorConfig(raw)
	if err != nil {
		return empty, err
	}
	if cfg.Environment != "staging" && cfg.Environment != "integration" && cfg.Environment != "sandbox" {
		return empty, errors.New("Package 5 qualification must run outside production")
	}
	if cfg.Parent.RegistryID != "NABHOLD" || cfg.Child.RegistryID != "THAMANI-GLOBAL" ||
		cfg.Parent.OrganisationID == "" || cfg.Child.OrganisationID == "" ||
		cfg.Parent.VerificationCaseID == "" || cfg.Child.VerificationCaseID == "" ||
		cfg.Parent.DeclaredRegistrationNumber == "" || cfg.Child.DeclaredRegistrationNumber == "" ||
		cfg.IAMIssuer == "" || cfg.IntrospectionURL == "" || cfg.IntrospectionID == "" || cfg.IntrospectionSecret == "" {
		return empty, errors.New("incomplete authority configuration")
	}
	now := time.Now
	trust, err := parseTrust(cfg.Trust, now())
	if err != nil {
		return empty, err
	}
	if !filepath.IsAbs(registryPath) {
		return empty, errors.New("governed registry must have an absolute trusted path")
	}
	reg, err := organisation.LoadFirstPartyRegistry(registryPath)
	if err != nil {
		return empty, err
	}
	if len(reg.Entities) != 4 {
		return empty, errors.New("four governed first-party entities required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return empty, errors.New("DATABASE_URL required")
	}
	store, err := repository.Open(ctx, dsn)
	if err != nil {
		return empty, err
	}
	defer store.Close()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return empty, err
	}
	defer pool.Close()
	verifier, err := auth.NewOIDCVerifier(ctx, cfg.IAMIssuer, "baobab-control-plane")
	if err != nil {
		return empty, errors.New("current IAM verifier unavailable")
	}
	revocation := organisation.PEO05OAuthIntrospection{
		Endpoint: cfg.IntrospectionURL, ClientID: cfg.IntrospectionID,
		ClientSecret: cfg.IntrospectionSecret, HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
	tenants := organisation.PEO05ProtectedTenantReader{
		Store: store, Verifier: verifier, Revocation: revocation,
		Environment: cfg.Environment, Tokens: cfg.TenantTokens,
	}
	if err := tenants.CheckCredentials(); err != nil {
		return empty, err
	}
	founding := organisation.FoundingAcceptanceGate{
		Snapshot: organisation.PostgresFoundingSnapshot{Pool: pool},
		Evidence: organisation.FoundingEvidenceReconciler{
			Registrations: store, Corporate: store,
			Proofs: organisation.PostgresControlProofStore{Pool: pool, Trust: trust, Now: now},
			Trust:  trust, Now: now,
		},
		Now: now,
	}
	gate := organisation.PEO05Gate{Founding: founding, Tenants: tenants, Now: now}
	return gate.Assess(ctx, reg, cfg.Parent, cfg.Child)
}

func main() {
	flags := flag.NewFlagSet("qualify-founding-package5", flag.ExitOnError)
	registry := flags.String("registry", "", "absolute trusted Shared first-party registry path")
	config := flags.String("operator-config", "", "protected mode-0600 staging IAM and independent evidence configuration")
	flags.Parse(os.Args[1:])
	if len(flags.Args()) != 0 || *registry == "" || *config == "" {
		slog.Error("trusted registry and operator configuration required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	report, err := run(ctx, *registry, *config)
	if err != nil || report.VerifyNonAuthorising() != nil {
		slog.Error("PEO-05 staging qualification denied")
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		slog.Error("write read-only staging report failed")
		os.Exit(1)
	}
	if !report.AllStagingCandidates {
		os.Exit(2)
	}
}
