package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	ClockSkew       = 30 * time.Second
	MaximumLifetime = 15 * time.Minute
)

var ErrInvalidToken = errors.New("invalid access token")
var canonicalTenantID = regexp.MustCompile(`^tn_[a-z0-9]+$`)
var canonicalScope = regexp.MustCompile(`^[a-z][a-z0-9.-]*:[a-z][a-z0-9.-]*$`)

// protocolScopes are the OpenID Connect and Keycloak built-in scope names a
// workforce login token carries alongside its authority ("openid profile
// email", ...). They grant nothing at a Control Plane route, so they are
// ignored rather than rejected; every other scope must still be canonical.
var protocolScopes = map[string]struct{}{
	"openid": {}, "profile": {}, "email": {}, "address": {}, "phone": {}, "offline_access": {},
	"roles": {}, "web-origins": {}, "acr": {}, "basic": {}, "microprofile-jwt": {}, "organization": {},
}

type Principal struct {
	Subject   string
	Issuer    string
	ActorType string
	TenantID  string
	ClientID  string
	TokenID   string
	Scopes    map[string]struct{}
	// Roles holds this token's Keycloak realm roles (the realm_access.roles
	// claim) -- e.g. "cp:platform-admin", "cp:tenant-admin" (Gate IAM-5
	// phase 1, baobab-iam). Unlike Scopes, an empty Roles is not rejected:
	// most tokens (workloads, unprivileged humans) legitimately carry none,
	// and Keycloak also populates this claim with unrelated default realm
	// roles (e.g. "offline_access") that role-aware authorization simply
	// never checks for.
	Roles map[string]struct{}
	// ClientRoles holds the roles this token carries for the verifier's
	// configured IAM client (resource_access.<client>.roles), e.g.
	// "onboarding-requester". Empty unless the verifier was built
	// WithClientRoles.
	ClientRoles map[string]struct{}
}

func (p Principal) HasScope(scope string) bool { _, ok := p.Scopes[scope]; return ok }

// HasRole reports whether this token's realm_access.roles claim carries the
// given role. Role-aware admin authorization (api/router.go) uses this
// together with a WorkforceMembership lookup: the realm role establishes
// what a principal MAY do platform-wide (e.g. "cp:tenant-admin" grants
// tenant-scoped admin capability *somewhere*), while WorkforceMembership
// establishes WHICH tenant(s) -- a realm role alone is never sufficient for
// a tenant-scoped action, per ADR-0009 §27/§122.
func (p Principal) HasRole(role string) bool { _, ok := p.Roles[role]; return ok }

// HasClientRole reports whether the token carries the given role for the
// verifier's configured IAM client.
func (p Principal) HasClientRole(role string) bool { _, ok := p.ClientRoles[role]; return ok }

type TokenVerifier interface {
	Verify(context.Context, string) (Principal, error)
}

type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
	// rolesClient is the IAM client whose resource_access roles become
	// Principal.ClientRoles; empty reads none.
	rolesClient string
}

// WithClientRoles makes the verifier read clientID's resource_access roles
// into Principal.ClientRoles.
func (v *OIDCVerifier) WithClientRoles(clientID string) *OIDCVerifier {
	v.rolesClient = clientID
	return v
}

func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return &OIDCVerifier{verifier: provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}})}, nil
}

type claims struct {
	Subject     string `json:"sub"`
	Scope       string `json:"scope"`
	ActorType   string `json:"actor_type"`
	TenantID    string `json:"tenant_id"`
	ClientID    string `json:"azp"`
	TokenID     string `json:"jti"`
	IssuedAt    int64  `json:"iat"`
	NotBefore   int64  `json:"nbf"`
	ExpiresAt   int64  `json:"exp"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
	ResourceAccess map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: verification failed", ErrInvalidToken)
	}
	var c claims
	if err = token.Claims(&c); err != nil {
		return Principal{}, fmt.Errorf("%w: claims are invalid", ErrInvalidToken)
	}
	now := time.Now()
	if c.Subject == "" || len(c.Subject) > 255 || c.TokenID == "" || len(c.TokenID) > 255 || (c.ActorType != "human" && c.ActorType != "workload") {
		return Principal{}, fmt.Errorf("%w: required claims are missing", ErrInvalidToken)
	}
	if c.TenantID != "" && (len(c.TenantID) < 6 || len(c.TenantID) > 63 || !canonicalTenantID.MatchString(c.TenantID)) {
		return Principal{}, fmt.Errorf("%w: tenant claim is invalid", ErrInvalidToken)
	}
	if len(c.ClientID) > 255 {
		return Principal{}, fmt.Errorf("%w: authorised party is invalid", ErrInvalidToken)
	}
	issuedAt, expiresAt := time.Unix(c.IssuedAt, 0), time.Unix(c.ExpiresAt, 0)
	if c.IssuedAt == 0 || c.ExpiresAt == 0 || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > MaximumLifetime || issuedAt.After(now.Add(ClockSkew)) {
		return Principal{}, fmt.Errorf("%w: token lifetime is invalid", ErrInvalidToken)
	}
	if c.NotBefore != 0 && time.Unix(c.NotBefore, 0).After(now.Add(ClockSkew)) {
		return Principal{}, fmt.Errorf("%w: token is not active", ErrInvalidToken)
	}
	scopes := make(map[string]struct{})
	for _, scope := range strings.Fields(c.Scope) {
		if _, protocol := protocolScopes[scope]; protocol {
			continue
		}
		if !canonicalScope.MatchString(scope) {
			return Principal{}, fmt.Errorf("%w: scope is invalid", ErrInvalidToken)
		}
		scopes[scope] = struct{}{}
	}
	if len(scopes) == 0 {
		return Principal{}, fmt.Errorf("%w: scope is required", ErrInvalidToken)
	}
	roles := make(map[string]struct{}, len(c.RealmAccess.Roles))
	for _, role := range c.RealmAccess.Roles {
		roles[role] = struct{}{}
	}
	clientRoles := map[string]struct{}{}
	if v.rolesClient != "" {
		for _, role := range c.ResourceAccess[v.rolesClient].Roles {
			clientRoles[role] = struct{}{}
		}
	}
	// ADR-0003 ("Identity Authority and Trust Boundaries"): the verified
	// issuer is part of the identity itself — a bare `sub` is only unique
	// within one issuer, and per ADR-0004 a stable canonical identity is
	// ultimately keyed by (issuer, subject), not subject alone. token.Issuer
	// comes from the verified ID token (checked against the configured
	// provider during v.verifier.Verify above), not from an unverified claim.
	return Principal{Subject: c.Subject, Issuer: token.Issuer, ActorType: c.ActorType, TenantID: c.TenantID, ClientID: c.ClientID, TokenID: c.TokenID, Scopes: scopes, Roles: roles, ClientRoles: clientRoles}, nil
}
