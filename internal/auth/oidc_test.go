package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type testIssuer struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &testIssuer{key: key}
	mux := http.NewServeMux()
	issuer.server = httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.server.URL, "jwks_uri": issuer.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
	})
	t.Cleanup(issuer.server.Close)
	return issuer
}

func (i *testIssuer) token(t *testing.T, overrides map[string]any) string {
	t.Helper()
	now := time.Now().Unix()
	claims := map[string]any{"iss": i.server.URL, "sub": "admin-123", "aud": "baobab-control-plane", "exp": now + 300, "iat": now, "jti": "token-123", "scope": "tenant:write", "actor_type": "human"}
	for key, value := range overrides {
		claims[key] = value
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: i.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOIDCVerifierAcceptsContractToken(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), issuer.token(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if principal.Subject != "admin-123" || principal.ActorType != "human" || !principal.HasScope("tenant:write") {
		t.Fatalf("unexpected principal: %#v", principal)
	}
	if principal.Issuer != issuer.server.URL {
		t.Fatalf("expected verified issuer %q on principal, got %q", issuer.server.URL, principal.Issuer)
	}
}

func TestOIDCVerifierRejectsExcessiveLifetime(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err = verifier.Verify(context.Background(), issuer.token(t, map[string]any{"iat": now, "exp": now + 901})); err == nil {
		t.Fatal("token longer than contract maximum was accepted")
	}
}

func TestOIDCVerifierRejectsWrongAudience(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifier.Verify(context.Background(), issuer.token(t, map[string]any{"aud": "another-service"})); err == nil {
		t.Fatal("wrong audience was accepted")
	}
}

func TestOIDCVerifierAcceptsWorkloadIdentityShape(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), issuer.token(t, map[string]any{"sub": "baobab-trade", "actor_type": "workload", "scope": "context:resolve", "azp": "baobab-trade", "tenant_id": "tn_01k4m7x9q2v6c8r3d5f1h0j4"}))
	if err != nil {
		t.Fatal(err)
	}
	if principal.ActorType != "workload" || principal.ClientID != "baobab-trade" || principal.TenantID != "tn_01k4m7x9q2v6c8r3d5f1h0j4" || !principal.HasScope("context:resolve") {
		t.Fatalf("unexpected workload principal: %#v", principal)
	}
}

func TestOIDCVerifierRejectsNonCanonicalTenantClaim(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifier.Verify(context.Background(), issuer.token(t, map[string]any{"actor_type": "workload", "scope": "context:resolve", "azp": "baobab-trade", "tenant_id": "zuribeans_za"})); err == nil {
		t.Fatal("noncanonical tenant claim was accepted")
	}
}

func TestOIDCVerifierExtractsRealmAccessRoles(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), issuer.token(t, map[string]any{
		"realm_access": map[string]any{"roles": []string{"cp:tenant-admin", "offline_access"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !principal.HasRole("cp:tenant-admin") {
		t.Fatalf("expected principal to carry cp:tenant-admin, got %#v", principal.Roles)
	}
	if !principal.HasRole("offline_access") {
		t.Fatalf("expected principal to carry unrelated default realm roles too, got %#v", principal.Roles)
	}
	if principal.HasRole("cp:platform-admin") {
		t.Fatalf("did not expect an unclaimed role to be present: %#v", principal.Roles)
	}
}

func TestOIDCVerifierAcceptsTokenWithNoRealmAccessClaim(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	// Roles is a claim workload tokens and many human tokens legitimately
	// omit entirely -- unlike Scope, its absence must not fail verification.
	principal, err := verifier.Verify(context.Background(), issuer.token(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if principal.HasRole("cp:platform-admin") {
		t.Fatalf("expected no roles on a token with no realm_access claim, got %#v", principal.Roles)
	}
}

func TestOIDCVerifierRejectsMalformedScope(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifier.Verify(context.Background(), issuer.token(t, map[string]any{"scope": "tenant:write invalid"})); err == nil {
		t.Fatal("malformed scope was accepted")
	}
}

// TestOIDCVerifierIgnoresProtocolScopes: a Keycloak workforce login token
// carries "openid profile email" beside its authority. Those grant nothing,
// so they are ignored; a non-canonical scope outside that set is still
// refused, and a token with no canonical scope at all still fails.
func TestOIDCVerifierIgnoresProtocolScopes(t *testing.T) {
	issuer := newTestIssuer(t)
	verifier, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), issuer.token(t, map[string]any{"scope": "openid profile email onboarding:request"}))
	if err != nil {
		t.Fatal(err)
	}
	if !principal.HasScope("onboarding:request") || principal.HasScope("openid") || len(principal.Scopes) != 1 {
		t.Fatalf("scopes = %#v, want only onboarding:request", principal.Scopes)
	}
	if _, err := verifier.Verify(context.Background(), issuer.token(t, map[string]any{"scope": "openid profile email"})); err == nil {
		t.Fatal("a token carrying only protocol scopes was accepted")
	}
	if _, err := verifier.Verify(context.Background(), issuer.token(t, map[string]any{"scope": "openid tenant-write"})); err == nil {
		t.Fatal("a non-canonical scope beside a protocol scope was accepted")
	}
}

// TestOIDCVerifierExtractsConfiguredClientRoles: only the configured IAM
// client's resource_access roles become ClientRoles -- a role another client
// carries never counts, and without WithClientRoles none are read.
func TestOIDCVerifierExtractsConfiguredClientRoles(t *testing.T) {
	issuer := newTestIssuer(t)
	claims := map[string]any{"resource_access": map[string]any{
		"baobab-control-plane-admin": map[string]any{"roles": []string{"onboarding-requester"}},
		"baobab-cms-admin":           map[string]any{"roles": []string{"onboarding-authoriser"}},
	}}
	plain, err := NewOIDCVerifier(context.Background(), issuer.server.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := plain.Verify(context.Background(), issuer.token(t, claims))
	if err != nil {
		t.Fatal(err)
	}
	if len(principal.ClientRoles) != 0 {
		t.Fatalf("a verifier without WithClientRoles read client roles: %#v", principal.ClientRoles)
	}
	principal, err = plain.WithClientRoles("baobab-control-plane-admin").Verify(context.Background(), issuer.token(t, claims))
	if err != nil {
		t.Fatal(err)
	}
	if !principal.HasClientRole("onboarding-requester") || principal.HasClientRole("onboarding-authoriser") {
		t.Fatalf("client roles = %#v, want only the configured client's onboarding-requester", principal.ClientRoles)
	}
}
