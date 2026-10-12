package scannerruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func privateFixture(t *testing.T, name string, raw []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScannerCurrentJWTIntrospectionRegistryAndRevocation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(time.Minute)
	var response atomic.Value
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "jwks_uri": srv.URL + "/jwks", "authorization_endpoint": srv.URL + "/auth", "token_endpoint": srv.URL + "/token", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "synthetic-key", Algorithm: "RS256", Use: "sig"}}})
		case "/introspect":
			id, secret, ok := r.BasicAuth()
			if !ok || id != "synthetic-introspector" || secret != "synthetic-private-credential" {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(response.Load())
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	jwt, err := auth.NewOIDCVerifier(oidc.ClientContext(context.Background(), srv.Client()), srv.URL, "baobab-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "synthetic-key"))
	if err != nil {
		t.Fatal(err)
	}
	token := func(overrides map[string]any) string {
		claims := map[string]any{"iss": srv.URL, "aud": []string{"baobab-control-plane"}, "sub": "synthetic-scanner", "jti": "synthetic-token-001",
			"azp": authority.ScannerClientID, "actor_type": "workload", "scope": authority.ScannerScope, "iat": now.Unix(), "exp": expiry.Unix()}
		for k, v := range overrides {
			claims[k] = v
		}
		raw, _ := json.Marshal(claims)
		signed, err := signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
		out, err := signed.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	registry := privateFixture(t, "registry.yaml", []byte("workloads:\n  "+authority.ScannerClientID+":\n    status: ACTIVE\n    environment: integration\n    credential_type: federated_workload_token\n    allowed_audiences: [baobab-control-plane]\n    allowed_scopes: [verification:scan-attest]\n"))
	baseline, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	v := ScannerVerifier{JWT: jwt, RegistryFile: registry, Environment: "integration", Endpoint: srv.URL + "/introspect",
		ClientIDFile: privateFixture(t, "id", []byte("synthetic-introspector")), ClientSecretFile: privateFixture(t, "secret", []byte("synthetic-private-credential")), HTTPClient: srv.Client(), Now: time.Now}
	proof := map[string]any{"active": true, "iss": srv.URL, "sub": "synthetic-scanner", "jti": "synthetic-token-001", "client_id": authority.ScannerClientID, "aud": []string{"baobab-control-plane"}, "scope": authority.ScannerScope, "exp": expiry.Unix()}
	response.Store(proof)
	raw := token(nil)
	if _, err = v.Verify(context.Background(), raw); err != nil {
		t.Fatalf("real signed fixture scanner denied: %v", err)
	}
	for _, overrides := range []map[string]any{{"aud": []string{"baobab-document-extractor"}}, {"azp": "baobab-document-extractor-staging"}, {"scope": "verification:document-extract"}, {"actor_type": "human"}, {"tenant_id": "tn_foreign"}, {"iss": "https://foreign.example.invalid"}} {
		if _, err = v.Verify(context.Background(), token(overrides)); err == nil {
			t.Fatalf("foreign identity claims accepted: %v", overrides)
		}
	}
	for key, badValue := range map[string]any{"active": false, "jti": "foreign-token", "sub": "foreign-subject", "client_id": "foreign-client", "aud": []string{"foreign-audience"}, "scope": "verification:decide", "exp": expiry.Add(time.Hour).Unix()} {
		bad := map[string]any{}
		for k, x := range proof {
			bad[k] = x
		}
		bad[key] = badValue
		response.Store(bad)
		if _, err = v.Verify(context.Background(), raw); err == nil {
			t.Fatalf("bad introspection %s accepted", key)
		}
	}
	response.Store(proof)
	for _, state := range []string{"REVOKED", "PROVISIONED", "SUSPENDED"} {
		updated := []byte(string(baseline))
		updated = bytes.ReplaceAll(updated, []byte("ACTIVE"), []byte(state))
		if err = os.WriteFile(registry, updated, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = v.Verify(context.Background(), raw); err == nil {
			t.Fatalf("fresh %s registry ignored", state)
		}
	}
	if err = os.WriteFile(registry, baseline, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Verify(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var leaked atomic.Int32
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer redirect.Close()
	entry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", redirect.URL)
		w.WriteHeader(307)
	}))
	defer entry.Close()
	v.Endpoint = entry.URL
	v.HTTPClient = entry.Client()
	if _, err = v.Verify(context.Background(), raw); err == nil || leaked.Load() != 0 {
		t.Fatal("redirect received scanner or introspection credentials")
	}
}
