package events

import "time"

// ADR-BCP-018 section 124 organisation lifecycle events, registered in
// baobab-platform/shared contracts/organisation/v1/asyncapi.yaml. Payloads
// carry identifiers and state only (section 125): never names, evidence
// references, registration numbers or metadata.
const (
	OrganisationCreated                = "com.baobab-platform.control-plane.organisation.created.v1"
	OrganisationVerified               = "com.baobab-platform.control-plane.organisation.verified.v1"
	OrganisationSuspended              = "com.baobab-platform.control-plane.organisation.suspended.v1"
	LegalEntityVerified                = "com.baobab-platform.control-plane.legal-entity.verified.v1"
	CorporateRelationshipActivated     = "com.baobab-platform.control-plane.corporate-relationship.activated.v1"
	CorporateRelationshipEnded         = "com.baobab-platform.control-plane.corporate-relationship.ended.v1"
	CorporateRelationshipConflicted    = "com.baobab-platform.control-plane.corporate-relationship.conflicted.v1"
	PlatformRelationshipActivated      = "com.baobab-platform.control-plane.platform-relationship.activated.v1"
	PlatformRelationshipEnded          = "com.baobab-platform.control-plane.platform-relationship.ended.v1"
	PlatformAccountCreated             = "com.baobab-platform.control-plane.platform-account.created.v1"
	PlatformAccountMembershipChanged   = "com.baobab-platform.control-plane.platform-account-membership.changed.v1"
	TenantOrganisationMappingActivated = "com.baobab-platform.control-plane.tenant-organisation-mapping.activated.v1"
	TenantLegalEntityMappingActivated  = "com.baobab-platform.control-plane.tenant-legal-entity-mapping.activated.v1"
	PlatformAccountStatusChanged       = "com.baobab-platform.control-plane.platform-account.status-changed.v1"
	TenantPlatformAccountBound         = "com.baobab-platform.control-plane.tenant-platform-account-binding.bound.v1"
	TenantPlatformAccountBindingEnded  = "com.baobab-platform.control-plane.tenant-platform-account-binding.ended.v1"
)

// organisationPayloadDefs maps each event type to its payload $def in
// contracts/organisation/v1/events.schema.json.
var organisationPayloadDefs = map[string]string{
	OrganisationCreated:                "OrganisationCreated",
	OrganisationVerified:               "OrganisationVerified",
	OrganisationSuspended:              "OrganisationSuspended",
	LegalEntityVerified:                "LegalEntityVerified",
	CorporateRelationshipActivated:     "CorporateRelationshipActivated",
	CorporateRelationshipEnded:         "CorporateRelationshipEnded",
	CorporateRelationshipConflicted:    "CorporateRelationshipConflicted",
	PlatformRelationshipActivated:      "PlatformRelationshipActivated",
	PlatformRelationshipEnded:          "PlatformRelationshipEnded",
	PlatformAccountCreated:             "PlatformAccountCreated",
	PlatformAccountMembershipChanged:   "PlatformAccountMembershipChanged",
	TenantOrganisationMappingActivated: "TenantOrganisationMappingActivated",
	TenantLegalEntityMappingActivated:  "TenantLegalEntityMappingActivated",
	PlatformAccountStatusChanged:       "PlatformAccountStatusChanged",
	TenantPlatformAccountBound:         "TenantPlatformAccountBound",
	TenantPlatformAccountBindingEnded:  "TenantPlatformAccountBindingEnded",
}

const organisationEventsSchema = "https://contracts.baobab-platform.com/organisation/v1/events.schema.json"

// OrganisationPayloadDef returns the events.schema.json $def for an
// organisation event type, or "" when the type is not one of them.
func OrganisationPayloadDef(eventType string) string {
	return organisationPayloadDefs[eventType]
}

// OrganisationChange is one state change of the ADR-BCP-018 model: the
// audit record it always produces and, when EventType is set, the lifecycle
// event it publishes. Changes that are audited but have no section 124 event
// (for example a PENDING relationship being recorded) leave EventType empty.
type OrganisationChange struct {
	// AuditAction is the audit_events action, e.g. "corporate_relationship.verified".
	AuditAction string
	// Target names the changed record, e.g. "corporate-relationship/crel_...".
	Target string
	// AggregateType and AggregateID key the outbox row (AggregateID is the row uuid).
	AggregateType string
	AggregateID   string
	// TenantID is set only for tenant-scoped records (mappings).
	TenantID  string
	EventType string
	Data      map[string]any
	// AuditPayload is recorded in audit_events. It may hold more than the
	// event (for example the evidence references behind a verification),
	// because audit is access-controlled and the event stream is not.
	AuditPayload map[string]any
}

// NewOrganisationEnvelope builds the canonical envelope for change.
func NewOrganisationEnvelope(change OrganisationChange, source, correlationID string) (Envelope, error) {
	return New(Params{
		Type:          change.EventType,
		Source:        source,
		Subject:       change.Target,
		DataSchema:    organisationEventsSchema + "#/$defs/" + organisationPayloadDefs[change.EventType],
		CorrelationID: correlationID,
		TenantID:      change.TenantID,
		Data:          change.Data,
	})
}

// Timestamp renders t the way event payloads carry date-times.
func Timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
