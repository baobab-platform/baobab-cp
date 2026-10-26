package subscription_test

import (
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
)

// Owner PG17 evidence only. Governed test relationships and admission actors
// are synthetic; this cannot qualify independently verified legal eligibility.
func TestCurrentInternalAuthorityNativeStatusDatesAndRevocation(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	tenantID, sub := e.tenant(t, g.sub, g.platform, "", "")
	decision := e.decision(t, apps, applicant, reviewer, decider, internalDecision(g.sub))
	c := e.classifier(g)
	record, _, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil {
		t.Fatal(err)
	}
	rowID, _ := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, sub)
	grants := e.grants(t, tenantID)
	before, err := e.repo.ListSubscriptionClassifications(e.ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	update := func(status string, from time.Time, to *time.Time) {
		t.Helper()
		if _, err := e.admin.Exec(e.ctx, `UPDATE product.product_subscription SET status=$1,effective_from=$2,effective_to=$3 WHERE subscription_id=$4::uuid`, status, from, to, rowID); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want bool) {
		t.Helper()
		got, err := c.CurrentInternalAuthority(e.ctx, tenantID, sub, record.Reference, e.now)
		if err != nil || got != want {
			t.Fatalf("authority=%v err=%v want=%v", got, err, want)
		}
	}
	update("ACTIVE", e.at, nil)
	check(true)
	update("SUSPENDED", e.at, nil)
	// The classification explanation legitimately retains current group
	// eligibility. It is not proof of a live ProductSubscription; this is
	// the exact distinction the old HTTP handler failed to make.
	explanation, err := c.Explain(e.ctx, sub)
	if err != nil || explanation.CurrentEligibility == nil || explanation.CurrentEligibility.EligibilityStatus != "ELIGIBLE" {
		t.Fatalf("expected historical classification with continuing group basis: %v", err)
	}
	check(false)
	update("ACTIVE", e.now.Add(time.Second), nil)
	check(false)
	end := e.now
	update("ACTIVE", e.at, &end)
	check(false)
	end = e.now.Add(time.Second)
	update("ACTIVE", e.at, &end)
	check(true)
	if err := e.repo.EndCorporateRelationship(e.ctx, g.ownsSub, e.now, "controlled test divestiture", orgActor()); err != nil {
		t.Fatal(err)
	}
	check(false)
	after, err := e.repo.ListSubscriptionClassifications(e.ctx, sub)
	if err != nil || len(after) != len(before) || after[0].ID != record.ID || e.grants(t, tenantID) != grants {
		t.Fatalf("authority read mutated classifications or grants: %v", err)
	}
}
