package organisation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// PEO05IdentityAuthority reads current canonical identity and explicit tenant
// membership. Identity federation does not itself mint tenant authority.
type PEO05IdentityAuthority interface {
	ResolveIdentity(context.Context, string, string) (domain.Principal, error)
	GetWorkforceMembership(context.Context, string, string) (domain.WorkforceMembership, error)
}

var _ PEO05IdentityAuthority = (*repository.PostgresRepository)(nil)

type PEO05RevocationStatus struct {
	Issuer     string
	Subject    string
	TokenID    string
	Audience   string
	CheckedAt  time.Time
	ExpiresAt  time.Time
	Active     bool
}

type PEO05RevocationAuthority interface {
	CurrentToken(context.Context, string, auth.Principal, time.Time) (PEO05RevocationStatus, error)
}

// PEO05AuthenticatedIAM never derives membership from an OIDC role alone.
// The bearer token is furnished by an authenticated, protected operator
// channel, not a tenant-supplied JSON field.
type PEO05AuthenticatedIAM struct {
	Verifier   auth.TokenVerifier
	Identities PEO05IdentityAuthority
	Revocation PEO05RevocationAuthority
	Token      string
}

func (a PEO05AuthenticatedIAM) CurrentTenantIAM(ctx context.Context, tenantID, orgID string, at time.Time) (PEO05IAMProof, error) {
	empty := PEO05IAMProof{}
	if ctx == nil || ctx.Err() != nil || a.Verifier == nil || a.Identities == nil || a.Revocation == nil ||
		a.Token == "" || tenantID == "" || orgID == "" || at.IsZero() {
		return empty, ErrPEO05Integrity
	}
	verified, err := a.Verifier.Verify(ctx, a.Token)
	if err != nil || verified.ActorType != "human" || verified.TokenID == "" ||
		verified.Issuer == "" || verified.Subject == "" || !verified.ExpiresAt.After(at) ||
		(verified.TenantID != "" && verified.TenantID != tenantID) {
		return empty, ErrPEO05Integrity
	}
	principal, err := a.Identities.ResolveIdentity(ctx, verified.Issuer, verified.Subject)
	if err != nil || principal.ID == "" || principal.Status != "ACTIVE" || principal.ActorType != "human" {
		return empty, ErrPEO05Integrity
	}
	member, err := a.Identities.GetWorkforceMembership(ctx, principal.ID, tenantID)
	if err != nil || member.PrincipalID != principal.ID || member.TenantID != tenantID ||
		member.Status != "ACTIVE" || !member.HasRole("cp:tenant-admin") ||
		!verified.HasRole("cp:tenant-admin") {
		return empty, ErrPEO05Integrity
	}
	revoked, err := a.Revocation.CurrentToken(ctx, a.Token, verified, at)
	if err != nil || !revoked.Active || revoked.Issuer != verified.Issuer ||
		revoked.Subject != verified.Subject || revoked.TokenID != verified.TokenID ||
		revoked.Audience != "baobab-control-plane" ||
		!currentPEO05Observation(revoked.CheckedAt, revoked.ExpiresAt, at) {
		return empty, ErrPEO05Integrity
	}
	expiry := revoked.ExpiresAt
	if verified.ExpiresAt.Before(expiry) {
		expiry = verified.ExpiresAt
	}
	if expiry.After(at.Add(time.Minute)) {
		expiry = at.Add(time.Minute)
	}
	if !expiry.After(at) {
		return empty, ErrPEO05Integrity
	}
	sum := sha256.Sum256([]byte(verified.Issuer + "\x00" + verified.TokenID))
	return PEO05IAMProof{
		TenantID: tenantID, OrganisationID: orgID,
		PrincipalID: principal.ID, IssuerID: verified.Issuer,
		Audience: "baobab-control-plane", MembershipStatus: "ACTIVE",
		EvidenceReference: "oauth2:introspection:sha256:" + hex.EncodeToString(sum[:]),
		RevocationCheckedAt: revoked.CheckedAt, ExpiresAt: expiry,
	}, nil
}

// PEO05OAuthIntrospection checks a token's current state with a protected,
// provider-neutral RFC 7662 authority. It requires a TLS-authenticated HTTPS
// endpoint and real client authentication; no unverified active flag is used.
type PEO05OAuthIntrospection struct {
	Endpoint     string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

func (c PEO05OAuthIntrospection) CurrentToken(ctx context.Context, raw string, principal auth.Principal, at time.Time) (PEO05RevocationStatus, error) {
	empty := PEO05RevocationStatus{}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Fragment != "" || u.RawQuery != "" || c.ClientID == "" || c.ClientSecret == "" ||
		c.HTTPClient == nil || raw == "" || ctx == nil || ctx.Err() != nil || at.IsZero() {
		return empty, ErrPEO05Integrity
	}
	body := strings.NewReader(url.Values{"token": {raw}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, body)
	if err != nil {
		return empty, ErrPEO05Integrity
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.ClientID, c.ClientSecret)
	response, err := c.HTTPClient.Do(req)
	if err != nil {
		return empty, ErrPEO05Integrity
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") == "" ||
		!strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		return empty, ErrPEO05Integrity
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) == 0 || len(data) > 65536 {
		return empty, ErrPEO05Integrity
	}
	var proof struct {
		Active bool            `json:"active"`
		Subject string         `json:"sub"`
		Issuer string          `json:"iss"`
		TokenID string         `json:"jti"`
		Audience json.RawMessage `json:"aud"`
		Expires int64          `json:"exp"`
	}
	if json.Unmarshal(data, &proof) != nil || !proof.Active || proof.Issuer != principal.Issuer ||
		proof.Subject != principal.Subject || proof.TokenID != principal.TokenID ||
		proof.Expires <= at.Unix() {
		return empty, ErrPEO05Integrity
	}
	var audiences []string
	if json.Unmarshal(proof.Audience, &audiences) != nil {
		var one string
		if json.Unmarshal(proof.Audience, &one) != nil {
			return empty, ErrPEO05Integrity
		}
		audiences = []string{one}
	}
	if len(audiences) != 1 || audiences[0] != "baobab-control-plane" {
		return empty, ErrPEO05Integrity
	}
	// Explicitly reject degenerate timestamp values even if they parse.
	exp := time.Unix(proof.Expires, 0).UTC()
	if exp.Before(at) || exp.After(at.Add(15*time.Minute)) {
		return empty, ErrPEO05Integrity
	}
	return PEO05RevocationStatus{
		Issuer: proof.Issuer, Subject: proof.Subject, TokenID: proof.TokenID,
		Audience: audiences[0], Active: true, CheckedAt: at.UTC(), ExpiresAt: exp,
	}, nil
}

// Keep the exact parse error decision opaque at the tenant boundary; do not
// disclose raw token values or private OAuth client details in errors/logs.
