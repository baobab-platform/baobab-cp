// ADR-BCP-018 gate ORG-05 — keeping CorporateGroup membership current.

package organisation

import (
	"context"
	"log/slog"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// groupDerivationActor is the workload identity the audit trail attributes
// derived membership changes to. No person decides them: they are the
// deterministic consequence of corporate relationships already governed.
const groupDerivationActor = "workload:control-plane-corporate-group-derivation"

// GroupDerivationWorker keeps every derivable CorporateGroup's membership
// converging on the verified corporate graph (ADR-BCP-018 sections 26-28).
//
// It is derived-state maintenance, not an administrative power. Database
// triggers request a derivation in the same transaction as every corporate
// relationship or group change; the worker derives asynchronously, so a
// failure never rolls back the authoritative change and is retried with
// backoff. A scheduled sweep requests every group again, repairing drift the
// triggers cannot see, such as a relationship passing its effective_to.
//
// Group membership confers no access, tenancy, PlatformAccount membership
// or INTERNAL eligibility (section 29): eligibility reads the corporate and
// platform relationships themselves, so a stale or failed derivation cannot
// affect them.
type GroupDerivationWorker struct {
	Deriver *CorporateGroupDeriver
	Queue   repository.GroupDerivationRepository
	Log     *slog.Logger
	Now     func() time.Time
	// Lease is how long a claimed derivation is withheld from other workers.
	Lease time.Duration
	// Batch is the most derivations one pass claims.
	Batch int
	// MaxBackoff caps the delay between retries of a failing derivation.
	MaxBackoff time.Duration
}

// GroupDerivationPass is the outcome of one pass.
type GroupDerivationPass struct {
	Derived int
	Failed  int
	Added   int
	Ended   int
}

func (w *GroupDerivationWorker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now().UTC()
}

func (w *GroupDerivationWorker) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

// backoff is 30s doubled per failed attempt, capped at MaxBackoff (default
// one hour).
func (w *GroupDerivationWorker) backoff(attempts int) time.Duration {
	limit := w.MaxBackoff
	if limit <= 0 {
		limit = time.Hour
	}
	delay := 30 * time.Second
	for i := 0; i < attempts && delay < limit; i++ {
		delay *= 2
	}
	return min(delay, limit)
}

// RunOnce derives every group whose derivation is due. It fails only when
// the queue itself is unavailable; a derivation that fails is recorded for
// retry and the pass carries on.
func (w *GroupDerivationWorker) RunOnce(ctx context.Context) (GroupDerivationPass, error) {
	lease, batch := w.Lease, w.Batch
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	if batch <= 0 {
		batch = 20
	}
	var pass GroupDerivationPass
	claims, err := w.Queue.ClaimCorporateGroupDerivations(ctx, w.now(), lease, batch)
	if err != nil {
		return pass, err
	}
	for _, claim := range claims {
		actor := repository.AuditActor{ActorID: groupDerivationActor, ActorType: "workload", CorrelationID: domain.NewUUIDv7()}
		report, err := w.Deriver.Derive(ctx, claim.GroupID, actor)
		at := w.now()
		if err != nil {
			if ctx.Err() != nil {
				return pass, ctx.Err()
			}
			pass.Failed++
			next := at.Add(w.backoff(claim.Attempts))
			w.log().Warn("corporate group derivation failed; will retry", "corporate_group_id", claim.GroupID,
				"attempts", claim.Attempts+1, "next_attempt_at", next, "correlation_id", actor.CorrelationID, "error", err)
			if err := w.Queue.RecordCorporateGroupDerivationFailure(ctx, claim.GroupID, claim.RequestSeq, at, err.Error(), next); err != nil {
				return pass, err
			}
			continue
		}
		pass.Derived++
		pass.Added += len(report.Added)
		pass.Ended += len(report.Ended)
		if len(report.Added) > 0 || len(report.Ended) > 0 {
			w.log().Info("corporate group membership converged", "corporate_group_id", claim.GroupID,
				"added", len(report.Added), "ended", len(report.Ended), "correlation_id", actor.CorrelationID)
		}
		if err := w.Queue.RecordCorporateGroupDerived(ctx, claim.GroupID, claim.RequestSeq, at, len(report.Added), len(report.Ended)); err != nil {
			return pass, err
		}
	}
	return pass, nil
}

// Sweep requests a derivation of every derivable group.
func (w *GroupDerivationWorker) Sweep(ctx context.Context) error {
	marked, err := w.Queue.RequestCorporateGroupDerivations(ctx, "scheduled reconciliation")
	if err == nil {
		w.log().Debug("corporate group reconciliation scheduled", "groups", marked)
	}
	return err
}

// Run derives due groups every interval and sweeps every sweepEvery, until
// ctx ends. It sweeps once at start.
func (w *GroupDerivationWorker) Run(ctx context.Context, interval, sweepEvery time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastSweep time.Time
	for {
		if lastSweep.IsZero() || w.now().Sub(lastSweep) >= sweepEvery {
			if err := w.Sweep(ctx); err != nil && ctx.Err() == nil {
				w.log().Error("corporate group reconciliation sweep failed", "error", err)
			} else {
				lastSweep = w.now()
			}
		}
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.log().Error("corporate group derivation pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
