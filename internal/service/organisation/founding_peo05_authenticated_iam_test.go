package organisation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type peo05VerifiedToken struct {
	value auth.Principal
	err   error
}

func (v peo05VerifiedToken) Verify(context.Context, string) (auth.Principal, error) {
	return v.value, v.err
}

type peo05Identities struct {
	principal  domain.Principal
	membership domain.WorkforceMembership
	err        error
}

func (v peo05Identities) ResolveIdentity(context.Context, string, string) (domain.Principal, error) {
	return v.principal, v.err
}
func (v peo05Identities) GetWorkforceMembership(context.Context, string, string) (domain.WorkforceMembership, error) {
	return v.membership, v.err
}

type peo05Revocation struct {
	proof PEO05RevocationStatus
	err   error
}

func (v peo05Revocation) CurrentToken(context.Context, string, auth.Principal, time.Time) (PEO05RevocationStatus, error) {
	return v.proof, v.err
}

func peo05IAMCase() (PEO05AuthenticatedIAM, time.Time) {
	now := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	token := auth.Principal{
		ActorType: "human", Subject: "subject-one", Issuer: "https://iam.example.test/",
		TokenID: "token-one", TenantID: "tn_testa", ExpiresAt: now.Add(time.Minute),
		Roles: map[string]struct{}{"cp:tenant-admin": {}},
	}
	return PEO05AuthenticatedIAM{
		Verifier: peo05VerifiedToken{value: token}, Token: "test-not-a-real-secret",
		Identities: peo05Identities{
			principal:  domain.Principal{ID: "principal-one", Status: "ACTIVE", ActorType: "human"},
			membership: domain.WorkforceMembership{PrincipalID: "principal-one", TenantID: "tn_testa", Status: "ACTIVE", Roles: []string{"cp:tenant-admin"}},
		},
		Revocation: peo05Revocation{proof: PEO05RevocationStatus{
			Active: true, Issuer: token.Issuer, Subject: token.Subject, TokenID: token.TokenID,
			Audience: "baobab-control-plane", CheckedAt: now, ExpiresAt: now.Add(time.Minute),
		}},
	}, now
}

func TestPEO05CIAMActiveMembershipAndCurrentIntrospection(t *testing.T) {
	reader, now := peo05IAMCase()
	got, err := reader.CurrentTenantIAM(context.Background(), "tn_testa", "org_test", now)
	if err != nil || got.TenantID != "tn_testa" || got.OrganisationID != "org_test" ||
		got.PrincipalID != "principal-one" || !strings.HasPrefix(got.EvidenceReference, "oauth2:introspection:sha256:") {
		t.Fatalf("tenant IAM proof invalid: %+v %v", got, err)
	}
}

func TestPEO05CIAMDoesNotTrustRealmRoleOrCrossTenant(t *testing.T) {
	for name, change := range map[string]func(*PEO05AuthenticatedIAM){
		"missing membership": func(a *PEO05AuthenticatedIAM) { a.Identities = peo05Identities{err: errors.New("no membership")} },
		"revoked membership": func(a *PEO05AuthenticatedIAM) {
			v := a.Identities.(peo05Identities)
			v.membership.Status = "DISABLED"
			a.Identities = v
		},
		"sibling tenant": func(a *PEO05AuthenticatedIAM) {
			v := a.Identities.(peo05Identities)
			v.membership.TenantID = "tn_sibling"
			a.Identities = v
		},
		"missing realm role": func(a *PEO05AuthenticatedIAM) {
			v := a.Verifier.(peo05VerifiedToken)
			v.value.Roles = nil
			a.Verifier = v
		},
		"missing membership role": func(a *PEO05AuthenticatedIAM) {
			v := a.Identities.(peo05Identities)
			v.membership.Roles = []string{"reader"}
			a.Identities = v
		},
		"disabled principal": func(a *PEO05AuthenticatedIAM) {
			v := a.Identities.(peo05Identities)
			v.principal.Status = "DISABLED"
			a.Identities = v
		},
		"revoked token": func(a *PEO05AuthenticatedIAM) {
			v := a.Revocation.(peo05Revocation)
			v.proof.Active = false
			a.Revocation = v
		},
		"revocation unavailable": func(a *PEO05AuthenticatedIAM) { a.Revocation = peo05Revocation{err: errors.New("offline")} },
		"wrong audience": func(a *PEO05AuthenticatedIAM) {
			v := a.Revocation.(peo05Revocation)
			v.proof.Audience = "another-api"
			a.Revocation = v
		},
		"stale introspection": func(a *PEO05AuthenticatedIAM) {
			v := a.Revocation.(peo05Revocation)
			v.proof.CheckedAt = time.Time{}
			a.Revocation = v
		},
	} {
		t.Run(name, func(t *testing.T) {
			reader, now := peo05IAMCase()
			change(&reader)
			if _, err := reader.CurrentTenantIAM(context.Background(), "tn_testa", "org_test", now); !errors.Is(err, ErrPEO05Integrity) {
				t.Fatalf("invalid IAM source accepted: %v", err)
			}
		})
	}
}

func TestPEO05COAuthIntrospectionHTTPSAndRevocation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	active := true
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "client-id" || password != "client-secret" || r.FormValue("token") != "opaque-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"active": active, "iss": "https://issuer.test", "sub": "subject",
			"jti": "jti", "aud": []string{"baobab-control-plane"}, "exp": now.Add(2 * time.Minute).Unix(),
		})
	}))
	defer server.Close()
	reader := PEO05OAuthIntrospection{Endpoint: server.URL, HTTPClient: server.Client(),
		ClientID: "client-id", ClientSecret: "client-secret"}
	principal := auth.Principal{Issuer: "https://issuer.test", Subject: "subject", TokenID: "jti"}
	proof, err := reader.CurrentToken(context.Background(), "opaque-token", principal, now)
	if err != nil || !proof.Active || proof.Audience != "baobab-control-plane" {
		t.Fatalf("introspection rejected current token: %+v %v", proof, err)
	}
	active = false
	if _, err := reader.CurrentToken(context.Background(), "opaque-token", principal, now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("revoked token accepted: %v", err)
	}
	reader.Endpoint = "http://plain.example.test/introspect"
	if _, err := reader.CurrentToken(context.Background(), "opaque-token", principal, now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("plaintext introspection accepted: %v", err)
	}
	reader.Endpoint = server.URL
	reader.ClientSecret = ""
	if _, err := reader.CurrentToken(context.Background(), "opaque-token", principal, now); !errors.Is(err, ErrPEO05Integrity) {
		t.Fatalf("unauthenticated introspection accepted: %v", err)
	}
}
