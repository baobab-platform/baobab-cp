package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresCanonicalEntityRecordsWhatWasRegistered: every field a
// registration carries is persisted and read back (migration 000057); a row
// registered before that migration reports those fields as absent instead of
// placeholders; an organisation's display name is its profile's; and a
// lifecycle change distinguishes a missing entity from a stale version.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresCanonicalEntityRecordsWhatWasRegistered(t *testing.T) {
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
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	registered, legacy, organisation := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM registry.organisation_profile WHERE canonical_entity_id = $1::uuid`, organisation)
		admin.Exec(ctx, `DELETE FROM registry.canonical_entity WHERE canonical_entity_id = ANY($1::uuid[])`, []string{registered, legacy, organisation})
	})

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	if err := repo.CreateCanonicalEntity(ctx, domain.CanonicalEntity{
		ID: registered, CanonicalKey: "supplier:thamani_global:sup_" + registered[:8], EntityType: domain.EntityTypeSupplierOrganisation,
		Subtype: "COOPERATIVE", DisplayName: "Thamani Global Supplies", OwnerTenantID: "tn_01k4thamani",
		Authority: "thamani", Classification: "TENANT_CONFIDENTIAL", Status: "DRAFT", SchemaVersion: 1,
		EffectiveFrom: from, EffectiveTo: &to,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetCanonicalEntity(ctx, registered)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Thamani Global Supplies" || got.Subtype != "COOPERATIVE" || got.Authority != "thamani" ||
		got.Classification != "TENANT_CONFIDENTIAL" || got.SchemaVersion != 1 || !got.EffectiveFrom.Equal(from) ||
		got.EffectiveTo == nil || !got.EffectiveTo.Equal(to) || got.Status != "DRAFT" || got.Version != 1 {
		t.Fatalf("registration not recorded faithfully: %+v", got)
	}

	if _, err := admin.Exec(ctx, `INSERT INTO registry.canonical_entity (canonical_entity_id, entity_type, external_key, status)
		VALUES ($1::uuid, 'PRODUCT', 'legacy:product', 'active')`, legacy); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetCanonicalEntity(ctx, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "" || got.Authority != "" || got.Classification != "" || got.SchemaVersion != 0 || !got.EffectiveFrom.IsZero() {
		t.Fatalf("a legacy row must report unrecorded fields as absent, not placeholders: %+v", got)
	}

	if _, err := admin.Exec(ctx, `INSERT INTO registry.canonical_entity (canonical_entity_id, entity_type, status)
		VALUES ($1::uuid, 'ORGANISATION', 'active')`, organisation); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO registry.organisation_profile (canonical_entity_id, display_name, verification_state, source_authority, status, effective_from)
		VALUES ($1::uuid, 'Zuri Beans Uganda', 'UNVERIFIED', 'test', 'ACTIVE', now())`, organisation); err != nil {
		t.Fatal(err)
	}
	if got, err = repo.GetCanonicalEntity(ctx, organisation); err != nil || got.DisplayName != "Zuri Beans Uganda" || got.CanonicalKey != "" {
		t.Fatalf("organisation display name comes from its profile: %+v %v", got, err)
	}

	missing := domain.NewUUIDv7()
	if _, err := repo.GetCanonicalEntity(ctx, missing); !errors.Is(err, ErrCanonicalEntityNotFound) {
		t.Fatalf("missing entity: %v", err)
	}
	if _, err := repo.GetCanonicalEntity(ctx, "not-a-uuid"); !errors.Is(err, ErrCanonicalEntityNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
	stale := domain.CanonicalEntity{ID: registered, CanonicalKey: "supplier:thamani_global:sup_" + registered[:8], EntityType: domain.EntityTypeSupplierOrganisation, Status: "VALIDATED"}
	if err := repo.SaveCanonicalEntity(ctx, stale, 5); !errors.Is(err, ErrCanonicalEntityVersionConflict) {
		t.Fatalf("stale version: %v", err)
	}
	if err := repo.SaveCanonicalEntity(ctx, domain.CanonicalEntity{ID: missing, EntityType: "PRODUCT", Status: "VALIDATED"}, 1); !errors.Is(err, ErrCanonicalEntityNotFound) {
		t.Fatalf("missing entity on save: %v", err)
	}
	if err := repo.SaveCanonicalEntity(ctx, stale, 1); err != nil {
		t.Fatalf("current version: %v", err)
	}
}
