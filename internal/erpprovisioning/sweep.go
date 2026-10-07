package erpprovisioning

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// SweepPolicy bounds the recovery sweep. Events are the primary path, so the sweep reads ERP only for an operation that is open
// (not terminal) and has been quiet for Grace, backs off between reads of the same operation, and gives up on one that is older
// than MaxAge: ERP's sender stops retrying after its retry horizon, and an operation older than that needs an operator, not a loop.
type SweepPolicy struct {
	// Grace is how long an open operation may go without a recorded state before the sweep reads it.
	Grace time.Duration
	// BackoffBase doubles with every read of the same operation that found nothing newer, up to BackoffMax.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// MaxAge is the age of a submission beyond which the sweep stops reading it.
	MaxAge time.Duration
	// Batch is the most operations read in one pass.
	Batch int
}

// DefaultSweepPolicy: grace 5 minutes, back-off 1 minute doubling to 1 hour, give up after the 72 hour sender retry horizon of the
// signed delivery contract, 20 operations per pass.
func DefaultSweepPolicy() SweepPolicy {
	return SweepPolicy{Grace: 5 * time.Minute, BackoffBase: time.Minute, BackoffMax: time.Hour, MaxAge: 72 * time.Hour, Batch: 20}
}

// Backoff is the wait after the nth recovery read of one operation (n >= 1).
func (p SweepPolicy) Backoff(n int) time.Duration {
	d := p.BackoffBase
	for i := 1; i < n && d < p.BackoffMax; i++ {
		d *= 2
	}
	return min(d, p.BackoffMax)
}

// Claimer hands out overdue open operations. Claiming is the lease: an operation claimed is not offered again until its back-off
// has passed, so instances sharing a database never read the same operation at once.
type Claimer interface {
	// ClaimOverdue returns one overdue operation id and records the claim, or false when none is due.
	ClaimOverdue(ctx context.Context, now time.Time, p SweepPolicy) (operationID string, attempt int, found bool, err error)
}

// Reconciler reads one operation from ERP and records it.
type Reconciler interface {
	Reconcile(ctx context.Context, operationID string) (State, bool, error)
}

// Sweeper is the bounded recovery path for missed provisioning.changed events.
type Sweeper struct {
	Claims Claimer
	Worker Reconciler
	Policy SweepPolicy
	Now    func() time.Time
}

// SweepResult counts one pass.
type SweepResult struct{ Read, Advanced, Failed int }

// RunOnce reads up to Policy.Batch overdue operations. A failed read is logged and left to its back-off; it never stops the pass.
func (s *Sweeper) RunOnce(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	for i := 0; i < s.Policy.Batch; i++ {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		op, attempt, found, err := s.Claims.ClaimOverdue(ctx, now().UTC(), s.Policy)
		if err != nil {
			return res, err
		}
		if !found {
			return res, nil
		}
		res.Read++
		st, advanced, err := s.Worker.Reconcile(ctx, op)
		switch {
		case err != nil:
			res.Failed++
			// A state that disagrees with the submission is an integrity problem, not a transient one: it is loud, and the
			// back-off still applies, so it is not hammered.
			level := slog.LevelWarn
			if errors.Is(err, ErrStateDisagrees) {
				level = slog.LevelError
			}
			slog.Log(ctx, level, "erp provisioning recovery read failed", "operation_id", op, "attempt", attempt, "error", err)
		case advanced:
			res.Advanced++
			slog.InfoContext(ctx, "erp provisioning recovered a missed state", "operation_id", op, "state", st.State, "revision", st.Revision, "attempt", attempt)
		}
	}
	return res, nil
}

// Run sweeps every interval until ctx ends.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
				slog.Error("erp provisioning recovery sweep incomplete", "error", err)
			}
		}
	}
}

// ClaimOverdue implements Claimer over provisioning.erp_submission (migration 000100). Due means: not terminal, younger than
// MaxAge, quiet for Grace, and not claimed within the back-off of its previous reads. SKIP LOCKED lets instances take different rows.
func (l PostgresLedger) ClaimOverdue(ctx context.Context, now time.Time, p SweepPolicy) (string, int, bool, error) {
	var op string
	var attempts int
	err := l.DB.QueryRow(ctx, `
		UPDATE provisioning.erp_submission s
		   SET sweep_attempts = s.sweep_attempts + 1, last_swept_at = $1::timestamptz
		 WHERE s.operation_id = (
		       SELECT operation_id FROM provisioning.erp_submission
		        WHERE last_state NOT IN ('active', 'failed', 'cancelled')
		          AND submitted_at > $1::timestamptz - make_interval(secs => $2::float8)
		          AND updated_at <= $1::timestamptz - make_interval(secs => $3::float8)
		          AND (last_swept_at IS NULL OR last_swept_at <= $1::timestamptz - make_interval(secs =>
		                LEAST($5::float8, $4::float8 * power(2, GREATEST(LEAST(sweep_attempts, 30), 1) - 1))))
		        ORDER BY updated_at
		        LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING s.operation_id::text, s.sweep_attempts`,
		now, p.MaxAge.Seconds(), p.Grace.Seconds(), p.BackoffBase.Seconds(), p.BackoffMax.Seconds()).Scan(&op, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, false, nil
	}
	return op, attempts, err == nil, err
}
