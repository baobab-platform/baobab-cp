// Command reconcile-first-party reconciles the Control Plane's first-party
// Organisation and LegalEntityProfile records, and their tenants' primary
// organisation mappings, with baobab-platform/shared's legal-entity registry
// (ADR-BCP-018 section 13, gates ORG-03 and ORG-12).
//
// Usage:
//
//	DATABASE_URL=... reconcile-first-party -registry /baobab/contracts/legal-entity/registry.yaml
//
// The registry path defaults to the location inside the published contracts
// image. The command prints a JSON report and exits 0 when every entity
// reconciled, 2 when blocking drift needs a governed decision, and 1 on error.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	svcorg "github.com/nabhold/baobab-cp/internal/service/organisation"
)

// reconcilerActor is the workload identity the audit trail attributes
// reconciliation changes to.
const reconcilerActor = "workload:control-plane-first-party-reconciler"

func main() {
	registryPath := flag.String("registry", "/baobab/contracts/legal-entity/registry.yaml", "path to Shared's contracts/legal-entity/registry.yaml")
	flag.Parse()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	registry, err := svcorg.LoadFirstPartyRegistry(*registryPath)
	if err != nil {
		slog.Error("load first-party registry", "path", *registryPath, "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	repo, err := repository.Open(ctx, databaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	actor := repository.AuditActor{ActorID: reconcilerActor, ActorType: "workload", CorrelationID: domain.NewUUIDv7()}
	report, err := (&svcorg.FirstPartyReconciler{Orgs: repo}).Reconcile(ctx, registry, actor)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(report); encodeErr != nil {
		slog.Error("write report", "error", encodeErr)
	}
	if err != nil {
		slog.Error("first-party reconciliation failed", "correlation_id", actor.CorrelationID, "error", err)
		os.Exit(1)
	}
	if report.Blocking {
		slog.Warn("first-party reconciliation found blocking drift", "correlation_id", actor.CorrelationID)
		os.Exit(2)
	}
	slog.Info("first-party reconciliation complete", "correlation_id", actor.CorrelationID, "registry_digest", report.RegistryDigest)
}
