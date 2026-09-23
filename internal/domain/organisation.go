// Target path: baobab-platform/baobab-cp/internal/domain/organisation.go
//
// ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account
// and Tenant Relationship Model.
//
// Purpose
// -------
// Runtime domain types for the generic Organisation model and related
// relationship / mapping concepts. Control Plane is the runtime authority;
// Shared is the contract/schema authority.
//
// Design constraints (normative)
// ------------------------------
//   - Organisation != LegalEntity != Tenant
//   - Organisation is a profile/extension of CanonicalEntity, not a parallel
//     identity system. Identity is always canonical_entity_id.
//   - BUYER_ORGANISATION and SUPPLIER_ORGANISATION (ADR-BCP-016 / ADR-0006)
//     remain valid specialised EntityTypes; they migrate forward without
//     destructive rewrite.
//   - CorporateRelationship != CounterpartyRelationship (ADR-BCP-014)
//   - CorporateRelationship != ADR-BCP-012 operational LegalEntityRelationship
//   - CorporateRelationship != PlatformRelationship
//   - PlatformAccount != Tenant; PlatformAccount is not an authorization boundary
//   - CorporateGroup is not an authorization boundary
//   - Tenant.LegalEntityID remains a compatibility projection of the default
//     TenantLegalEntityMapping (is_default = true) until consumers migrate
//   - All consequential relationship resolution MUST fail closed when
//     authoritative evidence is missing or conflicted
//
// Reuse
// -----
// Prefer CanonicalEntity, CanonicalRelationship, Mapping, ExternalReference
// and MappingScope. Do not invent a second identity spine.

package domain

import (
	"time"
)

// ---------------------------------------------------------------------------
// Verification and lifecycle (aligned with Shared contracts/organisation/v1)
// ---------------------------------------------------------------------------

// VerificationState is the evidence state for identity or relationship claims.
// Consequential decisions MUST fail closed when state is anything other than
// VerificationVerified.
type VerificationState string

const (
	VerificationUnverified    VerificationState = "UNVERIFIED"
	VerificationPendingReview VerificationState = "PENDING_REVIEW"
	VerificationVerified      VerificationState = "VERIFIED"
	VerificationConflicted    VerificationState = "CONFLICTED"
	VerificationRejected      VerificationState = "REJECTED"
	VerificationExpired       VerificationState = "EXPIRED"
)

// IsAuthoritative reports whether this state may be used for consequential
// decisions (INTERNAL eligibility, related-party treatment, etc.).
func (s VerificationState) IsAuthoritative() bool {
	return s == VerificationVerified
}

// OrganisationForm describes what sort of organisation this is.
// It MUST NOT encode commercial role (SUPPLIER/CUSTOMER); those are
// relationships or CounterpartyRoles (ADR-BCP-014).
type OrganisationForm string

const (
	OrgFormCompany                  OrganisationForm = "COMPANY"
	OrgFormPartnership              OrganisationForm = "PARTNERSHIP"
	OrgFormTrust                    OrganisationForm = "TRUST"
	OrgFormAssociation              OrganisationForm = "ASSOCIATION"
	OrgFormPublicBody               OrganisationForm = "PUBLIC_BODY"
	OrgFormNonprofit                OrganisationForm = "NONPROFIT"
	OrgFormUnincorporated           OrganisationForm = "UNINCORPORATED_ORGANISATION"
	OrgFormSoleProprietor           OrganisationForm = "SOLE_PROPRIETOR"
	OrgFormCooperative              OrganisationForm = "COOPERATIVE"
	OrgFormGovernmentEntity         OrganisationForm = "GOVERNMENT_ENTITY"
	OrgFormFinancialInstitution     OrganisationForm = "FINANCIAL_INSTITUTION"
	OrgFormOther                    OrganisationForm = "OTHER"
)

// ---------------------------------------------------------------------------
// Organisation profile (anchored to CanonicalEntity)
// ---------------------------------------------------------------------------

// Organisation is the runtime profile for a real-world organisation.
// Identity is always CanonicalEntityID; this type never stands alone.
//
// Existing BUYER_ORGANISATION / SUPPLIER_ORGANISATION CanonicalEntity rows
// may gain an Organisation profile without changing their EntityType.
type Organisation struct {
	// CanonicalEntityID is the sole identity. Required.
	CanonicalEntityID string `json:"canonical_entity_id"`

	DisplayName      string           `json:"display_name"`
	OfficialName     string           `json:"official_name,omitempty"`
	TradingNames     []string         `json:"trading_names,omitempty"`
	OrganisationForm OrganisationForm `json:"organisation_form,omitempty"`
	Jurisdiction     string           `json:"jurisdiction,omitempty"`

	VerificationState VerificationState `json:"verification_state"`
	SourceAuthority   string            `json:"source_authority"`
	Status            string            `json:"status"` // DRAFT|VALIDATED|ACTIVE|...

	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`

	// Metadata is non-authoritative extension data.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ---------------------------------------------------------------------------
// LegalEntityProfile (runtime; first-party ids still governed by Shared registry)
// ---------------------------------------------------------------------------

// LegalEntityProfile is a legally recognised person attached to an Organisation.
// organisation_id and legal_entity_id remain distinct even when 1:1.
//
// First-party ids (NABHOLD, ZURIBEANS, THAMANI-GLOBAL, EQUATOR-ESTATE) continue
// to match Shared contracts/legal-entity/registry.yaml. External profiles are
// Control-Plane-issued and do not require a Shared registry entry (tenancy v1.1).
type LegalEntityProfile struct {
	LegalEntityID              string            `json:"legal_entity_id"`
	OrganisationID             string            `json:"organisation_id"` // CanonicalEntityID
	LegalName                  string            `json:"legal_name"`
	JurisdictionOfIncorporation string           `json:"jurisdiction_of_incorporation,omitempty"`
	LegalStatus                string            `json:"legal_status"`
	SourceAuthority            string            `json:"source_authority"`
	VerificationState          VerificationState `json:"verification_state"`
	EffectiveFrom              time.Time         `json:"effective_from"`
	EffectiveTo                *time.Time        `json:"effective_to,omitempty"`
	EvidenceReferences         []string          `json:"evidence_references,omitempty"`
	Metadata                   map[string]any    `json:"metadata,omitempty"`
}

// ---------------------------------------------------------------------------
// CorporateRelationship (ownership / control — NOT commercial, NOT platform)
// ---------------------------------------------------------------------------

// CorporateRelationshipType enumerates ownership/control edges.
// Extensible. Does not include commercial roles.
type CorporateRelationshipType string

const (
	CorpRelOwns         CorporateRelationshipType = "OWNS"
	CorpRelControls     CorporateRelationshipType = "CONTROLS"
	CorpRelParentOf     CorporateRelationshipType = "PARENT_OF"
	CorpRelSubsidiaryOf CorporateRelationshipType = "SUBSIDIARY_OF"
	CorpRelAffiliateOf  CorporateRelationshipType = "AFFILIATE_OF"
	CorpRelJointVenture CorporateRelationshipType = "JOINT_VENTURE"
	CorpRelSisterOf     CorporateRelationshipType = "SISTER_OF"
	CorpRelBranchOf     CorporateRelationshipType = "BRANCH_OF"
	CorpRelRelatedTo    CorporateRelationshipType = "RELATED_TO"
)

// CorporateRelationship is an effective-dated ownership/control edge between
// two Organisations. Used for INTERNAL subscription eligibility (ADR-BCP-017),
// related-party treatment and corporate reporting.
//
// MUST NOT be used as a runtime authorization boundary.
// same corporate group != cross-tenant access.
type CorporateRelationship struct {
	ID                   string                    `json:"id"`
	SourceOrganisationID string                    `json:"source_organisation_id"`
	TargetOrganisationID string                    `json:"target_organisation_id"`
	RelationshipType     CorporateRelationshipType `json:"relationship_type"`
	OwnershipPercentage  *float64                  `json:"ownership_percentage,omitempty"`
	VerificationState    VerificationState         `json:"verification_state"`
	Status               string                    `json:"status"` // ACTIVE|SUSPENDED|RETIRED|CONFLICTED
	EffectiveFrom        time.Time                 `json:"effective_from"`
	EffectiveTo          *time.Time                `json:"effective_to,omitempty"`
	EvidenceReferences   []string                  `json:"evidence_references,omitempty"`
	SourceAuthority      string                    `json:"source_authority"`
	Metadata             map[string]any            `json:"metadata,omitempty"`
}

// IsConsequential reports whether this relationship may be used for
// INTERNAL eligibility or related-party decisions. Fail closed otherwise.
func (r CorporateRelationship) IsConsequential(at time.Time) bool {
	if !r.VerificationState.IsAuthoritative() {
		return false
	}
	if r.Status != "ACTIVE" {
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

// ---------------------------------------------------------------------------
// CorporateGroup (projection — not an authorization boundary)
// ---------------------------------------------------------------------------

// CorporateGroup is a governed economic/corporate group projection derived
// from the CorporateRelationship graph. Membership confers no runtime
// permission.
type CorporateGroup struct {
	ID                string     `json:"id"`
	DisplayName       string     `json:"display_name"`
	RootOrganisationID string    `json:"root_organisation_id"`
	Status            string     `json:"status"`
	EffectiveFrom     time.Time  `json:"effective_from"`
	EffectiveTo       *time.Time `json:"effective_to,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// CorporateGroupMembership links an Organisation to a CorporateGroup.
type CorporateGroupMembership struct {
	ID               string     `json:"id"`
	CorporateGroupID string     `json:"corporate_group_id"`
	OrganisationID   string     `json:"organisation_id"`
	MembershipType   string     `json:"membership_type"` // ROOT|SUBSIDIARY|AFFILIATE|...
	Status           string     `json:"status"`
	EffectiveFrom    time.Time  `json:"effective_from"`
	EffectiveTo      *time.Time `json:"effective_to,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// ---------------------------------------------------------------------------
// PlatformRelationship and PlatformAccount
// ---------------------------------------------------------------------------

// PlatformRelationshipType answers: how does this Organisation relate to Baobab?
// Privileged types (PLATFORM_OWNER, PLATFORM_OPERATOR, PLATFORM_GROUP_AFFILIATE)
// are server-authoritative; applicants MUST NOT self-assign them (ADR-BCP-017).
type PlatformRelationshipType string

const (
	PlatformRelOwner           PlatformRelationshipType = "PLATFORM_OWNER"
	PlatformRelOperator        PlatformRelationshipType = "PLATFORM_OPERATOR"
	PlatformRelGroupAffiliate  PlatformRelationshipType = "PLATFORM_GROUP_AFFILIATE"
	PlatformRelPartner         PlatformRelationshipType = "PLATFORM_PARTNER"
	PlatformRelExternalClient  PlatformRelationshipType = "EXTERNAL_CLIENT"
	PlatformRelManagedEntity   PlatformRelationshipType = "MANAGED_ENTITY"
)

// PlatformRelationship is lifecycle-managed and server-authoritative for
// privileged types. PLATFORM_GROUP_AFFILIATE != runtime permission.
type PlatformRelationship struct {
	ID                string                   `json:"id"`
	OrganisationID    string                   `json:"organisation_id"`
	RelationshipType  PlatformRelationshipType `json:"relationship_type"`
	VerificationState VerificationState        `json:"verification_state"`
	Status            string                   `json:"status"`
	EffectiveFrom     time.Time                `json:"effective_from"`
	EffectiveTo       *time.Time               `json:"effective_to,omitempty"`
	EvidenceReferences []string                `json:"evidence_references,omitempty"`
	SourceAuthority   string                   `json:"source_authority"`
	Metadata          map[string]any           `json:"metadata,omitempty"`
}

// IsConsequential reports whether this platform relationship may be used for
// INTERNAL eligibility derivation. Fail closed otherwise.
func (r PlatformRelationship) IsConsequential(at time.Time) bool {
	if !r.VerificationState.IsAuthoritative() {
		return false
	}
	if r.Status != "ACTIVE" {
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

// PlatformAccount is a commercial/administrative grouping. It is NOT a Tenant
// and does NOT confer data access or authorization.
// same PlatformAccount != shared authorization.
type PlatformAccount struct {
	ID                    string     `json:"id"`
	DisplayName           string     `json:"display_name"`
	PrimaryOrganisationID string     `json:"primary_organisation_id,omitempty"`
	Status                string     `json:"status"`
	EffectiveFrom         time.Time  `json:"effective_from"`
	EffectiveTo           *time.Time `json:"effective_to,omitempty"`
	BillingReference      string     `json:"billing_reference,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

// PlatformAccountMembership links an Organisation or Tenant to a PlatformAccount
// for commercial purposes only.
type PlatformAccountMembership struct {
	ID                string     `json:"id"`
	PlatformAccountID string     `json:"platform_account_id"`
	MemberType        string     `json:"member_type"` // ORGANISATION|TENANT
	MemberID          string     `json:"member_id"`
	Role              string     `json:"role,omitempty"` // commercial role, not IAM
	Status            string     `json:"status"`
	EffectiveFrom     time.Time  `json:"effective_from"`
	EffectiveTo       *time.Time `json:"effective_to,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// ---------------------------------------------------------------------------
// Explicit tenant mappings (replace implicit singular LegalEntityID over time)
// ---------------------------------------------------------------------------

// TenantOrganisationMapping associates a Tenant with one or more Organisations.
type TenantOrganisationMapping struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	OrganisationID string     `json:"organisation_id"` // CanonicalEntityID
	IsDefault      bool       `json:"is_default"`
	Status         string     `json:"status"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveTo    *time.Time `json:"effective_to,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// TenantLegalEntityMapping associates a Tenant with one or more LegalEntityProfiles.
// The row with IsDefault=true is the compatibility projection of Tenant.LegalEntityID.
type TenantLegalEntityMapping struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	LegalEntityID string     `json:"legal_entity_id"`
	IsDefault     bool       `json:"is_default"`
	Status        string     `json:"status"`
	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}
