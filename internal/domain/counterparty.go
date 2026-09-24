// ADR-BCP-018 gate ORG-13 — counterparty roles and organisation resolution
// candidates (sections 11, 99-101, 112; ADR-BCP-014 sections 10-17;
// ADR-BCP-023 sections 50-52).
//
// Contract: baobab-platform/shared contracts/organisation/v1/counterparty.schema.json.

package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// CounterpartyRoleTypes are the commercial capacities an Organisation may
// hold for a tenant (ADR-BCP-014 section 13; THIRD_PARTY_LOGISTICS is that
// section's 3PL). Adding one is a Shared contract change.
var CounterpartyRoleTypes = map[string]bool{
	"CUSTOMER": true, "BUYER": true, "SUPPLIER": true, "VENDOR": true, "DISTRIBUTOR": true,
	"RESELLER": true, "CARRIER": true, "FREIGHT_FORWARDER": true, "CUSTOMS_BROKER": true,
	"WAREHOUSE_OPERATOR": true, "THIRD_PARTY_LOGISTICS": true, "INSURER": true, "BANK": true,
	"PAYMENT_PROVIDER": true, "AGENT": true, "SERVICE_PROVIDER": true, "AFFILIATE": true,
	"INTERCOMPANY_COUNTERPARTY": true,
}

// Counterparty roles the ADR-BCP-016 bounded kinds migrate to.
const (
	CounterpartyRoleBuyer    = "BUYER"
	CounterpartyRoleSupplier = "SUPPLIER"
)

// LegacyOrganisationRole maps an ADR-BCP-016 bounded organisation kind to
// the counterparty role it is generalised into (ADR-BCP-018 section 11).
var LegacyOrganisationRole = map[string]string{
	EntityTypeBuyerOrganisation:    CounterpartyRoleBuyer,
	EntityTypeSupplierOrganisation: CounterpartyRoleSupplier,
}

// CounterpartyRole is an Organisation's effective-dated commercial role for
// one tenant. It is not identity and grants no access (ADR-BCP-014 section
// 17). At most one live role exists per organisation, tenant and role.
type CounterpartyRole struct {
	ID               string     `json:"id"`
	OrganisationID   string     `json:"organisation_id"`
	TenantID         string     `json:"tenant_id"`
	Role             string     `json:"role"`
	Status           string     `json:"status"`
	EffectiveFrom    time.Time  `json:"effective_from"`
	EffectiveTo      *time.Time `json:"effective_to,omitempty"`
	SourceAuthority  string     `json:"source_authority"`
	LegacyEntityType string     `json:"legacy_entity_type,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Validate enforces the Shared CounterpartyRole contract.
func (r CounterpartyRole) Validate() error {
	var errs []error
	if r.OrganisationID == "" || r.TenantID == "" || strings.TrimSpace(r.SourceAuthority) == "" {
		errs = append(errs, errors.New("counterparty role: organisation_id, tenant_id and source_authority are required"))
	}
	if !CounterpartyRoleTypes[r.Role] {
		errs = append(errs, fmt.Errorf("counterparty role: unknown role %q", r.Role))
	}
	switch r.Status {
	case RelationshipStatusPending, RelationshipStatusActive, RelationshipStatusSuspended:
	case RelationshipStatusEnded:
		if r.EffectiveTo == nil {
			errs = append(errs, errors.New("counterparty role: ENDED requires effective_to"))
		}
	default:
		errs = append(errs, fmt.Errorf("counterparty role: invalid status %q", r.Status))
	}
	if r.LegacyEntityType != "" && LegacyOrganisationRole[r.LegacyEntityType] == "" {
		errs = append(errs, fmt.Errorf("counterparty role: invalid legacy_entity_type %q", r.LegacyEntityType))
	}
	errs = append(errs, validateWindow("counterparty role", r.EffectiveFrom, r.EffectiveTo))
	return errors.Join(errs...)
}

// HeldAt reports whether the role is ACTIVE and in effect at at. PENDING
// and SUSPENDED roles are recorded but never relied on.
func (r CounterpartyRole) HeldAt(at time.Time) bool {
	return r.Status == RelationshipStatusActive && inWindow(at, r.EffectiveFrom, r.EffectiveTo)
}

// Resolution candidate statuses.
const (
	ResolutionCandidateOpen               = "OPEN"
	ResolutionCandidateDistinct           = "DISTINCT"
	ResolutionCandidateDuplicateConfirmed = "DUPLICATE_CONFIRMED"
)

// MatchedIdentifier is a governed identifier two Organisations both carry,
// normalised as identity resolution compares it.
type MatchedIdentifier struct {
	Type                string `json:"type"`
	NormalisedValue     string `json:"normalised_value"`
	IssuingJurisdiction string `json:"issuing_jurisdiction,omitempty"`
}

// ResolutionCandidateDecision is a reviewer's decision on a quarantined
// candidate. It records whether two Organisations are the same real
// organisation and merges nothing: a merge is an ADR-BCP-021 controlled
// change (ADR-BCP-023 section 52). The reviewer is the authenticated
// principal, never part of the decision.
type ResolutionCandidateDecision struct {
	Decision                string   `json:"decision"`
	Reason                  string   `json:"reason"`
	SurvivingOrganisationID string   `json:"surviving_organisation_id,omitempty"`
	EvidenceReferences      []string `json:"evidence_references,omitempty"`
}

// Validate enforces the Shared ResolutionCandidateDecision contract.
func (d ResolutionCandidateDecision) Validate() error {
	var errs []error
	if r := strings.TrimSpace(d.Reason); r == "" || len(d.Reason) > 2000 {
		errs = append(errs, errors.New("resolution decision: a reason of at most 2000 characters is required"))
	}
	switch d.Decision {
	case ResolutionCandidateDistinct:
		if d.SurvivingOrganisationID != "" {
			errs = append(errs, errors.New("resolution decision: DISTINCT names no surviving organisation"))
		}
	case ResolutionCandidateDuplicateConfirmed:
		if d.SurvivingOrganisationID == "" {
			errs = append(errs, errors.New("resolution decision: DUPLICATE_CONFIRMED requires surviving_organisation_id"))
		}
	default:
		errs = append(errs, fmt.Errorf("resolution decision: invalid decision %q", d.Decision))
	}
	for _, ref := range d.EvidenceReferences {
		if strings.TrimSpace(ref) == "" || len(ref) > 512 {
			errs = append(errs, errors.New("resolution decision: evidence references must be 1-512 characters"))
			break
		}
	}
	return errors.Join(errs...)
}

// OrganisationResolutionCandidate is a quarantined pair of Organisations
// that share at least one governed identifier (ADR-BCP-018 sections 99-100).
type OrganisationResolutionCandidate struct {
	ID                 string                       `json:"id"`
	OrganisationIDs    [2]string                    `json:"organisation_ids"`
	MatchedIdentifiers []MatchedIdentifier          `json:"matched_identifiers"`
	Status             string                       `json:"status"`
	DetectedAt         time.Time                    `json:"detected_at"`
	Source             string                       `json:"source"`
	Decision           *ResolutionCandidateDecision `json:"decision,omitempty"`
	DecidedBy          string                       `json:"decided_by,omitempty"`
	DecidedAt          *time.Time                   `json:"decided_at,omitempty"`
}

// Involves reports whether organisationID is one of the candidate's pair.
func (c OrganisationResolutionCandidate) Involves(organisationID string) bool {
	return organisationID != "" && (c.OrganisationIDs[0] == organisationID || c.OrganisationIDs[1] == organisationID)
}
