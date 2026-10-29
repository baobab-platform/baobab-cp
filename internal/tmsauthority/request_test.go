package tmsauthority

import (
	"encoding/json"
	"strings"
	"testing"
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
	for op, kind := range resourceTypes {
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
