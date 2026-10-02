package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// audienceTokens stands in for per-audience OIDC verification: a token is
// valid for an audience only if it is registered under it.
type audienceTokens struct {
	byAudience  map[string]tokenVerifier
	unavailable bool
}

func (a audienceTokens) For(_ context.Context, audience string) (auth.TokenVerifier, error) {
	if a.unavailable {
		return nil, errors.New("issuer unreachable")
	}
	return a.byAudience[audience], nil
}

const (
	validateIssuer    = "https://iam.test/realms/baobab"
	validatorBearer   = "validator-bearer"
	tradeToken        = "trade-token.header.signature-0001"
	tradeRotatedToken = "trade-token.header.signature-0002"
)

type validateFixture struct {
	t        *testing.T
	handler  http.Handler
	contexts *repository.Repository
	store    *fakeStore
	owned    string // bounded, owned by the trade principal
	trade    string // trade's canonical principal id
	log      *bytes.Buffer
}

func newValidateFixture(t *testing.T, mutate func(*Dependencies)) *validateFixture {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	trade := resolutionIdentity(t, repo, "trade-sub")
	other := resolutionIdentity(t, repo, "other-sub")
	validatorID := resolutionIdentity(t, repo, "erp-sub")
	store := &fakeStore{}
	f := &validateFixture{t: t, contexts: repo, store: store, trade: trade, log: &bytes.Buffer{}}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(f.log, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	expires := time.Now().UTC().Add(15 * time.Minute)
	seed := func(id, principal string, expiresAt *time.Time) string {
		c := domain.Context{ID: id, PrincipalID: principal, TenantID: "tn_validate", LegalEntityID: "VALIDATE-LE", MarketID: "mkt_contractug", OrganisationID: "org_validate",
			CorrelationID: "correlation-1", ResolvedAt: time.Now().UTC().Add(-time.Hour), ExpiresAt: expiresAt,
			Provenance: map[string]domain.ContextSource{"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified}}}
		if err := repo.CreateContext(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.owned = seed(domain.NewUUIDv7(), trade, &expires)
	seed("00000000-0000-4000-8000-0000000000a1", other, &expires) // owned by another principal
	seed("00000000-0000-4000-8000-0000000000a2", trade, nil)      // unbounded
	past := time.Now().UTC().Add(-time.Minute)
	seed("00000000-0000-4000-8000-0000000000a3", trade, &past)          // expired
	seed("00000000-0000-4000-8000-0000000000a4", validatorID, &expires) // the validator's OWN context

	registryFile := filepath.Join(t.TempDir(), "workload-registry.yaml")
	if err := os.WriteFile(registryFile, []byte(`schema: {name: baobab-platform-workload-registry, version: "1.0"}
workloads:
  baobab-erp-workload:
    status: ACTIVE
    allowed_scopes: ["context:validate", "context:resolve"]
    validates_audiences: ["baobab-erp"]
  baobab-trade-workload:
    status: ACTIVE
    allowed_scopes: ["context:resolve"]
  baobab-unregistered-validator:
    status: ACTIVE
    allowed_scopes: ["context:validate"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := auth.LoadWorkloadRegistryFile(registryFile)
	if err != nil {
		t.Fatal(err)
	}

	workload := func(client, sub string, scopes ...string) auth.Principal {
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: sub, Issuer: validateIssuer, ActorType: "workload", ClientID: client, TokenID: "t-" + client, Scopes: set}
	}
	subject := func(sub, jti, tenant string) auth.Principal {
		p := workload("baobab-trade-workload", sub, "context:resolve")
		p.TokenID, p.TenantID = jti, tenant
		return p
	}
	human := subject("trade-sub", "h", "")
	human.ActorType = "human"
	deps := Dependencies{
		Store: store,
		WorkloadVerifier: tokenVerifier{
			validatorBearer: workload("baobab-erp-workload", "erp-sub", "context:validate"),
			"resolve-only":  workload("baobab-erp-workload", "erp-sub", "context:resolve"),
			"unregistered":  workload("baobab-unregistered-validator", "erp-sub", "context:validate"),
		},
		AdminVerifier:    tokenVerifier{},
		WorkloadRegistry: registry,
		Contexts:         repo,
		Identities:       repo,
		SubjectVerifiers: audienceTokens{byAudience: map[string]tokenVerifier{
			"baobab-erp": {
				tradeToken:                         subject("trade-sub", "jti-1", ""),
				tradeRotatedToken:                  subject("trade-sub", "jti-2", ""),
				"trade-token.same-tenant.sig-0001": subject("trade-sub", "jti-3", "tn_validate"),
				"trade-token.other-tenant.sig-001": subject("trade-sub", "jti-4", "tn_elsewhere"),
				"other-token.header.signature-001": subject("other-sub", "jti-5", ""),
				"human-token.header.signature-001": human,
				"erp-token.header.signature-00001": workload("baobab-erp-workload", "erp-sub", "context:validate"),
			},
			// Valid, but addressed to the Control Plane itself: not an audience
			// the validator is registered for.
			"baobab-control-plane": {"cp-token.header.signature-000001": subject("trade-sub", "jti-6", "")},
		}},
	}
	if mutate != nil {
		mutate(&deps)
	}
	f.handler = New(deps)
	return f
}

func (f *validateFixture) post(bearer, body string) *httptest.ResponseRecorder {
	return f.postTo("/v1/platform-context/validate", bearer, body)
}

func (f *validateFixture) postTo(path, bearer, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	return w
}

func vbody(contextID, token string) string {
	return `{"context_id":"` + contextID + `","subject_token":"` + token + `"}`
}

func problemOf(t *testing.T, w *httptest.ResponseRecorder) (code, detail string) {
	t.Helper()
	var p struct{ Code, Detail string }
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	return p.Code, p.Detail
}

func (f *validateFixture) expect(w *httptest.ResponseRecorder, status int, code string) {
	f.t.Helper()
	if got, _ := problemOf(f.t, w); w.Code != status || (code != "" && got != code) {
		f.t.Fatalf("want %d %s, got %d %s", status, code, w.Code, w.Body.String())
	}
}

func TestValidateConfirmsAContextForItsOwner(t *testing.T) {
	f := newValidateFixture(t, nil)
	w := f.post(validatorBearer, vbody(f.owned, tradeToken))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	// Exactly the trusted facts a resource server needs: no legal entity, no
	// principal, nothing the Control Plane resolved internally.
	for _, key := range []string{"context_id", "tenant_id", "resolved_at", "expires_at", "market_id", "organisation_id"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("response lacks %s: %v", key, raw)
		}
	}
	if len(raw) != 6 || raw["context_id"] != f.owned || raw["tenant_id"] != "tn_validate" {
		t.Fatalf("response: %v", raw)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a validation must not be cached: %q", w.Header().Get("Cache-Control"))
	}
}

// A context another principal resolved, an unknown id, an expired context and
// an unbounded one are four faces of one answer.
func TestValidateRefusesIndistinguishably(t *testing.T) {
	f := newValidateFixture(t, nil)
	reference := f.post(validatorBearer, vbody("00000000-0000-4000-8000-0000000000ff", tradeToken))
	f.expect(reference, http.StatusNotFound, "CONTEXT_NOT_FOUND")
	for name, id := range map[string]string{
		"owned by another principal": "00000000-0000-4000-8000-0000000000a1",
		"unbounded":                  "00000000-0000-4000-8000-0000000000a2",
		"expired":                    "00000000-0000-4000-8000-0000000000a3",
	} {
		w := f.post(validatorBearer, vbody(id, tradeToken))
		f.expect(w, http.StatusNotFound, "CONTEXT_NOT_FOUND")
		_, d1 := problemOf(t, w)
		_, d2 := problemOf(t, reference)
		if d1 != d2 {
			t.Fatalf("%s is distinguishable from an unknown context: %q vs %q", name, d1, d2)
		}
	}
}

// The validator's own identity is not the test: it owning the context does
// not help a subject token of someone else.
func TestValidateJudgesTheSubjectNotTheValidator(t *testing.T) {
	f := newValidateFixture(t, nil)
	validatorsOwn := "00000000-0000-4000-8000-0000000000a4"
	f.expect(f.post(validatorBearer, vbody(validatorsOwn, tradeToken)), http.StatusNotFound, "CONTEXT_NOT_FOUND")
}

func TestValidateRefusesAnUnverifiableSubject(t *testing.T) {
	f := newValidateFixture(t, nil)
	secret := "garbage-token.header.signature-0001"
	for name, token := range map[string]string{
		"unknown token":   secret,
		"wrong audience":  "cp-token.header.signature-000001",
		"a human subject": "human-token.header.signature-001",
	} {
		w := f.post(validatorBearer, vbody(f.owned, token))
		f.expect(w, http.StatusUnauthorized, "SUBJECT_TOKEN_INVALID")
		if strings.Contains(w.Body.String(), token) {
			t.Fatalf("%s: the response quotes the subject token", name)
		}
	}
	_ = secret
	for name, body := range map[string]string{
		"missing subject_token":   `{"context_id":"` + f.owned + `"}`,
		"missing context_id":      `{"subject_token":"` + tradeToken + `"}`,
		"malformed subject_token": `{"context_id":"` + f.owned + `","subject_token":"not a jwt, with spaces and a secret"}`,
		"too short subject_token": `{"context_id":"` + f.owned + `","subject_token":"a.b.c"}`,
		"not a uuid":              vbody("ctx_not_a_uuid", tradeToken),
	} {
		w := f.post(validatorBearer, body)
		f.expect(w, http.StatusBadRequest, "VALIDATION_FAILED")
		if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "spaces") {
			t.Fatalf("%s: the response quotes the rejected value: %s", name, w.Body.String())
		}
	}
}

// The caller states nothing the Control Plane trusts.
func TestValidateRejectsCallerControlledFields(t *testing.T) {
	f := newValidateFixture(t, nil)
	for _, field := range []string{"principal_id", "subject", "client_id", "azp", "actor_type", "expected_audience", "audience", "tenant_id", "issuer"} {
		body := `{"context_id":"` + f.owned + `","subject_token":"` + tradeToken + `","` + field + `":"x"}`
		f.expect(f.post(validatorBearer, body), http.StatusBadRequest, "VALIDATION_FAILED")
	}
}

func TestValidateAcceptsARotatedTokenOfTheSameClient(t *testing.T) {
	f := newValidateFixture(t, nil)
	if w := f.post(validatorBearer, vbody(f.owned, tradeRotatedToken)); w.Code != http.StatusOK {
		t.Fatalf("a rotated token (new jti, same client) must validate its own context: %d %s", w.Code, w.Body.String())
	}
}

func TestValidateTenantClaimOfTheSubject(t *testing.T) {
	f := newValidateFixture(t, nil)
	if w := f.post(validatorBearer, vbody(f.owned, "trade-token.same-tenant.sig-0001")); w.Code != http.StatusOK {
		t.Fatalf("an equal tenant claim must allow: %d %s", w.Code, w.Body.String())
	}
	f.expect(f.post(validatorBearer, vbody(f.owned, "trade-token.other-tenant.sig-001")), http.StatusForbidden, "TENANT_CONTEXT_MISMATCH")
	// An absent claim -- every real workload -- allows (TestValidateConfirms...).
}

func TestValidateRefusesASuspendedTenant(t *testing.T) {
	f := newValidateFixture(t, nil)
	f.store.tenant = domain.Tenant{TenantID: "tn_validate", LegalEntityID: "VALIDATE-LE", DesiredState: "suspended", ObservedState: "suspended"}
	f.expect(f.post(validatorBearer, vbody(f.owned, tradeToken)), http.StatusForbidden, "TENANT_NOT_ACTIVE")
}

// A tenant mismatch or a suspension must not be a way to learn about a
// context the subject does not own.
func TestValidateTenantChecksCannotProbeForeignContexts(t *testing.T) {
	f := newValidateFixture(t, nil)
	f.store.tenant = domain.Tenant{TenantID: "tn_validate", ObservedState: "suspended"}
	f.expect(f.post(validatorBearer, vbody("00000000-0000-4000-8000-0000000000a1", tradeToken)), http.StatusNotFound, "CONTEXT_NOT_FOUND")
	f.expect(f.post(validatorBearer, vbody("00000000-0000-4000-8000-0000000000a1", "trade-token.other-tenant.sig-001")), http.StatusNotFound, "CONTEXT_NOT_FOUND")
}

func TestValidateScopesAreDisjointFromResolve(t *testing.T) {
	f := newValidateFixture(t, nil)
	if w := f.post("resolve-only", vbody(f.owned, tradeToken)); w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("context:resolve alone must not validate: %d %s", w.Code, w.Body.String())
	}
	raw := `{"context_id":"` + f.owned + `","capability_key":"commerce.order.create","correlation_id":"0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a99"}`
	if w := f.postTo("/v1/capabilities/resolve", validatorBearer, raw); w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("context:validate alone must not resolve: %d %s", w.Code, w.Body.String())
	}
}

func TestValidateRequiresARegisteredValidationTarget(t *testing.T) {
	f := newValidateFixture(t, nil)
	f.expect(f.post("unregistered", vbody(f.owned, tradeToken)), http.StatusForbidden, "CONTEXT_VALIDATION_NOT_PERMITTED")
}

func TestValidateIsUnavailableWhenTheIssuerIsUnreachable(t *testing.T) {
	f := newValidateFixture(t, func(d *Dependencies) { d.SubjectVerifiers = audienceTokens{unavailable: true} })
	// An unreachable issuer must not read as "this caller does not own it".
	f.expect(f.post(validatorBearer, vbody(f.owned, tradeToken)), http.StatusServiceUnavailable, "CONTEXT_VALIDATION_UNAVAILABLE")
}

// The route does not exist unless it can be served safely.
func TestValidateRouteIsAbsentWithoutItsDependencies(t *testing.T) {
	f := newValidateFixture(t, func(d *Dependencies) { d.SubjectVerifiers = nil })
	if w := f.post(validatorBearer, vbody(f.owned, tradeToken)); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// subject_token is a bearer credential: it appears in no log record, and the
// audit record names the parties and the decision.
func TestValidateNeverLogsTheSubjectToken(t *testing.T) {
	f := newValidateFixture(t, nil)
	f.post(validatorBearer, vbody(f.owned, tradeToken))
	f.post(validatorBearer, vbody(f.owned, "garbage-token.header.signature-0001"))
	f.post(validatorBearer, vbody("00000000-0000-4000-8000-0000000000a1", tradeToken))
	logged := f.log.String()
	for _, secret := range []string{tradeToken, "garbage-token.header.signature-0001", validatorBearer} {
		if strings.Contains(logged, secret) {
			t.Fatalf("a credential reached the log: %s", logged)
		}
	}
	for _, want := range []string{"context validation", f.owned, "tn_validate", "VALID", "DENIED", "owner_confirmed", "subject_not_owner", f.trade} {
		if !strings.Contains(logged, want) {
			t.Fatalf("audit record lacks %q:\n%s", want, logged)
		}
	}
}
