package organisation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func peo05ProtectedCredentials() PEO05ProtectedTenantReader {
	return PEO05ProtectedTenantReader{
		Store:       &repository.PostgresRepository{},
		Verifier:    peo05VerifiedToken{},
		Revocation:  peo05Revocation{},
		Environment: "staging",
		Tokens: map[string]string{
			"tn_nabhold":   "human-token-1",
			"tn_thamani":   "human-token-2",
			"tn_zuribeans": "human-token-3",
			"tn_equator":   "human-token-4",
		},
	}
}

func TestPEO05DRequiresFourUniqueProtectedTenantTokens(t *testing.T) {
	base := peo05ProtectedCredentials()
	if err := base.CheckCredentials(); err != nil {
		t.Fatalf("correctly isolated credentials rejected: %v", err)
	}
	cases := map[string]func(*PEO05ProtectedTenantReader){
		"duplicate token": func(r *PEO05ProtectedTenantReader) { r.Tokens["tn_equator"] = r.Tokens["tn_thamani"] },
		"missing tenant":  func(r *PEO05ProtectedTenantReader) { delete(r.Tokens, "tn_equator") },
		"malformed tenant": func(r *PEO05ProtectedTenantReader) {
			r.Tokens["invalid:tenant"] = r.Tokens["tn_equator"]
			delete(r.Tokens, "tn_equator")
		},
		"missing verifier":      func(r *PEO05ProtectedTenantReader) { r.Verifier = nil },
		"missing revocation":    func(r *PEO05ProtectedTenantReader) { r.Revocation = nil },
		"missing CP repository": func(r *PEO05ProtectedTenantReader) { r.Store = nil },
		"missing environment":   func(r *PEO05ProtectedTenantReader) { r.Environment = "" },
		"oversized secret":      func(r *PEO05ProtectedTenantReader) { r.Tokens["tn_equator"] = string(make([]byte, 16385)) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := peo05ProtectedCredentials()
			change(&fixture)
			if err := fixture.CheckCredentials(); err == nil {
				t.Fatal("invalid tenant credential set accepted")
			}
		})
	}
}

func TestPEO05DRefusesUnapprovedCallerBeforeAnyDatabaseRead(t *testing.T) {
	reader := peo05ProtectedCredentials()
	reader.Tokens = nil
	_, err := reader.ReadPEO05Tenant(context.Background(), "tn_nabhold", "org_nabhold", time.Now())
	if !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("missing protected credentials did not deny: %v", err)
	}
}
