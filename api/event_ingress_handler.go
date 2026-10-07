package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/eventingress"
)

// EventIngressHandler is POST /v1/integration/events (receiveEngineEvent): the signed delivery of one canonical engine event.
//
// It is not a business API and carries no bearer token: the delivery signature authenticates the sending engine's dispatcher, and
// the same operation is the only one that accepts it. The event is verified before it is parsed or stored, recorded durably before
// the sender is answered, and applied afterwards by the processor, so 202 and 200 mean "recorded", never "acted on". No answer,
// log line or metric carries the signature, key material or event data.
type EventIngressHandler struct {
	Receiver eventingress.Receiver
	// Wake, when set, is nudged (without blocking) after an event is recorded so the processor applies it promptly.
	Wake chan<- struct{}
}

func (h EventIngressHandler) Receive(w http.ResponseWriter, r *http.Request) {
	reject := func(rejection *eventingress.Rejection) {
		slog.WarnContext(r.Context(), "event delivery refused", "result", rejection.Result, "code", rejection.Code, "status", rejection.Status, "correlation_id", correlationID(r))
		problem(w, r, rejection.Status, rejection.Code, rejection.Detail, rejection.Retryable)
	}
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/cloudevents+json" {
		reject(&eventingress.Rejection{Status: http.StatusBadRequest, Code: "EVENT_DELIVERY_MALFORMED", Detail: "the content type must be application/cloudevents+json", Result: "malformed"})
		return
	}
	headers := eventingress.Headers{KeyID: r.Header.Get("Baobab-Key-Id"), Timestamp: r.Header.Get("Baobab-Timestamp"), Signature: r.Header.Get("Baobab-Signature")}
	// One byte over the limit is enough to know the body is too large without reading an unbounded stream.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, eventingress.MaxBodyBytes+1))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge) || len(body) > eventingress.MaxBodyBytes:
		reject(&eventingress.Rejection{Status: http.StatusRequestEntityTooLarge, Code: "EVENT_TOO_LARGE", Detail: "the event exceeds the 1 MiB delivery limit", Result: "too_large"})
		return
	case err != nil:
		reject(&eventingress.Rejection{Status: http.StatusBadRequest, Code: "EVENT_DELIVERY_MALFORMED", Detail: "the request body could not be read", Result: "malformed"})
		return
	}
	receipt, rejection := h.Receiver.Receive(r.Context(), headers, body)
	if rejection != nil {
		reject(rejection)
		return
	}
	if h.Wake != nil {
		select {
		case h.Wake <- struct{}{}:
		default:
		}
	}
	status := http.StatusAccepted
	if receipt.Status == eventingress.Duplicate {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"event_id": receipt.EventID, "status": string(receipt.Status), "received_at": receipt.ReceivedAt.UTC().Format(time.RFC3339)})
}
