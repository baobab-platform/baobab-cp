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

// TestPostgresLookupExternalIdentityReturnsStatus proves delegated Principal
// resolution's read (ADR-BCP-028) against PostgreSQL: a stopped external
// identity is returned with its status, never hidden as unknown, and only a
// missing (issuer, subject) is ErrIdentityNotFound.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresLookupExternalIdentityReturnsStatus(t *testing.T) {
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
	principal := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	issuer, subject := "https://iam.example.test/", "kratos-delegated-lookup-test"
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE issuer=$1 AND subject=$2`, issuer, subject)
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id=$1`, principal.ID)
	}
	cleanup()
	t.Cleanup(cleanup)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	if _, _, err := repo.LookupExternalIdentity(ctx, issuer, subject); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("unknown identity err = %v", err)
	}
	if err := repo.CreateIdentity(ctx, principal); err != nil {
		t.Fatal(err)
	}
	external := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: principal.ID, Issuer: issuer, Subject: subject, Status: "ACTIVE"}
	if err := repo.LinkExternalIdentity(ctx, external); err != nil {
		t.Fatal(err)
	}
	gotPrincipal, gotExternal, err := repo.LookupExternalIdentity(ctx, issuer, subject)
	if err != nil || gotPrincipal.ID != principal.ID || gotPrincipal.ActorType != "human" || gotExternal.ID != external.ID ||
		gotExternal.PrincipalID != principal.ID || gotExternal.Status != "ACTIVE" {
		t.Fatalf("lookup = %+v %+v %v", gotPrincipal, gotExternal, err)
	}
	if _, err := admin.Exec(ctx, `UPDATE identity.external_identity SET status='REVOKED' WHERE external_identity_id=$1`, external.ID); err != nil {
		t.Fatal(err)
	}
	if _, gotExternal, err = repo.LookupExternalIdentity(ctx, issuer, subject); err != nil || gotExternal.Status != "REVOKED" {
		t.Fatalf("revoked lookup = %+v %v", gotExternal, err)
	}
	if _, err := repo.ResolveIdentity(ctx, issuer, subject); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("ResolveIdentity still resolves a revoked identity: %v", err)
	}
}
