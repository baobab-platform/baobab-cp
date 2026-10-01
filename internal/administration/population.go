package administration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Population statuses (Shared authority-migration.schema.json
// ReviewedPopulation). Only an APPROVED population is planned.
const (
	PopulationDraft       = "DRAFT"
	PopulationUnderReview = "UNDER_REVIEW"
	PopulationApproved    = "APPROVED"
)

// Realm roles the migration replaces. They are evidence of what a person can
// do today, bounding what a reviewed record may propose; they never expand to
// permissions by themselves (roles-to-grants decision, ruling 3).
const (
	RoleEvidencePlatformAdmin = "cp:platform-admin"
	RoleEvidenceTenantAdmin   = "cp:tenant-admin"
)

// ReviewedAdministrator is one record of a ReviewedPopulation: the canonical
// principal, the roles they hold today, and the one grant proposed for them.
type ReviewedAdministrator struct {
	PrincipalID                string     `json:"principal_id"`
	CurrentRoles               []string   `json:"current_roles"`
	Permission                 string     `json:"permission"`
	Scope                      Scope      `json:"scope"`
	OrganisationAttestationRef string     `json:"organisation_attestation_ref,omitempty"`
	GrantType                  GrantType  `json:"grant_type"`
	ValidFrom                  time.Time  `json:"valid_from"`
	ValidUntil                 *time.Time `json:"valid_until,omitempty"`
	DelegableDepth             int        `json:"delegable_depth,omitempty"`
	RiskClass                  RiskClass  `json:"risk_class"`
	Reason                     string     `json:"reason"`
	ReviewedBy                 string     `json:"reviewed_by"`
	ReviewedAt                 time.Time  `json:"reviewed_at"`
}

// ReviewedPopulation is the list Platform Security / Control Plane Governance
// reviews and approves. It is the only source of the administrator
// population; IAM role holders are evidence in it, never the source.
type ReviewedPopulation struct {
	PopulationID   string                  `json:"population_id"`
	Version        int                     `json:"version"`
	Status         string                  `json:"status"`
	PreparedAt     time.Time               `json:"prepared_at"`
	ApprovedBy     string                  `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time              `json:"approved_at,omitempty"`
	Administrators []ReviewedAdministrator `json:"administrators"`
}

// ParsePopulation decodes a population document strictly: an unknown field is
// an error, so a record cannot carry a claim the contract does not define.
func ParsePopulation(raw []byte) (ReviewedPopulation, error) {
	var p ReviewedPopulation
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return ReviewedPopulation{}, fmt.Errorf("population is not valid: %w", err)
	}
	return p, nil
}

// PopulationFinding is one problem with a population, named so a reviewer can
// find the record.
type PopulationFinding struct {
	Record  int    `json:"record"` // 0-based; -1 for the document
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Finding codes.
const (
	FindingDocument     = "DOCUMENT_INVALID"
	FindingPrincipal    = "PRINCIPAL_INVALID"
	FindingPermission   = "PERMISSION_INVALID"
	FindingScope        = "SCOPE_INVALID"
	FindingAttestation  = "ATTESTATION_MISSING"
	FindingRisk         = "RISK_BELOW_PERMISSION"
	FindingGrantShape   = "GRANT_SHAPE_INVALID"
	FindingPolicy       = "POLICY_REFUSED"
	FindingReviewer     = "REVIEWER_INVALID"
	FindingRoleEvidence = "ROLE_EVIDENCE_INVALID"
	FindingRoleCeiling  = "BROADER_THAN_ROLE"
	FindingDuplicate    = "DUPLICATE_RECORD"
	FindingApproval     = "APPROVAL_INVALID"
)

// PlannedGrant is what an approved, valid record becomes: the request to make
// of the grant administration API by an authorised administrator. Planning
// writes nothing and issues nothing.
type PlannedGrant struct {
	Record int `json:"record"`
	// Route is how the request is made: DIRECT (POST /v1/admin/grants) for
	// LOW and MODERATE authority, CHANGESET (an ADMINISTRATIVE_GRANT_ISSUANCE
	// changeset with an independent approver) for HIGH and CRITICAL.
	Route   string         `json:"route"`
	Request map[string]any `json:"request"`
}

// PopulationReport is the outcome of checking a population.
type PopulationReport struct {
	PopulationID string              `json:"population_id"`
	Status       string              `json:"status"`
	Valid        bool                `json:"valid"`
	Findings     []PopulationFinding `json:"findings"`
	// Plan is present only for an APPROVED population with no findings.
	Plan []PlannedGrant `json:"plan,omitempty"`
}

// tenantCeiling are the scope levels a cp:tenant-admin could administer
// through a workforce membership; a record for that role goes no wider.
var tenantCeiling = []ScopeLevel{LevelTenant, LevelDigitalEstate, LevelResource}

// CheckPopulation validates a reviewed population against the catalogue, the
// scope grammar, the separation-of-duties bounds and the roles the people
// hold today (grants must be equal to or narrower than the roles they
// replace; ADR-BCP-020 section 144). It plans requests only for an APPROVED,
// finding-free population, and never issues anything: the grants are made by
// authorised administrators through the grant administration API.
func CheckPopulation(c *Catalogue, sod *SoD, p ReviewedPopulation, now time.Time) PopulationReport {
	report := PopulationReport{PopulationID: p.PopulationID, Status: p.Status, Findings: []PopulationFinding{}}
	add := func(record int, code, format string, a ...any) {
		report.Findings = append(report.Findings, PopulationFinding{Record: record, Code: code, Message: fmt.Sprintf(format, a...)})
	}
	if p.PopulationID == "" || p.Version < 1 || len(p.Administrators) == 0 {
		add(-1, FindingDocument, "population_id, a version of at least 1 and at least one record are required")
	}
	switch p.Status {
	case PopulationDraft, PopulationUnderReview:
	case PopulationApproved:
		if p.ApprovedBy == "" || p.ApprovedAt == nil {
			add(-1, FindingApproval, "an APPROVED population names approved_by and approved_at")
		} else if !principalID.MatchString(p.ApprovedBy) {
			add(-1, FindingApproval, "approved_by is not a canonical principal id")
		}
	default:
		add(-1, FindingDocument, "status %q is not DRAFT, UNDER_REVIEW or APPROVED", p.Status)
	}
	seen := map[string]int{}
	for i, r := range p.Administrators {
		if !principalID.MatchString(r.PrincipalID) {
			add(i, FindingPrincipal, "principal_id is not a canonical principal id")
		}
		if p.Status == PopulationApproved && r.PrincipalID == p.ApprovedBy {
			add(i, FindingApproval, "nobody approves a population that grants them authority")
		}
		perm, ok := c.Permission(r.Permission)
		if !ok {
			add(i, FindingPermission, "permission %q is not registered", r.Permission)
			continue
		}
		if err := r.Scope.Validate(); err != nil {
			add(i, FindingScope, "%v", err)
		} else if !slices.Contains(perm.ScopeLevels, r.Scope.Level) {
			add(i, FindingScope, "%s may not be granted at %s", r.Permission, r.Scope.Level)
		}
		if (r.Scope.TenantID != "" || r.Scope.OrganisationID != "" || len(r.Scope.OrganisationIDs) > 0) && strings.TrimSpace(r.OrganisationAttestationRef) == "" {
			add(i, FindingAttestation, "a tenant or organisation scope needs organisation_attestation_ref (ruling 3)")
		}
		if effective := EffectiveRisk(perm, r.Scope); !r.RiskClass.AtLeast(effective) {
			add(i, FindingRisk, "risk_class %s is below the %s this grant carries", r.RiskClass, effective)
		}
		if strings.TrimSpace(r.Reason) == "" || len(r.Reason) > 1000 {
			add(i, FindingGrantShape, "reason is required and at most 1000 characters")
		}
		switch {
		case r.GrantType == TypeStanding && r.ValidUntil != nil:
			add(i, FindingGrantShape, "a STANDING grant has no valid_until")
		case r.GrantType != TypeStanding && (r.ValidUntil == nil || !r.ValidUntil.After(r.ValidFrom)):
			add(i, FindingGrantShape, "a %s grant needs a valid_until after valid_from", r.GrantType)
		case r.GrantType != TypeStanding && r.GrantType != TypeTimeBound && r.GrantType != TypeJustInTime:
			add(i, FindingGrantShape, "grant_type %q is unknown", r.GrantType)
		}
		if r.DelegableDepth < 0 || r.DelegableDepth > 2 || (r.DelegableDepth > 0 && !perm.Delegable) {
			add(i, FindingGrantShape, "delegable_depth is 0 to 2 and only for a delegable permission")
		}
		if err := sod.GrantBound(KindIssuance, EffectiveRisk(perm, r.Scope), r.GrantType, r.ValidFrom, r.ValidUntil); err != nil {
			add(i, FindingPolicy, "%v", err)
		}
		if !principalID.MatchString(r.ReviewedBy) || r.ReviewedBy == r.PrincipalID || r.ReviewedAt.IsZero() {
			add(i, FindingReviewer, "a reviewer other than the grantee, and the review time, are required (nobody reviews their own authority)")
		}
		roleOK := false
		for _, role := range r.CurrentRoles {
			roleOK = roleOK || role == RoleEvidencePlatformAdmin || role == RoleEvidenceTenantAdmin
		}
		if !roleOK {
			add(i, FindingRoleEvidence, "current_roles must show a role this grant replaces (%s or %s)", RoleEvidencePlatformAdmin, RoleEvidenceTenantAdmin)
		} else if !slices.Contains(r.CurrentRoles, RoleEvidencePlatformAdmin) && !slices.Contains(tenantCeiling, r.Scope.Level) {
			add(i, FindingRoleCeiling, "a tenant administrator's role reaches tenants, not %s: the grant would be broader than the role", r.Scope.Level)
		}
		if r.OrganisationAttestationRef != "" && len(r.OrganisationAttestationRef) > 256 {
			add(i, FindingAttestation, "organisation_attestation_ref is at most 256 characters")
		}
		key := r.PrincipalID + "|" + r.Permission + "|" + scopeKey(r.Scope)
		if first, dup := seen[key]; dup {
			add(i, FindingDuplicate, "repeats record %d", first)
		}
		seen[key] = i
	}
	report.Valid = len(report.Findings) == 0
	if report.Valid && p.Status == PopulationApproved {
		for i, r := range p.Administrators {
			perm, _ := c.Permission(r.Permission)
			route := "DIRECT"
			if EffectiveRisk(perm, r.Scope).AtLeast(RiskHigh) {
				route = "CHANGESET"
			}
			req := map[string]any{"principal_id": r.PrincipalID, "permission": r.Permission, "scope": r.Scope,
				"grant_type": r.GrantType, "valid_from": r.ValidFrom.UTC().Format(time.RFC3339), "reason": r.Reason}
			if r.ValidUntil != nil {
				req["valid_until"] = r.ValidUntil.UTC().Format(time.RFC3339)
			}
			if r.DelegableDepth > 0 {
				req["delegable_depth"] = r.DelegableDepth
			}
			report.Plan = append(report.Plan, PlannedGrant{Record: i, Route: route, Request: req})
		}
	}
	return report
}

func scopeKey(s Scope) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
