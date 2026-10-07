package eventingress

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	erpSource = "urn:baobab-platform:service:baobab-erp"
	erpType   = "com.baobab-platform.erp.provisioning.changed.v1"
)

// provisioningEvent is the example ERP publishes (Shared erp/v1 examples/provisioning-changed.json), with members overridable.
func provisioningEvent(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	event := map[string]any{
		"specversion": "1.0", "id": "1d05e706-89eb-51bb-a643-686aa039df63", "type": erpType, "source": erpSource,
		"subject": "provisioning:1d5a1ea4-6355-4ca2-b851-4ebde6847035", "time": "2026-09-01T08:05:00Z", "datacontenttype": "application/json",
		"dataschema": "https://contracts.baobab-platform.com/erp/v1/provisioning-state.schema.json", "baobabscope": "tenant",
		"correlationid": "e7a5b216-c90d-4a36-99f3-e1d81276638f", "tenantid": "tn_01k4m7x9q2v6c8r3d5f1h0j4",
		"idempotencykey": "erp-provisioning-1d5a1ea4-6355-4ca2-b851-4ebde6847035-r5",
		"data": map[string]any{"operation_id": "1d5a1ea4-6355-4ca2-b851-4ebde6847035", "tenant_id": "tn_01k4m7x9q2v6c8r3d5f1h0j4",
			"legal_entity_ids": []string{"ZURIBEANS"}, "state": "active", "revision": 5, "updated_at": "2026-09-01T08:05:00Z"},
	}
	if change != nil {
		change(event)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type rig struct {
	receiver Receiver
	inbox    *MemoryInbox
	key      Key
}

func newRig(t *testing.T) *rig {
	t.Helper()
	policy, err := LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	inbox := &MemoryInbox{Clock: func() time.Time { return instant }}
	return &rig{inbox: inbox, key: testKey, receiver: Receiver{Keys: lookup(testKey), Policy: policy, Inbox: inbox, Now: func() time.Time { return instant }}}
}

func (r *rig) deliver(body []byte) (Receipt, *Rejection) {
	return r.receiver.Receive(context.Background(), signed(r.key, "2026-10-07T17:30:00Z", body), body)
}

func wantRejection(t *testing.T, got *Rejection, status int, code string) {
	t.Helper()
	if got == nil || got.Status != status || got.Code != code {
		t.Fatalf("want %d %s, got %+v", status, code, got)
	}
}

func TestAGenuineEventIsRecordedExactlyOnceAndARedeliveryIsADuplicate(t *testing.T) {
	r := newRig(t)
	body := provisioningEvent(t, nil)
	first, rejection := r.deliver(body)
	if rejection != nil || first.Status != Accepted || first.EventID != "1d05e706-89eb-51bb-a643-686aa039df63" {
		t.Fatalf("first=%+v %v", first, rejection)
	}
	again, rejection := r.deliver(body)
	if rejection != nil || again.Status != Duplicate || !again.ReceivedAt.Equal(first.ReceivedAt) {
		t.Fatalf("again=%+v %v", again, rejection)
	}
	if r.inbox.Len() != 1 || r.inbox.Status(erpSource, first.EventID) != "pending" {
		t.Fatalf("the event must be recorded once, pending: %d", r.inbox.Len())
	}
}

func TestTheSameIdentityWithOtherContentIsAConflictAndChangesNothing(t *testing.T) {
	r := newRig(t)
	if _, rejection := r.deliver(provisioningEvent(t, nil)); rejection != nil {
		t.Fatal(rejection)
	}
	other := provisioningEvent(t, func(e map[string]any) { e["data"].(map[string]any)["revision"] = 6 })
	_, rejection := r.deliver(other)
	wantRejection(t, rejection, 409, "EVENT_ID_CONFLICT")
	if rejection.Retryable {
		t.Fatal("a conflict is permanent")
	}
	if r.inbox.Len() != 1 {
		t.Fatal("a conflict must not record anything")
	}
}

func TestNothingIsParsedOrStoredForADeliveryThatIsNotAuthenticated(t *testing.T) {
	body := provisioningEvent(t, nil)
	for name, c := range map[string]struct {
		mutate func(*rig)
		h      func(r *rig) Headers
		body   []byte
		status int
		code   string
	}{
		"a wrong signature": {nil, func(r *rig) Headers {
			h := signed(r.key, "2026-10-07T17:30:00Z", body)
			h.Signature = Sign(r.key, "2026-10-07T17:30:00Z", []byte("x"))
			return h
		}, body, 401, "EVENT_DELIVERY_UNAUTHENTICATED"},
		"a stale timestamp": {nil, func(r *rig) Headers { return signed(r.key, "2026-10-07T17:00:00Z", body) }, body, 401, "EVENT_DELIVERY_UNAUTHENTICATED"},
		"an unknown key":    {nil, func(r *rig) Headers { k := r.key; k.ID = "other-key-1"; return signed(k, "2026-10-07T17:30:00Z", body) }, body, 401, "EVENT_DELIVERY_UNAUTHENTICATED"},
		"a revoked key":     {func(r *rig) { r.key.Revoked = true; r.receiver.Keys = lookup(r.key) }, func(r *rig) Headers { return signed(r.key, "2026-10-07T17:30:00Z", body) }, body, 401, "EVENT_DELIVERY_UNAUTHENTICATED"},
		"malformed headers": {nil, func(r *rig) Headers { return Headers{KeyID: "x", Timestamp: "now", Signature: "sig"} }, body, 400, "EVENT_DELIVERY_MALFORMED"},
		// A body that is not even JSON is still judged for authentication first: an unsigned garbage body is a 401, not a parse error.
		"garbage that is not signed": {nil, func(r *rig) Headers { return signed(r.key, "2026-10-07T17:30:00Z", body) }, []byte("not json"), 401, "EVENT_DELIVERY_UNAUTHENTICATED"},
	} {
		r := newRig(t)
		if c.mutate != nil {
			c.mutate(r)
		}
		_, rejection := r.receiver.Receive(context.Background(), c.h(r), c.body)
		if rejection == nil || rejection.Status != c.status || rejection.Code != c.code {
			t.Errorf("%s: %+v", name, rejection)
		}
		if r.inbox.Len() != 0 {
			t.Errorf("%s: something was stored", name)
		}
	}
}

func TestEveryAuthenticationFailureIsTheSameAnswer(t *testing.T) {
	r := newRig(t)
	body := provisioningEvent(t, nil)
	var answers []string
	for _, h := range []Headers{
		{KeyID: "unknown-key", Timestamp: "2026-10-07T17:30:00Z", Signature: Sign(Key{ID: "unknown-key", Secret: testSecret}, "2026-10-07T17:30:00Z", body)},
		signed(r.key, "2026-10-07T16:00:00Z", body),
		{KeyID: r.key.ID, Timestamp: "2026-10-07T17:30:00Z", Signature: "hmac-sha256=" + strings.Repeat("0", 64)},
	} {
		_, rejection := r.receiver.Receive(context.Background(), h, body)
		if rejection == nil {
			t.Fatal("accepted")
		}
		answers = append(answers, rejection.Detail+"|"+rejection.Code)
	}
	if answers[0] != answers[1] || answers[1] != answers[2] {
		t.Fatalf("authentication failures are distinguishable: %v", answers)
	}
}

func TestOnlyAcceptedEventsFromTheirProducerAreRecorded(t *testing.T) {
	for name, c := range map[string]struct {
		change func(map[string]any)
		status int
		code   string
	}{
		"an event type the Control Plane does not consume": {func(e map[string]any) { e["type"] = "com.baobab-platform.erp.invoice.changed.v1" }, 422, "EVENT_NOT_ACCEPTED"},
		"another producer's source":                        {func(e map[string]any) { e["source"] = "urn:baobab-platform:service:baobab-trade" }, 422, "EVENT_NOT_ACCEPTED"},
		"a platform scoped event":                          {func(e map[string]any) { e["baobabscope"] = "platform"; delete(e, "tenantid") }, 422, "EVENT_NOT_ACCEPTED"},
		"another dataschema": {func(e map[string]any) {
			e["dataschema"] = "https://contracts.baobab-platform.com/erp/v1/provisioning-request.schema.json"
		}, 422, "EVENT_NOT_ACCEPTED"},
		"data that is not a provisioning state":       {func(e map[string]any) { e["data"] = map[string]any{"operation_id": "x"} }, 422, "EVENT_PAYLOAD_INVALID"},
		"a state with an unknown member":              {func(e map[string]any) { e["data"].(map[string]any)["internal"] = "x" }, 422, "EVENT_PAYLOAD_INVALID"},
		"data for another tenant than the envelope's": {func(e map[string]any) { e["data"].(map[string]any)["tenant_id"] = "tn_01k4someoneelse0000000000" }, 422, "EVENT_PAYLOAD_INVALID"},
		"a state with an impossible state value":      {func(e map[string]any) { e["data"].(map[string]any)["state"] = "exploded" }, 422, "EVENT_PAYLOAD_INVALID"},
		"an envelope with a legacy shape":             {func(e map[string]any) { delete(e, "specversion"); e["event_id"] = "x" }, 400, "EVENT_ENVELOPE_INVALID"},
		"an envelope with no id":                      {func(e map[string]any) { delete(e, "id") }, 400, "EVENT_ENVELOPE_INVALID"},
	} {
		r := newRig(t)
		_, rejection := r.deliver(provisioningEvent(t, c.change))
		if rejection == nil || rejection.Status != c.status || rejection.Code != c.code {
			t.Errorf("%s: %+v", name, rejection)
		}
		if r.inbox.Len() != 0 {
			t.Errorf("%s: something was stored", name)
		}
	}
}

func TestAKeyRegisteredForAnotherProducerCannotDeliverThisProducersEvents(t *testing.T) {
	r := newRig(t)
	r.key = Key{ID: "trade-delivery-2026", Sender: "baobab-trade", Secret: testSecret}
	r.receiver.Keys = lookup(r.key)
	_, rejection := r.deliver(provisioningEvent(t, nil))
	wantRejection(t, rejection, 422, "EVENT_NOT_ACCEPTED")
	if r.inbox.Len() != 0 {
		t.Fatal("stored")
	}
}

func TestAnOversizeBodyIsRefusedBeforeAnythingElse(t *testing.T) {
	r := newRig(t)
	_, rejection := r.receiver.Receive(context.Background(), Headers{}, make([]byte, MaxBodyBytes+1))
	wantRejection(t, rejection, 413, "EVENT_TOO_LARGE")
}

func TestAStoreOutageIsRetryableAndRecordsNothing(t *testing.T) {
	r := newRig(t)
	r.inbox.FailAccept = errors.New("connection refused to 10.0.0.9")
	_, rejection := r.deliver(provisioningEvent(t, nil))
	wantRejection(t, rejection, 503, "EVENT_STORE_UNAVAILABLE")
	if !rejection.Retryable || strings.Contains(rejection.Detail, "10.0.0.9") {
		t.Fatalf("an outage is retryable and never shows the cause: %+v", rejection)
	}
}

func TestPolicyIsTheSharedContractAndEveryAcceptedTypeCanBeValidated(t *testing.T) {
	policy, err := LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Accepted) != 1 || policy.Accepted[0].Type != erpType || policy.Accepted[0].PendingMaxAge != 24*time.Hour {
		t.Fatalf("policy=%+v", policy.Accepted)
	}
	for _, a := range policy.Accepted {
		if _, ok := payloadSchemas[a.Type]; !ok {
			t.Errorf("%s is accepted by policy but has no payload schema", a.Type)
		}
	}
	if got := policy.Producers(); len(got) != 1 || got[0] != "baobab-erp" {
		t.Fatalf("producers=%v", got)
	}
	if policy.Retention() < 7*24*time.Hour {
		t.Fatal("receipts must be kept at least seven days")
	}
}

func TestTheTraceparentRuleTheSchemaStatesWithLookAheadIsStillEnforced(t *testing.T) {
	for name, c := range map[string]struct {
		value string
		ok    bool
	}{
		"a valid value":       {"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", true},
		"an all-zero trace":   {"00-00000000000000000000000000000000-b7ad6b7169203331-01", false},
		"an all-zero span":    {"00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01", false},
		"another version":     {"01-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", false},
		"uppercase":           {"00-0AF7651916CD43DD8448EB211C80319C-b7ad6b7169203331-01", false},
		"trailing characters": {"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01-x", false},
	} {
		r := newRig(t)
		_, rejection := r.deliver(provisioningEvent(t, func(e map[string]any) { e["traceparent"] = c.value }))
		if (rejection == nil) != c.ok {
			t.Errorf("%s: %+v", name, rejection)
		}
	}
}
