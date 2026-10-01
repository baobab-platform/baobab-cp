package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
)

// ShadowObservation is a number of identical shadow comparisons made on one
// UTC day: the role decision, the grant decision and how they compare for one
// permission. It carries no principal, tenant or request content.
type ShadowObservation struct {
	Day        time.Time
	Permission string
	Legacy     string
	Grants     string
	Agreement  string
	Decisions  int64
	FirstAt    time.Time
	LastAt     time.Time
}

// ShadowEvidenceRepository keeps the roles-versus-grants comparison beyond a
// process restart (ADR-BCP-020 section 144), as evidence for the owner's
// decision to move a permission from roles to grants. It authorises nothing.
type ShadowEvidenceRepository interface {
	// RecordShadowObservations adds counts to the day's aggregates.
	RecordShadowObservations(ctx context.Context, observations []ShadowObservation) error
	// ShadowEvidence sums the comparison per permission over every day kept.
	ShadowEvidence(ctx context.Context) ([]administration.PermissionEvidence, error)
}

var _ ShadowEvidenceRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) RecordShadowObservations(ctx context.Context, observations []ShadowObservation) error {
	if r == nil || r.pool == nil {
		return errors.New("postgres repository is not configured")
	}
	if len(observations) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin shadow evidence: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, o := range observations {
		if o.Decisions <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO policy.administrative_shadow_daily (day, permission, legacy, grants, agreement, decisions, first_observed_at, last_observed_at)
			VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (day, permission, legacy, grants, agreement) DO UPDATE SET
				decisions = policy.administrative_shadow_daily.decisions + EXCLUDED.decisions,
				first_observed_at = LEAST(policy.administrative_shadow_daily.first_observed_at, EXCLUDED.first_observed_at),
				last_observed_at = GREATEST(policy.administrative_shadow_daily.last_observed_at, EXCLUDED.last_observed_at)`,
			o.Day.UTC(), o.Permission, o.Legacy, o.Grants, o.Agreement, o.Decisions, o.FirstAt.UTC(), o.LastAt.UTC()); err != nil {
			return fmt.Errorf("record shadow evidence: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) ShadowEvidence(ctx context.Context) ([]administration.PermissionEvidence, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("postgres repository is not configured")
	}
	rows, err := r.pool.Query(ctx, `
		SELECT permission,
		       sum(decisions)::bigint,
		       COALESCE(sum(decisions) FILTER (WHERE agreement = 'agree'), 0)::bigint,
		       COALESCE(sum(decisions) FILTER (WHERE agreement = 'grants_broader'), 0)::bigint,
		       COALESCE(sum(decisions) FILTER (WHERE agreement = 'grants_narrower'), 0)::bigint,
		       COALESCE(sum(decisions) FILTER (WHERE agreement = 'not_evaluated'), 0)::bigint,
		       COALESCE(sum(decisions) FILTER (WHERE grants IN ('unresolved', 'error')), 0)::bigint,
		       count(DISTINCT day)::int,
		       min(first_observed_at), max(last_observed_at)
		FROM policy.administrative_shadow_daily
		GROUP BY permission
		ORDER BY permission`)
	if err != nil {
		return nil, fmt.Errorf("read shadow evidence: %w", err)
	}
	defer rows.Close()
	var out []administration.PermissionEvidence
	for rows.Next() {
		var ev administration.PermissionEvidence
		var first, last time.Time
		if err := rows.Scan(&ev.Permission, &ev.Decisions, &ev.Agree, &ev.GrantsBroader, &ev.GrantsNarrower, &ev.NotEvaluated,
			&ev.UnresolvedOrError, &ev.ObservedDays, &first, &last); err != nil {
			return nil, fmt.Errorf("scan shadow evidence: %w", err)
		}
		ev.FirstObservedAt, ev.LastObservedAt = &first, &last
		out = append(out, ev)
	}
	return out, rows.Err()
}
