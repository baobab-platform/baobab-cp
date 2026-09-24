// Target path: internal/domain/organisation.go
//
// ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account
// and Tenant Relationship Model (runtime domain types).
//
// Control Plane is the runtime authority; Shared is the contract authority.
// Vocabulary and required fields align with contracts/organisation/v1.
//
// Invariants:
//   Organisation != LegalEntity != Tenant
//   CorporateRelationship != CounterpartyRelationship / PlatformRelationship
//   PlatformAccount != Tenant; CorporateGroup != authorization boundary
//   Consequential resolution fails closed without authoritative evidence

package domain

import "time"

// VerificationState is the evidence state for organisation-domain records.
type VerificationState string

const (
	VerificationUnverified    VerificationState = "UNVERIFIED"
	VerificationPendingReview VerificationState = "PENDING_REVIEW"
	VerificationVerified      VerificationState = "VERIFIED"
	VerificationConflicted    VerificationState = "CONFLICTED"
	VerificationRejected      VerificationState = "REJECTED"
	VerificationExpired       VerificationState = "EXPIRED"
)

// IsAuthoritative reports whether the state may be used for consequential decisions.
func (s VerificationState) IsAuthoritative() bool {
	return s == VerificationVerified
}

// OrganisationForm is organisational form only — never commercial role.
type OrganisationForm string

const (
	OrgFormCompany                    OrganisationForm = "COMPANY"
	OrgFormPartnership                OrganisationForm = "PARTNERSHIP"
	OrgFormTrust                      OrganisationForm = "TRUST"
	OrgFormAssociation                OrganisationForm = "ASSOCIATION"
	OrgFormPublicBody                 OrganisationForm = "PUBLIC_BODY"
	OrgFormNonprofit                  OrganisationForm = "NONPROFIT"
	OrgFormUnincorporatedOrganisation OrganisationForm = "UNINCORPORATED_ORGANISATION"
	OrgFormSoleProprietor             OrganisationForm = "SOLE_PROPRIETOR"
	OrgFormCooperative                OrganisationForm = "COOPERATIVE"
	OrgFormGovernmentEntity           OrganisationForm = "GOVERNMENT_ENTITY"
	OrgFormFinancialInstitution       OrganisationForm = "FINANCIAL_INSTITUTION"
	OrgFormOther                      OrganisationForm = "OTHER"
)

// Organisation is a profile on CanonicalEntity (identity = CanonicalEntityID).
type Organisation struct {
	CanonicalEntityID string            `json:"canonical_entity_id"`
	DisplayName       string            `json:"display_name"`
	OfficialName      string            `json:"official_name,omitempty"`
	TradingNames      []string          `json:"trading_names,omitempty"`
	OrganisationForm  OrganisationForm  `json:"organisation_form,omitempty"`
	Jurisdiction      string            `json:"jurisdiction,omitempty"`
	VerificationState VerificationState `json:"verification_state"`
	SourceAuthority   string            `json:"source_authority"`
	Status            string            `json:"status"`
	EffectiveFrom     time.Time         `json:"effective_from"`
	EffectiveTo       *time.Time        `json:"effective_to,omitempty"`
	Metadata          map[string]any    `json:"metadata,omitempty"`
}

// LegalEntityProfile attaches a legal person to an Organisation.
// Runtime authority is Control Plane; Shared governs first-party registry ids only.
type LegalEntityProfile struct {
	LegalEntityID               string            `json:"legal_entity_id"`
	OrganisationID              string            `json:"organisation_id"`
	LegalName                   string            `json:"legal_name"`
	JurisdictionOfIncorporation string            `json:"jurisdiction_of_incorporation,omitempty"`
	LegalStatus                 string            `json:"legal_status"`
	SourceAuthority             string            `json:"source_authority"`
	VerificationState           VerificationState `json:"verification_state"`
	EffectiveFrom               time.Time         `json:"effective_from"`
	EffectiveTo                 *time.Time        `json:"effective_to,omitempty"`
	EvidenceReferences          []string          `json:"evidence_references,omitempty"`
	Metadata                    map[string]any    `json:"metadata,omitempty"`
}

// CorporateRelationshipType is the ADR-BCP-018 vocabulary (no redundant inverses).
type CorporateRelationshipType string

const (
	CorpRelOwns             CorporateRelationshipType = "OWNS"
	CorpRelControls         CorporateRelationshipType = "CONTROLS"
	CorpRelBranchOf         CorporateRelationshipType = "BRANCH_OF"
	CorpRelAffiliateOf      CorporateRelationshipType = "AFFILIATE_OF"
	CorpRelJointVentureWith CorporateRelationshipType = "JOINT_VENTURE_WITH"
	CorpRelSuccessorOf      CorporateRelationshipType = "SUCCESSOR_OF"
)

// CorporateRelationship is an effective-dated ownership/control edge.
// MUST NOT be used as a runtime authorization boundary.
type CorporateRelationship struct {
	ID                   string                    `json:"id"`
	SourceOrganisationID string                    `json:"source_organisation_id"`
	TargetOrganisationID string                    `json:"target_organisation_id"`
	RelationshipType     CorporateRelationshipType `json:"relationship_type"`
	OwnershipPercentage  *float64                  `json:"ownership_percentage,omitempty"`
	ControlBasis         string                    `json:"control_basis,omitempty"`
	DirectOrDerived      string                    `json:"direct_or_derived,omitempty"`
	VerificationState    VerificationState         `json:"verification_state"`
	Status               string                    `json:"status"`
	EffectiveFrom        time.Time                 `json:"effective_from"`
	EffectiveTo          *time.Time                `json:"effective_to,omitempty"`
	SourceAuthority      string                    `json:"source_authority"`
	EvidenceReferences   []string                  `json:"evidence_references,omitempty"`
	VerifiedBy           string                    `json:"verified_by,omitempty"`
	VerifiedAt           *time.Time                `json:"verified_at,omitempty"`
	Classification       string                    `json:"classification,omitempty"`
	Metadata             map[string]any            `json:"metadata,omitempty"`
}

// IsConsequential reports whether this edge may drive INTERNAL eligibility
// or related-party decisions. Fail closed without VERIFIED + ACTIVE + window.
// Only OWNS and CONTROLS are consequential for INTERNAL eligibility policy.
func (r CorporateRelationship) IsConsequential(at time.Time) bool {
	if !r.VerificationState.IsAuthoritative() {
		return false
	}
	if r.Status != "ACTIVE" {
		return false
	}
	if r.SourceAuthority == "" {
		return false
	}
	if at.Before(r.EffectiveFrom) {
		return false
	}
	if r.EffectiveTo != nil && !at.Before(*r.EffectiveTo) {
		return false
	}
	switch r.RelationshipType {
	case CorpRelOwns, CorpRelControls:
		return true
	default:
		return false
	}
}

// CorporateGroup is a projection; root is optional when no unique parent exists.
type CorporateGroup struct {
	ID                 string         `json:"id"`
	DisplayName        string         `json:"display_name"`
	RootOrganisationID string         `json:"root_organisation_id,omitempty"`
	Status             string         `json:"status"`
	EffectiveFrom      time.Time      `json:"effective_from"`
	EffectiveTo        *time.Time     `json:"effective_to,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

// CorporateGroupMembership is derived from CorporateRelationship edges.
// Confers no runtime permission. Must carry derivation lineage.
type CorporateGroupMembership struct {
	ID                   string         `json:"id"`
	CorporateGroupID     string         `json:"corporate_group_id"`
	OrganisationID       string         `json:"organisation_id"`
	MembershipType       string         `json:"membership_type"`
	Status               string         `json:"status"`
	EffectiveFrom        time.Time      `json:"effective_from"`
	EffectiveTo          *time.Time     `json:"effective_to,omitempty"`
	BasisRelationshipIDs []string       `json:"basis_relationship_ids"`
	DerivedAt            time.Time      `json:"derived_at"`
	DerivationVersion    string         `json:"derivation_version"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

// PlatformRelationshipType enumerates platform affiliation kinds.
type PlatformRelationshipType string

const (
	PlatformRelOwner          PlatformRelationshipType = "PLATFORM_OWNER"
	PlatformRelOperator       PlatformRelationshipType = "PLATFORM_OPERATOR"
	PlatformRelGroupAffiliate PlatformRelationshipType = "PLATFORM_GROUP_AFFILIATE"
	PlatformRelPartner        PlatformRelationshipType = "PLATFORM_PARTNER"
	PlatformRelExternalClient PlatformRelationshipType = "EXTERNAL_CLIENT"
	PlatformRelManagedEntity  PlatformRelationshipType = "MANAGED_ENTITY"
)

// PlatformRelationship links an Organisation to the platform.
// An organisation may hold concurrent relationship types.
type PlatformRelationship struct {
	ID                  string                   `json:"id"`
	PlatformID          string                   `json:"platform_id"`
	OrganisationID      string                   `json:"organisation_id"`
	RelationshipType    PlatformRelationshipType `json:"relationship_type"`
	VerificationState   VerificationState        `json:"verification_state"`
	Status              string                   `json:"status"`
	EffectiveFrom       time.Time                `json:"effective_from"`
	EffectiveTo         *time.Time               `json:"effective_to,omitempty"`
	BasisRelationshipID string                   `json:"basis_relationship_id,omitempty"`
	AdmissionDecisionID string                   `json:"admission_decision_id,omitempty"`
	SourceAuthority     string                   `json:"source_authority"`
	EvidenceReferences  []string                 `json:"evidence_references,omitempty"`
	VerifiedBy          string                   `json:"verified_by,omitempty"`
	VerifiedAt          *time.Time               `json:"verified_at,omitempty"`
	Metadata            map[string]any           `json:"metadata,omitempty"`
}

// IsActive reports whether the platform relationship is in force at at.
func (r PlatformRelationship) IsActive(at time.Time) bool {
	if r.Status != "ACTIVE" && r.Status != "PENDING" {
		return false
	}
	if !r.VerificationState.IsAuthoritative() && r.Status != "PENDING" {
		return false
	}
	if at.Before(r.EffectiveFrom) {
		return false
	}
	if r.EffectiveTo != nil && !at.Before(*r.EffectiveTo) {
		return false
	}
	return true
}

// PlatformAccount is commercial/administrative. NOT a Tenant.
type PlatformAccount struct {
	ID                    string         `json:"id"`
	DisplayName           string         `json:"display_name"`
	PrimaryOrganisationID string         `json:"primary_organisation_id,omitempty"`
	Status                string         `json:"status"`
	EffectiveFrom         time.Time      `json:"effective_from"`
	EffectiveTo           *time.Time     `json:"effective_to,omitempty"`
	BillingReference      string         `json:"billing_reference,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

// PlatformAccountMembership links organisations or tenants to an account.
type PlatformAccountMembership struct {
	ID                string         `json:"id"`
	PlatformAccountID string         `json:"platform_account_id"`
	MemberType        string         `json:"member_type"`
	MemberID          string         `json:"member_id"`
	Role              string         `json:"role,omitempty"`
	Status            string         `json:"status"`
	EffectiveFrom     time.Time      `json:"effective_from"`
	EffectiveTo       *time.Time     `json:"effective_to,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// TenantOrganisationMapping is an explicit tenant↔organisation link.
type TenantOrganisationMapping struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	OrganisationID string         `json:"organisation_id"`
	MappingRole    string         `json:"mapping_role"`
	IsDefault      bool           `json:"is_default"`
	Status         string         `json:"status"`
	EffectiveFrom  time.Time      `json:"effective_from"`
	EffectiveTo    *time.Time     `json:"effective_to,omitempty"`
	Provenance     string         `json:"provenance"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// TenantLegalEntityMapping is an explicit tenant↔legal-entity link.
// The default row projects Tenant.LegalEntityID for compatibility.
type TenantLegalEntityMapping struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	LegalEntityID string         `json:"legal_entity_id"`
	MappingRole   string         `json:"mapping_role"`
	IsDefault     bool           `json:"is_default"`
	Status        string         `json:"status"`
	EffectiveFrom time.Time      `json:"effective_from"`
	EffectiveTo   *time.Time     `json:"effective_to,omitempty"`
	Provenance    string         `json:"provenance"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// InternalEligibilityEvidence is the input to DeriveInternalEligibility.
type InternalEligibilityEvidence struct {
	OrganisationID         string
	PlatformRelationships  []PlatformRelationship
	CorporateRelationships []CorporateRelationship
	EvaluatedAt            time.Time
}

// DeriveInternalEligibility decides ADR-BCP-017 INTERNAL product eligibility.
//
// Rules (fail closed):
//   - PLATFORM_OWNER or PLATFORM_OPERATOR (verified, active) → eligible
//   - PLATFORM_GROUP_AFFILIATE only when linked by consequential OWNS/CONTROLS
//     to a known platform-owner organisation
//   - EXTERNAL_CLIENT / PARTNER alone → not eligible
//   - Corporate group membership / PlatformAccount never considered
func DeriveInternalEligibility(ev InternalEligibilityEvidence, platformOwnerOrgIDs map[string]struct{}) bool {
	at := ev.EvaluatedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	for _, pr := range ev.PlatformRelationships {
		if pr.OrganisationID != ev.OrganisationID {
			continue
		}
		if !pr.IsActive(at) || !pr.VerificationState.IsAuthoritative() {
			continue
		}
		switch pr.RelationshipType {
		case PlatformRelOwner, PlatformRelOperator:
			return true
		case PlatformRelGroupAffiliate:
			if affiliateOwnsPathToOwner(ev.OrganisationID, ev.CorporateRelationships, platformOwnerOrgIDs, at) {
				return true
			}
		}
	}
	return false
}

func affiliateOwnsPathToOwner(orgID string, rels []CorporateRelationship, owners map[string]struct{}, at time.Time) bool {
	if len(owners) == 0 {
		return false
	}
	for _, r := range rels {
		if !r.IsConsequential(at) {
			continue
		}
		var other string
		switch {
		case r.SourceOrganisationID == orgID:
			other = r.TargetOrganisationID
		case r.TargetOrganisationID == orgID:
			other = r.SourceOrganisationID
		default:
			continue
		}
		if _, ok := owners[other]; ok {
			return true
		}
	}
	return false
}
