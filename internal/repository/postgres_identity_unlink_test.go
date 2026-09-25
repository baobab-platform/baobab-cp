package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresUnlinkExternalIdentityAuditedEnforcesLastCredentialGuard is
// Gate IAM-3 phase 6's (docs/governance/gate-iam-3-canonical-identity-scope.md)
// regression test for ADR-0004 §18's unlinking flow and §52's audit
// requirement, proven against a real PostgreSQL 17 instance: unlinking one
// of two ACTIVE credentials marks it UNLINKED and writes exactly one
// audit_events row; unlinking a Principal's last remaining credential is
// denied (and leaves it ACTIVE, with no audit row) unless administrative.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresUnlinkExternalIdentityAuditedEnforcesLastCredentialGuard(t *testing.T) {
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

	principalID := domain.NewPrincipalID()
	primaryIssuer, primarySubject := "https://iam.nabhold.com/realms/baobab", "keycloak-sub-unlink-test"
	secondaryIssuer, secondarySubject := "https://accounts.google.com", "google-sub-unlink-test"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE principal_id=$1::uuid`, principalID)
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id=$1`, principalID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	principal := domain.Principal{ID: principalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, principal); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	primary := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: principalID, Issuer: primaryIssuer, Subject: primarySubject, ProviderType: "keycloak", Status: "ACTIVE"}
	secondary := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: principalID, Issuer: secondaryIssuer, Subject: secondarySubject, ProviderType: "google", Status: "ACTIVE"}
	if err := repo.LinkExternalIdentity(ctx, primary); err != nil {
		t.Fatalf("link primary: %v", err)
	}
	if err := repo.LinkExternalIdentity(ctx, secondary); err != nil {
		t.Fatalf("link secondary: %v", err)
	}

	actor := AuditActor{ActorID: principalID, ActorType: "human", CorrelationID: "33333333-3333-4333-8333-333333333333"}

	// Unlinking the secondary credential while the primary remains ACTIVE
	// must succeed and be audited exactly once.
	if err := repo.UnlinkExternalIdentityAudited(ctx, principalID, secondaryIssuer, secondarySubject, false, actor, "user removed a login method"); err != nil {
		t.Fatalf("unlink secondary: %v", err)
	}
	if _, err := repo.ResolveIdentity(ctx, secondaryIssuer, secondarySubject); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("expected the unlinked identity to no longer resolve, got %v", err)
	}
	var unlinkAuditCount int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target=$1 AND action='identity.external_identity.unlinked'`, "principal:"+principalID).Scan(&unlinkAuditCount); err != nil {
		t.Fatalf("count unlink audit events: %v", err)
	}
	if unlinkAuditCount != 1 {
		t.Fatalf("expected exactly 1 audit_events row for the unlink, got %d", unlinkAuditCount)
	}

	// Now only the primary credential is ACTIVE -- a non-administrative
	// unlink of it must be denied, leave it ACTIVE, and write no audit row.
	if err := repo.UnlinkExternalIdentityAudited(ctx, principalID, primaryIssuer, primarySubject, false, actor, "attempted self-unlink"); !errors.Is(err, ErrLastCredentialDenied) {
		t.Fatalf("expected ErrLastCredentialDenied, got %v", err)
	}
	resolved, err := repo.ResolveIdentity(ctx, primaryIssuer, primarySubject)
	if err != nil || resolved.ID != principalID {
		t.Fatalf("expected the last credential to remain linked, got principal=%+v err=%v", resolved, err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target=$1 AND action='identity.external_identity.unlinked'`, "principal:"+principalID).Scan(&unlinkAuditCount); err != nil {
		t.Fatalf("count unlink audit events after denied unlink: %v", err)
	}
	if unlinkAuditCount != 1 {
		t.Fatalf("expected the denied unlink to leave audit_events unchanged at 1 row, got %d", unlinkAuditCount)
	}

	// Administrative unlink of the last credential succeeds.
	if err := repo.UnlinkExternalIdentityAudited(ctx, principalID, primaryIssuer, primarySubject, true, actor, "administrative account disablement"); err != nil {
		t.Fatalf("expected administrative unlink of the last credential to succeed, got %v", err)
	}
	if _, err := repo.ResolveIdentity(ctx, primaryIssuer, primarySubject); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("expected the administratively unlinked identity to no longer resolve, got %v", err)
	}
}
