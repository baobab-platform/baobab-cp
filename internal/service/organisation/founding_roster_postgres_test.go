package organisation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// Accepted ADR-BCP-027: source-controlled first-party identity alone never
// creates a Tenant, verified company, parent liability, or internal entitlement.
func TestPEO04BPostgresFourFoundingOrganisationsStayUnprovisionedUntilReviewed(t *testing.T) {
	e := newEnv(t)
	suffix := strings.ToUpper(token())[:10]
	type record struct{ key, kind, claim string }
	roster := []record{
		{"FN-" + suffix, "LEGAL_PERSON", "REGISTERED_EVIDENCED"},
		{"FT-" + suffix, "LEGAL_PERSON", "REGISTERED_CLAIMED"},
		{"FZ-" + suffix, "OPERATING_BUSINESS", "NOT_INCORPORATED"},
		{"FE-" + suffix, "OPERATING_BUSINESS", "NOT_INCORPORATED"},
	}
	var body strings.Builder
	body.WriteString("schema:\n  name: baobab-platform-legal-entity-registry\nentities:\n")
	for i, item := range roster {
		body.WriteString(fmt.Sprintf("  - id: %q\n    legal_name: %q\n    role: subsidiary\n    identity_class: %s\n    incorporation_claim: %s\n",
			item.key, fmt.Sprintf("Synthetic independently governed business %d", i), item.kind, item.claim))
	}
	reg, err := ParseFirstPartyRegistry([]byte(body.String()))
	if err != nil {
		t.Fatal(err)
	}
	svc := FirstPartyReconciler{Orgs: e.repo, Now: func() time.Time { return time.Now().UTC() }}
	before, err := svc.Reconcile(context.Background(), reg, actor())
	if err != nil || before.Blocking || len(before.Entities) != 4 {
		t.Fatalf("cannot recognise independent founding roster: %+v %v", before, err)
	}
	actual := map[string]bool{}
	for _, row := range before.Entities {
		if !domain.IsUUID(row.Outcome.OrganisationID) || actual[row.Outcome.OrganisationID] {
			t.Fatalf("first-party identities merged: %+v", row)
		}
		actual[row.Outcome.OrganisationID] = true
	}
	at := time.Now().UTC()
	snapshot := PostgresFoundingSnapshot{Pool: e.admin}
	outcomes, err := QualifyFoundingRoster(e.ctx, reg, snapshot, at)
	if err != nil || len(outcomes) != 4 {
		t.Fatalf("source-backed qualification failed: %+v %v", outcomes, err)
	}
	for _, v := range outcomes {
		if !v.OrganisationRecognised || v.TenantID != "" || v.TenantPrimaryIsolated ||
			v.CompanyVerificationRecorded || v.CurrentSponsorshipEligible || v.ActiveScopedMandateCount != 0 ||
			v.Readiness != "PENDING" {
			t.Fatalf("governed identity incorrectly auto-onboarded or verified: %+v", v)
		}
		var companyProfiles int
		if err = e.admin.QueryRow(e.ctx, `SELECT count(*) FROM registry.legal_entity_profile
   WHERE organisation_id=$1::uuid`, v.OrganisationID).Scan(&companyProfiles); err != nil || companyProfiles != 0 {
			t.Fatalf("first-party registry made fake company: %s %d %v", v.FirstPartyID, companyProfiles, err)
		}
	}
	after, err := svc.Reconcile(context.Background(), reg, actor())
	if err != nil || after.Blocking {
		t.Fatalf("replay drifted founding organisations: %+v %v", after, err)
	}
	for _, v := range after.Entities {
		if v.Outcome.Created || len(v.Outcome.Changes) != 0 || len(v.TenantsMapped) != 0 {
			t.Fatalf("replayed first-party admission mutated stable entity: %+v", v)
		}
	}
}
