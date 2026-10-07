// Package eventingress is the Control Plane's side of signed event delivery (Shared events/v1/signed-delivery.schema.json,
// control-plane/v1 receiveEngineEvent, docs/architecture/signed-event-delivery.md in baobab-platform/shared).
//
// An engine's outbox dispatcher POSTs one canonical event; the Control Plane proves who sent exactly which bytes when, records
// the event durably, and answers. Applying it is a separate step (Processor): a 2xx means "recorded", never "acted on", and the
// event is a trigger to inspect authoritative state, not the state. Delivery is at-least-once and the envelope's (source, id)
// is the idempotency boundary.
//
// Nothing here knows ERP. The accepted event types, their producers and sources come from the embedded event-ingress.yaml, and the
// handler that applies each type is registered by the caller.
package eventingress
