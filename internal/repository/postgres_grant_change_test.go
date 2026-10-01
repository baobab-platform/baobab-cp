package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestAdministrativeGrantChangesets covers migration 000090 and ADR-BCP-020
// gate ADA-06 end to end: HIGH authority is issued and delegated only
// through a changeset whose approver is neither the requester nor the
// grantee, the plan checks block what must not be granted, a source grant
// that moves makes an approved delegation stale, and the created grant
// carries the approval as its approval_reference.
func TestAdministrativeGrantChangesets(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	newPrincipal := func(status string) string {
		t.Helper()
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: status}
		if err := repo.CreateIdentity(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	grantee, delegate, dormant, delegator, bounded, holder := newPrincipal("ACTIVE"), newPrincipal("ACTIVE"), newPrincipal("SUSPENDED"), newPrincipal("ACTIVE"), newPrincipal("ACTIVE"), newPrincipal("ACTIVE")
	maker, checker := "prn_maker"+suffix, "prn_checker"+suffix
	who := []string{grantee, delegate, dormant, delegator, bounded, holder}
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE requested_by = ANY($1))`, append([]string{maker}, who...))
		admin.Exec(ctx, `DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE requested_by = ANY($1))`, append([]string{maker}, who...))
		admin.Exec(ctx, `UPDATE changeset.changeset SET current_plan_id = NULL WHERE requested_by = ANY($1)`, append([]string{maker}, who...))
		admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE requested_by = ANY($1)`, append([]string{maker}, who...))
		admin.Exec(ctx, `DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE requested_by = ANY($1))`, append([]string{maker}, who...))
		admin.Exec(ctx, `DELETE FROM changeset.changeset WHERE requested_by = ANY($1)`, append([]string{maker}, who...))
		admin.Exec(ctx, `UPDATE policy.administrative_grant SET superseded_by_grant_id = NULL, supersedes_grant_id = NULL WHERE principal_id = ANY($1)`, who)
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND delegated_from IS NOT NULL`, who)
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1)`, who)
	}
	cleanup()
	t.Cleanup(cleanup)

	recordSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/Changeset")
	planSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetPlan")
	conforms := func(label string, schema *contracts.Schema, v any) {
		t.Helper()
		if err := contracts.ValidateValue(schema, v); err != nil {
			t.Fatalf("%s does not conform: %v", label, err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	until := now.Add(48 * time.Hour)
	tenantScope := administration.Scope{Level: administration.LevelTenant, TenantID: "tn_cg" + suffix}
	correlation := domain.NewUUIDv7()
	as := func(id string) AuditActor {
		return AuditActor{ActorID: id, ActorType: "human", CorrelationID: correlation}
	}

	keyN := 0
	draft := func(requester string, d changeset.DesiredChange) changeset.Changeset {
		t.Helper()
		base, _, err := repo.TargetRevision(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		c, err := changeset.Draft(changeset.CreateRequest{Title: "Grant change", Reason: "Cover the on-call rota.", DesiredChange: d},
			domain.NewResourceID("cs"), requester, "API", correlation, base, now)
		if err != nil {
			t.Fatal(err)
		}
		keyN++
		created, err := repo.CreateChangeset(ctx, c, "key-grant-"+suffix+string(rune('a'+keyN)), "hash", as(requester))
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	submit := func(requester string, c changeset.Changeset) changeset.Changeset {
		t.Helper()
		out, err := repo.SubmitChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("plan"), now, as(requester))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	issuance := func(to string) changeset.DesiredChange {
		return changeset.DesiredChange{Kind: changeset.KindGrantIssuance, PrincipalID: to, Permission: "tenant.suspend", Scope: &tenantScope,
			GrantType: administration.TypeTimeBound, ValidUntil: &until}
	}
	decide := func(requester, approver string, c changeset.Changeset) (changeset.Approval, error) {
		t.Helper()
		plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
		if err != nil {
			t.Fatal(err)
		}
		return repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion,
			PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}, domain.NewResourceID("apd"), approver, "", now, as(approver))
	}

	// --- Issuance: a HIGH grant through maker-checker.
	c := submit(maker, draft(maker, issuance(grantee)))
	if c.State != changeset.StateAwaitingApproval || c.RiskClass != "HIGH" {
		t.Fatalf("issuance did not reach approval: %+v", c)
	}
	conforms("issuance changeset", recordSchema, c)
	plan, _ := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
	conforms("issuance plan", planSchema, plan)
	if plan.Steps[0].Operation != changeset.OpIssueGrant || plan.Steps[0].Resources.GranteePrincipalID != grantee {
		t.Fatalf("unexpected plan steps %+v", plan.Steps)
	}

	if _, err := decide(maker, maker, c); !errors.Is(err, ErrChangesetSelfApproval) {
		t.Fatalf("requester approving: %v", err)
	}
	if _, err := decide(maker, grantee, c); !errors.Is(err, ErrGrantSelfApproval) {
		t.Fatalf("grantee approving their own grant: %v", err)
	}
	approval, err := decide(maker, checker, c)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = repo.GetChangeset(ctx, c.ChangesetID)
	op, _, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-grant-"+suffix, "h", maker, now, as(maker))
	if err != nil || op.Status != "SUCCEEDED" {
		t.Fatalf("apply: %+v %v", op, err)
	}
	grants, _, err := repo.AdministrativeGrantsOf(ctx, grantee)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants after apply: %d %v", len(grants), err)
	}
	g := grants[0]
	if g.Permission != "tenant.suspend" || g.Source != administration.SourceDirect || g.GrantedBy != maker ||
		g.ApprovalReference != approval.ApprovalID || g.Status != administration.StatusActive || g.RiskClass != administration.RiskHigh {
		t.Fatalf("unexpected issued grant %+v", g)
	}
	var audited int
	admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.created' AND target = $1`, g.GrantID).Scan(&audited)
	if audited != 1 {
		t.Fatalf("issuing wrote %d audit events", audited)
	}

	// --- Plan checks block what must not be granted.
	blocked := func(label, code string, requester string, d changeset.DesiredChange) {
		t.Helper()
		out := submit(requester, draft(requester, d))
		if out.State != changeset.StateBlocked || !hasCode(out.BlockingReasons, code) {
			t.Fatalf("%s: want BLOCKED with %s, got %s %+v", label, code, out.State, out.BlockingReasons)
		}
		// A blocked changeset still holds its target (section 66).
		if _, err := repo.CancelChangeset(ctx, out.ChangesetID, out.Revision, "test", now, as(requester)); err != nil {
			t.Fatal(err)
		}
	}
	blocked("a duplicate grant", "GRANT_ALREADY_HELD", maker, issuance(grantee))
	blocked("an inactive grantee", "GRANT_GRANTEE_NOT_ACTIVE", maker, issuance(dormant))
	blocked("a grant requested for oneself", "GRANT_REQUESTER_IS_GRANTEE", delegate, issuance(delegate))
	standing := changeset.DesiredChange{Kind: changeset.KindGrantIssuance, PrincipalID: delegate, Permission: "administrator.grant",
		Scope: &administration.Scope{Level: administration.LevelPlatform}, GrantType: administration.TypeStanding}
	blocked("a STANDING CRITICAL grant", "GRANT_VALIDITY_INVALID", maker, standing)
	notRegistered := issuance(delegate)
	notRegistered.Permission = "tenant.own"
	blocked("an unregistered permission", "GRANT_PERMISSION_NOT_GRANTABLE", maker, notRegistered)
	longCritical := standing
	longCritical.GrantType = administration.TypeTimeBound
	far := now.Add(25 * time.Hour)
	longCritical.ValidUntil = &far
	blocked("a CRITICAL grant beyond its bound", "GRANT_VALIDITY_INVALID", maker, longCritical)
	boundedCritical := longCritical
	boundedCritical.PrincipalID = bounded
	short := now.Add(23 * time.Hour)
	boundedCritical.ValidUntil = &short
	if out := submit(maker, draft(maker, boundedCritical)); out.State != changeset.StateAwaitingApproval || out.RiskClass != "CRITICAL" {
		t.Fatalf("a bounded CRITICAL grant must reach approval as CRITICAL: %+v", out)
	}

	// --- Delegation of HIGH authority.
	source := administration.Grant{GrantID: domain.NewResourceID("agr"), PrincipalID: delegator, Permission: "tenant.suspend", Scope: tenantScope,
		GrantType: administration.TypeStanding, Source: administration.SourceDirect, DelegableDepth: 1, RiskClass: administration.RiskHigh,
		ValidFrom: now.Add(-time.Hour), Status: administration.StatusActive, GrantedBy: "prn_ops" + suffix, Reason: "seed", CreatedAt: now, Version: 1}
	if err := repo.CreateAdministrativeGrant(ctx, source, as("prn_ops"+suffix)); err != nil {
		t.Fatal(err)
	}
	delegation := func(to string, sourceID string) changeset.DesiredChange {
		return changeset.DesiredChange{Kind: changeset.KindGrantDelegation, SourceGrantID: sourceID, PrincipalID: to, Permission: "tenant.suspend",
			Scope: &tenantScope, ValidUntil: &until}
	}
	blocked("delegating someone else's grant", "DELEGATION_REQUESTER_NOT_HOLDER", maker, delegation(delegate, source.GrantID))
	blocked("a delegation to oneself", "GRANT_REQUESTER_IS_GRANTEE", delegator, delegation(delegator, source.GrantID))
	beyond := delegation(delegate, source.GrantID)
	beyond.DelegableDepth = 1 // the source allows one hop, so the delegate may allow none
	blocked("more hops than the source has left", "DELEGATION_EXCEEDS_SOURCE", delegator, beyond)

	d := submit(delegator, draft(delegator, delegation(delegate, source.GrantID)))
	if d.State != changeset.StateAwaitingApproval || d.RiskClass != "HIGH" {
		t.Fatalf("delegation did not reach approval: %+v", d)
	}
	dplan, _ := repo.CurrentChangesetPlan(ctx, d.ChangesetID)
	conforms("delegation plan", planSchema, dplan)
	if _, err := decide(delegator, delegate, d); !errors.Is(err, ErrGrantSelfApproval) {
		t.Fatalf("the delegate approving their own delegation: %v", err)
	}
	dapproval, err := decide(delegator, checker, d)
	if err != nil {
		t.Fatal(err)
	}
	d, _ = repo.GetChangeset(ctx, d.ChangesetID)
	// The source moves after approval: the plan is stale.
	if _, err := admin.Exec(ctx, `UPDATE policy.administrative_grant SET version = version + 1 WHERE grant_id = $1::uuid`, mustRowID(source.GrantID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyChangeset(ctx, d.ChangesetID, d.Revision, domain.NewResourceID("op"), "apply-stale-"+suffix, "h", delegator, now, as(delegator)); !errors.Is(err, ErrChangesetPlanStale) {
		t.Fatalf("a delegation whose source moved applied: %v", err)
	}
	d = submit(delegator, d)
	dapproval, err = decide(delegator, checker, d)
	if err != nil {
		t.Fatal(err)
	}
	d, _ = repo.GetChangeset(ctx, d.ChangesetID)
	if _, _, err := repo.ApplyChangeset(ctx, d.ChangesetID, d.Revision, domain.NewResourceID("op"), "apply-deleg-"+suffix, "h", delegator, now, as(delegator)); err != nil {
		t.Fatal(err)
	}
	held, sources, err := repo.AdministrativeGrantsOf(ctx, delegate)
	if err != nil || len(held) != 1 {
		t.Fatalf("delegate grants: %d %v", len(held), err)
	}
	dg := held[0]
	if dg.Source != administration.SourceDelegation || dg.DelegatedFromGrantID != source.GrantID || dg.GrantedBy != delegator ||
		dg.ApprovalReference != dapproval.ApprovalID || dg.DelegationDepth != 1 {
		t.Fatalf("unexpected delegation %+v", dg)
	}
	if !administration.Usable(dg, sources, now, administration.Relations{}) {
		t.Fatal("an approved delegation must be usable by the evaluator")
	}
	_ = dapproval

	// --- Replacement that adds HIGH authority: a changeset naming replaces_grant_id.
	oldEnd := now.Add(24 * time.Hour)
	heldGrant := administration.Grant{GrantID: domain.NewResourceID("agr"), PrincipalID: holder, Permission: "tenant.suspend", Scope: tenantScope,
		GrantType: administration.TypeTimeBound, Source: administration.SourceDirect, RiskClass: administration.RiskHigh, ValidUntil: &oldEnd,
		ValidFrom: now.Add(-time.Hour), Status: administration.StatusActive, GrantedBy: "prn_ops" + suffix, Reason: "seed", CreatedAt: now, Version: 1}
	if err := repo.CreateAdministrativeGrant(ctx, heldGrant, as("prn_ops"+suffix)); err != nil {
		t.Fatal(err)
	}
	replacing := func(for_ string, replaces string) changeset.DesiredChange {
		d := issuance(for_)
		d.ReplacesGrantID = replaces
		return d
	}
	// Without replaces_grant_id the same request is a duplicate; with it, it is not.
	blocked("a duplicate without replaces_grant_id", "GRANT_ALREADY_HELD", maker, issuance(holder))
	blocked("replacing someone else's grant", "GRANT_REPLACED_GRANT_INVALID", maker, replacing(grantee, heldGrant.GrantID))
	blocked("replacing a grant that does not exist", "GRANT_REPLACED_GRANT_INVALID", maker, replacing(holder, "agr_01k9doesnotexist"))
	r := submit(maker, draft(maker, replacing(holder, heldGrant.GrantID)))
	if r.State != changeset.StateAwaitingApproval {
		t.Fatalf("a replacement changeset did not reach approval: %s %+v", r.State, r.BlockingReasons)
	}
	// Another change replacing the same grant is blocked while this one is open.
	// (It targets the same grantee, so the section 66 lock reports first; the plan check holds on its own.)
	if _, err := decide(maker, checker, r); err != nil {
		t.Fatal(err)
	}
	r, _ = repo.GetChangeset(ctx, r.ChangesetID)
	if _, _, err := repo.ApplyChangeset(ctx, r.ChangesetID, r.Revision, domain.NewResourceID("op"), "apply-replace-"+suffix, "h", maker, now, as(maker)); err != nil {
		t.Fatal(err)
	}
	after, _ := repo.GetAdministrativeGrant(ctx, heldGrant.GrantID)
	if after.Status != administration.StatusRevoked || after.SupersededByGrantID == "" || after.ValidUntil == nil || !after.ValidUntil.Equal(oldEnd) {
		t.Fatalf("the replaced grant was not revoked unchanged: %+v", after)
	}
	next, err := repo.GetAdministrativeGrant(ctx, after.SupersededByGrantID)
	if err != nil || next.SupersedesGrantID != heldGrant.GrantID || next.PrincipalID != holder || next.Status != administration.StatusActive ||
		next.ApprovalReference == "" || next.GrantedBy != maker || next.ValidUntil == nil || !next.ValidUntil.Equal(until) {
		t.Fatalf("unexpected replacement grant %+v %v", next, err)
	}
}
