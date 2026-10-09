package legalactor

import "time"

// ProposeCommand follows Shared organisation/v2/ legal-actor-mandate-commands.
// The maker cannot set a mandate ID, status, approver or verified authority.
type ProposeCommand struct {
	TenantID string `json:"tenant_id"`
	OperatingOrganisationID string `json:"operating_organisation_id"`
	ResponsibleLegalEntityID string `json:"responsible_legal_entity_id"`
	Roles []string `json:"roles"`
	ActivityScope []string `json:"activity_scope"`
	MarketScope []string `json:"market_scope"`
	CapabilityScope []string `json:"capability_scope,omitempty"`
	AuthorityBasisReference string `json:"authority_basis_reference"`
	EvidenceReferences []string `json:"evidence_references"`
	EffectiveFrom time.Time `json:"effective_from"`
	EffectiveTo *time.Time `json:"effective_to,omitempty"`
	SupersedesMandateID string `json:"supersedes_mandate_id,omitempty"`
}

// DecideCommand is made by a DIFFERENT authenticated privileged human.
type DecideCommand struct {
	Decision string `json:"decision"`
	DecisionBasisReference string `json:"decision_basis_reference"`
	EvidenceReferences []string `json:"evidence_references"`
	LegalActorVerificationReference string `json:"legal_actor_verification_reference,omitempty"`
}

type CommandReceipt struct {
	MandateID string `json:"mandate_id"`
	TenantID string `json:"tenant_id"`
	OperatingOrganisationID string `json:"operating_organisation_id"`
	Status string `json:"status"`
	Command string `json:"command"`
	DecisionID string `json:"decision_id,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}
