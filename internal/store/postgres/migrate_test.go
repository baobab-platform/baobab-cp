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
	if len(migrations) != 58 {
		t.Fatalf("expected 58 canonical migrations, got %d", len(migrations))
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

func TestMigrationString(t *testing.T) {
	migration := Migration{Version: 7, Name: "000007_digital_estates.sql"}
	if got := migration.String(); got != "000007 000007_digital_estates.sql" {
		t.Fatalf("unexpected migration string: %q", got)
	}
}
