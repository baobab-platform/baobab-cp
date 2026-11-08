package repository

import (
    "context"
    "errors"
    "os"
    "testing"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/store/postgres"
    "github.com/jackc/pgx/v5/pgxpool"
)

// TestPeopleProviderChoiceDefaultDenialPG17 is real PostgreSQL evidence for
// the no-default policy: no chosen provider may be invented from the
// available ERP engine, and choice isolation is enforced at the database.
func TestPeopleProviderChoiceDefaultDenialPG17(t *testing.T) {
    url := os.Getenv("TEST_DATABASE_URL")
    if url == "" {
        t.Skip("requires real PostgreSQL 17 TEST_DATABASE_URL")
    }
    ctx := context.Background()
    store, err := postgres.Open(ctx, url)
    if err != nil { t.Fatal(err) }
    t.Cleanup(store.Close)
    if err := store.ApplyMigrations(ctx); err != nil { t.Fatal(err) }

    connection, err := pgxpool.New(ctx, url)
    if err != nil { t.Fatal(err) }
    t.Cleanup(connection.Close)
    var count int
    err = connection.QueryRow(ctx, `
       SELECT count(*)
       FROM pg_constraint
       WHERE conname = 'people_choice_one_active_source'
         AND contype = 'x'
         AND conrelid = 'capability.people_provider_choice'::regclass
    `).Scan(&count)
    if err != nil || count != 1 {
        t.Fatalf("one authoritative provider exclusion missing: count=%d error=%v", count, err)
    }
    repo, err := Open(ctx, url)
    if err != nil { t.Fatal(err) }
    t.Cleanup(repo.Close)
    result, err := repo.ReadPeopleChoice(ctx, "tn_unregisteredpeople01", "TEST-EMPLOYER",
        "ZA", "payroll.run.read", time.Now().UTC())
    if !errors.Is(err, ErrPeopleChoiceUnavailable) {
        t.Fatalf("unregistered provider must deny, got %+v %v", result, err)
    }
}
