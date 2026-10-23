package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

// Synthetic only: no real server application, estate, Tenant, Market or
// token. This proves CP's boundary, not connected IAM acceptance or a
// registered assignment.
const (
	assignmentID     = "4b3a2c1d-0e9f-4a8b-9c7d-6e5f4a3b2c1d"
	assignmentServer = "synthetic-server-checkout"
	assignmentRef    = "trade/native-seller-configuration/synthetic-01"
	assignmentDigest = "5d41402abc4b2a76b9719d911017c5925d41402abc4b2a76b9719d911017c592"
	assignmentMarket = "market_synthetic"
	assignmentCaller = "synthetic-trade-workload"
)

type assignmentRegistry struct{ allowed bool }

func (r assignmentRegistry) IsActive(string) bool { return r.allowed }
func (r assignmentRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == assignmentCaller && scope == estateAssignmentResolveScope
}

type assignmentStore struct {
	assignments []postgres.EstateResourceAssignment
	attested    bool
	err         error
	failRec     bool
	records     []postgres.EstateResourceAssignmentResolution
	asked       []string
}

func (s *assignmentStore) ProposeEstateResourceAssignment(context.Context, string, basestore.RequestMetadata, string,
	postgres.EstateResourceAssignmentProposal) (postgres.EstateResourceAssignment, error) {
	return postgres.EstateResourceAssignment{}, errors.New("unused")
}
func (s *assignmentStore) TransitionEstateResourceAssignment(_ context.Context, _ string, meta basestore.RequestMetadata,
	actor, id string, t postgres.EstateResourceAssignmentTransition) (postgres.EstateResourceAssignment, error) {
	s.asked = append(s.asked, "transition|"+actor+"|"+meta.ActorID+"|"+id+"|"+t.Action)
	a := activeAssignment(nil)
	a.Revision = t.ExpectedRevision + 1
	return a, nil
}
func (s *assignmentStore) EstateResourceAssignment(context.Context, string) (postgres.EstateResourceAssignment, error) {
	return activeAssignment(nil), nil
}
func (s *assignmentStore) EstateResourceAssignmentCandidates(_ context.Context, scope, subject, estate string) (
	[]postgres.EstateResourceAssignment, error) {
	s.asked = append(s.asked, scope+"|"+subject+"|"+estate)
	var out []postgres.EstateResourceAssignment
	for _, a := range s.assignments {
		if a.Scope == scope && a.DigitalEstateID == estate &&
			(a.WorkloadClientID == subject || a.NativeConfigurationReference == subject) {
			out = append(out, a)
		}
	}
	return out, s.err
}
func (s *assignmentStore) NativeConfigurationAttested(_ context.Context, caller, engine, reference, digest string) (bool, error) {
	s.asked = append(s.asked, "native|"+caller+"|"+engine+"|"+reference+"|"+digest)
	return s.attested, s.err
}
func (s *assignmentStore) RecordEstateResourceAssignmentResolution(_ context.Context, r postgres.EstateResourceAssignmentResolution) error {
	if s.failRec {
		return errors.New("record unavailable")
	}
	s.records = append(s.records, r)
	return nil
}

func activeAssignment(change func(*postgres.EstateResourceAssignment)) postgres.EstateResourceAssignment {
	a := postgres.EstateResourceAssignment{AssignmentID: assignmentID, Scope: "WORKLOAD", WorkloadClientID: assignmentServer,
		DigitalEstateID: estateID, TenantID: estateTenant, TradeEngineInstanceID: estateEngine,
		MarketScope: []string{assignmentMarket}, PermittedOperations: []string{"trade.order.commit"}, Status: "ACTIVE",
		Revision: 2, EffectiveFrom: time.Now().Add(-time.Hour), ProposedBy: "0d9c8b7a-6f5e-4d3c-8b2a-1f0e9d8c7b6a",
		ApprovedBy: "7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d", ProposalReference: "changeset:synthetic-assign-0001",
		ApprovalReference: "approval:synthetic-assign-0001", EvidenceReference: "changeset:synthetic-assign-0001"}
	if change != nil {
		change(&a)
	}
	return a
}

func nativeAssignment(change func(*postgres.EstateResourceAssignment)) postgres.EstateResourceAssignment {
	return activeAssignment(func(a *postgres.EstateResourceAssignment) {
		a.Scope, a.WorkloadClientID = "NATIVE_RESOURCE", ""
		a.NativeConfigurationReference, a.NativeConfigurationDigest = assignmentRef, assignmentDigest
		a.NativeResource = &postgres.NativeRef{Reference: assignmentRef, Digest: assignmentDigest}
		if change != nil {
			change(a)
		}
	})
}

type assignmentFixture struct {
	store        *assignmentStore
	subjects     *resolveSubjects
	introspector *resolveIntrospector
	caller       auth.Principal
	registry     assignmentRegistry
	environment  string
	enabled      bool
}

func newAssignmentFixture() *assignmentFixture {
	exp := time.Now().Add(5 * time.Minute).UTC()
	return &assignmentFixture{
		store:       &assignmentStore{assignments: []postgres.EstateResourceAssignment{activeAssignment(nil), nativeAssignment(nil)}, attested: true},
		environment: "staging", enabled: true, registry: assignmentRegistry{allowed: true},
		caller: auth.Principal{Issuer: resolveIssuer, Subject: assignmentCaller, ActorType: "workload", ClientID: assignmentCaller,
			TokenID: "workload-token", Scopes: map[string]struct{}{estateAssignmentResolveScope: {}}},
		subjects: &resolveSubjects{verifier: fakeVerifier{principal: auth.Principal{Issuer: resolveIssuer, Subject: assignmentServer,
			ActorType: "workload", ClientID: assignmentServer, TokenID: "server-token", ExpiresAt: exp,
			Scopes: map[string]struct{}{nativeCommitmentSubmitScope: {}}}}},
		introspector: &resolveIntrospector{result: auth.Introspection{Active: true, Issuer: resolveIssuer, Subject: assignmentServer,
			ClientID: assignmentServer, Audience: []string{buyerSubjectAudience},
			Scopes: map[string]struct{}{nativeCommitmentSubmitScope: {}}, ExpiresAt: exp}},
	}
}

func (f *assignmentFixture) server(change func(*auth.Principal)) {
	p := f.subjects.verifier.(fakeVerifier).principal
	change(&p)
	f.subjects.verifier = fakeVerifier{principal: p}
}

func workloadBody(change map[string]any) map[string]any {
	body := map[string]any{"scope": "WORKLOAD", "workload_evidence": resolveToken, "workload_evidence_type": accessTokenType,
		"digital_estate_id": estateID, "engine_instance_id": estateEngine, "market_id": assignmentMarket,
		"operation": "trade.order.commit", "correlation_id": resolveCorrelation}
	for k, v := range change {
		if v == nil {
			delete(body, k)
		} else {
			body[k] = v
		}
	}
	return body
}

func nativeBody(change map[string]any) map[string]any {
	body := workloadBody(map[string]any{"scope": "NATIVE_RESOURCE", "workload_evidence": nil, "workload_evidence_type": nil,
		"native_resource": map[string]any{"native_configuration_reference": assignmentRef, "native_configuration_digest": assignmentDigest}})
	for k, v := range change {
		body[k] = v
	}
	return body
}

func (f *assignmentFixture) post(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	handler := New(Dependencies{Store: &fakeStore{}, Environment: f.environment,
		WorkloadVerifier: fakeVerifier{principal: f.caller}, WorkloadRegistry: f.registry,
		EstateResourceAssignmentEnabled: f.enabled, EstateResourceAssignments: f.store,
		BuyerSubjects: f.subjects, BuyerSubjectIntrospector: f.introspector})
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/estate/v1/resource-assignments/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", resolveCorrelation)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func attestationOf(t *testing.T, res *httptest.ResponseRecorder) assignmentAttestation {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	if err := contracts.Validate(assignmentAttestationSchema, res.Body.Bytes()); err != nil {
		t.Fatalf("attestation does not match the Shared contract: %v", err)
	}
	var out assignmentAttestation
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEstateResourceAssignmentAttestsAVerifiedServerApplication(t *testing.T) {
	f := newAssignmentFixture()
	out := attestationOf(t, f.post(t, workloadBody(nil)))
	if out.Scope != "WORKLOAD" || out.WorkloadClientID != assignmentServer || out.AssignmentRevision != 2 ||
		out.TenantID != estateTenant || out.Operation != "trade.order.commit" || out.MarketID != assignmentMarket {
		t.Fatalf("attestation = %+v", out)
	}
	if out.ExpiresAt.Sub(out.IssuedAt) > assignmentAttestationTTL {
		t.Fatalf("lifetime %s exceeds the contract", out.ExpiresAt.Sub(out.IssuedAt))
	}
	if f.introspector.calls != 1 || f.subjects.audience != buyerSubjectAudience {
		t.Fatalf("evidence was not verified and introspected for the Trade audience")
	}
	if len(f.store.records) != 1 || f.store.records[0].Outcome != "ATTESTED" || f.store.records[0].AttestationID != out.AttestationID ||
		f.store.records[0].SubjectReference != assignmentServer {
		t.Fatalf("records = %+v", f.store.records)
	}
}

func TestEstateResourceAssignmentAttestsAnAttestedNativeConfiguration(t *testing.T) {
	f := newAssignmentFixture()
	out := attestationOf(t, f.post(t, nativeBody(nil)))
	if out.Scope != "NATIVE_RESOURCE" || out.WorkloadClientID != "" {
		t.Fatalf("attestation = %+v", out)
	}
	want := "native|" + assignmentCaller + "|" + estateEngine + "|" + assignmentRef + "|" + assignmentDigest
	if f.store.asked[0] != want {
		t.Fatalf("native configuration was checked as %q, want %q", f.store.asked[0], want)
	}
	if f.introspector.calls != 0 {
		t.Fatal("a native resource request introspected workload evidence")
	}
}

func TestEstateResourceAssignmentNeverAttestsOutlivedEvidence(t *testing.T) {
	f := newAssignmentFixture()
	soon := time.Now().Add(20 * time.Second).UTC()
	f.server(func(p *auth.Principal) { p.ExpiresAt = soon })
	out := attestationOf(t, f.post(t, workloadBody(nil)))
	if out.ExpiresAt.After(soon) {
		t.Fatalf("attestation %s outlives the server evidence %s", out.ExpiresAt, soon)
	}
	until := time.Now().Add(15 * time.Second)
	f = newAssignmentFixture()
	f.store.assignments = []postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.EffectiveUntil = &until })}
	out = attestationOf(t, f.post(t, workloadBody(nil)))
	if out.AssignmentEffectiveUntil == nil || out.ExpiresAt.After(until) {
		t.Fatalf("attestation %s outlives the assignment end %v", out.ExpiresAt, out.AssignmentEffectiveUntil)
	}
}

func TestEstateResourceAssignmentRefusesUnverifiedSubjects(t *testing.T) {
	cases := map[string]struct {
		fixture func(*assignmentFixture)
		body    map[string]any
		status  int
		code    string
	}{
		"evidence without the submit scope": {func(f *assignmentFixture) {
			f.server(func(p *auth.Principal) { p.Scopes = map[string]struct{}{bffHandoffScope: {}} })
		}, nil, http.StatusForbidden, "WORKLOAD_EVIDENCE_INVALID"},
		"human evidence": {func(f *assignmentFixture) { f.server(func(p *auth.Principal) { p.ActorType = "human" }) }, nil, http.StatusForbidden, "WORKLOAD_EVIDENCE_INVALID"},
		"expired evidence": {func(f *assignmentFixture) {
			f.server(func(p *auth.Principal) { p.ExpiresAt = time.Now().Add(-time.Second) })
		}, nil, http.StatusForbidden, "WORKLOAD_EVIDENCE_INVALID"},
		"forged evidence":  {func(f *assignmentFixture) { f.subjects.verifier = fakeVerifier{err: errors.New("bad signature")} }, nil, http.StatusForbidden, "WORKLOAD_EVIDENCE_INVALID"},
		"revoked evidence": {func(f *assignmentFixture) { f.introspector.result.Active = false }, nil, http.StatusForbidden, "WORKLOAD_EVIDENCE_INVALID"},
		"introspection for another client": {func(f *assignmentFixture) { f.introspector.result.ClientID = "synthetic-other" }, nil,
			http.StatusForbidden, "WORKLOAD_EVIDENCE_INVALID"},
		"introspection outage":         {func(f *assignmentFixture) { f.introspector.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"issuer outage":                {func(f *assignmentFixture) { f.subjects.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"native configuration drifted": {func(f *assignmentFixture) { f.store.attested = false }, nativeBody(nil), http.StatusForbidden, "NATIVE_RESOURCE_NOT_ATTESTED"},
		"another digest of the assigned configuration": {func(f *assignmentFixture) {}, nativeBody(map[string]any{"native_resource": map[string]any{
			"native_configuration_reference": assignmentRef, "native_configuration_digest": "0" + assignmentDigest[1:]}}),
			http.StatusNotFound, "ASSIGNMENT_NOT_FOUND"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAssignmentFixture()
			tc.fixture(f)
			body := tc.body
			if body == nil {
				body = workloadBody(nil)
			}
			res := f.post(t, body)
			if res.Code != tc.status || problemCode(t, res) != tc.code {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
		})
	}
}

func TestEstateResourceAssignmentRefusesAnythingButExactlyOneActiveAssignment(t *testing.T) {
	later := time.Now().Add(time.Hour)
	cases := map[string]struct {
		assignments []postgres.EstateResourceAssignment
		status      int
		code        string
	}{
		"none":         {nil, http.StatusNotFound, "ASSIGNMENT_NOT_FOUND"},
		"other estate": {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.DigitalEstateID = "estate_other" })}, http.StatusNotFound, "ASSIGNMENT_NOT_FOUND"},
		"other engine": {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.TradeEngineInstanceID = "ei_other01" })}, http.StatusForbidden, "ENGINE_INSTANCE_NOT_PERMITTED"},
		"proposed":     {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.Status = "PROPOSED" })}, http.StatusForbidden, "ASSIGNMENT_NOT_ACTIVE"},
		"suspended":    {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.Status = "SUSPENDED" })}, http.StatusForbidden, "ASSIGNMENT_NOT_ACTIVE"},
		"revoked":      {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.Status = "REVOKED" })}, http.StatusForbidden, "ASSIGNMENT_NOT_ACTIVE"},
		"not yet":      {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.EffectiveFrom = later })}, http.StatusForbidden, "ASSIGNMENT_NOT_ACTIVE"},
		"other market": {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) { a.MarketScope = []string{"market_other"} })}, http.StatusForbidden, "MARKET_NOT_PERMITTED"},
		"other operation": {[]postgres.EstateResourceAssignment{activeAssignment(func(a *postgres.EstateResourceAssignment) {
			a.PermittedOperations = []string{"trade.draft-order.finalize"}
		})}, http.StatusForbidden, "OPERATION_NOT_PERMITTED"},
		"two active": {[]postgres.EstateResourceAssignment{activeAssignment(nil), activeAssignment(func(a *postgres.EstateResourceAssignment) {
			a.AssignmentID = "1f2e3d4c-5b6a-4978-8a9b-0c1d2e3f4a5b"
		})}, http.StatusConflict, "ASSIGNMENT_AMBIGUOUS"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAssignmentFixture()
			f.store.assignments = tc.assignments
			res := f.post(t, workloadBody(nil))
			if res.Code != tc.status || problemCode(t, res) != tc.code {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
			if len(f.store.records) != 1 || f.store.records[0].Outcome != tc.code || f.store.records[0].AssignmentID != "" {
				t.Fatalf("denial record = %+v", f.store.records)
			}
		})
	}
}

func TestEstateResourceAssignmentCallerAndRequestBoundary(t *testing.T) {
	cases := map[string]struct {
		fixture func(*assignmentFixture)
		body    map[string]any
		status  int
		code    string
	}{
		"inactive workload":        {func(f *assignmentFixture) { f.registry.allowed = false }, nil, http.StatusForbidden, ""},
		"human caller":             {func(f *assignmentFixture) { f.caller.ActorType = "human" }, nil, http.StatusForbidden, ""},
		"asserted tenant":          {nil, workloadBody(map[string]any{"tenant_id": estateTenant}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"asserted assignment":      {nil, workloadBody(map[string]any{"assignment_id": assignmentID}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"medusa key as evidence":   {nil, workloadBody(map[string]any{"publishable_key": "pk_synthetic"}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"non-commitment operation": {nil, workloadBody(map[string]any{"operation": "trade.cart.modify"}), http.StatusBadRequest, "VALIDATION_FAILED"},
		"workload without evidence": {nil, workloadBody(map[string]any{"workload_evidence": nil}), http.StatusBadRequest,
			"VALIDATION_FAILED"},
		"native with evidence": {nil, nativeBody(map[string]any{"workload_evidence": resolveToken}), http.StatusBadRequest,
			"VALIDATION_FAILED"},
		"store outage":           {func(f *assignmentFixture) { f.store.err = errors.New("down") }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
		"unrecorded attestation": {func(f *assignmentFixture) { f.store.failRec = true }, nil, http.StatusServiceUnavailable, "RESOLUTION_UNAVAILABLE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAssignmentFixture()
			if tc.fixture != nil {
				tc.fixture(f)
			}
			body := tc.body
			if body == nil {
				body = workloadBody(nil)
			}
			res := f.post(t, body)
			if res.Code != tc.status || (tc.code != "" && problemCode(t, res) != tc.code) {
				t.Fatalf("status = %d code = %q, want %d %s: %s", res.Code, problemCode(t, res), tc.status, tc.code, res.Body.String())
			}
			if bytes.Contains(res.Body.Bytes(), []byte(resolveToken)) {
				t.Fatal("the workload evidence was echoed")
			}
		})
	}
}

func TestEstateResourceAssignmentRoutesAreDefaultOffAndNonproduction(t *testing.T) {
	for name, change := range map[string]func(*assignmentFixture){
		"disabled":    func(f *assignmentFixture) { f.enabled = false },
		"production":  func(f *assignmentFixture) { f.environment = "production" },
		"integration": func(f *assignmentFixture) { f.environment = "integration" },
		"unset":       func(f *assignmentFixture) { f.environment = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newAssignmentFixture()
			change(f)
			if res := f.post(t, workloadBody(nil)); res.Code != http.StatusNotFound && res.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d: route mounted", res.Code)
			}
		})
	}
}

type assignmentIdentities struct{ principalID string }

func (i assignmentIdentities) ResolveIdentity(context.Context, string, string) (domain.Principal, error) {
	return domain.Principal{ID: i.principalID, ActorType: "human", Status: "ACTIVE"}, nil
}
func (i assignmentIdentities) GetPrincipal(_ context.Context, id string) (domain.Principal, error) {
	return domain.Principal{ID: id, ActorType: "human", Status: "ACTIVE"}, nil
}
func (assignmentIdentities) CreateIdentity(context.Context, domain.Principal) error {
	return errors.New("write forbidden in assignment test")
}
func (assignmentIdentities) LinkExternalIdentity(context.Context, domain.ExternalIdentity) error {
	return errors.New("write forbidden in assignment test")
}

// An approval needs the approve scope and a suspension the write scope; the
// actor is always the caller's canonical Principal, never a body field.
func TestEstateResourceAssignmentTransitionsNeedTheirOwnScope(t *testing.T) {
	const actor = "7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d"
	cases := []struct {
		action string
		scopes []string
		status int
	}{
		{"APPROVE", []string{estateAssignmentWriteScope}, http.StatusForbidden},
		{"APPROVE", []string{estateAssignmentApproveScope}, http.StatusOK},
		{"REINSTATE", []string{estateAssignmentWriteScope}, http.StatusForbidden},
		{"SUSPEND", []string{estateAssignmentApproveScope}, http.StatusForbidden},
		{"SUSPEND", []string{estateAssignmentWriteScope}, http.StatusOK},
		{"REVOKE", []string{estateAssignmentWriteScope}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.action+"/"+tc.scopes[0], func(t *testing.T) {
			store := &assignmentStore{}
			h := estateResourceAssignmentHandler{api: &API{identities: assignmentIdentities{principalID: actor}}, store: store}
			scopes := map[string]struct{}{}
			for _, s := range tc.scopes {
				scopes[s] = struct{}{}
			}
			body, _ := json.Marshal(map[string]any{"action": tc.action, "expected_revision": 1, "reason": "synthetic",
				"evidence_reference": "changeset:synthetic"})
			req := httptest.NewRequest(http.MethodPost, "/v1/estate/resource-assignments/"+assignmentID+"/transitions",
				bytes.NewReader(body))
			req.Header.Set("Idempotency-Key", "synthetic-key-0000001")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("assignmentID", assignmentID)
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = auth.WithPrincipal(ctx, auth.Principal{Issuer: resolveIssuer, Subject: "admin-subject", ActorType: "human", Scopes: scopes})
			res := httptest.NewRecorder()
			h.transition(res, req.WithContext(ctx))
			if res.Code != tc.status {
				t.Fatalf("status = %d: %s", res.Code, res.Body.String())
			}
			if tc.status == http.StatusOK {
				want := "transition|" + actor + "|" + actor + "|" + assignmentID + "|" + tc.action
				if len(store.asked) != 1 || store.asked[0] != want {
					t.Fatalf("store saw %v, want %s", store.asked, want)
				}
				if err := contracts.Validate(assignmentRecordSchema, res.Body.Bytes()); err != nil {
					t.Fatalf("response does not match the Shared contract: %v", err)
				}
			} else if len(store.asked) != 0 {
				t.Fatal("a refused transition reached the store")
			}
		})
	}
}
