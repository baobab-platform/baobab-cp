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

// TestPostgresIdentityResolvesAndProvisions is Gate IAM-3 phase 2's
// (docs/governance/gate-iam-3-canonical-identity-scope.md) regression test
// for ADR-0004 §11's identity resolution flow and §7/§54's UNIQUE(issuer,
// subject) invariant, proven against a real PostgreSQL 17 instance rather
// than the in-memory repository alone.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresIdentityResolvesAndProvisions(t *testing.T) {
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

	issuer, subject := "https://iam.nabhold.com/realms/baobab", "40000000-0000-0000-0000-000000000001"
	principalID := domain.NewPrincipalID()
	otherPrincipalID := domain.NewPrincipalID()

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE issuer=$1 AND subject=$2`, issuer, subject)
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id = ANY($1)`, []string{principalID, otherPrincipalID})
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	if _, err := repo.ResolveIdentity(ctx, issuer, subject); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("expected ErrIdentityNotFound before provisioning, got %v", err)
	}

	principal := domain.Principal{ID: principalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, principal); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	external := domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID,
		Issuer: issuer, Subject: subject, ProviderType: "keycloak", Status: "ACTIVE",
	}
	if err := repo.LinkExternalIdentity(ctx, external); err != nil {
		t.Fatalf("link external identity: %v", err)
	}

	// ADR-0004 §7/§54: a given (issuer, subject) resolves to at most one
	// Canonical Identity -- a second principal must not be linkable to this
	// exact (issuer, subject) pair.
	other := domain.Principal{ID: otherPrincipalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, other); err != nil {
		t.Fatalf("create second identity: %v", err)
	}
	conflicting := domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: other.ID,
		Issuer: issuer, Subject: subject, Status: "ACTIVE",
	}
	if err := repo.LinkExternalIdentity(ctx, conflicting); !errors.Is(err, ErrExternalIdentityAlreadyLinked) {
		t.Fatalf("expected ErrExternalIdentityAlreadyLinked for a duplicate (issuer, subject), got %v", err)
	}

	resolved, err := repo.ResolveIdentity(ctx, issuer, subject)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if resolved.ID != principal.ID {
		t.Fatalf("expected resolved principal %s, got %s", principal.ID, resolved.ID)
	}
	if resolved.ActorType != "human" || resolved.Status != "ACTIVE" {
		t.Fatalf("unexpected resolved principal: %+v", resolved)
	}
	if resolved.CreatedAt.IsZero() || resolved.UpdatedAt.IsZero() {
		t.Fatal("expected created_at/updated_at to round-trip from the database defaults, got zero values")
	}
}
