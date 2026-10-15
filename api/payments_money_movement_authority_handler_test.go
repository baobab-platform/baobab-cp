package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

const paymentsMoneyMovementClient = "baobab-payments-legal-actor-assessor-staging"

type paymentsMoneyMovementRegistry struct {
	allowed        bool
	withoutContext bool
}

func (r paymentsMoneyMovementRegistry) IsActive(string) bool { return r.allowed }
func (r paymentsMoneyMovementRegistry) AllowsScope(clientID, scope string) bool {
	return r.allowed && clientID == paymentsMoneyMovementClient &&
		(scope == paymentsMoneyMovementScope || (scope == "context:resolve" && !r.withoutContext))
}

type paymentsMoneyMovementStoreFixture struct {
	bound      bool
	boundErr   error
	resolution legalactor.Resolution
	recordErr  error
	requests   []legalactor.Request
	bindings   []string
	recorded   []postgres.PaymentsMoneyMovementDecision
}

func (f *paymentsMoneyMovementStoreFixture) ResolveOperatingLegalActor(_ context.Context, r legalactor.Request) (legalactor.Resolution, error) {
	f.requests = append(f.requests, r)
	return f.resolution, nil
}

func (f *paymentsMoneyMovementStoreFixture) PaymentsEngineInstanceBound(_ context.Context, tenant, instance string, _ time.Time) (bool, error) {
	f.bindings = append(f.bindings, tenant+"|"+instance)
	return f.bound, f.boundErr
}

func (f *paymentsMoneyMovementStoreFixture) RecordPaymentsMoneyMovementDecision(_ context.Context, d postgres.PaymentsMoneyMovementDecision) (string, error) {
	if f.recordErr != nil {
		return "", f.recordErr
	}
	f.recorded = append(f.recorded, d)
	return "a3e9c7d1-5b2f-4e6a-8c1d-9f0b7e3a2c55", nil
}

type paymentsMoneyMovementFixture struct {
	handler http.Handler
	store   *paymentsMoneyMovementStoreFixture
	context domain.Context
}

type paymentsMoneyMovementOptions struct {
	allowlist     []string
	registered    bool
	tenantState   string
	contextExpiry time.Duration
	owned         bool
	environment   string
	enabled       bool
	// The registry no longer allows context:resolve, though the token still carries it.
	contextRevoked bool
}

func defaultPaymentsMoneyMovementOptions() paymentsMoneyMovementOptions {
	return paymentsMoneyMovementOptions{allowlist: []string{paymentsMoneyMovementClient}, registered: true,
		tenantState: "active", contextExpiry: time.Minute, owned: true, environment: "integration", enabled: true}
}

func newPaymentsMoneyMovementFixture(t *testing.T, store *paymentsMoneyMovementStoreFixture, o paymentsMoneyMovementOptions) paymentsMoneyMovementFixture {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "workload", Status: "ACTIVE"}
	if err := repo.CreateIdentity(context.Background(), principal); err != nil {
		t.Fatal(err)
	}
	issuer, subject := "https://iam.example.test", paymentsMoneyMovementClient
	if err := repo.LinkExternalIdentity(context.Background(), domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID, Issuer: issuer,
		Subject: subject, Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expiry := now.Add(o.contextExpiry)
	ctx := domain.Context{ID: domain.NewUUIDv7(), PrincipalID: principal.ID,
		TenantID: "tn_synthetic01", OrganisationID: "2c8e4f6a-1b3d-4e5f-8a7c-9d0e1f2a3b4c", CountryCode: "UG",
		MarketID: "market-ug", CorrelationID: domain.NewUUIDv7(), ResolvedAt: now.Add(-time.Second), ExpiresAt: &expiry}
	if !o.owned {
		ctx.PrincipalID = domain.NewPrincipalID()
	}
	repo.Contexts[ctx.ID] = ctx
	handler := New(Dependencies{
		Store:       &fakeStore{tenant: domain.Tenant{TenantID: ctx.TenantID, ObservedState: o.tenantState}},
		Environment: o.environment, PaymentsMoneyMovementAuthorityEnabled: o.enabled,
		PaymentsMoneyMovementAuthority: store, PaymentsMoneyMovementAuthorityClientIDs: o.allowlist,
		WorkloadVerifier: fakeVerifier{principal: auth.Principal{
			Issuer: issuer, Subject: subject, ActorType: "workload", ClientID: paymentsMoneyMovementClient,
			TenantID: ctx.TenantID, TokenID: "token-payments-money-movement",
			Scopes: map[string]struct{}{"context:resolve": {}, paymentsMoneyMovementScope: {}}}},
		WorkloadRegistry: paymentsMoneyMovementRegistry{allowed: o.registered, withoutContext: o.contextRevoked},
		Contexts:         repo, Identities: repo,
	})
	return paymentsMoneyMovementFixture{handler: handler, store: store, context: ctx}
}

type fingerprintVector struct {
	OperationClass         string              `json:"operation_class"`
	OperationReference     string              `json:"operation_reference"`
	Instruction            paymentsInstruction `json:"instruction"`
	Canonical              string              `json:"canonical"`
	InstructionFingerprint string              `json:"instruction_fingerprint"`
}

// The Shared golden vectors are not embedded; read them from the pinned
// checkout when CI provides one, otherwise from the sibling clone.
func paymentsFingerprintVectors(t *testing.T) []fingerprintVector {
	t.Helper()
	dir := os.Getenv("SHARED_CONTRACTS_DIR")
	if dir == "" {
		dir = "../../shared"
	}
	raw, err := os.ReadFile(filepath.Join(dir, "contracts/payments/v2/examples/fingerprint-vectors.json"))
	if err != nil && os.Getenv("SHARED_CONTRACTS_DIR") != "" {
		t.Fatalf("pinned Shared checkout lacks the fingerprint vectors: %v", err)
	}
	if err != nil {
		t.Skipf("Shared checkout unavailable: %v", err)
	}
	var doc struct {
		Vectors []fingerprintVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Vectors
}

// captureVector is the Shared capture vector, whose tenant, Organisation and
// market match the fixture context.
func captureVector() fingerprintVector {
	parent := "pay_01J9Z8M4"
	return fingerprintVector{
		OperationClass:     "payments.capture.execute",
		OperationReference: "payments/capture/pay_01J9Z8M4/key-91bc",
		Instruction: paymentsInstruction{TenantID: "tn_synthetic01",
			OperatingOrganisationID: "2c8e4f6a-1b3d-4e5f-8a7c-9d0e1f2a3b4c", Market: "UG", Currency: "UGX",
			AmountMinor: 150000, PaymentReference: "pay_01J9Z8M4", ParentReference: &parent,
			MerchantProfileReference: "mprof_synthetic01"},
	}
}

func (f paymentsMoneyMovementFixture) post(t *testing.T, v fingerprintVector, override map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	fingerprint, err := paymentsInstructionFingerprint(v.OperationClass, v.OperationReference, v.Instruction)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"context_id": f.context.ID, "engine_instance_id": "ei_paystaging01",
		"operation_class": v.OperationClass, "operation_reference": v.OperationReference,
		"attempt": 2, "instruction": v.Instruction, "instruction_fingerprint": fingerprint,
	}
	for k, val := range override {
		if val == nil {
			delete(body, k)
		} else {
			body[k] = val
		}
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/payments/v1/money-movement-authority/resolve", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer verified-stub")
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	f.handler.ServeHTTP(out, req)
	return out
}

func decodePaymentsDecision(t *testing.T, out *httptest.ResponseRecorder) paymentsMoneyMovementDecision {
	t.Helper()
	if out.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
	}
	var d paymentsMoneyMovementDecision
	if err := json.Unmarshal(out.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPaymentsInstructionFingerprintMatchesSharedVectors(t *testing.T) {
	vectors := paymentsFingerprintVectors(t)
	if len(vectors) < 6 {
		t.Fatalf("expected the Shared vectors, got %d", len(vectors))
	}
	for _, v := range vectors {
		got, err := paymentsInstructionFingerprint(v.OperationClass, v.OperationReference, v.Instruction)
		if err != nil || got != v.InstructionFingerprint {
			t.Fatalf("%s: fingerprint %s, want %s", v.OperationReference, got, v.InstructionFingerprint)
		}
	}
}

func TestPaymentsMoneyMovementRouteIsOptInNonproductionAndAllowlisted(t *testing.T) {
	for name, mutate := range map[string]func(*paymentsMoneyMovementOptions){
		"disabled":     func(o *paymentsMoneyMovementOptions) { o.enabled = false },
		"production":   func(o *paymentsMoneyMovementOptions) { o.environment = "production" },
		"unset-env":    func(o *paymentsMoneyMovementOptions) { o.environment = "" },
		"no-allowlist": func(o *paymentsMoneyMovementOptions) { o.allowlist = []string{" ", ""} },
	} {
		t.Run(name, func(t *testing.T) {
			o := defaultPaymentsMoneyMovementOptions()
			mutate(&o)
			f := newPaymentsMoneyMovementFixture(t, &paymentsMoneyMovementStoreFixture{bound: true}, o)
			if out := f.post(t, captureVector(), nil); out.Code != http.StatusNotFound {
				t.Fatalf("route must stay unmounted, got %d", out.Code)
			}
		})
	}
}

func TestPaymentsMoneyMovementRejectsUntrustedCallers(t *testing.T) {
	for name, mutate := range map[string]func(*paymentsMoneyMovementOptions){
		"not-allowlisted": func(o *paymentsMoneyMovementOptions) {
			o.allowlist = []string{"baobab-erp-legal-actor-assessor-staging"}
		},
		"not-registered":                    func(o *paymentsMoneyMovementOptions) { o.registered = false },
		"context-scope-revoked-in-registry": func(o *paymentsMoneyMovementOptions) { o.contextRevoked = true },
	} {
		t.Run(name, func(t *testing.T) {
			o := defaultPaymentsMoneyMovementOptions()
			mutate(&o)
			store := &paymentsMoneyMovementStoreFixture{bound: true}
			if out := newPaymentsMoneyMovementFixture(t, store, o).post(t, captureVector(), nil); out.Code != http.StatusForbidden {
				t.Fatalf("status=%d", out.Code)
			}
			if len(store.requests)+len(store.recorded)+len(store.bindings) != 0 {
				t.Fatal("untrusted caller reached CP authority")
			}
		})
	}
	o := defaultPaymentsMoneyMovementOptions()
	o.owned = false
	store := &paymentsMoneyMovementStoreFixture{bound: true}
	if out := newPaymentsMoneyMovementFixture(t, store, o).post(t, captureVector(), nil); out.Code != http.StatusNotFound || len(store.recorded) != 0 {
		t.Fatalf("another workload's context must not redeem, got %d", out.Code)
	}
}

// The caller can never select the legal actor, scope, approval or time.
func TestPaymentsMoneyMovementRejectsCallerSelectedAuthority(t *testing.T) {
	for _, field := range []string{"tenant_id", "responsible_legal_entity_id", "legal_entity_id", "mandate_id",
		"legal_actor_role", "activity", "capability_key", "market", "effective_at", "approved",
		"provider_permissions_granted"} {
		store := &paymentsMoneyMovementStoreFixture{bound: true}
		f := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions())
		if out := f.post(t, captureVector(), map[string]any{field: "forged"}); out.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", field, out.Code)
		}
		if len(store.recorded) != 0 || len(store.requests) != 0 {
			t.Fatalf("%s: forged request reached authority", field)
		}
	}
	store := &paymentsMoneyMovementStoreFixture{bound: true}
	f := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions())
	if out := f.post(t, captureVector(), map[string]any{"operation_class": "payments.anything.execute"}); out.Code != http.StatusBadRequest {
		t.Fatalf("uncatalogued class: status=%d", out.Code)
	}
}

func TestPaymentsMoneyMovementAuthorizesEachActivatableClassFromCatalogue(t *testing.T) {
	parent := "pay_01J9Z8M4"
	original := "ORIGINAL_PAYMENT_METHOD"
	base := captureVector().Instruction
	cases := []struct {
		class, activity, capability string
		parent, destination         *string
	}{
		{"payments.authorization.create", "PAYMENT_COLLECTION", "payment.payment.authorize", nil, nil},
		{"payments.capture.execute", "PAYMENT_COLLECTION", "payment.payment.capture", &parent, nil},
		{"payments.refund.execute", "PAYMENT_REFUND", "payment.refund.create", &parent, &original},
		{"payments.authorization.void", "PAYMENT_VOID", "payment.intent.cancel", &parent, nil},
	}
	for _, c := range cases {
		t.Run(c.class, func(t *testing.T) {
			now := time.Now().UTC()
			store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(now)}
			f := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions())
			in := base
			in.ParentReference, in.DestinationReference = c.parent, c.destination
			v := fingerprintVector{OperationClass: c.class, OperationReference: "payments/test/" + c.activity, Instruction: in}
			out := f.post(t, v, nil)
			d := decodePaymentsDecision(t, out)
			if out.Header().Get("Cache-Control") != "no-store, max-age=0" {
				t.Fatal("money-movement authority could be cached")
			}
			got := store.requests[0]
			if got.TenantID != f.context.TenantID || got.OperatingOrganisationID != f.context.OrganisationID ||
				got.Market != "UG" || got.Role != "PAYMENT_BENEFICIARY" || got.Activity != c.activity ||
				got.Capability != c.capability {
				t.Fatalf("scope not derived from context and catalogue: %+v", got)
			}
			if store.bindings[0] != f.context.TenantID+"|ei_paystaging01" {
				t.Fatalf("engine binding checked for the wrong tenant: %v", store.bindings)
			}
			if d.Outcome != "AUTHORIZED" || d.ProviderPermissionsGranted || d.MandateID == "" ||
				d.CapabilityKey != c.capability || d.Activity != c.activity || d.Attempt != 2 ||
				d.InstructionFingerprint == "" || d.ExpiresAt == nil {
				t.Fatalf("incomplete AUTHORIZED decision: %+v", d)
			}
			if lease := d.ExpiresAt.Sub(d.EvaluatedAt); lease <= 0 || lease > 30*time.Second {
				t.Fatalf("lease %s exceeds 30 s", lease)
			}
			r := store.recorded[0]
			if len(store.recorded) != 1 || r.MandateID != d.MandateID || r.InstructionFingerprint != d.InstructionFingerprint ||
				r.Attempt != 2 || r.ClientID != paymentsMoneyMovementClient || r.CapabilityKey != c.capability {
				t.Fatalf("decision not recorded before disclosure: %+v", store.recorded)
			}
		})
	}
}

// Collection authority is never disbursement authority: no mandate, context
// or binding is consulted, and the decision can never be AUTHORIZED.
func TestPaymentsMoneyMovementPayoutIsNeverActivated(t *testing.T) {
	store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC())}
	f := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions())
	in := captureVector().Instruction
	destination := "bnf_synthetic01"
	in.ParentReference, in.DestinationReference = nil, &destination
	d := decodePaymentsDecision(t, f.post(t, fingerprintVector{OperationClass: "payments.payout.execute",
		OperationReference: "payments/payout/payout_01/key", Instruction: in}, nil))
	if d.Outcome != "OPERATION_NOT_ACTIVATED" || d.LegalActorRole != "" || d.CapabilityKey != "" ||
		d.MandateID != "" || d.Activity != "BENEFICIARY_DISBURSEMENT" {
		t.Fatalf("payout must never be activated: %+v", d)
	}
	if len(store.requests)+len(store.bindings) != 0 || len(store.recorded) != 1 {
		t.Fatalf("payout consulted authority or was not recorded: %+v", store)
	}
}

func TestPaymentsMoneyMovementInstructionMustMatchContextAndFingerprint(t *testing.T) {
	v := captureVector()
	for name, mutate := range map[string]func(*fingerprintVector, map[string]any){
		"fingerprint-drift": func(_ *fingerprintVector, o map[string]any) {
			o["instruction_fingerprint"] = "0000000000000000000000000000000000000000000000000000000000000000"
		},
		"other-tenant": func(x *fingerprintVector, _ map[string]any) { x.Instruction.TenantID = "tn_other01" },
		"other-market": func(x *fingerprintVector, _ map[string]any) { x.Instruction.Market = "KE" },
		"other-organisation": func(x *fingerprintVector, _ map[string]any) {
			x.Instruction.OperatingOrganisationID = "3d9f5a7b-2c4e-4f6a-9b8d-0e1f2a3b4c5d"
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC())}
			f := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions())
			x, o := v, map[string]any{}
			mutate(&x, o)
			d := decodePaymentsDecision(t, f.post(t, x, o))
			if d.Outcome != "INSTRUCTION_MISMATCH" || len(store.requests) != 0 || len(store.bindings) != 0 || d.MandateID != "" {
				t.Fatalf("mismatched instruction must deny before mandate resolution: %+v", d)
			}
		})
	}
	// A fingerprint computed over another amount is a mismatch too.
	store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC())}
	f := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions())
	other := v
	other.Instruction.AmountMinor = 1
	stale, _ := paymentsInstructionFingerprint(other.OperationClass, other.OperationReference, other.Instruction)
	if d := decodePaymentsDecision(t, f.post(t, v, map[string]any{"instruction_fingerprint": stale})); d.Outcome != "INSTRUCTION_MISMATCH" {
		t.Fatalf("amount change must invalidate: %+v", d)
	}
}

func TestPaymentsMoneyMovementLeaseNeverOutlivesContext(t *testing.T) {
	o := defaultPaymentsMoneyMovementOptions()
	o.contextExpiry = 5 * time.Second
	store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC())}
	f := newPaymentsMoneyMovementFixture(t, store, o)
	d := decodePaymentsDecision(t, f.post(t, captureVector(), nil))
	if d.ExpiresAt == nil || d.ExpiresAt.After(*f.context.ExpiresAt) {
		t.Fatalf("lease outlives context: %v > %v", d.ExpiresAt, f.context.ExpiresAt)
	}
}

func TestPaymentsMoneyMovementDenialsDiscloseNoActor(t *testing.T) {
	now := time.Now().UTC()
	for _, outcome := range []legalactor.Outcome{legalactor.NoApplicableMandate, legalactor.Ambiguous,
		legalactor.RevokedOrExpired, legalactor.ActorNotVerified} {
		store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: legalactor.Resolution{
			Outcome: outcome, EvaluatedAt: now, PolicyReference: legalactor.PolicyReference}}
		d := decodePaymentsDecision(t, newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions()).post(t, captureVector(), nil))
		if d.Outcome != string(outcome) || d.MandateID != "" || d.ResponsibleLegalEntityID != "" ||
			d.ExpiresAt != nil || len(d.EvidenceReferences) != 0 || len(store.recorded) != 1 {
			t.Fatalf("%s leaked actor or was not recorded: %+v", outcome, d)
		}
	}
	broken := authorizedResolution(now)
	broken.MandateEffectiveFrom = nil
	store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: broken}
	d := decodePaymentsDecision(t, newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions()).post(t, captureVector(), nil))
	if d.Outcome != "ACTOR_NOT_VERIFIED" || d.MandateID != "" {
		t.Fatalf("incomplete resolution must deny: %+v", d)
	}
}

func TestPaymentsMoneyMovementUnboundEngineAndInactiveTenantDeny(t *testing.T) {
	store := &paymentsMoneyMovementStoreFixture{bound: false, resolution: authorizedResolution(time.Now().UTC())}
	d := decodePaymentsDecision(t, newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions()).post(t, captureVector(), nil))
	if d.Outcome != "ENGINE_INSTANCE_NOT_BOUND" || len(store.requests) != 0 || d.MandateID != "" {
		t.Fatalf("unbound engine instance must deny before mandate resolution: %+v", d)
	}
	o := defaultPaymentsMoneyMovementOptions()
	o.tenantState = "suspended"
	store = &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC())}
	d = decodePaymentsDecision(t, newPaymentsMoneyMovementFixture(t, store, o).post(t, captureVector(), nil))
	if d.Outcome != "CONTEXT_NOT_USABLE" || len(store.requests) != 0 || len(store.bindings) != 0 || d.TenantID != "" {
		t.Fatalf("inactive tenant must deny without scope: %+v", d)
	}
}

func TestPaymentsMoneyMovementFailsClosedWhenUnrecordedOrUnverifiable(t *testing.T) {
	store := &paymentsMoneyMovementStoreFixture{bound: true, resolution: authorizedResolution(time.Now().UTC()),
		recordErr: errors.New("db down")}
	out := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions()).post(t, captureVector(), nil)
	if out.Code != http.StatusServiceUnavailable || bytes.Contains(out.Body.Bytes(), []byte("SYNTHETIC-LE-01")) {
		t.Fatalf("unrecorded decision disclosed: %d %s", out.Code, out.Body.String())
	}
	store = &paymentsMoneyMovementStoreFixture{boundErr: errors.New("db down")}
	if out := newPaymentsMoneyMovementFixture(t, store, defaultPaymentsMoneyMovementOptions()).post(t, captureVector(), nil); out.Code != http.StatusServiceUnavailable {
		t.Fatalf("binding failure must be unavailable, got %d", out.Code)
	}
}

func TestPaymentsMoneyMovementCatalogue(t *testing.T) {
	catalogue, err := paymentsMoneyMovementCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalogue) != 5 {
		t.Fatalf("expected five catalogued operations, got %d", len(catalogue))
	}
	for class, op := range catalogue {
		if class == "payments.payout.execute" {
			if op.Activatable || op.Role != nil || op.CapabilityKey != nil {
				t.Fatalf("payout must not be activatable: %+v", op)
			}
			continue
		}
		if !op.Activatable || op.Role == nil || *op.Role != "PAYMENT_BENEFICIARY" || op.CapabilityKey == nil {
			t.Fatalf("%s: incomplete activatable entry: %+v", class, op)
		}
	}
}
