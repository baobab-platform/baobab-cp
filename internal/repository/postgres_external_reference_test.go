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

// TestPostgresExternalReferenceRoundTrip proves CreateExternalReference and
// GetCanonicalEntityByExternalReference against a real PostgreSQL instance
// (registry.external_reference, migration 000010 -- ADR-BCP-016's
// Keycloak-Organization-to-CanonicalEntity onboarding link).
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresExternalReferenceRoundTrip(t *testing.T) {
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

	const entityID = "60000000-0000-0000-0000-0000000000e1"
	const refID = "60000000-0000-0000-0000-0000000000e2"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM registry.external_reference WHERE external_reference_id = $1::uuid`, refID)
		admin.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id = $1::uuid`, entityID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	if err := repo.CreateCanonicalEntity(ctx, domain.CanonicalEntity{
		ID: entityID, CanonicalKey: "buyer:extreftest:org-1", EntityType: domain.EntityTypeBuyerOrganisation,
		DisplayName: "External Reference Test Organisation", Authority: "baobab", Classification: "INTERNAL", Status: "ACTIVE",
	}); err != nil {
		t.Fatalf("seed canonical entity: %v", err)
	}

	ref := domain.ExternalReference{
		ID: refID, CanonicalEntityID: entityID, EngineID: "baobab-iam",
		NativeType: "keycloak_organization", NativeID: "kc-org-extreftest",
	}
	created, err := repo.CreateExternalReference(ctx, ref)
	if err != nil {
		t.Fatalf("create external reference: %v", err)
	}
	if created.ID != refID {
		t.Fatalf("expected the minted id to be echoed back, got %q", created.ID)
	}

	if _, err := repo.CreateExternalReference(ctx, ref); !errors.Is(err, ErrExternalReferenceAlreadyLinked) {
		t.Fatalf("expected ErrExternalReferenceAlreadyLinked for a duplicate link, got %v", err)
	}

	resolved, err := repo.GetCanonicalEntityByExternalReference(ctx, "baobab-iam", "keycloak_organization", "kc-org-extreftest")
	if err != nil {
		t.Fatalf("get canonical entity by external reference: %v", err)
	}
	if resolved.ID != entityID || resolved.EntityType != domain.EntityTypeBuyerOrganisation {
		t.Fatalf("unexpected resolved canonical entity: %+v", resolved)
	}

	if _, err := repo.GetCanonicalEntityByExternalReference(ctx, "baobab-iam", "keycloak_organization", "does-not-exist"); err == nil {
		t.Fatal("expected an unlinked native id to fail")
	}

	unknownEntityRef := domain.ExternalReference{
		ID: domain.NewUUIDv7(), CanonicalEntityID: domain.NewUUIDv7(), EngineID: "baobab-iam",
		NativeType: "keycloak_organization", NativeID: "kc-org-orphan",
	}
	if _, err := repo.CreateExternalReference(ctx, unknownEntityRef); !errors.Is(err, ErrCanonicalEntityNotFound) {
		t.Fatalf("expected ErrCanonicalEntityNotFound for a reference to a nonexistent canonical entity, got %v", err)
	}
}
