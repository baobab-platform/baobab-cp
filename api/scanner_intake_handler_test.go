package api

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
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type scannerRegistryFixture struct{ active bool }

func (s scannerRegistryFixture) IsActive(client string) bool {
	return s.active && client == authority.ScannerClientID
}
func (s scannerRegistryFixture) AllowsScope(client, scope string) bool {
	return s.IsActive(client) && scope == authority.ScannerScope
}

type scannerVerifierFixture struct {
	p           auth.Principal
	calls       int
	revokeAfter int
}

func (s *scannerVerifierFixture) Verify(context.Context, string) (auth.Principal, error) {
	s.calls++
	if s.revokeAfter > 0 && s.calls > s.revokeAfter {
		return auth.Principal{}, errors.New("synthetic revoked token")
	}
	return s.p, nil
}

type scannerServiceFixture struct{ calls, writes int }

func (s *scannerServiceFixture) Receive(ctx context.Context, in authority.ScanSubmission, actor repository.AuditActor, current func(context.Context) error) (authority.ScanReceipt, error) {
	s.calls++
	if current(ctx) != nil || current(ctx) != nil {
		return authority.ScanReceipt{}, authority.ErrScannerIntake
	}
	s.writes++
	return authority.ScanReceipt{CaseID: in.CaseID, ClaimID: in.ClaimID, EvidenceID: in.Proof.EvidenceID, ArtifactID: in.Proof.ArtifactID, Digest: in.Proof.Digest, Standing: "SCAN_PROOF_RECORDED"}, nil
}

func scannerInput() string {
	raw, _ := json.Marshal(authority.ScanSubmission{CaseID: "vcase_synthetic", ClaimID: "ecl_synthetic", Document: []byte("%PDF-1.7\nsynthetic"),
		Proof: authority.ScanProof{EvidenceID: "evr_synthetic", ArtifactID: "eart_synthetic", MediaType: "application/pdf", Digest: "sha256:" + strings.Repeat("a", 64),
			ScannerID: "synthetic-av", ScannerVersion: "v1", Outcome: "CLEAN", ScannedAt: time.Now().UTC().Truncate(time.Microsecond), Signature: make([]byte, 64)}})
	return string(raw)
}

func TestScannerPrivateRouteDefaultOffAndRequestTimeGuards(t *testing.T) {
	for _, tc := range []struct {
		name, environment, client, actor, scope string
		enabled, registry                       bool
		revoke                                  int
		mutate                                  func(string) string
		want                                    int
	}{
		{name: "signed scanner", environment: "integration", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, want: 201},
		{name: "default off", environment: "integration", registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, want: 404},
		{name: "production", environment: "production", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, want: 404},
		{name: "unknown environment", environment: "", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, want: 404},
		{name: "no registry", environment: "integration", enabled: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, want: 404},
		{name: "extractor", environment: "integration", enabled: true, registry: true, client: "baobab-document-extractor-staging", actor: "workload", scope: authority.ScannerScope, want: 403},
		{name: "human", environment: "integration", enabled: true, registry: true, client: authority.ScannerClientID, actor: "human", scope: authority.ScannerScope, want: 403},
		{name: "wrong scope", environment: "integration", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: "verification:decide", want: 403},
		{name: "revoked before custody", environment: "integration", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, revoke: 1, want: 503},
		{name: "body privilege injection", environment: "integration", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, mutate: func(s string) string { return strings.Replace(s, `"claim_id":`, `"actor_id":"forged","claim_id":`, 1) }, want: 400},
		{name: "unknown signer key", environment: "integration", enabled: true, registry: true, client: authority.ScannerClientID, actor: "workload", scope: authority.ScannerScope, mutate: func(s string) string { return strings.Replace(s, `"proof":{`, `"proof":{"public_key":"forged",`, 1) }, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &scannerServiceFixture{}
			verifier := &scannerVerifierFixture{p: auth.Principal{Issuer: "https://synthetic.example.invalid", Subject: "synthetic-scanner", ClientID: tc.client, ActorType: tc.actor, TokenID: "synthetic-jti", ExpiresAt: time.Now().Add(time.Minute), Scopes: map[string]struct{}{tc.scope: {}}}, revokeAfter: tc.revoke}
			deps := Dependencies{Store: &fakeStore{}, Environment: tc.environment, ScannerIntakeEnabled: tc.enabled, ScannerIntake: service, ScannerVerifier: verifier}
			if tc.registry {
				deps.WorkloadRegistry = scannerRegistryFixture{active: true}
			}
			body := scannerInput()
			if tc.mutate != nil {
				body = tc.mutate(body)
			}
			req := httptest.NewRequest(http.MethodPost, authority.ScannerIntakePath, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer synthetic-token")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			New(deps).ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("want %d got %d: %s", tc.want, w.Code, w.Body.String())
			}
			if tc.want != 201 && service.writes != 0 {
				t.Fatal("denied request reached custody")
			}
			if tc.want == 201 && (service.writes != 1 || strings.Contains(w.Body.String(), "document_base64") || strings.Contains(w.Body.String(), "signature_base64") || !strings.Contains(w.Body.String(), `"authority_granted":false`)) {
				t.Fatal("response leaked payload or granted authority")
			}
			if w.Header().Get("Cache-Control") == "" {
				t.Fatal("private response lacked no-store policy")
			}
		})
	}
}
