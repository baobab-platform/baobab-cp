package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAdministrativeGrantAdministration covers migration 000089 and the
// ADA-05 repository: issuing is replay-safe, transitions follow the
// lifecycle at the grant's version, revoking ends every delegation, and the
// platform activates and expires grants itself. Each change is audited.
// digest makes a 64-character request hash, as the handler does.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestAdministrativeGrantAdministration(t *testing.T) {
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

	tail := domain.NewUUIDv7()[24:]
	jane, bob, ops, other := "prn_jane"+tail, "prn_bob"+tail, "prn_ops"+tail, "prn_other"+tail
	t.Cleanup(func() {
		who := []string{jane, bob, other}
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant_command WHERE actor_id = $1`, ops)
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND delegated_from IS NOT NULL`, who)
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1)`, who)
	})
	actor := func() AuditActor {
		return AuditActor{ActorID: ops, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	catalogue := administration.MustDefaultCatalogue()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant := administration.Scope{Level: administration.LevelTenant, TenantID: "tn_acmeug"}
	plan := func(principal string, depth int) administration.Grant {
		g, err := administration.PlanIssue(catalogue, ops, administration.IssueRequest{PrincipalID: principal, Permission: "tenant.view",
			Scope: tenant, GrantType: administration.TypeStanding, DelegableDepth: depth, Reason: "test"}, now)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}

	// Issuing: replay with the same request returns the same grant, a
	// different request under the same key is refused, and it is audited.
	g := plan(jane, 1)
	issued, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-0000001", digest("hash-a"), g)
	if err != nil {
		t.Fatal(err)
	}
	if issued.GrantID == "" || issued.Version != 1 || issued.Status != administration.StatusActive {
		t.Fatalf("issued %+v", issued)
	}
	again, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-0000001", digest("hash-a"), plan(jane, 1))
	if err != nil || again.GrantID != issued.GrantID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-0000001", digest("hash-b"), plan(jane, 1)); !errors.Is(err, ErrGrantCommandKeyReused) {
		t.Fatalf("a reused key must be refused, got %v", err)
	}
	var count int
	admin.QueryRow(ctx, `SELECT count(*) FROM policy.administrative_grant WHERE principal_id = $1`, jane).Scan(&count)
	if count != 1 {
		t.Fatalf("a replay created %d grants", count)
	}
	admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.created' AND target = $1`, issued.GrantID).Scan(&count)
	if count != 1 {
		t.Fatalf("issuing wrote %d audit events", count)
	}

	// Inspecting and paging.
	for i := 0; i < 2; i++ {
		if _, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-extra"+string(rune('a'+i))+"000000", digest("h"+string(rune('a'+i))), plan(other, 0)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetAdministrativeGrant(ctx, issued.GrantID)
	if err != nil || got.PrincipalID != jane {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := repo.GetAdministrativeGrant(ctx, "agr_doesnotexist"); !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("missing grant: %v", err)
	}
	page, next, err := repo.ListAdministrativeGrants(ctx, GrantFilter{PrincipalID: other}, 1, "")
	if err != nil || len(page) != 1 || next == "" {
		t.Fatalf("first page: %d %q %v", len(page), next, err)
	}
	page2, next2, err := repo.ListAdministrativeGrants(ctx, GrantFilter{PrincipalID: other}, 1, next)
	if err != nil || len(page2) != 1 || next2 != "" || page2[0].GrantID == page[0].GrantID {
		t.Fatalf("second page: %+v %q %v", page2, next2, err)
	}
	if _, _, err := repo.ListAdministrativeGrants(ctx, GrantFilter{}, 10, "not-a-cursor"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("bad cursor: %v", err)
	}

	// Delegation from the source Jane holds.
	delegated, err := repo.DelegateAdministrativeGrant(ctx, AuditActor{ActorID: jane, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		"key-delegate-000001", "hash-d", issued.GrantID,
		func(source administration.Grant, sources map[string]administration.Grant) (administration.Grant, error) {
			return administration.PlanDelegation(catalogue, jane, source, sources, administration.DelegationRequest{
				PrincipalID: bob, Permission: "tenant.view", Scope: tenant, ValidUntil: now.Add(time.Hour), Reason: "cover"}, now, administration.Relations{})
		})
	if err != nil {
		t.Fatal(err)
	}
	if delegated.Source != administration.SourceDelegation || delegated.DelegatedFromGrantID != issued.GrantID {
		t.Fatalf("delegated %+v", delegated)
	}
	// Someone who does not hold the source cannot delegate it.
	_, err = repo.DelegateAdministrativeGrant(ctx, AuditActor{ActorID: other, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		"key-delegate-000002", "hash-e", issued.GrantID,
		func(source administration.Grant, sources map[string]administration.Grant) (administration.Grant, error) {
			return administration.PlanDelegation(catalogue, other, source, sources, administration.DelegationRequest{
				PrincipalID: bob, Permission: "tenant.view", Scope: tenant, ValidUntil: now.Add(time.Hour), Reason: "x"}, now, administration.Relations{})
		})
	var refusal *administration.Refusal
	if !errors.As(err, &refusal) || refusal.Code != administration.CodeDelegationInvalid {
		t.Fatalf("delegating someone else's grant: %v", err)
	}

	// Transitions: stale version, invalid command, then the lifecycle.
	if _, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-suspend-000001", digest("t1"), issued.GrantID,
		administration.CommandSuspend, "review", 7, now); !errors.Is(err, ErrGrantVersionMismatch) {
		t.Fatalf("stale version: %v", err)
	}
	if _, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-withdraw-00001", digest("t2"), issued.GrantID,
		administration.CommandWithdraw, "no", 1, now); !errors.As(err, &refusal) || refusal.Code != administration.CodeTransitionInvalid {
		t.Fatalf("withdrawing an active grant: %v", err)
	}
	suspended, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-suspend-000002", digest("t3"), issued.GrantID,
		administration.CommandSuspend, "review", 1, now)
	if err != nil || suspended.Status != administration.StatusSuspended || suspended.Version != 2 {
		t.Fatalf("suspend: %+v %v", suspended, err)
	}
	replay, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-suspend-000002", digest("t3"), issued.GrantID,
		administration.CommandSuspend, "review", 1, now)
	if err != nil || replay.Version != 2 {
		t.Fatalf("a replayed transition must not apply twice: %+v %v", replay, err)
	}
	resumed, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-resume-0000001", digest("t4"), issued.GrantID,
		administration.CommandResume, "cleared", 2, now)
	if err != nil || resumed.Status != administration.StatusActive || resumed.Version != 3 {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	revoked, err := repo.TransitionAdministrativeGrant(ctx, actor(), "key-revoke-0000001", digest("t5"), issued.GrantID,
		administration.CommandRevoke, "left the team", 3, now)
	if err != nil || revoked.Status != administration.StatusRevoked || revoked.RevokedBy != ops || revoked.RevocationReason != "left the team" {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	cascaded, err := repo.GetAdministrativeGrant(ctx, delegated.GrantID)
	if err != nil || cascaded.Status != administration.StatusRevoked || cascaded.RevokedBy != ops {
		t.Fatalf("revoking a source must revoke its delegations: %+v %v", cascaded, err)
	}
	admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.REVOKED' AND target = ANY($1)`,
		[]string{issued.GrantID, delegated.GrantID}).Scan(&count)
	if count != 2 {
		t.Fatalf("revocation and its cascade wrote %d audit events, want 2", count)
	}

	// The platform activates and expires grants itself.
	future := plan(bob, 0)
	future.ValidFrom, future.Status = now.Add(time.Hour), administration.StatusPending
	pending, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-pending01", digest("hp"), future)
	if err != nil {
		t.Fatal(err)
	}
	lapsing := plan(bob, 0)
	until := now.Add(2 * time.Hour)
	lapsing.GrantType, lapsing.ValidUntil = administration.TypeTimeBound, &until
	timed, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-issue-timed0001", digest("ht"), lapsing)
	if err != nil {
		t.Fatal(err)
	}
	if a, e, err := repo.SweepAdministrativeGrants(ctx, now.Add(10*time.Minute)); err != nil || a != 0 {
		t.Fatalf("early sweep activated %d (expired %d): %v", a, e, err)
	}
	if _, _, err := repo.SweepAdministrativeGrants(ctx, now.Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if p, _ := repo.GetAdministrativeGrant(ctx, pending.GrantID); p.Status != administration.StatusActive {
		t.Fatalf("the sweep must activate a PENDING grant whose start has come, got %s", p.Status)
	}
	if _, _, err := repo.SweepAdministrativeGrants(ctx, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e, _ := repo.GetAdministrativeGrant(ctx, timed.GrantID); e.Status != administration.StatusExpired {
		t.Fatalf("the sweep must expire a grant past its end, got %s", e.Status)
	}
}

// TestAdministrativeGrantReplacement covers migration 000091: a replacement
// creates the new grant, revokes the old one and ends its delegations in one
// transaction; both records remain and name each other; a failing plan
// leaves everything untouched; a replay does not apply twice; a stale
// version, a second replacement and a non-DIRECT grant are refused.
func TestAdministrativeGrantReplacement(t *testing.T) {
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

	tail := domain.NewUUIDv7()[24:]
	jane, bob, ops := "prn_jane"+tail, "prn_bob"+tail, "prn_ops"+tail
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant_command WHERE actor_id = $1`, ops)
		for i := 0; i < 3; i++ {
			admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND grant_id NOT IN
				(SELECT delegated_from FROM policy.administrative_grant WHERE delegated_from IS NOT NULL)
				AND grant_id NOT IN (SELECT supersedes_grant_id FROM policy.administrative_grant WHERE supersedes_grant_id IS NOT NULL)`, []string{jane, bob})
		}
		admin.Exec(ctx, `UPDATE policy.administrative_grant SET superseded_by_grant_id = NULL WHERE principal_id = ANY($1)`, []string{jane, bob})
		admin.Exec(ctx, `UPDATE policy.administrative_grant SET supersedes_grant_id = NULL WHERE principal_id = ANY($1)`, []string{jane, bob})
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND delegated_from IS NOT NULL`, []string{jane, bob})
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1)`, []string{jane, bob})
	})
	actor := func() AuditActor {
		return AuditActor{ActorID: ops, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	catalogue := administration.MustDefaultCatalogue()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant := administration.Scope{Level: administration.LevelTenant, TenantID: "tn_acmeug"}
	until := now.Add(48 * time.Hour)
	seed, err := administration.PlanIssue(catalogue, ops, administration.IssueRequest{PrincipalID: jane, Permission: "tenant.view", Scope: tenant,
		GrantType: administration.TypeStanding, DelegableDepth: 1, Reason: "seed"}, now)
	if err != nil {
		t.Fatal(err)
	}
	old, err := repo.IssueAdministrativeGrant(ctx, actor(), "key-seed-0000000001", digest("seed"), seed)
	if err != nil {
		t.Fatal(err)
	}
	// Jane delegates part of it to Bob.
	delegated, err := repo.DelegateAdministrativeGrant(ctx, AuditActor{ActorID: jane, ActorType: "human", CorrelationID: domain.NewUUIDv7()},
		"key-deleg-0000000001", digest("deleg"), old.GrantID, func(source administration.Grant, chain map[string]administration.Grant) (administration.Grant, error) {
			return administration.PlanDelegation(catalogue, jane, source, chain, administration.DelegationRequest{PrincipalID: bob, Permission: "tenant.view",
				Scope: tenant, ValidUntil: until, Reason: "cover"}, now, administration.Relations{})
		})
	if err != nil {
		t.Fatal(err)
	}

	narrower := administration.ReplaceRequest{Permission: "tenant.view", Scope: tenant, GrantType: administration.TypeTimeBound, ValidUntil: &until, Reason: "Time-box it."}
	planWith := func(q administration.ReplaceRequest) func(administration.Grant) (administration.Grant, error) {
		return func(o administration.Grant) (administration.Grant, error) {
			return administration.PlanReplacement(catalogue, ops, o, q, now, administration.Relations{})
		}
	}

	// A failing plan changes nothing; a stale version is refused.
	failing := func(administration.Grant) (administration.Grant, error) {
		return administration.Grant{}, &administration.Refusal{Code: administration.CodeApprovalRequired, Detail: "no"}
	}
	if _, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000001", digest("r1"), old.GrantID, 1, "x", now, failing); err == nil {
		t.Fatal("a refused plan replaced a grant")
	}
	if _, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000002", digest("r2"), old.GrantID, 9, "x", now, planWith(narrower)); !errors.Is(err, ErrGrantVersionMismatch) {
		t.Fatalf("a stale version: %v", err)
	}
	if got, _ := repo.GetAdministrativeGrant(ctx, old.GrantID); got.Status != administration.StatusActive || got.Version != 1 {
		t.Fatalf("a refused replacement changed the grant: %+v", got)
	}

	res, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000003", digest("r3"), old.GrantID, 1, "Time-box it.", now, planWith(narrower))
	if err != nil {
		t.Fatal(err)
	}
	if res.Replacement.SupersedesGrantID != old.GrantID || res.Replacement.Status != administration.StatusActive || res.Replacement.PrincipalID != jane ||
		res.Replacement.GrantType != administration.TypeTimeBound {
		t.Fatalf("unexpected replacement %+v", res.Replacement)
	}
	if res.Superseded.Status != administration.StatusRevoked || res.Superseded.SupersededByGrantID != res.Replacement.GrantID ||
		res.Superseded.RevokedBy != ops || res.Superseded.Version != 2 || res.Superseded.Permission != old.Permission {
		t.Fatalf("unexpected superseded grant %+v", res.Superseded)
	}
	// The old record's authority-bearing fields are untouched.
	if res.Superseded.Scope.TenantID != old.Scope.TenantID || res.Superseded.GrantType != old.GrantType || res.Superseded.ValidUntil != nil {
		t.Fatalf("the replaced grant was amended: %+v", res.Superseded)
	}
	if len(res.RevokedDelegations) != 1 || res.RevokedDelegations[0] != delegated.GrantID {
		t.Fatalf("revoked delegations = %v, want [%s]", res.RevokedDelegations, delegated.GrantID)
	}
	if d, _ := repo.GetAdministrativeGrant(ctx, delegated.GrantID); d.Status != administration.StatusRevoked {
		t.Fatalf("the delegation survived its source's replacement: %+v", d)
	}
	var audited int
	admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.superseded' AND target = $1`, old.GrantID).Scan(&audited)
	if audited != 1 {
		t.Fatalf("replacement wrote %d audit events", audited)
	}
	// No moment with two live grants of the same authority: the old one is revoked as the new commits.
	var live int
	admin.QueryRow(ctx, `SELECT count(*) FROM policy.administrative_grant WHERE principal_id = $1 AND permission = 'tenant.view'
		AND status IN ('ACTIVE', 'PENDING', 'SUSPENDED') AND delegated_from IS NULL`, jane).Scan(&live)
	if live != 1 {
		t.Fatalf("%d live grants after a replacement, want 1", live)
	}

	// Replay: the same key and request return the same result and do not apply again.
	again, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000003", digest("r3"), old.GrantID, 1, "Time-box it.", now, planWith(narrower))
	if err != nil || again.Replacement.GrantID != res.Replacement.GrantID || len(again.RevokedDelegations) != 1 {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000003", digest("other"), old.GrantID, 1, "x", now, planWith(narrower)); !errors.Is(err, ErrGrantCommandKeyReused) {
		t.Fatalf("a reused key: %v", err)
	}
	// A replaced grant is not replaced twice; neither is a delegation.
	var refusal *administration.Refusal
	if _, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000004", digest("r4"), old.GrantID, 2, "x", now, planWith(narrower)); !errors.As(err, &refusal) || refusal.Code != administration.CodeReplacementInvalid {
		t.Fatalf("replacing a replaced grant: %v", err)
	}
	if _, err := repo.ReplaceAdministrativeGrant(ctx, actor(), "key-rep-0000000005", digest("r5"), delegated.GrantID, 2, "x", now, planWith(narrower)); err == nil {
		t.Fatal("a revoked delegation was replaced")
	}
	// The database refuses a grant replaced by two.
	if _, err := admin.Exec(ctx, `UPDATE policy.administrative_grant SET superseded_by_grant_id = $2::uuid WHERE grant_id = $1::uuid`,
		mustRowID(delegated.GrantID), mustRowID(res.Replacement.GrantID)); err == nil {
		t.Fatal("two grants were marked as replaced by the same replacement")
	}
}

// TestEffectiveRelationsFollowTenantOrganisationMappings: EffectiveRelations
// returns only mappings that are ACTIVE and inside their window, by the same
// definition attestation uses (domain.TenantOrganisationMapping.InEffect),
// and an organisation grant's delegation across levels works end to end
// through a changeset while the mapping holds (ADR-BCP-018 section 50).
func TestEffectiveRelationsFollowTenantOrganisationMappings(t *testing.T) {
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
	tenant, legalEntity := "tn_rel"+suffix, "REL-TEST-"+strings.ToUpper(suffix)
	var orgs [5]string
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM registry.tenant_organisation_mapping WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenant)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntity)
		for _, o := range orgs {
			if o != "" {
				admin.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id = $1::uuid`, o)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntity); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO tenants(registration_basis, bootstrap_reason, bootstrap_evidence_reference, tenant_id,
		legal_entity_id, display_name, isolation_strategy, residency_region, desired_state, observed_state)
		VALUES ('BOOTSTRAP', 'Test fixture registered outside admission', 'test-fixture', $1, $2, 'Relations test', 'row_level_security',
		'af-south-1', 'active', 'active')`, tenant, legalEntity); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	past, future := now.Add(-48*time.Hour), now.Add(48*time.Hour)
	rows := []struct {
		name, status, role string
		from               time.Time
		to                 *time.Time
		effective          bool
	}{
		{"active and in window", "ACTIVE", "PRIMARY_ORGANISATION", past, nil, true},
		{"active until the future", "ACTIVE", "OPERATING_ORGANISATION", past, &future, true},
		{"ended by status", "ENDED", "ADDITIONAL_ORGANISATION", past, nil, false},
		{"active but expired", "ACTIVE", "ADDITIONAL_ORGANISATION", past.Add(-time.Hour), &past, false},
		{"not yet effective", "ACTIVE", "ADDITIONAL_ORGANISATION", future, nil, false},
	}
	want := map[string]bool{}
	for i, row := range rows {
		if err := admin.QueryRow(ctx, `INSERT INTO registry.canonical_entity(entity_type, status) VALUES ('ORGANISATION', 'active') RETURNING canonical_entity_id::text`).Scan(&orgs[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO registry.tenant_organisation_mapping(tenant_id, organisation_id, mapping_role, status, effective_from, effective_to, provenance)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, 'test')`, tenant, orgs[i], row.role, row.status, row.from, row.to); err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		want[orgs[i]] = row.effective
	}
	rel, err := repo.EffectiveRelations(ctx, []string{tenant, "tn_nobody"}, now)
	if err != nil {
		t.Fatal(err)
	}
	for org, effective := range want {
		if rel.Maps(tenant, org) != effective {
			t.Errorf("organisation %s: Maps = %v, want %v", org, rel.Maps(tenant, org), effective)
		}
	}
	if len(rel.OrganisationsOf("tn_nobody")) != 0 {
		t.Error("a tenant nobody maps has organisations")
	}
	if empty, err := repo.EffectiveRelations(ctx, nil, now); err != nil || len(empty.TenantOrganisations) != 0 {
		t.Errorf("no tenants asked for: %v %v", empty, err)
	}
	// Later, the first mapping ends: authority that rested on it stops.
	if later, _ := repo.EffectiveRelations(ctx, []string{tenant}, future.Add(time.Hour)); later.Maps(tenant, orgs[1]) {
		t.Error("a mapping reached past its effective_to")
	}

	// The organisation grant delegates a tenant slice through the repository transaction.
	catalogue := administration.MustDefaultCatalogue()
	jane, bob, ops := "prn_jane"+suffix, "prn_bob"+suffix, "prn_ops"+suffix
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant_command WHERE actor_id = ANY($1)`, []string{ops, jane})
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND delegated_from IS NOT NULL`, []string{jane, bob})
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1)`, []string{jane, bob})
	})
	orgScope := administration.Scope{Level: administration.LevelOrganisation, OrganisationID: orgs[0]}
	seed, err := administration.PlanIssue(catalogue, ops, administration.IssueRequest{PrincipalID: jane, Permission: "tenant.view", Scope: orgScope,
		GrantType: administration.TypeStanding, DelegableDepth: 1, Reason: "seed"}, now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := repo.IssueAdministrativeGrant(ctx, AuditActor{ActorID: ops, ActorType: "human", CorrelationID: domain.NewUUIDv7()}, "key-org-seed-00000001", digest("orgseed"), seed)
	if err != nil {
		t.Fatal(err)
	}
	delegate := func(rel administration.Relations, key string) (administration.Grant, error) {
		return repo.DelegateAdministrativeGrant(ctx, AuditActor{ActorID: jane, ActorType: "human", CorrelationID: domain.NewUUIDv7()}, key, digest(key), source.GrantID,
			func(src administration.Grant, chain map[string]administration.Grant) (administration.Grant, error) {
				return administration.PlanDelegation(catalogue, jane, src, chain, administration.DelegationRequest{PrincipalID: bob, Permission: "tenant.view",
					Scope: administration.Scope{Level: administration.LevelTenant, TenantID: tenant}, ValidUntil: now.Add(time.Hour), Reason: "Cover the tenant."}, now, rel)
			})
	}
	if _, err := delegate(administration.Relations{}, "key-org-deleg-0000001"); err == nil {
		t.Fatal("a tenant was delegated from an organisation with no mapping supplied")
	}
	got, err := delegate(rel, "key-org-deleg-0000002")
	if err != nil {
		t.Fatalf("delegating a mapped tenant: %v", err)
	}
	if got.Scope.Level != administration.LevelTenant || got.DelegatedFromGrantID != source.GrantID {
		t.Fatalf("unexpected delegation %+v", got)
	}
	// The grant reads back the same, and the evaluator honours it only while the mapping holds.
	grants, sources, err := repo.AdministrativeGrantsOf(ctx, bob)
	if err != nil || len(grants) != 1 {
		t.Fatalf("delegate grants: %d %v", len(grants), err)
	}
	if !administration.Usable(grants[0], sources, now.Add(time.Second), rel) || administration.Usable(grants[0], sources, now.Add(time.Second), administration.Relations{}) {
		t.Fatal("the delegation must be usable exactly while the mapping is effective")
	}
}
