package events

// ADR-BCP-017 section 40 ClientApplication lifecycle events, registered in
// baobab-platform/shared contracts/admission/v1/asyncapi.yaml. Payloads
// carry identifiers and state only: never names, text, evidence or
// principals.
const (
	ClientApplicationCreated              = "com.baobab-platform.control-plane.client-application.created.v1"
	ClientApplicationSubmitted            = "com.baobab-platform.control-plane.client-application.submitted.v1"
	ClientApplicationInformationRequested = "com.baobab-platform.control-plane.client-application.information-requested.v1"
	ClientApplicationWithdrawn            = "com.baobab-platform.control-plane.client-application.withdrawn.v1"
	ClientApplicationApproved             = "com.baobab-platform.control-plane.client-application.approved.v1"
	ClientApplicationRejected             = "com.baobab-platform.control-plane.client-application.rejected.v1"
)

// admissionPayloadDefs maps each event type to its payload $def in
// contracts/admission/v1/events.schema.json.
var admissionPayloadDefs = map[string]string{
	ClientApplicationCreated:              "ClientApplicationCreated",
	ClientApplicationSubmitted:            "ClientApplicationSubmitted",
	ClientApplicationInformationRequested: "ClientApplicationInformationRequested",
	ClientApplicationWithdrawn:            "ClientApplicationWithdrawn",
	ClientApplicationApproved:             "ClientApplicationApproved",
	ClientApplicationRejected:             "ClientApplicationRejected",
}

const admissionEventsSchema = "https://contracts.baobab-platform.com/admission/v1/events.schema.json"

// AdmissionPayloadDef returns the events.schema.json $def for an admission
// event type, or "" when the type is not one of them.
func AdmissionPayloadDef(eventType string) string {
	return admissionPayloadDefs[eventType]
}

// NewAdmissionEnvelope wraps an admission event payload in the canonical
// envelope. Applications belong to no tenant, so the scope is platform.
func NewAdmissionEnvelope(eventType, subject string, data map[string]any, source, correlationID string) (Envelope, error) {
	return New(Params{
		Type:          eventType,
		Source:        source,
		Subject:       subject,
		DataSchema:    admissionEventsSchema + "#/$defs/" + admissionPayloadDefs[eventType],
		CorrelationID: correlationID,
		Data:          data,
	})
}
