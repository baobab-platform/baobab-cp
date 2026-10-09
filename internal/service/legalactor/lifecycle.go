package legalactor

import "time"

// LifecycleCommand is a privileged human command. It never takes a requested
// legal actor, Organisation, approval principal or provider authority.
type LifecycleCommand struct {
	Action                  string   `json:"action"`
	AuthorityBasisReference string   `json:"authority_basis_reference"`
	EvidenceReferences      []string `json:"evidence_references"`
}

// LifecycleReceipt is a CP-fact status receipt, NOT a downstream entitlement.
type LifecycleReceipt struct {
	MandateID               string    `json:"mandate_id"`
	TenantID                string    `json:"tenant_id"`
	OperatingOrganisationID string    `json:"operating_organisation_id"`
	Status                  string    `json:"status"`
	Action                  string    `json:"action"`
	RecordedAt              time.Time `json:"recorded_at"`
}
