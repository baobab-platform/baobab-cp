package organisation

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// PEO05ProtectedTenantReader composes Package 5 runtime authorities with
// distinct human IAM credentials for each tenant. Neither this type nor its
// credential map is exposed through applicant, tenant or browser endpoints.
type PEO05ProtectedTenantReader struct {
	Store       *repository.PostgresRepository
	Verifier    auth.TokenVerifier
	Revocation  PEO05RevocationAuthority
	Environment string
	Tokens      map[string]string
}

func (r PEO05ProtectedTenantReader) CheckCredentials() error {
	if r.Store == nil || r.Verifier == nil || r.Revocation == nil ||
		r.Environment == "" || len(r.Tokens) != 4 {
		return ErrPEO05Integrity
	}
	seen := map[[32]byte]bool{}
	for tenant, token := range r.Tokens {
		if !domain.ValidTenantID(tenant) || token == "" || len(token) > 16384 {
			return ErrPEO05Integrity
		}
		fingerprint := sha256.Sum256([]byte(token))
		if seen[fingerprint] {
			return errors.New("PEO-05 separate tenant credentials required")
		}
		seen[fingerprint] = true
	}
	return nil
}

func (r PEO05ProtectedTenantReader) ReadPEO05Tenant(ctx context.Context, tenantID, orgID string, now time.Time) (PEO05TenantEvidence, error) {
	if err := r.CheckCredentials(); err != nil {
		return PEO05TenantEvidence{}, ErrPEO05Integrity
	}
	token, ok := r.Tokens[tenantID]
	if !ok || token == "" {
		return PEO05TenantEvidence{}, ErrPEO05Integrity
	}
	return (AuthoritativePEO05Reader{
		Core:      r.Store,
		Freshness: PEO05CanonicalPlanFreshness{Registry: r.Store, Environment: r.Environment},
		IAM: PEO05AuthenticatedIAM{
			Verifier: r.Verifier, Identities: r.Store,
			Revocation: r.Revocation, Token: token,
		},
		Providers: PEO05CertifiedProviders{Store: r.Store},
	}).ReadPEO05Tenant(ctx, tenantID, orgID, now)
}
