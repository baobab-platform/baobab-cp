// ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account
// and Tenant Relationship Model (runtime domain types).
//
// Control Plane is the runtime authority; baobab-platform/shared
// contracts/organisation/v1 is the contract authority. Field names, enums
// and the Validate rules below mirror that contract.
//
// Invariants:
//   Organisation != LegalEntity != Tenant
//   CorporateRelationship != CounterpartyRelationship / PlatformRelationship
//   PlatformAccount != Tenant; CorporateGroup != authorization boundary
//   Applicant claim != verified fact; consequential resolution fails closed

package domain

import (
	"errors"
	"fmt"
	"time"
)

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

func (s VerificationState) valid() bool {
	switch s {
	case VerificationUnverified, VerificationPendingReview, VerificationVerified,
		VerificationConflicted, VerificationRejected, VerificationExpired:
		return true
	}
	return false
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

// Legal status of a legal person. UNKNOWN until an authoritative source confirms it.
const (
	LegalStatusActive           = "ACTIVE"
	LegalStatusDormant          = "DORMANT"
	LegalStatusInAdministration = "IN_ADMINISTRATION"
	LegalStatusInLiquidation    = "IN_LIQUIDATION"
	LegalStatusDissolved        = "DISSOLVED"
	LegalStatusUnknown          = "UNKNOWN"
)

// Record lifecycle statuses shared by relationships, memberships and mappings.
const (
	RelationshipStatusPending   = "PENDING"
	RelationshipStatusActive    = "ACTIVE"
	RelationshipStatusSuspended = "SUSPENDED"
	RelationshipStatusEnded     = "ENDED"
	RelationshipStatusRetired   = "RETIRED"
)

// OrganisationIdentifier is a registration, tax or other official identifier.
type OrganisationIdentifier struct {
	Type                string     `json:"type"`
	Value               string     `json:"value"`
	IssuingJurisdiction string     `json:"issuing_jurisdiction,omitempty"`
	Issuer              string     `json:"issuer,omitempty"`
	Verified            bool       `json:"verified,omitempty"`
	ValidFrom           *time.Time `json:"valid_from,omitempty"`
	ValidTo             *time.Time `json:"valid_to,omitempty"`
}

// OrganisationAddress is a typed postal address.
type OrganisationAddress struct {
	AddressType string   `json:"address_type"`
	Lines       []string `json:"lines,omitempty"`
	Locality    string   `json:"locality,omitempty"`
	Region      string   `json:"region,omitempty"`
	PostalCode  string   `json:"postal_code,omitempty"`
	CountryCode string   `json:"country_code,omitempty"`
	Verified    bool     `json:"verified,omitempty"`
}

// Organisation is a profile on CanonicalEntity (identity = CanonicalEntityID).
type Organisation struct {
	CanonicalEntityID  string                   `json:"canonical_entity_id"`
	DisplayName        string                   `json:"display_name"`
	OfficialName       string                   `json:"official_name,omitempty"`
	TradingNames       []string                 `json:"trading_names,omitempty"`
	OrganisationForm   OrganisationForm         `json:"organisation_form,omitempty"`
	Jurisdiction       string                   `json:"jurisdiction,omitempty"`
	VerificationState  VerificationState        `json:"verification_state"`
	SourceAuthority    string                   `json:"source_authority"`
	Status             string                   `json:"status"`
	EffectiveFrom      time.Time                `json:"effective_from"`
	EffectiveTo        *time.Time               `json:"effective_to,omitempty"`
	Identifiers        []OrganisationIdentifier `json:"identifiers,omitempty"`
	Addresses          []OrganisationAddress    `json:"addresses,omitempty"`
	EvidenceReferences []string                 `json:"evidence_references,omitempty"`
	Metadata           map[string]any           `json:"metadata,omitempty"`
}

// Validate enforces the contract rules that do not need other records.
func (o Organisation) Validate() error {
	var errs []error
	if o.CanonicalEntityID == "" || o.DisplayName == "" || o.SourceAuthority == "" || o.Status == "" {
		errs = append(errs, errors.New("organisation: canonical_entity_id, display_name, source_authority and status are required"))
	}
	if !o.VerificationState.valid() {
		errs = append(errs, fmt.Errorf("organisation: invalid verification_state %q", o.VerificationState))
	}
	errs = append(errs, validateWindow("organisation", o.EffectiveFrom, o.EffectiveTo))
	return errors.Join(errs...)
}

// LegalEntityProfile attaches a legal person to an Organisation.
type LegalEntityProfile struct {
	LegalEntityID               string                   `json:"legal_entity_id"`
	OrganisationID              string                   `json:"organisation_id"`
	LegalName                   string                   `json:"legal_name"`
	JurisdictionOfIncorporation string                   `json:"jurisdiction_of_incorporation,omitempty"`
	RegistrationIdentifiers     []OrganisationIdentifier `json:"registration_identifiers,omitempty"`
	IncorporationDate           *time.Time               `json:"incorporation_date,omitempty"`
	LegalStatus                 string                   `json:"legal_status"`
	SourceAuthority             string                   `json:"source_authority"`
	VerificationState           VerificationState        `json:"verification_state"`
	EffectiveFrom               time.Time                `json:"effective_from"`
	EffectiveTo                 *time.Time               `json:"effective_to,omitempty"`
	EvidenceReferences          []string                 `json:"evidence_references,omitempty"`
	VerifiedBy                  string                   `json:"verified_by,omitempty"`
	VerifiedAt                  *time.Time               `json:"verified_at,omitempty"`
	Metadata                    map[string]any           `json:"metadata,omitempty"`
}

// Validate enforces the contract rules, including evidence-backed VERIFIED.
func (p LegalEntityProfile) Validate() error {
	var errs []error
	if p.LegalEntityID == "" || p.OrganisationID == "" || p.LegalName == "" || p.SourceAuthority == "" {
		errs = append(errs, errors.New("legal entity profile: legal_entity_id, organisation_id, legal_name and source_authority are required"))
	}
	if p.LegalEntityID != "" && p.LegalEntityID == p.OrganisationID {
		errs = append(errs, errors.New("legal entity profile: legal_entity_id must differ from organisation_id"))
	}
	switch p.LegalStatus {
	case LegalStatusActive, LegalStatusDormant, LegalStatusInAdministration,
		LegalStatusInLiquidation, LegalStatusDissolved, LegalStatusUnknown:
	default:
		errs = append(errs, fmt.Errorf("legal entity profile: invalid legal_status %q", p.LegalStatus))
	}
	if !p.VerificationState.valid() {
		errs = append(errs, fmt.Errorf("legal entity profile: invalid verification_state %q", p.VerificationState))
	}
	if p.VerificationState == VerificationVerified && (len(p.EvidenceReferences) == 0 || p.VerifiedAt == nil) {
		errs = append(errs, errors.New("legal entity profile: VERIFIED requires evidence_references and verified_at"))
	}
	errs = append(errs, validateWindow("legal entity profile", p.EffectiveFrom, p.EffectiveTo))
	return errors.Join(errs...)
}

// CorporateRelationshipType is the ADR-BCP-018 vocabulary (no stored inverses).
type CorporateRelationshipType string

const (
	CorpRelOwns             CorporateRelationshipType = "OWNS"
	CorpRelControls         CorporateRelationshipType = "CONTROLS"
	CorpRelBranchOf         CorporateRelationshipType = "BRANCH_OF"
	CorpRelAffiliateOf      CorporateRelationshipType = "AFFILIATE_OF"
	CorpRelJointVentureWith CorporateRelationshipType = "JOINT_VENTURE_WITH"
	CorpRelSuccessorOf      CorporateRelationshipType = "SUCCESSOR_OF"
)

// Direct and derived corporate facts.
const (
	CorporateFactDirect  = "DIRECT"
	CorporateFactDerived = "DERIVED"
)

// CorporateRelationship is a directed, effective-dated corporate fact:
// SourceOrganisationID RelationshipType TargetOrganisationID ("A OWNS B").
// It carries no tenant and is never a runtime authorization boundary.
type CorporateRelationship struct {
	ID                   string                    `json:"id"`
	SourceOrganisationID string                    `json:"source_organisation_id"`
	TargetOrganisationID string                    `json:"target_organisation_id"`
	RelationshipType     CorporateRelationshipType `json:"relationship_type"`
	OwnershipPercentage  *float64                  `json:"ownership_percentage,omitempty"`
	ControlBasis         string                    `json:"control_basis,omitempty"`
	DirectOrDerived      string                    `json:"direct_or_derived"`
	BasisRelationshipIDs []string                  `json:"basis_relationship_ids,omitempty"`
	DerivedAt            *time.Time                `json:"derived_at,omitempty"`
	DerivationVersion    string                    `json:"derivation_version,omitempty"`
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

// Validate enforces the contract rules for a single corporate fact.
func (r CorporateRelationship) Validate() error {
	var errs []error
	if r.SourceOrganisationID == "" || r.TargetOrganisationID == "" || r.SourceAuthority == "" {
		errs = append(errs, errors.New("corporate relationship: source, target and source_authority are required"))
	}
	if r.SourceOrganisationID != "" && r.SourceOrganisationID == r.TargetOrganisationID {
		errs = append(errs, errors.New("corporate relationship: an organisation cannot relate to itself"))
	}
	switch r.RelationshipType {
	case CorpRelOwns, CorpRelControls, CorpRelBranchOf, CorpRelAffiliateOf, CorpRelJointVentureWith, CorpRelSuccessorOf:
	default:
		errs = append(errs, fmt.Errorf("corporate relationship: invalid relationship_type %q", r.RelationshipType))
	}
	if p := r.OwnershipPercentage; p != nil && (*p <= 0 || *p > 100) {
		errs = append(errs, fmt.Errorf("corporate relationship: ownership_percentage %v outside (0,100]", *p))
	}
	switch r.DirectOrDerived {
	case CorporateFactDirect:
	case CorporateFactDerived:
		if len(r.BasisRelationshipIDs) == 0 || r.DerivedAt == nil || r.DerivationVersion == "" {
			errs = append(errs, errors.New("corporate relationship: DERIVED facts require basis_relationship_ids, derived_at and derivation_version"))
		}
	default:
		errs = append(errs, fmt.Errorf("corporate relationship: invalid direct_or_derived %q", r.DirectOrDerived))
	}
	if !r.VerificationState.valid() {
		errs = append(errs, fmt.Errorf("corporate relationship: invalid verification_state %q", r.VerificationState))
	}
	if r.VerificationState == VerificationVerified && (len(r.EvidenceReferences) == 0 || r.VerifiedBy == "" || r.VerifiedAt == nil) {
		errs = append(errs, errors.New("corporate relationship: VERIFIED requires evidence_references, verified_by and verified_at"))
	}
	errs = append(errs, validateRelationshipStatus("corporate relationship", r.Status))
	errs = append(errs, validateWindow("corporate relationship", r.EffectiveFrom, r.EffectiveTo))
	return errors.Join(errs...)
}

// IsConsequential reports whether this edge may drive INTERNAL eligibility or
// related-party decisions at at. Fails closed unless VERIFIED, in force
// (ACTIVE, or ENDED for the period before its end) and inside its effective
// window. Only OWNS and CONTROLS are consequential for eligibility.
func (r CorporateRelationship) IsConsequential(at time.Time) bool {
	if !r.VerificationState.IsAuthoritative() || !inForce(r.Status, r.EffectiveTo) || r.SourceAuthority == "" {
		return false
	}
	if !inWindow(at, r.EffectiveFrom, r.EffectiveTo) {
		return false
	}
	return r.RelationshipType == CorpRelOwns || r.RelationshipType == CorpRelControls
}

// CorporateGroup is a projection; root is optional when no unique parent exists.
type CorporateGroup struct {
	ID                 string         `json:"id"`
	DisplayName        string         `json:"display_name"`
	RootOrganisationID string         `json:"root_organisation_id,omitempty"`
	Status             string         `json:"status"`
	GroupingPolicy     string         `json:"grouping_policy"`
	EffectiveFrom      time.Time      `json:"effective_from"`
	EffectiveTo        *time.Time     `json:"effective_to,omitempty"`
	Classification     string         `json:"classification,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

// CorporateGroupMembership is derived from CorporateRelationship edges or a
// governed manual basis. Confers no runtime permission.
type CorporateGroupMembership struct {
	ID                   string         `json:"id"`
	CorporateGroupID     string         `json:"corporate_group_id"`
	OrganisationID       string         `json:"organisation_id"`
	GroupRole            string         `json:"group_role,omitempty"`
	BasisRelationshipIDs []string       `json:"basis_relationship_ids"`
	ManualBasisReference string         `json:"manual_basis_reference,omitempty"`
	Status               string         `json:"status"`
	EffectiveFrom        time.Time      `json:"effective_from"`
	EffectiveTo          *time.Time     `json:"effective_to,omitempty"`
	DerivedAt            time.Time      `json:"derived_at"`
	DerivationVersion    string         `json:"derivation_version"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

// Validate enforces lineage: relationship basis or governed manual basis.
func (m CorporateGroupMembership) Validate() error {
	var errs []error
	if len(m.BasisRelationshipIDs) == 0 && m.ManualBasisReference == "" {
		errs = append(errs, errors.New("corporate group membership: requires basis_relationship_ids or manual_basis_reference"))
	}
	if m.DerivedAt.IsZero() || m.DerivationVersion == "" {
		errs = append(errs, errors.New("corporate group membership: derived_at and derivation_version are required"))
	}
	errs = append(errs, validateWindow("corporate group membership", m.EffectiveFrom, m.EffectiveTo))
	return errors.Join(errs...)
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

// IsPrivileged reports whether the type is server-authoritative first-party
// status that an applicant can never self-assign (ADR-BCP-018 section 70).
func (t PlatformRelationshipType) IsPrivileged() bool {
	return t == PlatformRelOwner || t == PlatformRelOperator || t == PlatformRelGroupAffiliate
}

// PlatformRelationship links an Organisation to the platform. An organisation
// may hold concurrent relationship types. It carries no runtime authorization.
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
	Classification      string                   `json:"classification,omitempty"`
	Metadata            map[string]any           `json:"metadata,omitempty"`
}

// Validate enforces the contract rules for a platform relationship.
func (r PlatformRelationship) Validate() error {
	var errs []error
	if r.PlatformID == "" || r.OrganisationID == "" || r.SourceAuthority == "" {
		errs = append(errs, errors.New("platform relationship: platform_id, organisation_id and source_authority are required"))
	}
	switch r.RelationshipType {
	case PlatformRelOwner, PlatformRelOperator, PlatformRelGroupAffiliate, PlatformRelPartner, PlatformRelExternalClient, PlatformRelManagedEntity:
	default:
		errs = append(errs, fmt.Errorf("platform relationship: invalid relationship_type %q", r.RelationshipType))
	}
	if r.RelationshipType == PlatformRelGroupAffiliate && r.BasisRelationshipID == "" {
		errs = append(errs, errors.New("platform relationship: PLATFORM_GROUP_AFFILIATE requires basis_relationship_id"))
	}
	if !r.VerificationState.valid() {
		errs = append(errs, fmt.Errorf("platform relationship: invalid verification_state %q", r.VerificationState))
	}
	if r.VerificationState == VerificationVerified && (len(r.EvidenceReferences) == 0 || r.VerifiedBy == "" || r.VerifiedAt == nil) {
		errs = append(errs, errors.New("platform relationship: VERIFIED requires evidence_references, verified_by and verified_at"))
	}
	errs = append(errs, validateRelationshipStatus("platform relationship", r.Status))
	errs = append(errs, validateWindow("platform relationship", r.EffectiveFrom, r.EffectiveTo))
	return errors.Join(errs...)
}

// IsConsequential reports whether the relationship may drive a decision at
// at: VERIFIED, in force (ACTIVE, or ENDED for the period before its end)
// and inside its effective window. PENDING records are
// visible for review but never consequential.
func (r PlatformRelationship) IsConsequential(at time.Time) bool {
	return r.VerificationState.IsAuthoritative() &&
		inForce(r.Status, r.EffectiveTo) &&
		inWindow(at, r.EffectiveFrom, r.EffectiveTo)
}

// PlatformAccount is commercial/administrative. NOT a Tenant.
type PlatformAccount struct {
	ID                      string         `json:"id"`
	DisplayName             string         `json:"display_name"`
	PrimaryOrganisationID   string         `json:"primary_organisation_id,omitempty"`
	Status                  string         `json:"status"`
	ContractReferences      []string       `json:"contract_references,omitempty"`
	BillingProfileReference string         `json:"billing_profile_reference,omitempty"`
	SupportProfileReference string         `json:"support_profile_reference,omitempty"`
	EffectiveFrom           time.Time      `json:"effective_from"`
	EffectiveTo             *time.Time     `json:"effective_to,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
}

// PlatformAccount roles (ADR-BCP-018 section 43). Commercial, never permissions.
const (
	AccountRolePrimaryOrganisation = "PRIMARY_ACCOUNT_ORGANISATION"
	AccountRoleContractingParty    = "CONTRACTING_PARTY"
	AccountRoleBillingParty        = "BILLING_PARTY"
	AccountRoleServiceRecipient    = "SERVICE_RECIPIENT"
	AccountRoleMember              = "ACCOUNT_MEMBER"
)

// PlatformAccountMembership links an Organisation to an account.
type PlatformAccountMembership struct {
	ID                string         `json:"id"`
	PlatformAccountID string         `json:"platform_account_id"`
	OrganisationID    string         `json:"organisation_id"`
	AccountRole       string         `json:"account_role"`
	Status            string         `json:"status"`
	EffectiveFrom     time.Time      `json:"effective_from"`
	EffectiveTo       *time.Time     `json:"effective_to,omitempty"`
	EvidenceReference string         `json:"evidence_reference,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

// PlatformAccount lifecycle (ADR-BCP-018 section 83).
const (
	PlatformAccountPending   = "PENDING"
	PlatformAccountActive    = "ACTIVE"
	PlatformAccountSuspended = "SUSPENDED"
	PlatformAccountClosed    = "CLOSED"
)

// platformAccountTransitions is Shared platform.schema.json
// platformAccountTransitions: CLOSED is final.
var platformAccountTransitions = map[string][]string{
	PlatformAccountPending:   {PlatformAccountActive, PlatformAccountClosed},
	PlatformAccountActive:    {PlatformAccountSuspended, PlatformAccountClosed},
	PlatformAccountSuspended: {PlatformAccountActive, PlatformAccountClosed},
	PlatformAccountClosed:    {},
}

// PlatformAccountTransitionAllowed reports whether an account may move
// from one status to another.
func PlatformAccountTransitionAllowed(from, to string) bool {
	for _, next := range platformAccountTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// Tenant PlatformAccount binding statuses.
const (
	TenantPlatformAccountBindingActive = "ACTIVE"
	TenantPlatformAccountBindingEnded  = "ENDED"
)

// TenantPlatformAccountBinding is the explicit, effective-dated binding of a
// tenant to the PlatformAccount whose commercial terms it consumes under
// (ADR-BCP-018 sections 45, 48, 119). At most one is ACTIVE per tenant.
// Commercial provenance only: nothing resolves access through it.
type TenantPlatformAccountBinding struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	PlatformAccountID string     `json:"platform_account_id"`
	OrganisationID    string     `json:"organisation_id"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	EvidenceReference string     `json:"evidence_reference,omitempty"`
	BoundBy           string     `json:"bound_by"`
	EffectiveFrom     time.Time  `json:"effective_from"`
	EffectiveTo       *time.Time `json:"effective_to,omitempty"`
	EndReason         string     `json:"end_reason,omitempty"`
	EndedBy           string     `json:"ended_by,omitempty"`
}

// Tenant mapping roles (shared contracts/organisation/v1/mapping.schema.json).
const (
	TenantOrgRolePrimary    = "PRIMARY_ORGANISATION"
	TenantOrgRoleOperating  = "OPERATING_ORGANISATION"
	TenantOrgRoleAdditional = "ADDITIONAL_ORGANISATION"

	TenantLegalEntityRoleDefault    = "DEFAULT"
	TenantLegalEntityRoleAdditional = "ADDITIONAL"
)

// TenantOrganisationMapping is an explicit tenant↔organisation link.
type TenantOrganisationMapping struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	OrganisationID string         `json:"organisation_id"`
	MappingRole    string         `json:"mapping_role"`
	Status         string         `json:"status"`
	EffectiveFrom  time.Time      `json:"effective_from"`
	EffectiveTo    *time.Time     `json:"effective_to,omitempty"`
	Provenance     string         `json:"provenance"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// TenantLegalEntityMapping is an explicit tenant↔legal-entity link. The
// active DEFAULT row projects Tenant.LegalEntityID for compatibility.
type TenantLegalEntityMapping struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	LegalEntityID string         `json:"legal_entity_id"`
	MappingRole   string         `json:"mapping_role"`
	Status        string         `json:"status"`
	EffectiveFrom time.Time      `json:"effective_from"`
	EffectiveTo   *time.Time     `json:"effective_to,omitempty"`
	Provenance    string         `json:"provenance"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// InternalEligibilityEvidence is the input to DeriveInternalEligibility.
// CorporateRelationships must contain the inbound ancestry of OrganisationID
// (every edge on any directed path into it); edges elsewhere are ignored.
type InternalEligibilityEvidence struct {
	OrganisationID         string
	PlatformRelationships  []PlatformRelationship
	CorporateRelationships []CorporateRelationship
	EvaluatedAt            time.Time
}

// MaxCorporateControlDepth bounds ancestry traversal. Deeper structures fail
// closed and need an explicit governed decision rather than a longer walk.
const MaxCorporateControlDepth = 16

// DeriveInternalEligibility decides ADR-BCP-017 INTERNAL product eligibility.
//
// Rules (fail closed):
//   - a consequential PLATFORM_OWNER or PLATFORM_OPERATOR relationship → eligible
//   - a consequential PLATFORM_GROUP_AFFILIATE → eligible only when its basis
//     relationship is a consequential OWNS/CONTROLS edge *into* the affiliate
//     and a directed chain of consequential OWNS/CONTROLS edges runs from a
//     platform owner to that basis edge's source. Edge direction matters:
//     "affiliate OWNS owner" never qualifies.
//   - EXTERNAL_CLIENT / PARTNER / MANAGED_ENTITY → not eligible
//   - corporate group membership and PlatformAccount are never considered
func DeriveInternalEligibility(ev InternalEligibilityEvidence, platformOwnerOrgIDs map[string]struct{}) bool {
	return len(QualifyingPlatformRelationships(ev, platformOwnerOrgIDs)) > 0
}

// QualifyingPlatformRelationships returns the platform relationships that
// make ev.OrganisationID INTERNAL-eligible under DeriveInternalEligibility's
// rules, in input order: the evidence an INTERNAL classification records
// (ADR-BCP-017 section 13). None means not eligible.
func QualifyingPlatformRelationships(ev InternalEligibilityEvidence, platformOwnerOrgIDs map[string]struct{}) []PlatformRelationship {
	at := ev.EvaluatedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var out []PlatformRelationship
	for _, pr := range ev.PlatformRelationships {
		if pr.OrganisationID != ev.OrganisationID || !pr.IsConsequential(at) {
			continue
		}
		switch pr.RelationshipType {
		case PlatformRelOwner, PlatformRelOperator:
			out = append(out, pr)
		case PlatformRelGroupAffiliate:
			if affiliateControlledByOwner(ev.OrganisationID, pr.BasisRelationshipID, ev.CorporateRelationships, platformOwnerOrgIDs, at) {
				out = append(out, pr)
			}
		}
	}
	return out
}

// affiliateControlledByOwner walks consequential edges backwards (target →
// source) from the affiliate's basis edge until it reaches a platform owner.
// The visited set makes cross-holdings and cycles terminate without guessing.
func affiliateControlledByOwner(orgID, basisID string, rels []CorporateRelationship, owners map[string]struct{}, at time.Time) bool {
	if len(owners) == 0 || basisID == "" {
		return false
	}
	inbound := map[string][]CorporateRelationship{}
	var basis *CorporateRelationship
	for i := range rels {
		r := rels[i]
		if !r.IsConsequential(at) {
			continue
		}
		inbound[r.TargetOrganisationID] = append(inbound[r.TargetOrganisationID], r)
		if r.ID == basisID {
			basis = &rels[i]
		}
	}
	if basis == nil || basis.TargetOrganisationID != orgID {
		return false
	}
	visited := map[string]bool{orgID: true}
	frontier := []string{basis.SourceOrganisationID}
	for depth := 0; depth < MaxCorporateControlDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, node := range frontier {
			if visited[node] {
				continue
			}
			visited[node] = true
			if _, ok := owners[node]; ok {
				return true
			}
			for _, edge := range inbound[node] {
				next = append(next, edge.SourceOrganisationID)
			}
		}
		frontier = next
	}
	return false
}

// inForce reports whether a relationship's status lets it be in force inside
// its effective window: ACTIVE, or ENDED with the effective_to the end
// recorded. An ended fact stays true for the period it covered, so as-of
// queries before the end still see it (ADR-BCP-018 sections 74 and 86).
func inForce(status string, effectiveTo *time.Time) bool {
	return status == RelationshipStatusActive || (status == RelationshipStatusEnded && effectiveTo != nil)
}

func inWindow(at, from time.Time, to *time.Time) bool {
	if at.Before(from) {
		return false
	}
	return to == nil || at.Before(*to)
}

func validateWindow(kind string, from time.Time, to *time.Time) error {
	if from.IsZero() {
		return fmt.Errorf("%s: effective_from is required", kind)
	}
	if to != nil && to.Before(from) {
		return fmt.Errorf("%s: effective_to precedes effective_from", kind)
	}
	return nil
}

func validateRelationshipStatus(kind, status string) error {
	switch status {
	case RelationshipStatusPending, RelationshipStatusActive, RelationshipStatusSuspended,
		RelationshipStatusEnded, RelationshipStatusRetired:
		return nil
	}
	return fmt.Errorf("%s: invalid status %q", kind, status)
}
