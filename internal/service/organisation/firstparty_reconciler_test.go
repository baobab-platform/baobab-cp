package organisation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
)

// TestFirstPartyReconciliation drives ADR-BCP-018 section 13 reconciliation
// against real PostgreSQL. Each case uses a synthetic first-party id so the
// test is repeatable and never touches real first-party records.
func TestFirstPartyReconciliation(t *testing.T) {
	e := newEnv(t)
	run := strings.ToUpper(token())[:10]
	id := func(c string) string { return "FP-" + c + "-" + run }
	seeded, claimed, tenantOnly, rejected, conflicted := id("SEED"), id("CLAIM"), id("TENANT"), id("REJECT"), id("CONFLICT")

	// claimed: registration-style claim with an applicant-supplied name. Its
	// mappings take the provisioner's default (wall-clock) effective date,
	// later than the reconciliation time below: a future-dated PRIMARY
	// mapping must still count as the tenant's mapping (regression test).
	claimedOrg := e.canonicalOrganisation(t)
	claimReq := ProvisionRequest{TenantID: e.tenantFor(t, claimed), CanonicalEntityID: claimedOrg, LegalEntityID: claimed,
		DisplayName: "Applicant typed name", SourceAuthority: "control-plane-registration", SkipPlatformRel: true, Actor: actor()}
	if _, err := (&Provisioner{Orgs: e.repo}).ProvisionTenantOrganisation(e.ctx, claimReq); err != nil {
		t.Fatal(err)
	}
	// tenantOnly: a pre-000045 tenant with no organisation structure at all.
	tenantOnlyTenant := e.tenantFor(t, tenantOnly)
	if _, err := e.repo.EnsureDefaultTenantLegalEntityMapping(e.ctx, tenantOnlyTenant, tenantOnly, "migration-000045-backfill", e.at, actor()); err != nil {
		t.Fatal(err)
	}
	// rejected: a legal entity whose verification was rejected by review.
	rejectedOrg := e.canonicalOrganisation(t)
	if _, err := (&Provisioner{Orgs: e.repo}).ProvisionTenantOrganisation(e.ctx, ProvisionRequest{TenantID: e.tenantFor(t, rejected),
		CanonicalEntityID: rejectedOrg, LegalEntityID: rejected, DisplayName: "Rejected", SourceAuthority: "applicant-submission",
		SkipPlatformRel: true, Actor: actor()}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE registry.legal_entity_profile SET verification_state='REJECTED' WHERE legal_entity_id=$1`, rejected); err != nil {
		t.Fatal(err)
	}
	// conflicted: the tenant's primary organisation is a different organisation.
	conflictedTenant := e.tenantFor(t, conflicted)
	if _, err := e.repo.EnsureDefaultTenantLegalEntityMapping(e.ctx, conflictedTenant, conflicted, "test", e.at, actor()); err != nil {
		t.Fatal(err)
	}
	otherOrg := e.canonicalOrganisation(t)
	if _, err := e.repo.EnsureTenantOrganisationMapping(e.ctx, domain.TenantOrganisationMapping{TenantID: conflictedTenant, OrganisationID: otherOrg,
		MappingRole: domain.TenantOrgRolePrimary, Status: domain.RelationshipStatusActive, EffectiveFrom: e.at, Provenance: "test"}, actor()); err != nil {
		t.Fatal(err)
	}

	var yaml strings.Builder
	yaml.WriteString("schema:\n  name: \"baobab-platform-legal-entity-registry\"\nentities:\n")
	for _, c := range []struct{ id, name string }{{seeded, "Seeded Holdings"}, {claimed, "Governed Legal Name"},
		{tenantOnly, "Tenant Only Ltd"}, {rejected, "Rejected Ltd"}, {conflicted, "Conflicted Ltd"}} {
		fmt.Fprintf(&yaml, "  - id: %q\n    legal_name: %q\n    role: \"subsidiary\"\n", c.id, c.name)
	}
	reg, err := ParseFirstPartyRegistry([]byte(yaml.String()))
	if err != nil {
		t.Fatal(err)
	}
	reconciler := &FirstPartyReconciler{Orgs: e.repo, Now: func() time.Time { return e.at.Add(time.Hour) }}
	first := actor()
	report, err := reconciler.Reconcile(e.ctx, reg, first)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FirstPartyEntityRun{}
	for _, r := range report.Entities {
		byID[r.LegalEntityID] = r
	}

	// seeded: created VERIFIED from governance, no owner tenant.
	if r := byID[seeded]; !r.Outcome.Created || r.Outcome.OrganisationID == "" {
		t.Fatalf("seeded: %+v", r)
	}
	lep, _ := e.repo.GetLegalEntityProfile(e.ctx, seeded)
	if lep.VerificationState != domain.VerificationVerified || lep.SourceAuthority != "shared-governance" || lep.LegalName != "Seeded Holdings" ||
		lep.LegalStatus != domain.LegalStatusUnknown || len(lep.EvidenceReferences) != 1 || !strings.Contains(lep.EvidenceReferences[0], "@sha256:"+reg.Digest) ||
		lep.VerifiedBy != first.ActorID {
		t.Fatalf("seeded legal entity: %+v", lep)
	}

	// claimed: name corrected (as drift), both profiles verified, tenant already mapped.
	r := byID[claimed]
	if r.Outcome.Created || r.Outcome.OrganisationID != claimedOrg || len(r.TenantsMapped) != 0 {
		t.Fatalf("claimed: %+v", r)
	}
	if len(r.Outcome.Drift) == 0 || r.Outcome.Drift[0].Observed != "Applicant typed name" || r.Outcome.Drift[0].Governed != "Governed Legal Name" || r.Outcome.Drift[0].Blocking {
		t.Fatalf("claimed drift: %+v", r.Outcome.Drift)
	}
	lep, _ = e.repo.GetLegalEntityProfile(e.ctx, claimed)
	org, _ := e.repo.GetOrganisation(e.ctx, claimedOrg)
	if lep.LegalName != "Governed Legal Name" || lep.VerificationState != domain.VerificationVerified ||
		org.VerificationState != domain.VerificationVerified || org.OfficialName != "Governed Legal Name" || org.DisplayName != "Applicant typed name" {
		t.Fatalf("claimed after reconcile: lep=%+v org=%+v", lep, org)
	}

	// tenantOnly: seeded with the only defaulting tenant as owner, and mapped.
	r = byID[tenantOnly]
	if !r.Outcome.Created || len(r.TenantsMapped) != 1 || r.TenantsMapped[0] != tenantOnlyTenant {
		t.Fatalf("tenantOnly: %+v", r)
	}
	var owner string
	if err := e.admin.QueryRow(e.ctx, `SELECT COALESCE(tenant_id,'') FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid`, r.Outcome.OrganisationID).Scan(&owner); err != nil || owner != tenantOnlyTenant {
		t.Fatalf("owner tenant = %q, %v; want %s", owner, err, tenantOnlyTenant)
	}

	// rejected: blocking drift, nothing overturned.
	r = byID[rejected]
	if len(r.Outcome.Drift) != 1 || !r.Outcome.Drift[0].Blocking || len(r.Outcome.Changes) != 0 || !report.Blocking {
		t.Fatalf("rejected: %+v blocking=%v", r, report.Blocking)
	}
	lep, _ = e.repo.GetLegalEntityProfile(e.ctx, rejected)
	if lep.VerificationState != domain.VerificationRejected || lep.LegalName != "Rejected" {
		t.Fatalf("a rejected legal entity must be left untouched: %+v", lep)
	}

	// conflicted: tenant drift reported, existing primary mapping untouched.
	r = byID[conflicted]
	if len(r.TenantDrift) != 1 || r.TenantDrift[0].Observed != otherOrg || len(r.TenantsMapped) != 0 {
		t.Fatalf("conflicted: %+v", r)
	}

	// Events for this run: two seeds and the claim verification, nothing for the rejected record.
	got := map[string]int{}
	for _, typ := range e.outboxTypes(t, first.CorrelationID) {
		got[typ]++
	}
	if got["com.baobab-platform.control-plane.organisation.created.v1"] != 3 || // seeded, tenantOnly, conflicted
		got["com.baobab-platform.control-plane.legal-entity.verified.v1"] != 4 || // seeded, tenantOnly, conflicted, claimed
		got["com.baobab-platform.control-plane.organisation.verified.v1"] != 1 || // claimed
		got["com.baobab-platform.control-plane.tenant-organisation-mapping.activated.v1"] != 1 { // tenantOnly
		t.Fatalf("first run published %v", got)
	}

	// A second run with the same registry changes and publishes nothing; the
	// blocking drift is still reported.
	second := actor()
	again, err := reconciler.Reconcile(e.ctx, reg, second)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range again.Entities {
		if r.Outcome.Created || len(r.Outcome.Changes) != 0 || len(r.TenantsMapped) != 0 {
			t.Errorf("second run changed %s: %+v", r.LegalEntityID, r)
		}
	}
	if types := e.outboxTypes(t, second.CorrelationID); len(types) != 0 || !again.Blocking {
		t.Fatalf("second run published %v (blocking=%v)", types, again.Blocking)
	}
}
