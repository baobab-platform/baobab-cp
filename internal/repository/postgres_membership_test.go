package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresWorkforceMembershipLifecycle is Gate IAM-5 phase 3's
// (docs/governance/gate-iam-5-workforce-sso-scope.md) regression test for
// ADR-0009 §27's WorkforceMembership relationship and its UNIQUE(principal_id,
// tenant_id) invariant (§31-32: a mover changes an existing membership row
// rather than accumulating a second one for the same tenant), proven against
// a real PostgreSQL 17 instance rather than the in-memory repository alone.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresWorkforceMembershipLifecycle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	// tenantID/legalEntityID are derived from a fresh UUID rather than a
	// fixed literal: this test's admin pool is closed by its own defer
	// before t.Cleanup's registered cleanup runs (the same ordering already
	// present in TestPostgresIdentityResolvesAndProvisions), so a leaked
	// cleanup silently leaves rows behind -- a fixed tenant/legal-entity id
	// would then collide with itself on the next run, exactly as a fixed
	// (issuer, subject) pair would for that test. Randomizing sidesteps the
	// collision the same way that test's random principal IDs already do.
	suffix := strings.ReplaceAll(domain.NewWorkforceMembershipID(), "-", "")
	tenantID := "tn_" + suffix
	legalEntityID := "GATE-IAM-5-WFM-TEST-" + suffix
	principalID := domain.NewPrincipalID()

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.workforce_membership WHERE principal_id = $1::uuid`, principalID)
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id = $1::uuid`, principalID)
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = $1`, tenantID)
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntityID); err != nil {
		t.Fatalf("fixture: create legal entity: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO tenants(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region) VALUES ($1, $2, 'Gate IAM-5 Workforce Membership Test', 'row_level_security', 'af-south-1')`, tenantID, legalEntityID); err != nil {
		t.Fatalf("fixture: create tenant: %v", err)
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	if _, err := repo.GetWorkforceMembership(ctx, principalID, tenantID); !errors.Is(err, ErrWorkforceMembershipNotFound) {
		t.Fatalf("expected ErrWorkforceMembershipNotFound before creation, got %v", err)
	}

	principal := domain.Principal{ID: principalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, principal); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	membership := domain.WorkforceMembership{
		ID: domain.NewWorkforceMembershipID(), PrincipalID: principalID, TenantID: tenantID,
		Roles: []string{"cp:tenant-admin"}, Status: "ACTIVE",
	}
	if err := repo.CreateWorkforceMembership(ctx, membership); err != nil {
		t.Fatalf("create workforce membership: %v", err)
	}

	// ADR-0009 §31-32: a second membership for the same (principal, tenant)
	// pair must be rejected by the database's own UNIQUE constraint, not
	// merely by application logic -- this is the privilege-accumulation
	// failure mode a mover process must never produce.
	duplicate := domain.WorkforceMembership{
		ID: domain.NewWorkforceMembershipID(), PrincipalID: principalID, TenantID: tenantID,
		Roles: []string{"cp:platform-admin"}, Status: "ACTIVE",
	}
	if err := repo.CreateWorkforceMembership(ctx, duplicate); err == nil {
		t.Fatal("expected the UNIQUE(principal_id, tenant_id) constraint to reject a second membership for the same tenant")
	}

	resolved, err := repo.GetWorkforceMembership(ctx, principalID, tenantID)
	if err != nil {
		t.Fatalf("get workforce membership: %v", err)
	}
	if resolved.ID != membership.ID || resolved.Status != "ACTIVE" || !resolved.HasRole("cp:tenant-admin") {
		t.Fatalf("unexpected resolved membership: %+v", resolved)
	}

	memberships, err := repo.ListWorkforceMemberships(ctx, principalID)
	if err != nil {
		t.Fatalf("list workforce memberships: %v", err)
	}
	if len(memberships) != 1 || memberships[0].TenantID != tenantID {
		t.Fatalf("unexpected memberships: %+v", memberships)
	}

	if err := repo.SetWorkforceMembershipStatus(ctx, membership.ID, "SUSPENDED"); err != nil {
		t.Fatalf("set workforce membership status: %v", err)
	}
	suspended, err := repo.GetWorkforceMembership(ctx, principalID, tenantID)
	if err != nil {
		t.Fatalf("get workforce membership after suspend: %v", err)
	}
	if suspended.Status != "SUSPENDED" || !suspended.HasRole("cp:tenant-admin") {
		t.Fatalf("expected a status-only transition preserving roles, got %+v", suspended)
	}

	if err := repo.SetWorkforceMembershipStatus(ctx, domain.NewWorkforceMembershipID(), "ACTIVE"); !errors.Is(err, ErrWorkforceMembershipNotFound) {
		t.Fatalf("expected ErrWorkforceMembershipNotFound for an unknown membership id, got %v", err)
	}

	// A mover's actual privilege change (§31): SetWorkforceMembershipRoles
	// replaces roles on the existing row -- there is no valid second
	// CreateWorkforceMembership for this same (principal, tenant) pair, as
	// the UNIQUE constraint check above already proved.
	if err := repo.SetWorkforceMembershipRoles(ctx, membership.ID, []string{"cp:platform-admin"}); err != nil {
		t.Fatalf("set workforce membership roles: %v", err)
	}
	moved, err := repo.GetWorkforceMembership(ctx, principalID, tenantID)
	if err != nil {
		t.Fatalf("get workforce membership after role change: %v", err)
	}
	if moved.Status != "SUSPENDED" || !moved.HasRole("cp:platform-admin") || moved.HasRole("cp:tenant-admin") {
		t.Fatalf("expected roles replaced in place with status untouched, got %+v", moved)
	}
	if err := repo.SetWorkforceMembershipRoles(ctx, domain.NewWorkforceMembershipID(), []string{"cp:platform-admin"}); !errors.Is(err, ErrWorkforceMembershipNotFound) {
		t.Fatalf("expected ErrWorkforceMembershipNotFound for an unknown membership id, got %v", err)
	}
}

// TestPostgresMergeTransfersAndReconcilesWorkforceMemberships is Gate IAM-5
// phase 3's regression test for a review finding on baobab-platform/baobab-cp#104:
// MergePrincipalsAudited transferred external identities and identity
// references but not workforce memberships, stranding an administrator's
// tenant access on the archived source principal once its (issuer, subject)
// resolved to the target instead. Proven against a real PostgreSQL instance
// since the fix lives in the same transaction as the SQL-level UNIQUE
// constraint it must not violate: a non-conflicting membership transfers to
// the target, and a membership whose tenant the target already belongs to
// is disabled in place rather than transferred.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresMergeTransfersAndReconcilesWorkforceMemberships(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	suffix := strings.ReplaceAll(domain.NewWorkforceMembershipID(), "-", "")
	sharedTenantID := "tn_shared" + suffix
	onlySourceTenantID := "tn_source" + suffix
	legalEntityID := "GATE-IAM-5-WFM-MERGE-TEST-" + suffix
	sourceID := domain.NewPrincipalID()
	targetID := domain.NewPrincipalID()

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.workforce_membership WHERE principal_id = ANY($1::uuid[])`, []string{sourceID, targetID})
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id = ANY($1::uuid[])`, []string{sourceID, targetID})
		admin.Exec(ctx, `DELETE FROM tenants WHERE tenant_id = ANY($1)`, []string{sharedTenantID, onlySourceTenantID})
		admin.Exec(ctx, `DELETE FROM legal_entities WHERE legal_entity_id = $1`, legalEntityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := admin.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, legalEntityID); err != nil {
		t.Fatalf("fixture: create legal entity: %v", err)
	}
	for _, tenantID := range []string{sharedTenantID, onlySourceTenantID} {
		if _, err := admin.Exec(ctx, `INSERT INTO tenants(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region) VALUES ($1, $2, 'Gate IAM-5 Merge Test', 'row_level_security', 'af-south-1')`, tenantID, legalEntityID); err != nil {
			t.Fatalf("fixture: create tenant %s: %v", tenantID, err)
		}
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	source := domain.Principal{ID: sourceID, ActorType: "human", Status: "ACTIVE"}
	target := domain.Principal{ID: targetID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, source); err != nil {
		t.Fatalf("create source identity: %v", err)
	}
	if err := repo.CreateIdentity(ctx, target); err != nil {
		t.Fatalf("create target identity: %v", err)
	}

	nonConflicting := domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: sourceID, TenantID: onlySourceTenantID, Roles: []string{"cp:tenant-admin"}, Status: "ACTIVE"}
	if err := repo.CreateWorkforceMembership(ctx, nonConflicting); err != nil {
		t.Fatalf("create non-conflicting membership: %v", err)
	}
	conflictingSource := domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: sourceID, TenantID: sharedTenantID, Roles: []string{"cp:platform-admin"}, Status: "ACTIVE"}
	if err := repo.CreateWorkforceMembership(ctx, conflictingSource); err != nil {
		t.Fatalf("create conflicting source membership: %v", err)
	}
	conflictingTarget := domain.WorkforceMembership{ID: domain.NewWorkforceMembershipID(), PrincipalID: targetID, TenantID: sharedTenantID, Roles: []string{"cp:tenant-admin"}, Status: "ACTIVE"}
	if err := repo.CreateWorkforceMembership(ctx, conflictingTarget); err != nil {
		t.Fatalf("create conflicting target membership: %v", err)
	}

	if err := repo.MergePrincipalsAudited(ctx, sourceID, targetID, AuditActor{ActorID: "admin-1"}, "duplicate account"); err != nil {
		t.Fatalf("merge principals: %v", err)
	}

	transferred, err := repo.GetWorkforceMembership(ctx, targetID, onlySourceTenantID)
	if err != nil {
		t.Fatalf("expected the non-conflicting membership to resolve under the target, got %v", err)
	}
	if transferred.Status != "ACTIVE" || !transferred.HasRole("cp:tenant-admin") {
		t.Fatalf("unexpected transferred membership: %+v", transferred)
	}
	if _, err := repo.GetWorkforceMembership(ctx, sourceID, onlySourceTenantID); !errors.Is(err, ErrWorkforceMembershipNotFound) {
		t.Fatalf("expected the transferred membership to no longer resolve under the archived source, got %v", err)
	}

	authoritative, err := repo.GetWorkforceMembership(ctx, targetID, sharedTenantID)
	if err != nil {
		t.Fatalf("expected the target's own membership to still resolve, got %v", err)
	}
	if authoritative.ID != conflictingTarget.ID || authoritative.Status != "ACTIVE" || !authoritative.HasRole("cp:tenant-admin") {
		t.Fatalf("expected the target's pre-existing membership to remain authoritative and unchanged, got %+v", authoritative)
	}

	memberships, err := repo.ListWorkforceMemberships(ctx, sourceID)
	if err != nil {
		t.Fatalf("list source memberships: %v", err)
	}
	if len(memberships) != 1 || memberships[0].ID != conflictingSource.ID || memberships[0].Status != "DISABLED" {
		t.Fatalf("expected the conflicting source membership to remain, disabled, on the archived source, got %+v", memberships)
	}
}
