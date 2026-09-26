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

// TestPostgresIdentityReferenceMapsAndResolves is Gate IAM-3 phase 5's
// (docs/governance/gate-iam-3-canonical-identity-scope.md) regression test
// for ADR-0004 §23-29's engine-reference mapping and §29's
// UNIQUE(engine, external_type, external_id) collision-avoidance invariant,
// proven against a real PostgreSQL 17 instance rather than the in-memory
// repository alone.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresIdentityReferenceMapsAndResolves(t *testing.T) {
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
	otherPrincipalID := domain.NewPrincipalID()

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.identity_reference WHERE engine='baobab-trade' AND external_type='customer' AND external_id='C-100'`)
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id = ANY($1)`, []string{principalID, otherPrincipalID})
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	if _, err := repo.ResolveIdentityReference(ctx, "baobab-trade", "customer", "C-100"); !errors.Is(err, ErrIdentityReferenceNotFound) {
		t.Fatalf("expected ErrIdentityReferenceNotFound before mapping, got %v", err)
	}

	principal := domain.Principal{ID: principalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, principal); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	reference := domain.IdentityReference{
		ID: domain.NewIdentityReferenceID(), PrincipalID: principal.ID,
		Engine: "baobab-trade", EngineInstanceID: "ei_afsouth1production",
		ExternalType: "customer", ExternalID: "C-100", Status: "ACTIVE",
	}
	if err := repo.CreateIdentityReference(ctx, reference); err != nil {
		t.Fatalf("create identity reference: %v", err)
	}

	// ADR-0004 §29: a given engine-native actor resolves to at most one
	// Principal -- a second principal must not be mappable to this exact
	// (engine, external_type, external_id) tuple.
	other := domain.Principal{ID: otherPrincipalID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, other); err != nil {
		t.Fatalf("create second identity: %v", err)
	}
	conflicting := domain.IdentityReference{
		ID: domain.NewIdentityReferenceID(), PrincipalID: other.ID,
		Engine: "baobab-trade", ExternalType: "customer", ExternalID: "C-100", Status: "ACTIVE",
	}
	if err := repo.CreateIdentityReference(ctx, conflicting); !errors.Is(err, ErrIdentityReferenceAlreadyMapped) {
		t.Fatalf("expected ErrIdentityReferenceAlreadyMapped for a duplicate engine-native actor, got %v", err)
	}

	resolved, err := repo.ResolveIdentityReference(ctx, "baobab-trade", "customer", "C-100")
	if err != nil {
		t.Fatalf("resolve identity reference: %v", err)
	}
	if resolved.PrincipalID != principal.ID {
		t.Fatalf("expected resolved principal %s, got %s", principal.ID, resolved.PrincipalID)
	}
	if resolved.EngineInstanceID != "ei_afsouth1production" {
		t.Fatalf("expected engine_instance_id to round-trip, got %q", resolved.EngineInstanceID)
	}
	if resolved.CreatedAt.IsZero() {
		t.Fatal("expected created_at to round-trip from the database default, got zero value")
	}

	references, err := repo.ListIdentityReferences(ctx, principal.ID)
	if err != nil {
		t.Fatalf("list identity references: %v", err)
	}
	if len(references) != 1 || references[0].ExternalID != "C-100" {
		t.Fatalf("unexpected identity references for principal %s: %+v", principal.ID, references)
	}
}
