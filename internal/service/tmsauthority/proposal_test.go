package tmsauthority

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testUUID = "123e4567-e89b-42d3-a456-426614174000"
const shaA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const shaB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func fixture() Request {
	return Request{
		ContextID: testUUID, Operation: "tms.booking.submit", ResourceType: "TransportBooking",
		ResourceID: "booking-a", NativeBindingReference: "bind-a",
		IntentDigest: shaA, PlanDigest: shaB,
		ExpectedBindingRevision: "revision-a", ExpectedFencingGeneration: 2,
	}
}

func TestProvisionalTMSDecisionCannotActivateProvider(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	r := fixture()
	c := Candidate{
		Request: r, SubjectIssuer: "https://issuer.synthetic.invalid",
		SubjectReference: "subject-a", ProviderReleaseDigest: shaA,
		LegalActorReference: "legal-a", MarketReference: "ZA",
		CurrentBindingRevision:   r.ExpectedBindingRevision,
		CurrentFencingGeneration: r.ExpectedFencingGeneration,
		ContextExpiresAt:         now.Add(time.Minute), MandateExpiresAt: now.Add(time.Minute),
		BindingActive: true, MandateActive: true,
	}
	if err := Preflight(c, now); err != nil {
		t.Fatal(err)
	}
	for _, altered := range []Candidate{
		{Request: r},
		func() Candidate { x := c; x.CurrentBindingRevision = "stale"; return x }(),
		func() Candidate { x := c; x.CurrentFencingGeneration = 1; return x }(),
		func() Candidate { x := c; x.MandateActive = false; return x }(),
		func() Candidate { x := c; x.ContextExpiresAt = now.Add(-time.Second); return x }(),
		func() Candidate { x := c; x.LegalActorReference = ""; return x }(),
	} {
		if err := Preflight(altered, now); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("untrusted or stale facts accepted")
		}
		if decision := Evaluate(altered, now); decision.CanExecute || decision.CanActivate || decision.Outcome != "HELD" {
			t.Fatal("synthetic inputs activated proposed authority")
		}
	}
	decision := Evaluate(c, now)
	if decision.Outcome != "HELD" || decision.CanExecute || decision.CanActivate ||
		decision.ExpiresAt.After(now) || decision.Reason != ReasonContractNotAccepted {
		t.Fatal("proposed authority became executable")
	}
	b, _ := json.Marshal(decision)
	if strings.Contains(string(b), "subject-a") || strings.Contains(string(b), "legal-a") {
		t.Fatal("untrusted subject or legal actor reflected in response")
	}
}

func TestClosedTMSRequestShape(t *testing.T) {
	r := fixture()
	b, _ := json.Marshal(map[string]any{
		"context_id": r.ContextID, "operation": r.Operation,
		"resource_type": r.ResourceType, "resource_id": r.ResourceID,
		"native_binding_reference": r.NativeBindingReference,
		"intent_digest":            r.IntentDigest, "plan_digest": r.PlanDigest,
		"expected_binding_revision":   r.ExpectedBindingRevision,
		"expected_fencing_generation": r.ExpectedFencingGeneration,
	})
	read, err := ParseRequest(b)
	if err != nil || read != r {
		t.Fatalf("closed request not parsed: %v", err)
	}
	for _, bad := range []string{
		"{}", string(b) + "{}",
		strings.Replace(string(b), `"booking-a"`, `"booking-a","tenant_id":"tn_forged"`, 1),
		strings.Replace(string(b), `"booking-a"`, `"booking-a","resource_id":"other"`, 1),
		strings.Replace(string(b), `"tms.booking.submit"`, `"tms.unknown"`, 1),
		strings.Replace(string(b), `"TransportBooking"`, `"TransportMovement"`, 1),
		strings.Replace(string(b), `"revision-a"`, "null", 1),
	} {
		if _, err := ParseRequest([]byte(bad)); err == nil {
			t.Fatal("invalid or ambiguous JSON accepted")
		}
	}
	for op, kind := range operations {
		x := r
		x.Operation = op
		x.ResourceType = kind
		if err := x.Validate(); err != nil {
			t.Fatalf("closed operation %s invalid: %v", op, err)
		}
		x.ResourceType = "TransportCarrier"
		if err := x.Validate(); err == nil {
			t.Fatalf("arbitrary resource on %s", op)
		}
	}
}
