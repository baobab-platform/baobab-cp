package postgres

import (
	"strings"
	"testing"
)

func TestLoadMigrationsReturnsCanonicalOrderedForwardMigrations(t *testing.T) {
	migrations, err := LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations failed: %v", err)
	}
	if len(migrations) != 111 {
		t.Fatalf("expected 111 canonical migrations, got %d", len(migrations))
	}
	for index, migration := range migrations {
		expectedVersion := index + 1
		if migration.Version != expectedVersion {
			t.Fatalf("migration %d has version %d", expectedVersion, migration.Version)
		}
		if !strings.HasPrefix(migration.Name, "000") || strings.HasSuffix(migration.Name, ".down.sql") || strings.HasSuffix(migration.Name, ".up.sql") {
			t.Fatalf("unexpected migration file selected: %s", migration.Name)
		}
		if len(migration.Checksum) != 64 {
			t.Fatalf("migration %s has invalid checksum %q", migration.Name, migration.Checksum)
		}
		if strings.TrimSpace(migration.SQL) == "" {
			t.Fatalf("migration %s is empty", migration.Name)
		}
	}
}

// A migration file that is embedded but not registered in canonicalMigrationNames is
// silently never applied (PEO-02/03 migrations 000107-000110 were added this way and
// every dependent integration test failed with "relation does not exist").
func TestEveryEmbeddedMigrationIsRegistered(t *testing.T) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	registered := make(map[string]bool, len(canonicalMigrationNames))
	for _, name := range canonicalMigrationNames {
		registered[name] = true
	}
	for _, entry := range entries {
		if !registered[entry.Name()] {
			t.Errorf("embedded migration %s is not in canonicalMigrationNames and would never be applied", entry.Name())
		}
	}
	if len(entries) != len(canonicalMigrationNames) {
		t.Errorf("embedded migration files (%d) and canonicalMigrationNames (%d) differ", len(entries), len(canonicalMigrationNames))
	}
}

func TestMigrationString(t *testing.T) {
	migration := Migration{Version: 7, Name: "000007_digital_estates.sql"}
	if got := migration.String(); got != "000007 000007_digital_estates.sql" {
		t.Fatalf("unexpected migration string: %q", got)
	}
}
