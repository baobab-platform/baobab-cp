// ADR-BCP-018 gate ORG-15 — relationship drift and audit lineage
// (sections 127-131, in ADR-BCP-008's drift model).
//
// Contract: baobab-platform/shared contracts/organisation/v1/observability.schema.json.

package domain

import (
	"encoding/json"
	"time"
)

// Relationship drift rules: the invariant a finding violates.
const (
	DriftAffiliateBasisNotInForce       = "AFFILIATE_BASIS_NOT_IN_FORCE"
	DriftGroupMembershipBasisNotInForce = "GROUP_MEMBERSHIP_BASIS_NOT_IN_FORCE"
	DriftRelationshipPastEffectiveTo    = "RELATIONSHIP_PAST_EFFECTIVE_TO"
	DriftTenantMappingToInactiveOrg     = "TENANT_MAPPING_TO_INACTIVE_ORGANISATION"
	DriftIamReferenceToInactiveOrg      = "IAM_REFERENCE_TO_INACTIVE_ORGANISATION"
	DriftAccountMembershipOnInactive    = "ACCOUNT_MEMBERSHIP_ON_INACTIVE_ACCOUNT"
	DriftCounterpartyRoleOnInactiveOrg  = "COUNTERPARTY_ROLE_ON_INACTIVE_ORGANISATION"
	// ADR-BCP-018 ORG-11: an INTERNAL subscription whose recorded
	// eligibility basis is no longer in force (a divestiture, an ended or
	// unverified relationship). Resolved by governed review or
	// reclassification, never by deleting the tenant or subscription.
	DriftInternalClassificationBasisNotInForce = "INTERNAL_CLASSIFICATION_BASIS_NOT_IN_FORCE"
)

// ADR-BCP-008 section 16 severities, most severe first.
var DriftSeverities = []string{"CRITICAL", "DEGRADED", "WARNING", "INFO"}

// RelationshipDriftFinding is one actionable relationship drift
// (ADR-BCP-018 section 128). It is resolved by review, never by an
// automatic cascade (section 129).
type RelationshipDriftFinding struct {
	Rule               string   `json:"rule"`
	DriftType          string   `json:"drift_type"`
	Severity           string   `json:"severity"`
	ResourceType       string   `json:"resource_type"`
	ResourceID         string   `json:"resource_id"`
	TenantID           string   `json:"tenant_id,omitempty"`
	OrganisationIDs    []string `json:"organisation_ids,omitempty"`
	RelatedResourceIDs []string `json:"related_resource_ids,omitempty"`
	ObservedState      string   `json:"observed_state"`
	DesiredState       string   `json:"desired_state"`
	Remediation        string   `json:"remediation"`
	AutoRepairable     bool     `json:"auto_repairable"`
}

// RelationshipDriftReport is one drift evaluation.
type RelationshipDriftReport struct {
	EvaluatedAt time.Time                  `json:"evaluated_at"`
	Findings    []RelationshipDriftFinding `json:"findings"`
	BySeverity  map[string]int             `json:"by_severity"`
	Truncated   bool                       `json:"truncated,omitempty"`
}

// AuditActorRecord is who performed an audited change.
type AuditActorRecord struct {
	ActorID   string `json:"actor_id"`
	ActorType string `json:"actor_type"`
	ClientID  string `json:"client_id,omitempty"`
}

// OrganisationAuditEntry is one audited change in an Organisation's lineage
// (ADR-BCP-018 section 131).
type OrganisationAuditEntry struct {
	AuditID       string           `json:"audit_id"`
	OccurredAt    time.Time        `json:"occurred_at"`
	Action        string           `json:"action"`
	Target        string           `json:"target"`
	TenantID      string           `json:"tenant_id,omitempty"`
	CorrelationID string           `json:"correlation_id,omitempty"`
	Actor         AuditActorRecord `json:"actor"`
	Payload       json.RawMessage  `json:"payload,omitempty"`
}
