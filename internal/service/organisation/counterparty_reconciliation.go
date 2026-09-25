// ADR-BCP-018 gate ORG-13 — buyer/supplier reconciliation (sections 11,
// 99-101, 112).

package organisation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ReconciliationSource marks resolution candidates found by a reconciliation run.
const ReconciliationSource = "organisation-reconciliation"

// defaultReconciliationBatch bounds each backfill transaction.
const defaultReconciliationBatch = 500

// CounterpartyReconciler runs ADR-BCP-018 section 112's migration: every
// ADR-BCP-016 buyer and supplier record gains an organisation profile and a
// tenant-scoped counterparty role (phases 1-2), then Organisations sharing a
// governed identifier are quarantined for review (phase 3). It changes no
// canonical id or entity type and merges nothing, and it is safe to rerun.
type CounterpartyReconciler struct {
	Repo repository.CounterpartyRepository
	// BatchSize bounds each backfill transaction; zero means 500.
	BatchSize int
	Now       func() time.Time
}

// ReconciliationReport summarises one run.
type ReconciliationReport struct {
	ProfilesAttached  int                           `json:"profiles_attached"`
	RolesMaterialised int                           `json:"roles_materialised"`
	Backlog           map[string]int                `json:"backlog"`
	Candidates        repository.CandidateDetection `json:"candidates"`
}

// Run migrates every pending legacy record, reports what could not be
// migrated and why, and refreshes the resolution candidates.
func (c *CounterpartyReconciler) Run(ctx context.Context, actor repository.AuditActor) (ReconciliationReport, error) {
	if c.Repo == nil {
		return ReconciliationReport{}, errors.New("counterparty reconciliation requires a repository")
	}
	batch := c.BatchSize
	if batch <= 0 {
		batch = defaultReconciliationBatch
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	var report ReconciliationReport
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		b, err := c.Repo.BackfillLegacyOrganisations(ctx, batch, actor)
		if err != nil {
			return report, fmt.Errorf("backfill legacy organisations: %w", err)
		}
		report.ProfilesAttached += len(b.ProfilesAttached)
		report.RolesMaterialised += len(b.RolesMaterialised)
		// A batch smaller than the limit on both phases means the backlog is
		// drained; a full batch may have more behind it.
		if len(b.ProfilesAttached) < batch && len(b.RolesMaterialised) < batch {
			break
		}
	}
	backlog, err := c.Repo.LegacyBackfillBacklog(ctx)
	if err != nil {
		return report, fmt.Errorf("legacy backfill backlog: %w", err)
	}
	report.Backlog = backlog
	if report.Candidates, err = c.Repo.DetectResolutionCandidates(ctx, now().UTC(), ReconciliationSource, actor); err != nil {
		return report, fmt.Errorf("detect resolution candidates: %w", err)
	}
	return report, nil
}
