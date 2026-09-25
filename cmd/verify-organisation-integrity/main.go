// Command verify-organisation-integrity checks the organisation model's
// cross-table invariants after a migration, restore or disaster recovery
// (ADR-BCP-018 sections 171 and 176, gate ORG-16).
//
// Usage:
//
//	DATABASE_URL=... verify-organisation-integrity [-limit 100]
//
// It only reads, in one snapshot. It prints a JSON report and exits 0 when
// every invariant holds, 2 when any is broken, and 1 on error. Violations
// are repaired by governed review, never automatically; see
// docs/runbooks/adr-bcp-018-organisation-operations.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/nabhold/baobab-cp/internal/repository"
)

func main() {
	limit := flag.Int("limit", 100, "maximum violations listed per check (counts are always complete)")
	flag.Parse()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	repo, err := repository.Open(ctx, databaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	report, err := repo.VerifyOrganisationIntegrity(ctx, *limit)
	if err != nil {
		slog.Error("organisation integrity verification failed", "error", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		slog.Error("write report", "error", err)
		os.Exit(1)
	}
	if !report.OK() {
		slog.Warn("organisation integrity violations found", "by_check", report.ByCheck)
		os.Exit(2)
	}
	slog.Info("organisation integrity verified")
}
