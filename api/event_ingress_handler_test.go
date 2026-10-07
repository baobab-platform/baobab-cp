package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/eventingress"
)

var ingressKey = eventingress.Key{ID: "erp-delivery-2026-10", Sender: "baobab-erp", Secret: bytes.Repeat([]byte{3}, 32)}

type ingressRig struct {
	handler http.Handler
	inbox   *eventingress.MemoryInbox
	wake    chan struct{}
	now     time.Time
}

func newIngressRig(t *testing.T) *ingressRig {
	t.Helper()
	policy, err := eventingress.LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 17, 30, 0, 0, time.UTC)
	inbox := &eventingress.MemoryInbox{Clock: func() time.Time { return now }}
	wake := make(chan struct{}, 1)
	receiver := &eventingress.Receiver{Policy: policy, Inbox: inbox, Now: func() time.Time { return now },
		Keys: func(id string) (eventingress.Key, bool) { return ingressKey, id == ingressKey.ID }}
	// No token verifiers: the route must not depend on either.
	return &ingressRig{handler: New(Dependencies{EventIngress: receiver, EventWake: wake}), inbox: inbox, wake: wake, now: now}
}

func provisioningChanged(change func(map[string]any)) []byte {
	event := map[string]any{
		"specversion": "1.0", "id": "1d05e706-89eb-51bb-a643-686aa039df63", "type": "com.baobab-platform.erp.provisioning.changed.v1",
		"source": "urn:baobab-platform:service:baobab-erp", "subject": "provisioning:1d5a1ea4-6355-4ca2-b851-4ebde6847035", "time": "2026-09-01T08:05:00Z",
		"datacontenttype": "application/json", "dataschema": "https://contracts.baobab-platform.com/erp/v1/provisioning-state.schema.json",
		"baobabscope": "tenant", "correlationid": "e7a5b216-c90d-4a36-99f3-e1d81276638f", "tenantid": "tn_01k4m7x9q2v6c8r3d5f1h0j4",
		"idempotencykey": "erp-provisioning-1d5a1ea4-6355-4ca2-b851-4ebde6847035-r5",
		"data": map[string]any{"operation_id": "1d5a1ea4-6355-4ca2-b851-4ebde6847035", "tenant_id": "tn_01k4m7x9q2v6c8r3d5f1h0j4",
			"legal_entity_ids": []string{"ZURIBEANS"}, "state": "active", "revision": 5, "updated_at": "2026-09-01T08:05:00Z"},
	}
	if change != nil {
		change(event)
	}
	raw, _ := json.Marshal(event)
	return raw
}

func (r *ingressRig) post(body []byte, mutate func(*http.Request)) *httptest.ResponseRecorder {
	ts := r.now.Format("2006-01-02T15:04:05Z")
	request := httptest.NewRequest(http.MethodPost, "/v1/integration/events", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/cloudevents+json")
	request.Header.Set("Baobab-Key-Id", ingressKey.ID)
	request.Header.Set("Baobab-Timestamp", ts)
	request.Header.Set("Baobab-Signature", eventingress.Sign(ingressKey, ts, body))
	if mutate != nil {
		mutate(request)
	}
	response := httptest.NewRecorder()
	r.handler.ServeHTTP(response, request)
	return response
}

func TestEventIngressRecordsOnceAnswersTheContractsReceiptsAndNudgesTheProcessor(t *testing.T) {
	rig := newIngressRig(t)
	body := provisioningChanged(nil)

	first := rig.post(body, nil)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	if err := contracts.Validate(contracts.MustSchema("events/v1/signed-delivery.schema.json#/$defs/DeliveryReceiptAccepted"), first.Body.Bytes()); err != nil {
		t.Fatalf("the 202 body is not a DeliveryReceiptAccepted: %v", err)
	}
	select {
	case <-rig.wake:
	default:
		t.Fatal("a newly recorded event must wake the processor")
	}
	second := rig.post(body, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("redelivery: %d %s", second.Code, second.Body.String())
	}
	if err := contracts.Validate(contracts.MustSchema("events/v1/signed-delivery.schema.json#/$defs/DeliveryReceiptDuplicate"), second.Body.Bytes()); err != nil {
		t.Fatalf("the 200 body is not a DeliveryReceiptDuplicate: %v", err)
	}
	if err := contracts.Validate(contracts.MustSchema("events/v1/signed-delivery.schema.json#/$defs/DeliveryReceiptAccepted"), second.Body.Bytes()); err == nil {
		t.Fatal("a 200 DUPLICATE must not satisfy the 202 receipt")
	}
	select {
	case <-rig.wake:
		t.Fatal("a duplicate must not wake the processor")
	default:
	}
	if rig.inbox.Len() != 1 {
		t.Fatalf("recorded=%d", rig.inbox.Len())
	}
	if first.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", first.Header().Get("Content-Type"))
	}
}

func problemStatus(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var p struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &p)
	if response.Code != status || p.Code != code {
		t.Fatalf("want %d %s, got %d %s", status, code, response.Code, response.Body.String())
	}
	if ct := response.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("a refusal is problem+json, got %q", ct)
	}
}

func TestEventIngressRefusalsFollowTheContract(t *testing.T) {
	body := provisioningChanged(nil)
	t.Run("a bad signature is a 401 and records nothing", func(t *testing.T) {
		rig := newIngressRig(t)
		response := rig.post(body, func(r *http.Request) { r.Header.Set("Baobab-Signature", "hmac-sha256="+strings.Repeat("0", 64)) })
		problemStatus(t, response, 401, "EVENT_DELIVERY_UNAUTHENTICATED")
		if rig.inbox.Len() != 0 {
			t.Fatal("stored")
		}
	})
	t.Run("missing headers are a 400", func(t *testing.T) {
		rig := newIngressRig(t)
		problemStatus(t, rig.post(body, func(r *http.Request) { r.Header.Del("Baobab-Key-Id") }), 400, "EVENT_DELIVERY_MALFORMED")
	})
	t.Run("another content type is a 400", func(t *testing.T) {
		rig := newIngressRig(t)
		problemStatus(t, rig.post(body, func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }), 400, "EVENT_DELIVERY_MALFORMED")
	})
	t.Run("a conflicting identity is a 409", func(t *testing.T) {
		rig := newIngressRig(t)
		rig.post(body, nil)
		problemStatus(t, rig.post(provisioningChanged(func(e map[string]any) { e["data"].(map[string]any)["revision"] = 9 }), nil), 409, "EVENT_ID_CONFLICT")
	})
	t.Run("an unaccepted event is a 422", func(t *testing.T) {
		rig := newIngressRig(t)
		problemStatus(t, rig.post(provisioningChanged(func(e map[string]any) { e["type"] = "com.baobab-platform.erp.invoice.changed.v1" }), nil), 422, "EVENT_NOT_ACCEPTED")
	})
	t.Run("a body over 1 MiB is a 413 and is not buffered without bound", func(t *testing.T) {
		rig := newIngressRig(t)
		problemStatus(t, rig.post(bytes.Repeat([]byte{'x'}, eventingress.MaxBodyBytes+1), nil), 413, "EVENT_TOO_LARGE")
		if rig.inbox.Len() != 0 {
			t.Fatal("stored")
		}
	})
	t.Run("a store outage is a retryable 503", func(t *testing.T) {
		rig := newIngressRig(t)
		rig.inbox.FailAccept = http.ErrAbortHandler
		problemStatus(t, rig.post(body, nil), 503, "EVENT_STORE_UNAVAILABLE")
	})
}

func TestTheStaleTimestampRefusalUsesTheServersClock(t *testing.T) {
	rig := newIngressRig(t)
	body := provisioningChanged(nil)
	old := rig.now.Add(-6 * time.Minute).Format("2006-01-02T15:04:05Z")
	response := rig.post(body, func(r *http.Request) {
		r.Header.Set("Baobab-Timestamp", old)
		r.Header.Set("Baobab-Signature", eventingress.Sign(ingressKey, old, body))
	})
	problemStatus(t, response, 401, "EVENT_DELIVERY_UNAUTHENTICATED")
}

func TestTheRouteIsAbsentUnlessEventIngressIsConfiguredAndAcceptsNoBearerToken(t *testing.T) {
	response := httptest.NewRecorder()
	New(Dependencies{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/integration/events", strings.NewReader("{}")))
	if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unconfigured: %d", response.Code)
	}
	rig := newIngressRig(t)
	// A bearer token is neither required nor accepted: sending one changes nothing.
	response = rig.post(provisioningChanged(nil), func(r *http.Request) { r.Header.Set("Authorization", "Bearer irrelevant") })
	if response.Code != http.StatusAccepted {
		t.Fatalf("with a bearer: %d", response.Code)
	}
}

func TestNoRefusalOrLogCarriesTheSignatureOrTheEvent(t *testing.T) {
	rig := newIngressRig(t)
	body := provisioningChanged(nil)
	response := rig.post(body, func(r *http.Request) { r.Header.Set("Baobab-Signature", "hmac-sha256="+strings.Repeat("a", 64)) })
	for _, secret := range []string{strings.Repeat("a", 64), "ZURIBEANS", "tn_01k4m7x9q2v6c8r3d5f1h0j4", string(ingressKey.Secret)} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("a refusal echoed %q", secret)
		}
	}
}
