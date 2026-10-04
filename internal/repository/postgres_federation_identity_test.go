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

func TestPostgresFederationIdentityReadsBothSidesOfExactMapping(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	issuerA := "https://mp2c-" + domain.NewUUIDv7() + ".example.test"
	issuerB := "urn:mp2c:" + domain.NewUUIDv7()
	subject := "same-stable-subject"
	ids := []string{domain.NewPrincipalID(), domain.NewPrincipalID()}
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE issuer=ANY($1)`, []string{issuerA, issuerB})
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id=ANY($1)`, ids)
	})
	externals := []domain.ExternalIdentity{}
	for i, issuer := range []string{issuerA, issuerB} {
		p := domain.Principal{ID: ids[i], ActorType: "human", Status: "ACTIVE"}
		if err := repo.CreateIdentity(ctx, p); err != nil {
			t.Fatal(err)
		}
		e := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: issuer, Subject: subject, Status: "ACTIVE"}
		if err := repo.LinkExternalIdentity(ctx, e); err != nil {
			t.Fatal(err)
		}
		externals = append(externals, e)
	}
	for _, want := range externals {
		got, err := repo.ReadFederationIdentity(ctx, want.Issuer, subject)
		if err != nil {
			t.Fatal(err)
		}
		if got.ExternalIdentity.ID != want.ID || got.Principal.ID != want.PrincipalID || got.ExternalIdentity.PrincipalID != got.Principal.ID || got.ExternalIdentity.Subject != subject || got.ExternalIdentity.Issuer != want.Issuer || got.Principal.CreatedAt.IsZero() || got.ExternalIdentity.CreatedAt.IsZero() {
			t.Fatal("exact identity mapping did not round-trip")
		}
	}
	if _, err := repo.ReadFederationIdentity(ctx, issuerA, "unknown"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatal("unknown mapping not classified as absent")
	}
	// The repository preserves lifecycle evidence; service denies nonactive rows.
	if _, err := admin.Exec(ctx, `UPDATE identity.external_identity SET status='REVOKED' WHERE external_identity_id=$1::uuid`, externals[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ReadFederationIdentity(ctx, issuerA, subject)
	if err != nil || got.ExternalIdentity.Status != "REVOKED" {
		t.Fatal("revocation evidence lost")
	}
}
