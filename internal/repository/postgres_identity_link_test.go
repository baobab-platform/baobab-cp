package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// TestPostgresLinkExternalIdentityAuditedWritesAtomically is Gate IAM-3
// phase 6's (docs/governance/gate-iam-3-canonical-identity-scope.md)
// regression test for ADR-0004 §16 ("Every successful link SHALL be
// auditable") and §15-17's linking flow, proven against a real PostgreSQL
// 17 instance: a successful link writes both the identity.external_identity
// row and an audit_events row in the same transaction, and a failed link
// (duplicate issuer/subject) writes neither.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresLinkExternalIdentityAuditedWritesAtomically(t *testing.T) {
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
	issuer, subject := "https://accounts.google.com", "google-sub-audited-test"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE issuer=$1 AND subject=$2`, issuer, subject)
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
	fetched, err := repo.GetPrincipal(ctx, principalID)
	if err != nil {
		t.Fatalf("get principal: %v", err)
	}
	if fetched.ID != principalID {
		t.Fatalf("expected fetched principal %s, got %s", principalID, fetched.ID)
	}

	external := domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principalID,
		Issuer: issuer, Subject: subject, ProviderType: "google", Status: "ACTIVE",
	}
	actor := AuditActor{ActorID: principalID, ActorType: "human", CorrelationID: "22222222-2222-4222-8222-222222222222"}
	if err := repo.LinkExternalIdentityAudited(ctx, external, actor, "linked a second login method"); err != nil {
		t.Fatalf("link external identity audited: %v", err)
	}

	resolved, err := repo.ResolveIdentity(ctx, issuer, subject)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if resolved.ID != principalID {
		t.Fatalf("expected resolved principal %s, got %s", principalID, resolved.ID)
	}

	var auditCount int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target=$1 AND action='identity.external_identity.linked'`, "principal:"+principalID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected exactly 1 audit_events row for the link, got %d", auditCount)
	}

	// A failed link (duplicate issuer/subject) must roll back entirely --
	// no second audit_events row for the failed attempt.
	if err := repo.LinkExternalIdentityAudited(ctx, external, actor, "duplicate attempt"); !errors.Is(err, ErrExternalIdentityAlreadyLinked) {
		t.Fatalf("expected ErrExternalIdentityAlreadyLinked, got %v", err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target=$1 AND action='identity.external_identity.linked'`, "principal:"+principalID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit events after failed link: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected the failed link to leave audit_events unchanged at 1 row, got %d", auditCount)
	}
}
