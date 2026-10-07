package erpprovisioning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/eventingress"
)

var deliveryKey = eventingress.Key{ID: "erp-delivery-2026-10", Sender: "baobab-erp", Secret: bytes.Repeat([]byte{9}, 32)}

func stateData(state string, revision int) json.RawMessage {
	raw, _ := json.Marshal(newState(state, revision))
	return raw
}

func TestApplyEventRecordsANewerStateAndIgnoresAStaleOne(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.w.Submit(ctx, provisionID); err != nil {
		t.Fatal(err)
	}
	if err := r.w.ApplyEvent(ctx, stateData("provisioning", 3)); err != nil || r.led.last[operationID] != 3 {
		t.Fatalf("err=%v last=%d", err, r.led.last[operationID])
	}
	if err := r.w.ApplyEvent(ctx, stateData("accepted", 2)); err != nil || r.led.last[operationID] != 3 {
		t.Fatalf("a late event rewound progress: err=%v last=%d", err, r.led.last[operationID])
	}
	if err := r.w.ApplyEvent(ctx, stateData("provisioning", 3)); err != nil {
		t.Fatalf("a redelivered event must be harmless: %v", err)
	}
}

func TestApplyEventTellsAWaitingEventFromOneThatCanNeverApply(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	// No submission recorded yet: the event may simply have arrived first, so it is an ordinary (retryable) failure.
	err := r.w.ApplyEvent(ctx, stateData("provisioning", 2))
	if !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("err=%v", err)
	}
	if _, permanent := eventingressPermanent(err); permanent {
		t.Fatal("an event for a not-yet-recorded operation must wait, not be dead-lettered")
	}
	if _, err := r.w.Submit(ctx, provisionID); err != nil {
		t.Fatal(err)
	}
	// A state for the recorded operation that names another tenant can never agree: permanent.
	other := newState("provisioning", 5)
	other["tenant_id"] = "tn_01k4someoneelse"
	raw, _ := json.Marshal(other)
	err = r.w.ApplyEvent(ctx, raw)
	if !errors.Is(err, ErrStateDisagrees) {
		t.Fatalf("err=%v", err)
	}
	if _, permanent := eventingressPermanent(err); !permanent {
		t.Fatal("a state that disagrees with the submission can never apply")
	}
	if r.led.last[operationID] >= 5 {
		t.Fatal("a disagreeing state was recorded")
	}
}

// eventingressPermanent reports whether an ApplyEvent error is the processor's "dead-letter at once". The processor decides through
// errors.As on an unexported type, so this goes through the processor itself.
func eventingressPermanent(err error) (error, bool) {
	inbox := &eventingress.MemoryInbox{}
	policy, perr := eventingress.LoadPolicy()
	if perr != nil {
		panic(perr)
	}
	body := []byte(`{"data":{}}`)
	sum := sha256.Sum256(body)
	_, _ = inbox.Accept(context.Background(), eventingress.Event{Source: "s", ID: "11111111-1111-4111-8111-111111111111", Type: ProvisioningChangedEvent, TenantID: "t", KeyID: "k", Body: body, BodySHA256: hex.EncodeToString(sum[:])})
	p := &eventingress.Processor{Inbox: inbox, Policy: policy, Handlers: map[string]eventingress.Apply{ProvisioningChangedEvent: func(context.Context, []byte) error { return err }}}
	summary, _ := p.RunOnce(context.Background())
	return err, summary.DeadLettered == 1
}

// The whole path, ERP's event to the Control Plane's recorded state: a signed delivery is verified and recorded, then applied by the
// processor through the worker, including the race where the event arrives before the submission has been recorded.
func TestASignedEventConvergesTheLedgerEvenWhenItArrivesBeforeTheSubmission(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	policy, err := eventingress.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 17, 30, 0, 0, time.UTC)
	inbox := &eventingress.MemoryInbox{Clock: func() time.Time { return now }}
	receiver := eventingress.Receiver{Policy: policy, Inbox: inbox, Now: func() time.Time { return now },
		Keys: func(id string) (eventingress.Key, bool) { return deliveryKey, id == deliveryKey.ID }}
	processor := &eventingress.Processor{Inbox: inbox, Policy: policy, Now: func() time.Time { return now },
		Handlers: map[string]eventingress.Apply{ProvisioningChangedEvent: r.w.ApplyEvent}}

	body, _ := json.Marshal(map[string]any{
		"specversion": "1.0", "id": "1d05e706-89eb-51bb-a643-686aa039df63", "type": ProvisioningChangedEvent,
		"source": "urn:baobab-platform:service:baobab-erp", "subject": "provisioning:" + operationID, "time": "2026-10-07T17:29:00Z",
		"datacontenttype": "application/json", "dataschema": "https://contracts.baobab-platform.com/erp/v1/provisioning-state.schema.json",
		"baobabscope": "tenant", "correlationid": "e7a5b216-c90d-4a36-99f3-e1d81276638f", "tenantid": tenant,
		"idempotencykey": "erp-provisioning-" + operationID + "-r4", "data": newState("active", 4),
	})
	ts := now.Format("2006-01-02T15:04:05Z")
	headers := eventingress.Headers{KeyID: deliveryKey.ID, Timestamp: ts, Signature: eventingress.Sign(deliveryKey, ts, body)}

	// 1. The event is delivered first: recorded (the sender is answered), but it cannot be applied yet.
	if receipt, rejection := receiver.Receive(ctx, headers, body); rejection != nil || receipt.Status != eventingress.Accepted {
		t.Fatalf("%+v %v", receipt, rejection)
	}
	if summary, _ := processor.RunOnce(ctx); summary.Retried != 1 || len(r.led.subs) != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	// 2. The Control Plane records its submission; the waiting event is applied on a later pass.
	if _, err := r.w.Submit(ctx, provisionID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if summary, _ := processor.RunOnce(ctx); summary.Applied != 1 || r.led.last[operationID] != 4 {
		t.Fatalf("summary=%+v last=%d", summary, r.led.last[operationID])
	}
	// 3. ERP redelivers (at-least-once): a duplicate receipt, applied nowhere twice.
	if receipt, rejection := receiver.Receive(ctx, headers, body); rejection != nil || receipt.Status != eventingress.Duplicate {
		t.Fatalf("%+v %v", receipt, rejection)
	}
	if summary, _ := processor.RunOnce(ctx); summary != (eventingress.Summary{}) {
		t.Fatalf("a duplicate was applied again: %+v", summary)
	}
}
