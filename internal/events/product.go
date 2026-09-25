package events

// ADR-SHARED-011 ProductSubscription events, registered in
// baobab-platform/shared contracts/product/v1/asyncapi.yaml. Payloads carry
// identifiers and state only: never the reason, the principal or the
// eligibility evidence, which the Control Plane serves to authorised
// operators.
const (
	ProductSubscriptionClassified = "com.baobab-platform.product.subscription.classified.v1"
)

const productEventsSchema = "https://contracts.baobab-platform.com/product/v1/events.schema.json"

// productPayloadDefs maps each event type to its payload $def in
// contracts/product/v1/events.schema.json.
var productPayloadDefs = map[string]string{
	ProductSubscriptionClassified: "subscriptionClassifiedEventData",
}

// ProductPayloadDef returns the events.schema.json $def for a product event
// type, or "" when the type is not one of them.
func ProductPayloadDef(eventType string) string {
	return productPayloadDefs[eventType]
}

// NewProductSubscriptionEnvelope wraps a ProductSubscription event payload
// in the canonical envelope. A subscription belongs to its tenant, so the
// event is tenant-scoped.
func NewProductSubscriptionEnvelope(eventType, subject, tenantID string, data map[string]any, source, correlationID string) (Envelope, error) {
	return New(Params{
		Type:          eventType,
		Source:        source,
		Subject:       subject,
		DataSchema:    productEventsSchema + "#/$defs/" + productPayloadDefs[eventType],
		CorrelationID: correlationID,
		TenantID:      tenantID,
		Data:          data,
	})
}
