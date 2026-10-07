package eventingress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
)

// Rejection is a refused delivery in the form the HTTP operation answers. Result is a bounded label for metrics.
type Rejection struct {
	Status    int
	Code      string
	Detail    string
	Retryable bool
	Result    string
}

func (r *Rejection) Error() string { return r.Code }

// Receiver verifies, validates and records one delivery. It never applies the event.
type Receiver struct {
	Keys   func(keyID string) (Key, bool)
	Policy Policy
	Inbox  Inbox
	Now    func() time.Time
}

type envelope struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Source      string          `json:"source"`
	DataSchema  string          `json:"dataschema"`
	BaobabScope string          `json:"baobabscope"`
	TenantID    string          `json:"tenantid"`
	Data        json.RawMessage `json:"data"`
}

// Receive judges a delivery in the contract's order: size, header syntax, authentication (before the body is parsed or stored),
// then the envelope, whether the type, source and key's sender are accepted, and the payload; only then is it recorded.
func (r Receiver) Receive(ctx context.Context, h Headers, body []byte) (Receipt, *Rejection) {
	if len(body) > MaxBodyBytes {
		return Receipt{}, &Rejection{Status: 413, Code: "EVENT_TOO_LARGE", Detail: "the event exceeds the 1 MiB delivery limit", Result: "too_large"}
	}
	key, err := Verify(r.Keys, h, body, r.now())
	switch {
	case errors.Is(err, ErrMalformed):
		return Receipt{}, &Rejection{Status: 400, Code: "EVENT_DELIVERY_MALFORMED", Detail: "the delivery headers are malformed", Result: "malformed"}
	case err != nil:
		// Every authentication failure is the same answer: no reason reaches the sender.
		return Receipt{}, &Rejection{Status: 401, Code: "EVENT_DELIVERY_UNAUTHENTICATED", Detail: "the event delivery could not be authenticated", Retryable: true, Result: "unauthenticated"}
	}
	if err := validateEnvelope(body); err != nil {
		return Receipt{}, &Rejection{Status: 400, Code: "EVENT_ENVELOPE_INVALID", Detail: "the body is not a canonical event envelope", Result: "invalid"}
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Receipt{}, &Rejection{Status: 400, Code: "EVENT_ENVELOPE_INVALID", Detail: "the body is not a canonical event envelope", Result: "invalid"}
	}
	allowed, ok := r.Policy.Lookup(env.Type)
	schema, hasSchema := payloadSchemas[env.Type]
	if !ok || !hasSchema || env.Source != allowed.Source || key.Sender != allowed.Producer {
		// An unaccepted type, a source that is not the producer's, or a key registered for another producer: the same answer, so the
		// sender learns nothing about which.
		return Receipt{}, &Rejection{Status: 422, Code: "EVENT_NOT_ACCEPTED", Detail: "the Control Plane does not accept this event from this sender", Result: "not_accepted"}
	}
	if env.BaobabScope != allowed.Scope || env.TenantID == "" || env.DataSchema != schema.DataSchema {
		return Receipt{}, &Rejection{Status: 422, Code: "EVENT_NOT_ACCEPTED", Detail: "the event's scope, tenant or dataschema is not the accepted one", Result: "not_accepted"}
	}
	if err := contracts.Validate(contracts.MustSchema(schema.Ref), env.Data); err != nil {
		return Receipt{}, &Rejection{Status: 422, Code: "EVENT_PAYLOAD_INVALID", Detail: "the event data does not satisfy its payload schema", Result: "invalid"}
	}
	digest := sha256.Sum256(body)
	receipt, err := r.Inbox.Accept(ctx, Event{Source: env.Source, ID: env.ID, Type: env.Type, TenantID: env.TenantID, KeyID: key.ID,
		Body: body, BodySHA256: hex.EncodeToString(digest[:])})
	switch {
	case errors.Is(err, ErrConflict):
		return Receipt{}, &Rejection{Status: 409, Code: "EVENT_ID_CONFLICT", Detail: "this event identity was already received with different content", Result: "conflict"}
	case err != nil:
		return Receipt{}, &Rejection{Status: 503, Code: "EVENT_STORE_UNAVAILABLE", Detail: "the event could not be recorded", Retryable: true, Result: "unavailable"}
	}
	return receipt, nil
}

func (r Receiver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
