// ADR-BCP-018 gate ORG-09 — organisation stages of external admission
// (sections 68-70, 99-100, 108). Contract: baobab-platform/shared
// contracts/organisation/v1/admission.schema.json.

package organisation

import (
	"context"
	"crypto/sha1" // #nosec G505 -- RFC 4122 name-based (v5) identifier, not a security hash
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// Identity resolution outcomes.
const (
	IdentityNewOrganisation      = "NEW_ORGANISATION"
	IdentityExistingOrganisation = "EXISTING_ORGANISATION"
	IdentityQuarantined          = "QUARANTINED"
)

// Reviewer identity decisions for a quarantined identity.
const (
	UseExistingOrganisation  = "USE_EXISTING_ORGANISATION"
	ConfirmNewOrganisation   = "CONFIRM_NEW_ORGANISATION"
	admissionSourceAuthority = "control-plane-admission"
	applicantSourceAuthority = "applicant-submission"
)

// Corporate relationship claim statuses.
const (
	ClaimRecorded              = "RECORDED_PENDING_REVIEW"
	ClaimCounterpartyUnknown   = "COUNTERPARTY_UNRESOLVED"
	ClaimCounterpartyAmbiguous = "COUNTERPARTY_AMBIGUOUS"
)

// Platform account assignment modes.
const (
	AccountExisting = "EXISTING_ACCOUNT"
	AccountNew      = "NEW_ACCOUNT"
	AccountNone     = "NONE"
)

var admissionDecisionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$`)

// ErrFirstPartyOrganisation: external admission cannot onboard an
// organisation that already holds a first-party platform relationship;
// first-party identities are governed through Shared reconciliation.
var ErrFirstPartyOrganisation = errors.New("organisation holds a first-party platform relationship")

// AdmissionRequest is the contract's OrganisationAdmissionRequest.
type AdmissionRequest struct {
	AdmissionDecisionID string                `json:"admission_decision_id"`
	ApplicationID       string                `json:"application_id,omitempty"`
	Applicant           ApplicantOrganisation `json:"applicant_organisation"`
	IdentityResolution  *IdentityResolution   `json:"identity_resolution,omitempty"`
	LegalVerification   *LegalVerification    `json:"legal_verification,omitempty"`
	CorporateClaims     []CorporateClaim      `json:"corporate_relationship_claims,omitempty"`
	PlatformAccount     AccountAssignment     `json:"platform_account"`
	EffectiveFrom       *time.Time            `json:"effective_from,omitempty"`
}

// ApplicantOrganisation is applicant evidence, never canonical state.
type ApplicantOrganisation struct {
	LegalName               string                          `json:"legal_name"`
	Jurisdiction            string                          `json:"jurisdiction_of_incorporation,omitempty"`
	OrganisationForm        domain.OrganisationForm         `json:"organisation_form,omitempty"`
	RegistrationIdentifiers []domain.OrganisationIdentifier `json:"registration_identifiers"`
}

// IdentityResolution is a reviewer's governed decision on a quarantined identity.
type IdentityResolution struct {
	Decision       string `json:"decision"`
	OrganisationID string `json:"organisation_id,omitempty"`
	Reason         string `json:"reason"`
}

// LegalVerification is the reviewer's verification of legal identity.
type LegalVerification struct {
	EvidenceReferences []string `json:"evidence_references"`
	Reason             string   `json:"reason"`
}

// CorporateClaim is a relationship the applicant declared.
type CorporateClaim struct {
	RelationshipType           domain.CorporateRelationshipType `json:"relationship_type"`
	ApplicantRole              string                           `json:"applicant_role"`
	CounterpartyOrganisationID string                           `json:"counterparty_organisation_id,omitempty"`
	CounterpartyIdentifiers    []domain.OrganisationIdentifier  `json:"counterparty_identifiers,omitempty"`
	OwnershipPercentage        *float64                         `json:"ownership_percentage,omitempty"`
	EvidenceReferences         []string                         `json:"evidence_references,omitempty"`
}

// AccountAssignment places the organisation in a PlatformAccount.
type AccountAssignment struct {
	Mode              string `json:"mode"`
	PlatformAccountID string `json:"platform_account_id,omitempty"`
	DisplayName       string `json:"display_name,omitempty"`
	AccountRole       string `json:"account_role,omitempty"`
}

// ClaimOutcome reports one corporate relationship claim.
type ClaimOutcome struct {
	Status                  string `json:"status"`
	CorporateRelationshipID string `json:"corporate_relationship_id,omitempty"`
}

// AdmissionOutcome is the contract's OrganisationAdmissionOutcome.
type AdmissionOutcome struct {
	AdmissionDecisionID          string         `json:"admission_decision_id"`
	TenantID                     string         `json:"tenant_id"`
	IdentityResolution           string         `json:"identity_resolution"`
	CandidateOrganisationIDs     []string       `json:"candidate_organisation_ids,omitempty"`
	OrganisationID               string         `json:"organisation_id,omitempty"`
	LegalEntityID                string         `json:"legal_entity_id,omitempty"`
	LegalEntityVerificationState string         `json:"legal_entity_verification_state,omitempty"`
	CorporateClaims              []ClaimOutcome `json:"corporate_relationship_claims,omitempty"`
	PlatformRelationshipID       string         `json:"platform_relationship_id,omitempty"`
	PlatformAccountID            string         `json:"platform_account_id,omitempty"`
	PlatformAccountMembershipID  string         `json:"platform_account_membership_id,omitempty"`
	TenantOrganisationMappingID  string         `json:"tenant_organisation_mapping_id,omitempty"`
	TenantLegalEntityMappingID   string         `json:"tenant_legal_entity_mapping_id,omitempty"`
}

var accountRoles = map[string]bool{
	domain.AccountRolePrimaryOrganisation: true, domain.AccountRoleContractingParty: true,
	domain.AccountRoleBillingParty: true, domain.AccountRoleServiceRecipient: true, domain.AccountRoleMember: true,
}

// Validate enforces the request rules the Shared schema states.
func (req AdmissionRequest) Validate() error {
	var errs []error
	if !admissionDecisionIDPattern.MatchString(req.AdmissionDecisionID) {
		errs = append(errs, errors.New("admission_decision_id is required and must be an opaque identifier"))
	}
	if strings.TrimSpace(req.Applicant.LegalName) == "" || len(req.Applicant.RegistrationIdentifiers) == 0 {
		errs = append(errs, errors.New("applicant_organisation needs a legal_name and at least one registration identifier"))
	}
	if r := req.IdentityResolution; r != nil {
		switch {
		case strings.TrimSpace(r.Reason) == "":
			errs = append(errs, errors.New("identity_resolution needs a reason"))
		case r.Decision == UseExistingOrganisation && r.OrganisationID == "":
			errs = append(errs, errors.New("identity_resolution USE_EXISTING_ORGANISATION needs organisation_id"))
		case r.Decision != UseExistingOrganisation && r.Decision != ConfirmNewOrganisation:
			errs = append(errs, fmt.Errorf("identity_resolution: unknown decision %q", r.Decision))
		}
	}
	if v := req.LegalVerification; v != nil && (len(v.EvidenceReferences) == 0 || strings.TrimSpace(v.Reason) == "") {
		errs = append(errs, errors.New("legal_verification needs evidence_references and a reason"))
	}
	for i, c := range req.CorporateClaims {
		if (c.CounterpartyOrganisationID == "") == (len(c.CounterpartyIdentifiers) == 0) {
			errs = append(errs, fmt.Errorf("corporate_relationship_claims[%d]: name the counterparty by organisation id or by identifiers, not both", i))
		}
		if c.ApplicantRole != "SOURCE" && c.ApplicantRole != "TARGET" {
			errs = append(errs, fmt.Errorf("corporate_relationship_claims[%d]: applicant_role must be SOURCE or TARGET", i))
		}
	}
	a := req.PlatformAccount
	switch a.Mode {
	case AccountExisting:
		if a.PlatformAccountID == "" {
			errs = append(errs, errors.New("platform_account EXISTING_ACCOUNT needs platform_account_id"))
		}
	case AccountNew:
		if strings.TrimSpace(a.DisplayName) == "" {
			errs = append(errs, errors.New("platform_account NEW_ACCOUNT needs display_name"))
		}
	case AccountNone:
	default:
		errs = append(errs, fmt.Errorf("platform_account: unknown mode %q", a.Mode))
	}
	if a.AccountRole != "" && !accountRoles[a.AccountRole] {
		errs = append(errs, fmt.Errorf("platform_account: unknown account_role %q", a.AccountRole))
	}
	return errors.Join(errs...)
}

// AdmissionOnboarder runs the organisation stages of an approved external
// admission onto the tenant it registered. Every stage is idempotent, so
// replaying the same admission decision converges and changes nothing; a
// failure part-way is recovered by replaying the request.
type AdmissionOnboarder struct {
	Orgs       repository.OrganisationAdmissionRepository
	PlatformID string
	Now        func() time.Time
}

// Onboard runs identity resolution, legal verification, corporate
// relationship assessment, the EXTERNAL_CLIENT platform relationship,
// PlatformAccount assignment and the tenant's mappings, in that order
// (ADR-BCP-018 sections 68 and 108). actor is the admission reviewer.
func (o *AdmissionOnboarder) Onboard(ctx context.Context, tenantID string, req AdmissionRequest, actor repository.AuditActor) (AdmissionOutcome, error) {
	out := AdmissionOutcome{AdmissionDecisionID: req.AdmissionDecisionID, TenantID: tenantID}
	if err := req.Validate(); err != nil {
		return out, err
	}
	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now()
	}
	at := now
	if req.EffectiveFrom != nil {
		at = req.EffectiveFrom.UTC()
	}
	platform := o.PlatformID
	if platform == "" {
		platform = DefaultPlatformID
	}

	// The tenant's registered organisation and legal entity (created as
	// unverified claims at registration).
	registeredOrg, registeredLE, err := o.registration(ctx, tenantID, now)
	if err != nil {
		return out, err
	}

	// 1. Identity resolution, on governed identifiers only.
	orgID, legalEntityID := registeredOrg, registeredLE
	candidates, err := o.Orgs.FindOrganisationsByIdentifiers(ctx, req.Applicant.RegistrationIdentifiers, registeredOrg)
	if err != nil {
		return out, fmt.Errorf("identity resolution: %w", err)
	}
	slices.Sort(candidates)
	switch r := req.IdentityResolution; {
	case r != nil && r.Decision == UseExistingOrganisation:
		if orgID, legalEntityID, err = o.existingOrganisation(ctx, r.OrganisationID); err != nil {
			return out, err
		}
		out.IdentityResolution = IdentityExistingOrganisation
	case r != nil && r.Decision == ConfirmNewOrganisation, len(candidates) == 0:
		out.IdentityResolution = IdentityNewOrganisation
	default:
		// Possible duplicate: stop before writing anything (section 100).
		out.IdentityResolution, out.CandidateOrganisationIDs = IdentityQuarantined, candidates
		return out, nil
	}
	out.OrganisationID, out.LegalEntityID = orgID, legalEntityID

	// 2. Legal verification. Applicant data is recorded as unverified
	// claims; only the reviewer's evidence verifies it.
	if _, err := o.Orgs.RecordLegalEntityClaims(ctx, legalEntityID, repository.LegalEntityClaims{
		LegalName: req.Applicant.LegalName, Jurisdiction: req.Applicant.Jurisdiction,
		Identifiers: req.Applicant.RegistrationIdentifiers,
	}, actor); err != nil {
		return out, fmt.Errorf("record legal entity claims: %w", err)
	}
	if v := req.LegalVerification; v != nil {
		ev := repository.Evidence{References: v.EvidenceReferences, VerifiedAt: now, Reason: v.Reason}
		profile, err := o.Orgs.GetLegalEntityProfile(ctx, legalEntityID)
		if err != nil {
			return out, err
		}
		if profile != nil && profile.VerificationState != domain.VerificationVerified {
			if err := o.Orgs.VerifyLegalEntityProfile(ctx, legalEntityID, ev, actor); err != nil {
				return out, fmt.Errorf("verify legal entity: %w", err)
			}
		}
		if err := o.Orgs.VerifyOrganisation(ctx, orgID, ev, actor); err != nil {
			return out, fmt.Errorf("verify organisation: %w", err)
		}
	}
	if profile, err := o.Orgs.GetLegalEntityProfile(ctx, legalEntityID); err != nil {
		return out, err
	} else if profile != nil {
		out.LegalEntityVerificationState = string(profile.VerificationState)
	}

	// 3. Corporate relationship assessment: claims only (section 69).
	for _, claim := range req.CorporateClaims {
		result, err := o.recordClaim(ctx, orgID, claim, at, actor)
		if err != nil {
			return out, err
		}
		out.CorporateClaims = append(out.CorporateClaims, result)
	}

	// 4. The platform relationship is server-authoritative (section 70):
	// EXTERNAL_CLIENT, verified on the approved decision.
	if out.PlatformRelationshipID, err = o.externalClient(ctx, platform, orgID, req.AdmissionDecisionID, at, now, actor); err != nil {
		return out, err
	}

	// 5. PlatformAccount assignment (commercial only; no tenant access).
	if out.PlatformAccountID, out.PlatformAccountMembershipID, err = o.assignAccount(ctx, orgID, req, at, actor); err != nil {
		return out, err
	}

	// 6. Tenant mappings: a reviewer-resolved existing organisation is an
	// explicit re-point of the tenant; the registered organisation is left
	// as it was, never merged.
	provenance := admissionSourceAuthority + ":" + req.AdmissionDecisionID
	if out.TenantOrganisationMappingID, err = o.Orgs.ReplacePrimaryTenantOrganisation(ctx, tenantID, orgID, at, provenance, actor); err != nil {
		return out, fmt.Errorf("tenant organisation mapping: %w", err)
	}
	if out.TenantLegalEntityMappingID, err = o.Orgs.EnsureDefaultTenantLegalEntityMapping(ctx, tenantID, legalEntityID, provenance, at, actor); err != nil {
		return out, fmt.Errorf("tenant legal entity mapping: %w", err)
	}
	return out, nil
}

// registration finds the tenant's live PRIMARY organisation and DEFAULT
// legal entity.
func (o *AdmissionOnboarder) registration(ctx context.Context, tenantID string, at time.Time) (string, string, error) {
	orgs, err := o.Orgs.ListLiveTenantOrganisationMappings(ctx, tenantID)
	if err != nil {
		return "", "", err
	}
	var org string
	for _, m := range orgs {
		if m.MappingRole == domain.TenantOrgRolePrimary {
			org = m.OrganisationID
		}
	}
	les, err := o.Orgs.ListTenantLegalEntityMappings(ctx, tenantID, at)
	if err != nil {
		return "", "", err
	}
	var le string
	for _, m := range les {
		if m.MappingRole == domain.TenantLegalEntityRoleDefault && m.Status == domain.RelationshipStatusActive {
			le = m.LegalEntityID
		}
	}
	if org == "" || le == "" {
		return "", "", fmt.Errorf("tenant %s has no registered primary organisation and default legal entity", tenantID)
	}
	return org, le, nil
}

// existingOrganisation checks a reviewer-named organisation and finds its
// single legal entity.
func (o *AdmissionOnboarder) existingOrganisation(ctx context.Context, orgID string) (string, string, error) {
	org, err := o.Orgs.GetOrganisation(ctx, orgID)
	if err != nil {
		return "", "", err
	}
	if org == nil {
		return "", "", fmt.Errorf("identity resolution names %s, which has no organisation profile", orgID)
	}
	profiles, err := o.Orgs.ListLegalEntityProfilesByOrganisation(ctx, orgID)
	if err != nil {
		return "", "", err
	}
	if len(profiles) != 1 {
		return "", "", fmt.Errorf("organisation %s has %d legal entities; the reviewer must resolve which one the tenant uses", orgID, len(profiles))
	}
	return orgID, profiles[0].LegalEntityID, nil
}

func (o *AdmissionOnboarder) recordClaim(ctx context.Context, orgID string, claim CorporateClaim, at time.Time, actor repository.AuditActor) (ClaimOutcome, error) {
	counterparty := claim.CounterpartyOrganisationID
	if counterparty != "" {
		if org, err := o.Orgs.GetOrganisation(ctx, counterparty); err != nil || org == nil {
			return ClaimOutcome{Status: ClaimCounterpartyUnknown}, err
		}
	} else {
		matches, err := o.Orgs.FindOrganisationsByIdentifiers(ctx, claim.CounterpartyIdentifiers, orgID)
		if err != nil {
			return ClaimOutcome{}, err
		}
		switch len(matches) {
		case 0:
			return ClaimOutcome{Status: ClaimCounterpartyUnknown}, nil
		case 1:
			counterparty = matches[0]
		default:
			return ClaimOutcome{Status: ClaimCounterpartyAmbiguous}, nil
		}
	}
	if counterparty == orgID {
		return ClaimOutcome{Status: ClaimCounterpartyUnknown}, nil
	}
	source, target := orgID, counterparty
	if claim.ApplicantRole == "TARGET" {
		source, target = counterparty, orgID
	}
	id, err := o.Orgs.EnsureCorporateRelationship(ctx, domain.CorporateRelationship{
		SourceOrganisationID: source, TargetOrganisationID: target, RelationshipType: claim.RelationshipType,
		OwnershipPercentage: claim.OwnershipPercentage, DirectOrDerived: domain.CorporateFactDirect,
		VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending,
		EffectiveFrom: at, SourceAuthority: applicantSourceAuthority, EvidenceReferences: claim.EvidenceReferences,
	}, actor)
	if err != nil {
		return ClaimOutcome{}, fmt.Errorf("record corporate relationship claim: %w", err)
	}
	return ClaimOutcome{Status: ClaimRecorded, CorporateRelationshipID: id}, nil
}

func (o *AdmissionOnboarder) externalClient(ctx context.Context, platform, orgID, decisionID string, at, now time.Time, actor repository.AuditActor) (string, error) {
	existing, err := o.Orgs.ListPlatformRelationships(ctx, orgID, now)
	if err != nil {
		return "", err
	}
	for _, pr := range existing {
		if pr.PlatformID == platform && pr.RelationshipType.IsPrivileged() && pr.Status != domain.RelationshipStatusEnded {
			return "", fmt.Errorf("%w (%s %s); first-party identities are admitted through governance reconciliation", ErrFirstPartyOrganisation, pr.ID, pr.RelationshipType)
		}
	}
	id, err := o.Orgs.EnsurePlatformRelationship(ctx, domain.PlatformRelationship{
		PlatformID: platform, OrganisationID: orgID, RelationshipType: domain.PlatformRelExternalClient,
		VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending,
		EffectiveFrom: at, AdmissionDecisionID: decisionID, SourceAuthority: admissionSourceAuthority,
	}, actor)
	if err != nil {
		return "", fmt.Errorf("external client relationship: %w", err)
	}
	pr, err := o.Orgs.GetPlatformRelationship(ctx, id)
	if err != nil {
		return "", err
	}
	if pr != nil && pr.VerificationState != domain.VerificationVerified {
		if err := o.Orgs.VerifyPlatformRelationship(ctx, id, repository.Evidence{
			References: []string{"admission-decision:" + decisionID}, VerifiedAt: now, Reason: "approved admission decision",
		}, actor); err != nil {
			return "", fmt.Errorf("verify external client relationship: %w", err)
		}
	}
	return id, nil
}

func (o *AdmissionOnboarder) assignAccount(ctx context.Context, orgID string, req AdmissionRequest, at time.Time, actor repository.AuditActor) (string, string, error) {
	a := req.PlatformAccount
	role := a.AccountRole
	var accountID string
	switch a.Mode {
	case AccountNone:
		return "", "", nil
	case AccountExisting:
		exists, err := o.Orgs.PlatformAccountExists(ctx, a.PlatformAccountID)
		if err != nil {
			return "", "", err
		}
		if !exists {
			return "", "", fmt.Errorf("platform account %s does not exist", a.PlatformAccountID)
		}
		accountID = a.PlatformAccountID
		if role == "" {
			role = domain.AccountRoleContractingParty
		}
	case AccountNew:
		// The id derives from the decision, so a replay finds the same account.
		accountID = accountIDFor(req.AdmissionDecisionID)
		if err := o.Orgs.CreatePlatformAccount(ctx, domain.PlatformAccount{ID: accountID, DisplayName: a.DisplayName,
			PrimaryOrganisationID: orgID, Status: "ACTIVE", EffectiveFrom: at,
			ContractReferences: []string{"admission-decision:" + req.AdmissionDecisionID}}, actor); err != nil {
			return "", "", fmt.Errorf("create platform account: %w", err)
		}
		if role == "" {
			role = domain.AccountRolePrimaryOrganisation
		}
	}
	membership, err := o.Orgs.EnsurePlatformAccountMembership(ctx, domain.PlatformAccountMembership{PlatformAccountID: accountID,
		OrganisationID: orgID, AccountRole: role, Status: "ACTIVE", EffectiveFrom: at,
		EvidenceReference: "admission-decision:" + req.AdmissionDecisionID}, actor)
	if err != nil {
		return "", "", fmt.Errorf("platform account membership: %w", err)
	}
	return accountID, membership, nil
}

// accountIDFor derives a stable PlatformAccount id from an admission
// decision: an RFC 4122 version 5 uuid in a namespace of its own.
func accountIDFor(decisionID string) string {
	namespace := []byte("baobab-cp:admission:platform-account:")
	sum := sha1.Sum(append(namespace, decisionID...)) // #nosec G401 -- identifier derivation only
	u := sum[:16]
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	id, _ := domain.FormatResourceID(domain.PlatformAccountIDPrefix,
		fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16]))
	return id
}
