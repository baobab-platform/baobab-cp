package administration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func goodPopulation() ReviewedPopulation {
	approved := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	return ReviewedPopulation{
		PopulationID: "launch-administrators", Version: 1, Status: PopulationApproved,
		PreparedAt: approved.Add(-48 * time.Hour), ApprovedBy: "prn_owner", ApprovedAt: &approved,
		Administrators: []ReviewedAdministrator{
			{PrincipalID: "prn_jane", CurrentRoles: []string{RoleEvidenceTenantAdmin}, Permission: "tenant.view",
				Scope: Scope{Level: LevelTenant, TenantID: "tn_acmeug"}, OrganisationAttestationRef: "attestation:acmeug",
				GrantType: TypeStanding, ValidFrom: approved, RiskClass: RiskLow, Reason: "Views the tenant she administers today.",
				ReviewedBy: "prn_reviewer", ReviewedAt: approved.Add(-time.Hour)},
		},
	}
}

func codes(r PopulationReport) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

func has(r PopulationReport, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestApprovedPopulationIsPlannedNotIssued(t *testing.T) {
	r := CheckPopulation(MustDefaultCatalogue(), MustDefaultSoD(), goodPopulation(), time.Now())
	if !r.Valid || len(r.Plan) != 1 || r.Plan[0].Route != "DIRECT" {
		t.Fatalf("a clean approved population plans a direct request: %+v", r)
	}
	body, _ := json.Marshal(r.Plan[0].Request)
	for _, want := range []string{`"principal_id":"prn_jane"`, `"permission":"tenant.view"`, `"grant_type":"STANDING"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request %s lacks %s", body, want)
		}
	}
	// A population that is not approved is checked, never planned.
	for _, status := range []string{PopulationDraft, PopulationUnderReview} {
		p := goodPopulation()
		p.Status, p.ApprovedBy, p.ApprovedAt = status, "", nil
		if r := CheckPopulation(MustDefaultCatalogue(), MustDefaultSoD(), p, time.Now()); !r.Valid || len(r.Plan) != 0 {
			t.Errorf("%s: valid but not planned expected: %+v", status, r)
		}
	}
}

func TestHighAuthorityIsPlannedAsAChangeset(t *testing.T) {
	p := goodPopulation()
	r0 := &p.Administrators[0]
	r0.CurrentRoles = []string{RoleEvidencePlatformAdmin}
	r0.Permission, r0.Scope, r0.OrganisationAttestationRef = "tenant.suspend", Scope{Level: LevelTenant, TenantID: "tn_acmeug"}, "attestation:acmeug"
	r0.RiskClass, r0.GrantType = RiskHigh, TypeTimeBound
	until := r0.ValidFrom.AddDate(0, 0, 30)
	r0.ValidUntil = &until
	r := CheckPopulation(MustDefaultCatalogue(), MustDefaultSoD(), p, time.Now())
	if !r.Valid || len(r.Plan) != 1 || r.Plan[0].Route != "CHANGESET" {
		t.Fatalf("HIGH authority goes through a changeset with an independent approver: %+v", r)
	}
}

func TestPopulationFindings(t *testing.T) {
	cat, sod := MustDefaultCatalogue(), MustDefaultSoD()
	for name, c := range map[string]struct {
		mutate func(*ReviewedPopulation)
		want   string
	}{
		"unknown permission": {func(p *ReviewedPopulation) { p.Administrators[0].Permission = "tenant.nope" }, FindingPermission},
		"level the permission lacks": {func(p *ReviewedPopulation) {
			a := &p.Administrators[0]
			a.CurrentRoles, a.Permission, a.RiskClass = []string{RoleEvidencePlatformAdmin}, "tenant.suspend", RiskHigh
			a.Scope = Scope{Level: LevelOrganisation, OrganisationID: "org_1"}
		}, FindingScope},
		"scope names two anchors": {func(p *ReviewedPopulation) { p.Administrators[0].Scope.OrganisationID = "org_1" }, FindingScope},
		"no attestation":          {func(p *ReviewedPopulation) { p.Administrators[0].OrganisationAttestationRef = "" }, FindingAttestation},
		"risk below the permission": {func(p *ReviewedPopulation) {
			p.Administrators[0].Permission, p.Administrators[0].RiskClass = "tenant.suspend", RiskLow
		}, FindingRisk},
		"no reason":                                {func(p *ReviewedPopulation) { p.Administrators[0].Reason = " " }, FindingGrantShape},
		"standing with an end":                     {func(p *ReviewedPopulation) { u := time.Now().Add(time.Hour); p.Administrators[0].ValidUntil = &u }, FindingGrantShape},
		"time bound without end":                   {func(p *ReviewedPopulation) { p.Administrators[0].GrantType = TypeTimeBound }, FindingGrantShape},
		"unknown grant type":                       {func(p *ReviewedPopulation) { p.Administrators[0].GrantType = "FOREVER" }, FindingGrantShape},
		"delegation of a non-delegable permission": {func(p *ReviewedPopulation) { p.Administrators[0].DelegableDepth = 3 }, FindingGrantShape},
		"reviewer is the grantee":                  {func(p *ReviewedPopulation) { p.Administrators[0].ReviewedBy = "prn_jane" }, FindingReviewer},
		"no review time":                           {func(p *ReviewedPopulation) { p.Administrators[0].ReviewedAt = time.Time{} }, FindingReviewer},
		"no role evidence":                         {func(p *ReviewedPopulation) { p.Administrators[0].CurrentRoles = nil }, FindingRoleEvidence},
		"unrelated role":                           {func(p *ReviewedPopulation) { p.Administrators[0].CurrentRoles = []string{"offline_access"} }, FindingRoleEvidence},
		"tenant admin proposed platform scope": {func(p *ReviewedPopulation) {
			p.Administrators[0].Permission, p.Administrators[0].Scope = "support.diagnostics.view", Scope{Level: LevelPlatform}
			p.Administrators[0].RiskClass = RiskModerate
		}, FindingRoleCeiling},
		"duplicate record":          {func(p *ReviewedPopulation) { p.Administrators = append(p.Administrators, p.Administrators[0]) }, FindingDuplicate},
		"approved without approver": {func(p *ReviewedPopulation) { p.ApprovedBy = "" }, FindingApproval},
		"approver is a grantee":     {func(p *ReviewedPopulation) { p.ApprovedBy = "prn_jane" }, FindingApproval},
		"bad principal":             {func(p *ReviewedPopulation) { p.Administrators[0].PrincipalID = "jane@example.com" }, FindingPrincipal},
		"unknown status":            {func(p *ReviewedPopulation) { p.Status = "LIVE" }, FindingDocument},
		"empty":                     {func(p *ReviewedPopulation) { p.Administrators = nil }, FindingDocument},
	} {
		p := goodPopulation()
		c.mutate(&p)
		r := CheckPopulation(cat, sod, p, time.Now())
		if r.Valid || !has(r, c.want) || len(r.Plan) != 0 {
			t.Errorf("%s: want finding %s and no plan, got valid=%v %v plan=%d", name, c.want, r.Valid, codes(r), len(r.Plan))
		}
	}
}

func TestCriticalPopulationRecordsFollowThePolicy(t *testing.T) {
	p := goodPopulation()
	r0 := &p.Administrators[0]
	r0.CurrentRoles = []string{RoleEvidencePlatformAdmin}
	r0.Permission, r0.Scope, r0.OrganisationAttestationRef = "tenant.decommission", Scope{Level: LevelPlatform}, ""
	r0.RiskClass = RiskCritical
	// STANDING CRITICAL is prohibited; so is a long TIME_BOUND one.
	if r := CheckPopulation(MustDefaultCatalogue(), MustDefaultSoD(), p, time.Now()); !has(r, FindingPolicy) {
		t.Errorf("a STANDING CRITICAL grant must be refused: %v", codes(r))
	}
	r0.GrantType = TypeTimeBound
	long := r0.ValidFrom.AddDate(0, 0, 30)
	r0.ValidUntil = &long
	if r := CheckPopulation(MustDefaultCatalogue(), MustDefaultSoD(), p, time.Now()); !has(r, FindingPolicy) {
		t.Errorf("a CRITICAL grant over 24 hours must be refused: %v", codes(r))
	}
	short := r0.ValidFrom.Add(23 * time.Hour)
	r0.ValidUntil = &short
	if r := CheckPopulation(MustDefaultCatalogue(), MustDefaultSoD(), p, time.Now()); !r.Valid || r.Plan[0].Route != "CHANGESET" {
		t.Errorf("a 23-hour CRITICAL grant is valid and goes through a changeset: %+v", r)
	}
}

func TestParsePopulationIsStrict(t *testing.T) {
	if _, err := ParsePopulation([]byte(`{"population_id":"x","version":1,"status":"DRAFT","prepared_at":"2026-10-01T00:00:00Z","administrators":[],"grant_everything":true}`)); err == nil {
		t.Error("an unknown field must be refused")
	}
	if _, err := ParsePopulation([]byte(`{`)); err == nil {
		t.Error("malformed JSON must be refused")
	}
}
