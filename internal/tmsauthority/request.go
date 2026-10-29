// Package tmsauthority contains non-authorising candidate diagnostics only.
package tmsauthority

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

var (
	ErrInvalidRequest = errors.New("invalid TMS native operation request")
	uuid              = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
	hash              = regexp.MustCompile("^sha256:[0-9a-f]{64}$")
	identifier        = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,159}$")
	resourceTypes     = map[string]string{
		"tms.plan.revise":           "TransportPlan",
		"tms.booking.intent.create": "TransportBooking",
		"tms.booking.submit":        "TransportBooking",
		"tms.booking.reconcile":     "TransportBooking",
		"tms.capacity.hold":         "CapacityReservation",
		"tms.capacity.release":      "CapacityReservation",
		"tms.movement.dispatch":     "TransportMovement",
		"tms.native.provision":      "NativeProviderInstance",
		"tms.native.suspend":        "NativeProviderInstance",
		"tms.native.deprovision":    "NativeProviderInstance",
	}
)

// Request carries untrusted references, never Tenant, legal actor, Principal,
// market, permission, provider credentials, IAM audience or result status.
// The future CP PDP must independently resolve these facts.
type Request struct {
	ContextID                 string
	Operation                 string
	ResourceType              string
	ResourceID                string
	NativeBindingReference    string
	IntentDigest              string
	PlanDigest                string
	ExpectedBindingRevision   string
	ExpectedFencingGeneration int64
}

var requestFields = []string{
	"context_id", "operation", "resource_type", "resource_id",
	"native_binding_reference", "intent_digest", "plan_digest",
	"expected_binding_revision", "expected_fencing_generation",
}

// ParseRequest denies unknown, duplicated, null, absent and trailing fields,
// unlike struct-based Decode (which silently accepts duplicate JSON keys).
func ParseRequest(raw []byte) (Request, error) {
	var out Request
	if len(raw) == 0 || len(raw) > 4096 {
		return out, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	tok, err := decoder.Token()
	if err != nil || tok != json.Delim('{') {
		return out, ErrInvalidRequest
	}
	fields := make(map[string]json.RawMessage, len(requestFields))
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return Request{}, ErrInvalidRequest
		}
		known := false
		for _, field := range requestFields {
			if field == key {
				known = true
				break
			}
		}
		if !known || fields[key] != nil {
			return Request{}, ErrInvalidRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(value, []byte("null")) {
			return Request{}, ErrInvalidRequest
		}
		fields[key] = value
	}
	if tok, err := decoder.Token(); err != nil || tok != json.Delim('}') {
		return Request{}, ErrInvalidRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF || len(fields) != len(requestFields) {
		return Request{}, ErrInvalidRequest
	}
	values := map[string]*string{
		"context_id":                &out.ContextID,
		"operation":                 &out.Operation,
		"resource_type":             &out.ResourceType,
		"resource_id":               &out.ResourceID,
		"native_binding_reference":  &out.NativeBindingReference,
		"intent_digest":             &out.IntentDigest,
		"plan_digest":               &out.PlanDigest,
		"expected_binding_revision": &out.ExpectedBindingRevision,
	}
	for field, dest := range values {
		if err := json.Unmarshal(fields[field], dest); err != nil {
			return Request{}, ErrInvalidRequest
		}
	}
	if err := json.Unmarshal(fields["expected_fencing_generation"], &out.ExpectedFencingGeneration); err != nil {
		return Request{}, ErrInvalidRequest
	}
	return out, out.Validate()
}

// Validate is structural only. Syntactically valid untrusted references
// MUST NOT be converted into current CP scope or native execution authority.
func (r Request) Validate() error {
	resource, exists := resourceTypes[r.Operation]
	if !exists || resource != r.ResourceType ||
		!uuid.MatchString(r.ContextID) || !identifier.MatchString(r.ResourceID) ||
		!identifier.MatchString(r.NativeBindingReference) ||
		!identifier.MatchString(r.ExpectedBindingRevision) ||
		!hash.MatchString(r.IntentDigest) || !hash.MatchString(r.PlanDigest) ||
		r.ExpectedFencingGeneration < 1 {
		return ErrInvalidRequest
	}
	return nil
}
