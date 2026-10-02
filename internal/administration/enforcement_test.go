package administration

import (
	"testing"
	"time"
)

func testPolicy(status string, enforced ...EnforcedPermission) *EnforcementPolicy {
	p := &EnforcementPolicy{Enforced: enforced}
	p.Criteria = EnforcementCriteria{Status: status, MinimumObservationDays: 14, MinimumDecisions: 100}
	p.Critical.Enforcement = "PROHIBITED"
	for i, r := range []RiskClass{RiskLow, RiskModerate, RiskHigh, RiskCritical} {
		p.Waves = append(p.Waves, struct {
			Wave      int       `yaml:"wave"`
			RiskClass RiskClass `yaml:"risk_class"`
		}{i + 1, r})
	}
	return p
}

func entry(permission string) EnforcedPermission {
	return EnforcedPermission{Permission: permission, ApprovedBy: "prn_owner", ApprovedAt: "2026-10-30T00:00:00Z", EvidenceRef: "readiness-2026-10-30"}
}

// TestShippedPolicyEnforcesNothing: the policy Shared ships leaves every
// permission with roles. The flip is the owner's act, not a default.
func TestShippedPolicyEnforcesNothing(t *testing.T) {
	policy, err := DefaultEnforcementPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Enforced) != 0 {
		t.Fatalf("the shipped policy enforces %d permissions; none is authorised", len(policy.Enforced))
	}
	// The architecture owner approved the criteria on 2026-10-02 with exactly
	// these values, and CRITICAL enforcement stays prohibited. Approving the
	// criteria enforces nothing.
	want := EnforcementCriteria{Status: "APPROVED", MinimumObservationDays: 14, MinimumDecisions: 100}
	if policy.Criteria != want {
		t.Fatalf("the approved criteria changed: %+v, want %+v", policy.Criteria, want)
	}
	if policy.Critical.Enforcement != "PROHIBITED" {
		t.Fatalf("CRITICAL enforcement is prohibited until the owner lifts it: %q", policy.Critical.Enforcement)
	}
	e := NewEnforcement(policy, MustDefaultCatalogue(), nil)
	for _, p := range MustDefaultCatalogue().Permissions() {
		if e.Mode(p.Key, Resource{TenantID: "tn_ug", Environment: "production"}) != ModeRoleAuthoritative {
			t.Errorf("%s is not role-authoritative", p.Key)
		}
	}
}

func TestEnforcementModeFailsToRoles(t *testing.T) {
	cat := MustDefaultCatalogue()
	res := Resource{TenantID: "tn_ug", Environment: "production"}
	cases := []struct {
		name     string
		policy   *EnforcementPolicy
		rollback []string
		resource Resource
		key      string
		want     string
	}{
		{"nobody enforced", testPolicy("APPROVED"), nil, res, "tenant.view", ModeRoleAuthoritative},
		{"approved and listed", testPolicy("APPROVED", entry("tenant.view")), nil, res, "tenant.view", ModeGrantsEnforced},
		{"listed under unapproved criteria", testPolicy("PROPOSED", entry("tenant.view")), nil, res, "tenant.view", ModeRoleAuthoritative},
		{"other permissions stay with roles", testPolicy("APPROVED", entry("tenant.view")), nil, res, "tenant.suspend", ModeRoleAuthoritative},
		{"unregistered permission", testPolicy("APPROVED", entry("tenant.nope")), nil, res, "tenant.nope", ModeRoleAuthoritative},
		{"entry without approver", testPolicy("APPROVED", EnforcedPermission{Permission: "tenant.view", ApprovedAt: "x", EvidenceRef: "y"}), nil, res, "tenant.view", ModeRoleAuthoritative},
		{"entry without evidence", testPolicy("APPROVED", EnforcedPermission{Permission: "tenant.view", ApprovedBy: "p", ApprovedAt: "x"}), nil, res, "tenant.view", ModeRoleAuthoritative},
		{"CRITICAL never, whatever the entry", testPolicy("APPROVED", entry("tenant.decommission")), nil, res, "tenant.decommission", ModeRoleAuthoritative},
		{"rolled back by key", testPolicy("APPROVED", entry("tenant.view")), []string{"tenant.view"}, res, "tenant.view", ModeRoleAuthoritative},
		{"rolled back for all", testPolicy("APPROVED", entry("tenant.view")), []string{" * "}, res, "tenant.view", ModeRoleAuthoritative},
		{"another key rolled back", testPolicy("APPROVED", entry("tenant.view")), []string{"tenant.suspend"}, res, "tenant.view", ModeGrantsEnforced},
	}
	for _, c := range cases {
		if got := NewEnforcement(c.policy, cat, c.rollback).Mode(c.key, c.resource); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	var none *Enforcement
	if none.Mode("tenant.view", res) != ModeRoleAuthoritative || none.Lists("tenant.view") {
		t.Error("no enforcement configured must leave roles authoritative")
	}
}

func TestEnforcementScopeNarrowsWhereGrantsDecide(t *testing.T) {
	scoped := entry("tenant.view")
	scoped.Scope = &struct {
		Environments []string `yaml:"environments"`
		Tenants      []string `yaml:"tenants"`
	}{Environments: []string{"staging"}, Tenants: []string{"tn_canary"}}
	e := NewEnforcement(testPolicy("APPROVED", scoped), MustDefaultCatalogue(), nil)
	for name, c := range map[string]struct {
		res  Resource
		want string
	}{
		"in scope":          {Resource{Environment: "staging", TenantID: "tn_canary"}, ModeGrantsEnforced},
		"other environment": {Resource{Environment: "production", TenantID: "tn_canary"}, ModeRoleAuthoritative},
		"other tenant":      {Resource{Environment: "staging", TenantID: "tn_other"}, ModeRoleAuthoritative},
		"names no tenant":   {Resource{Environment: "staging"}, ModeRoleAuthoritative},
	} {
		if got := e.Mode("tenant.view", c.res); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

func evidence(key string, decisions, days int64, mod func(*PermissionEvidence)) PermissionEvidence {
	ev := PermissionEvidence{Permission: key, Decisions: decisions, Agree: decisions, ObservedDays: int(days)}
	if mod != nil {
		mod(&ev)
	}
	return ev
}

func TestReadinessIsEvidenceNeverEnforcement(t *testing.T) {
	cat := MustDefaultCatalogue()
	now := time.Now()
	find := func(r MigrationReadiness, key string) PermissionReadiness {
		for _, p := range r.Permissions {
			if p.Permission == key {
				return p
			}
		}
		t.Fatalf("%s is missing from the report", key)
		return PermissionReadiness{}
	}
	approved := testPolicy("APPROVED")
	good := []PermissionEvidence{evidence("tenant.view", 500, 20, nil)}
	r := Readiness(approved, cat, nil, good, now)
	if row := find(r, "tenant.view"); !row.Ready || len(row.Blockers) != 0 || row.Enforcement != ModeRoleAuthoritative {
		t.Fatalf("clean evidence under approved criteria is ready, and ready is not enforced: %+v", row)
	}
	if len(r.Permissions) != len(cat.Permissions()) {
		t.Fatalf("every registered permission is reported: %d of %d", len(r.Permissions), len(cat.Permissions()))
	}
	for name, c := range map[string]struct {
		policy  *EnforcementPolicy
		ev      PermissionEvidence
		blocker string
	}{
		"criteria proposed":   {testPolicy("PROPOSED"), good[0], BlockerCriteriaNotApproved},
		"never observed":      {approved, PermissionEvidence{Permission: "tenant.view"}, BlockerNotObserved},
		"too few days":        {approved, evidence("tenant.view", 500, 3, nil), BlockerObservationPeriod},
		"too few decisions":   {approved, evidence("tenant.view", 50, 20, nil), BlockerDecisions},
		"grants broader":      {approved, evidence("tenant.view", 500, 20, func(e *PermissionEvidence) { e.GrantsBroader = 1 }), BlockerGrantsBroader},
		"grants narrower":     {approved, evidence("tenant.view", 500, 20, func(e *PermissionEvidence) { e.GrantsNarrower = 1 }), BlockerGrantsNarrower},
		"not evaluated":       {approved, evidence("tenant.view", 500, 20, func(e *PermissionEvidence) { e.NotEvaluated = 1 }), BlockerNotEvaluated},
		"unresolved or error": {approved, evidence("tenant.view", 500, 20, func(e *PermissionEvidence) { e.UnresolvedOrError = 1 }), BlockerUnresolvedOrError},
	} {
		row := find(Readiness(c.policy, cat, nil, []PermissionEvidence{c.ev}, now), "tenant.view")
		if row.Ready || !hasBlocker(row.Blockers, c.blocker) {
			t.Errorf("%s: want blocker %s, got ready=%v %v", name, c.blocker, row.Ready, row.Blockers)
		}
	}
	// A bounded tolerance raises the limit; broader never has one.
	tolerant := testPolicy("APPROVED")
	tolerant.Criteria.MaximumGrantsNarrower = 2
	if row := find(Readiness(tolerant, cat, nil, []PermissionEvidence{evidence("tenant.view", 500, 20, func(e *PermissionEvidence) { e.GrantsNarrower = 2 })}, now), "tenant.view"); !row.Ready {
		t.Errorf("narrower within the approved tolerance is ready: %+v", row)
	}
	// CRITICAL is never ready, with the best evidence.
	best := []PermissionEvidence{evidence("tenant.decommission", 5000, 90, nil)}
	if row := find(Readiness(approved, cat, nil, best, now), "tenant.decommission"); row.Ready || !hasBlocker(row.Blockers, BlockerCriticalProhibited) || row.Wave != 4 {
		t.Errorf("CRITICAL must be prohibited whatever the evidence: %+v", row)
	}
	// Waves order the report: LOW first, CRITICAL last.
	waves := ""
	for _, p := range r.Permissions {
		if p.Wave < 1 || p.Wave > 4 {
			t.Fatalf("wave %d", p.Wave)
		}
		waves += string(rune('0' + p.Wave))
	}
	for i := 1; i < len(waves); i++ {
		if waves[i] < waves[i-1] {
			t.Fatalf("report is not in wave order: %s", waves)
		}
	}
}

func hasBlocker(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
