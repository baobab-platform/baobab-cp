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

// TestPostgresRegisterBuyerIdentityIsAtomicUnderConcurrency proves ADR-BCP-028
// section 2.3 against PostgreSQL: concurrent first registrations of one
// (issuer, subject) create exactly one Principal, leave no orphan Principal
// behind, all return the same Principal, write one audit record, and never
// revive a stopped identity.
func TestPostgresRegisterBuyerIdentityIsAtomicUnderConcurrency(t *testing.T) {
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

	issuer, subject := "https://iam.example.test/", "kratos-concurrent-registration-test"
	const attempts = 8
	ids := make([]string, attempts)
	for i := range ids {
		ids[i] = domain.NewPrincipalID()
	}
	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM audit_events WHERE action='identity.principal.buyer_registered' AND target = ANY($1)`, targets(ids))
		admin.Exec(ctx, `DELETE FROM identity.external_identity WHERE issuer=$1 AND subject=$2`, issuer, subject)
		admin.Exec(ctx, `DELETE FROM identity.principal WHERE principal_id = ANY($1::uuid[])`, ids)
	}
	cleanup()
	t.Cleanup(cleanup)

	type result struct {
		principal string
		created   bool
		err       error
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		go func(id string) {
			<-start
			p, _, created, err := repo.RegisterBuyerIdentity(ctx,
				domain.Principal{ID: id, ActorType: "human", Status: "ACTIVE"},
				domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: id, Issuer: issuer, Subject: subject, Status: "ACTIVE"},
				AuditActor{ActorID: "baobab-trade-workload", ActorType: "workload", ClientID: "baobab-trade-workload", CorrelationID: domain.NewUUIDv7()})
			results <- result{p.ID, created, err}
		}(ids[i])
	}
	close(start)
	winners, principals := 0, map[string]bool{}
	for i := 0; i < attempts; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("registration failed: %v", r.err)
		}
		if r.created {
			winners++
		}
		principals[r.principal] = true
	}
	if winners != 1 || len(principals) != 1 {
		t.Fatalf("winners = %d, distinct principals = %d; want 1 and 1", winners, len(principals))
	}
	var rows, orphans, audits int
	mustScan(t, admin.QueryRow(ctx, `SELECT count(*) FROM identity.external_identity WHERE issuer=$1 AND subject=$2`, issuer, subject), &rows)
	mustScan(t, admin.QueryRow(ctx, `SELECT count(*) FROM identity.principal p WHERE p.principal_id = ANY($1::uuid[])
		AND NOT EXISTS (SELECT 1 FROM identity.external_identity e WHERE e.principal_id = p.principal_id)`, ids), &orphans)
	mustScan(t, admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='identity.principal.buyer_registered' AND target = ANY($1)`, targets(ids)), &audits)
	if rows != 1 || orphans != 0 || audits != 1 {
		t.Fatalf("external identities = %d, orphan principals = %d, audit records = %d; want 1, 0, 1", rows, orphans, audits)
	}

	// A stopped identity is returned as is, never re-created or revived.
	if _, err := admin.Exec(ctx, `UPDATE identity.external_identity SET status='DISABLED' WHERE issuer=$1 AND subject=$2`, issuer, subject); err != nil {
		t.Fatal(err)
	}
	retry := domain.NewPrincipalID()
	ids = append(ids, retry)
	_, external, created, err := repo.RegisterBuyerIdentity(ctx,
		domain.Principal{ID: retry, ActorType: "human", Status: "ACTIVE"},
		domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: retry, Issuer: issuer, Subject: subject, Status: "ACTIVE"},
		AuditActor{ActorID: "baobab-trade-workload", ActorType: "workload"})
	if err != nil || created || external.Status != "DISABLED" {
		t.Fatalf("re-registration of a stopped identity = %+v created=%v err=%v", external, created, err)
	}
}

func targets(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = "principal:" + id
	}
	return out
}

func mustScan(t *testing.T, row interface{ Scan(...any) error }, into *int) {
	t.Helper()
	if err := row.Scan(into); err != nil {
		t.Fatal(err)
	}
}
