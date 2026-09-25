package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func validEnsure() EnsureRequest {
	return EnsureRequest{TenantID: "tn_01a0d8f848e172df8e58", ProductSubscriptionID: "sub_2bcdbc23638a49c69e9a9cb5c83c7818",
		AuthoritativeRevision: 2, ProductID: "zuritrade", SubscriptionType: "INTERNAL",
		Classification: Classification{ClassificationID: "subcls_2bcdbc23638a49c69e9a9cb5c83c7818", ClassificationSource: "ADMISSION_DECISION",
			ClassificationReference: "adm_2bcdbc23638a49c69e9a9cb5c83c7818", ClassifiedAt: time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC)}}
}

func TestFileTokenSourceReadsTheProjectedToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if _, err := (FileTokenSource{Path: path}).Token(context.Background()); err == nil {
		t.Fatal("a missing token file fails closed")
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileTokenSource{Path: path}).Token(context.Background()); err == nil {
		t.Fatal("an empty token file fails closed")
	}
	if err := os.WriteFile(path, []byte("rotated-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, err := (FileTokenSource{Path: path}).Token(context.Background()); err != nil || tok != "rotated-token" {
		t.Fatalf("token: %q %v", tok, err)
	}
}

func TestClientRefusesWhatTheContractRefuses(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"billing_subscription_id":"bsub_1"}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Tokens: staticToken("t")}
	ctx := context.Background()

	if _, err := c.Ensure(ctx, validEnsure(), "short", ""); err == nil {
		t.Fatal("an invalid idempotency key is refused before any call")
	}
	bad := validEnsure()
	bad.AuthoritativeRevision = 0
	if _, err := c.Ensure(ctx, bad, "cp:0123456789abcdef:r0", ""); err == nil || !strings.Contains(err.Error(), "subscriptions/v1") {
		t.Fatalf("a request that does not conform is never sent: %v", err)
	}
	if calls != 0 {
		t.Fatalf("nothing reached the engine: %d calls", calls)
	}
	if _, err := c.Ensure(ctx, validEnsure(), "cp:0123456789abcdef:r2", ""); err == nil || !strings.Contains(err.Error(), "response does not conform") {
		t.Fatalf("a response that does not conform is refused: %v", err)
	}
}

func TestClientReportsBoundedProblems(t *testing.T) {
	for _, tc := range []struct {
		status    int
		body      string
		code      string
		retryable bool
	}{
		{409, `{"code":"STALE_AUTHORITATIVE_REVISION","retryable":false,"detail":"card 4111111111111111"}`, "STALE_AUTHORITATIVE_REVISION", false},
		{503, `not json`, "BILLING_ENGINE_ERROR", true},
		{400, `{"code":"lower case is not a code"}`, "BILLING_ENGINE_ERROR", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer t" || r.Header.Get("Idempotency-Key") == "" {
				t.Errorf("workload token and idempotency key are sent")
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := (&Client{BaseURL: srv.URL, Tokens: staticToken("t")}).Ensure(context.Background(), validEnsure(), "cp:0123456789abcdef:r2", "")
		srv.Close()
		var p *Problem
		if !errors.As(err, &p) || p.Status != tc.status || p.Code != tc.code || p.Retryable != tc.retryable {
			t.Fatalf("%d %s: %v", tc.status, tc.body, err)
		}
		if strings.Contains(err.Error(), "4111") {
			t.Fatal("problem details are never echoed")
		}
	}
}

func TestBackoffIsBounded(t *testing.T) {
	if backoff(1) != 30*time.Second || backoff(2) != time.Minute || backoff(50) != time.Hour {
		t.Fatalf("backoff: %v %v %v", backoff(1), backoff(2), backoff(50))
	}
}

func TestRegistrationsAreValidated(t *testing.T) {
	if _, err := ParseRegistration([]byte(`{"repository":"x"}`)); err == nil {
		t.Fatal("a registration that does not conform is refused")
	}
	simulatedInProduction := `{"repository":"baobab-subscriptions","capabilities":[{"capability_key":"billing.subscription.manage","name":"n",
		"domain":"billing","owner":"baobab-subscriptions","lifecycle":"ACTIVE","maturity":"EXPERIMENTAL","contracts":[{"major":1,
		"request_schema":"billing.schema.json#/$defs/EnsureBillingProjectionRequest"}]}],
		"provider":{"provider_key":"baobab-subscriptions.temporary-billing","name":"n","provider_type":"BAOBAB_ENGINE","engine_key":"temporary-billing",
		"lifecycle":"ACTIVE","ownership":"baobab-subscriptions","simulated":true,"production_permitted":true},
		"support":[{"capability_key":"billing.subscription.manage","contract_versions":[1]}]}`
	if _, err := ParseRegistration([]byte(simulatedInProduction)); err == nil {
		t.Fatal("a simulated provider can never be permitted in production")
	}
}
