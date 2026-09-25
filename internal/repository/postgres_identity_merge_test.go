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

// TestPostgresMergePrincipalsAuditedTransfersAndArchives is Gate IAM-3
// phase 6's (docs/governance/gate-iam-3-canonical-identity-scope.md)
// regression test for ADR-0004 §19-21's identity merge, proven against a
// real PostgreSQL 17 instance: every external_identity and
// identity_reference row belonging to the source Principal transfers to
// the target, the source is archived (never deleted, still resolvable via
// GetPrincipal), and two linked audit_events rows are written -- one per
// Principal, sharing a correlation ID.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresMergePrincipalsAuditedTransfersAndArchives(t *testing.T) {
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

	sourceID := domain.NewPrincipalID()
	targetID := domain.NewPrincipalID()
	sourceIssuer, sourceSubject := "https://accounts.google.com", "google-sub-merge-test"
	targetIssuer, targetSubject := "https://iam.nabhold.com/realms/baobab", "keycloak-sub-merge-test"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM identity.identity_reference WHERE principal_id=ANY($1::uuid[])`, []string{sourceID, targetID})
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE principal_id=ANY($1::uuid[])`, []string{sourceID, targetID})
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id=ANY($1::uuid[])`, []string{sourceID, targetID})
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	source := domain.Principal{ID: sourceID, ActorType: "human", Status: "ACTIVE"}
	target := domain.Principal{ID: targetID, ActorType: "human", Status: "ACTIVE"}
	if err := repo.CreateIdentity(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := repo.CreateIdentity(ctx, target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	sourceExternal := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: sourceID, Issuer: sourceIssuer, Subject: sourceSubject, ProviderType: "google", Status: "ACTIVE"}
	if err := repo.LinkExternalIdentity(ctx, sourceExternal); err != nil {
		t.Fatalf("link source external identity: %v", err)
	}
	targetExternal := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: targetID, Issuer: targetIssuer, Subject: targetSubject, ProviderType: "keycloak", Status: "ACTIVE"}
	if err := repo.LinkExternalIdentity(ctx, targetExternal); err != nil {
		t.Fatalf("link target external identity: %v", err)
	}
	sourceReference := domain.IdentityReference{ID: domain.NewIdentityReferenceID(), PrincipalID: sourceID, Engine: "baobab-trade", ExternalType: "customer", ExternalID: "C-merge-test", Status: "ACTIVE"}
	if err := repo.CreateIdentityReference(ctx, sourceReference); err != nil {
		t.Fatalf("create source identity reference: %v", err)
	}

	actor := AuditActor{ActorID: "admin-merge-test", ActorType: "human", CorrelationID: "44444444-4444-4444-8444-444444444444"}
	if err := repo.MergePrincipalsAudited(ctx, sourceID, targetID, actor, "duplicate accounts for the same person"); err != nil {
		t.Fatalf("merge principals audited: %v", err)
	}

	archivedSource, err := repo.GetPrincipal(ctx, sourceID)
	if err != nil {
		t.Fatalf("expected the archived source to remain resolvable via GetPrincipal: %v", err)
	}
	if archivedSource.Status != "ARCHIVED" {
		t.Fatalf("expected source status ARCHIVED, got %q", archivedSource.Status)
	}

	resolved, err := repo.ResolveIdentity(ctx, sourceIssuer, sourceSubject)
	if err != nil || resolved.ID != targetID {
		t.Fatalf("expected the transferred external identity to resolve to the target, got principal=%+v err=%v", resolved, err)
	}
	resolved, err = repo.ResolveIdentity(ctx, targetIssuer, targetSubject)
	if err != nil || resolved.ID != targetID {
		t.Fatalf("expected the target's own pre-existing external identity to still resolve to the target, got principal=%+v err=%v", resolved, err)
	}

	transferredRef, err := repo.ResolveIdentityReference(ctx, sourceReference.Engine, sourceReference.ExternalType, sourceReference.ExternalID)
	if err != nil || transferredRef.PrincipalID != targetID {
		t.Fatalf("expected the transferred identity reference to now belong to the target, got %+v err=%v", transferredRef, err)
	}

	for _, id := range []string{sourceID, targetID} {
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target=$1 AND action='identity.principal.merged'`, "principal:"+id).Scan(&count); err != nil {
			t.Fatalf("count merge audit events for %s: %v", id, err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 audit_events row targeting principal:%s, got %d", id, count)
		}
	}

	// Merging again with the now-archived source is rejected.
	if err := repo.MergePrincipalsAudited(ctx, sourceID, targetID, actor, "second attempt"); !errors.Is(err, ErrMergeNotEligible) {
		t.Fatalf("expected ErrMergeNotEligible for a re-merge of an archived source, got %v", err)
	}
}
