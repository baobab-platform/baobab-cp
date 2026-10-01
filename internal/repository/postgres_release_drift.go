// ADR-BCP-025 gate ER-05: release drift. The Control Plane compares an
// engine instance's desired release with its observed release on every
// accepted observation and on a periodic sweep (section 2.7). A condition is
// drift once it has held for its grace period (release-policy.yaml drift);
// opening drift, or changing its reason, publishes one event. Drift is
// reported, never acted on: it does not change capability resolution
// (section 2.8, amendment A4).

package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// EventReleaseDriftDetected is the section 2.10 event published when an
// engine instance's release drift opens or changes reason.
const (
	EventReleaseDriftDetected  = "com.baobab-platform.control-plane.engine-instance.release-drift-detected.v1"
	schemaReleaseDriftDetected = "https://contracts.baobab-platform.com/topology/v1/events.schema.json#/$defs/EngineInstanceReleaseDriftDetected"
)

// ReleaseDrift is one engine instance's open release drift.
type ReleaseDrift struct {
	EngineInstanceID string
	Reason           string
	Severity         string
	DetectedAt       time.Time
	ObservedState    string
	ObservedRelease  string
	DesiredRelease   string
}

// ReleaseDriftRepository evaluates and reads release drift.
type ReleaseDriftRepository interface {
	// EvaluateReleaseDrift compares one instance's desired and observed
	// release at now and records the outcome, publishing the drift event
	// when drift opens or changes reason. An instance that converged has no
	// drift.
	EvaluateReleaseDrift(ctx context.Context, engineInstanceID string, now time.Time) error
	// SweepReleaseDrift evaluates every instance that desires a release, has
	// been observed or carries a drift condition, and returns how many it
	// evaluated. One instance failing does not stop the rest.
	SweepReleaseDrift(ctx context.Context, now time.Time) (int, error)
	// ListReleaseDrift is the open drift of the instances a tenant's active
	// capability bindings name, for its provisioning drift view.
	ListReleaseDrift(ctx context.Context, tenantID string) ([]ReleaseDrift, error)
}

var _ ReleaseDriftRepository = (*PostgresRepository)(nil)

// ReleaseDriftSweepInterval is release-policy.yaml drift.sweep_interval_seconds.
func ReleaseDriftSweepInterval() (time.Duration, error) {
	doc, err := loadReleasePolicy()
	return time.Duration(doc.Drift.SweepIntervalSeconds) * time.Second, err
}

func releaseDriftPolicy() (release.DriftPolicy, error) {
	doc, err := loadReleasePolicy()
	if err != nil {
		return nil, err
	}
	policy := release.DriftPolicy{}
	for reason, rule := range doc.Drift.Reasons {
		policy[reason] = release.DriftRule{Grace: time.Duration(rule.GraceSeconds) * time.Second, Severity: rule.Severity, ReadinessEffect: rule.ReadinessEffect}
	}
	return policy, nil
}

func (r *PostgresRepository) EvaluateReleaseDrift(ctx context.Context, engineInstanceID string, now time.Time) error {
	if !domain.ValidEngineInstanceID(engineInstanceID) {
		return ErrEngineInstanceNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // committed below
	if err := r.evaluateReleaseDriftTx(ctx, tx, engineInstanceID, now.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) evaluateReleaseDriftTx(ctx context.Context, tx pgx.Tx, key string, now time.Time) error {
	policy, err := releaseDriftPolicy()
	if err != nil {
		return err
	}
	// One evaluation of an instance at a time: an observation and the sweep
	// may race, and each must see the other's row.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "release-drift:"+key); err != nil {
		return err
	}
	var (
		in            release.DriftInput
		engineCode    string
		desiredRelKey string
	)
	err = tx.QueryRow(ctx, `SELECT COALESCE(ei.environment, ''), ei.region, e.code, COALESCE(dr.release_key, ''),
			COALESCE(ei.desired_release_updated_at, ei.created_at)
		FROM topology.engine_instance ei JOIN topology.engine e ON e.engine_id = ei.engine_id
		LEFT JOIN topology.engine_release dr ON dr.engine_release_id = ei.desired_release_id
		WHERE ei.engine_instance_key = $1`, key).Scan(&in.Environment, &in.Region, &engineCode, &desiredRelKey, &in.DesiredSince)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEngineInstanceNotFound
	}
	if err != nil {
		return fmt.Errorf("read engine instance for drift: %w", err)
	}
	in.DesiredReleaseID = desiredRelKey
	in.DesiredSince = in.DesiredSince.UTC()
	observed, current, err := observedReleaseOf(ctx, tx, key, now)
	if err != nil {
		return err
	}
	in.Observed, in.Current = observed, current
	var observedKeys []string
	switch observed.State {
	case release.StateRelease:
		observedKeys = []string{observed.ReleaseID}
	case release.StateMixed:
		observedKeys = observed.ReleaseIDs
	}
	if len(observedKeys) > 0 {
		rows, err := tx.Query(ctx, `SELECT release_key FROM topology.engine_release WHERE release_key = ANY($1) AND status = 'REVOKED'`, observedKeys)
		if err != nil {
			return fmt.Errorf("read observed release status: %w", err)
		}
		if in.Revoked, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
	}

	var (
		stored         bool
		storedReason   string
		storedSince    time.Time
		storedOpenedAt *time.Time
		announced      *string
	)
	err = tx.QueryRow(ctx, `SELECT reason_code, condition_since, opened_at, announced_reason
		FROM topology.engine_instance_release_drift WHERE engine_instance_key = $1`, key).Scan(&storedReason, &storedSince, &storedOpenedAt, &announced)
	switch {
	case err == nil:
		stored = true
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read release drift: %w", err)
	}

	condition, drifting := release.ReleaseDriftCondition(in)
	if !drifting {
		if stored {
			if _, err := tx.Exec(ctx, `DELETE FROM topology.engine_instance_release_drift WHERE engine_instance_key = $1`, key); err != nil {
				return fmt.Errorf("clear release drift: %w", err)
			}
		}
		return nil
	}
	rule, known := policy[condition.Reason]
	if !known {
		return fmt.Errorf("release-policy.yaml has no drift rule for %s", condition.Reason)
	}

	since := now
	if stored && storedReason == condition.Reason {
		since = storedSince
	}
	opens := release.DriftOpen(rule, since, condition.NotBefore, now)
	var openedAt *time.Time
	if opens {
		at := since
		if condition.NotBefore.After(at) {
			at = condition.NotBefore
		}
		at = at.Add(rule.Grace)
		if at.After(now) {
			at = now
		}
		if stored && storedReason == condition.Reason && storedOpenedAt != nil {
			at = *storedOpenedAt
		}
		openedAt = &at
	}
	// What an event last announced stands only while the drift stays open
	// with that reason: closing, or becoming another reason that is not yet
	// open, forgets it, so reopening announces again.
	var announceAs *string
	if openedAt != nil && announced != nil && *announced == condition.Reason {
		announceAs = announced
	}
	publish := openedAt != nil && announceAs == nil
	if publish {
		reason := condition.Reason
		announceAs = &reason
	}

	observedRelease := ""
	if observed.State == release.StateRelease {
		observedRelease = observed.ReleaseID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO topology.engine_instance_release_drift
			(engine_instance_key, reason_code, condition_since, opened_at, announced_reason, observed_state,
			 observed_release_key, observation_key, desired_release_key, evaluated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10)
		ON CONFLICT (engine_instance_key) DO UPDATE SET reason_code = EXCLUDED.reason_code,
			condition_since = EXCLUDED.condition_since, opened_at = EXCLUDED.opened_at,
			announced_reason = EXCLUDED.announced_reason, observed_state = EXCLUDED.observed_state,
			observed_release_key = EXCLUDED.observed_release_key, observation_key = EXCLUDED.observation_key,
			desired_release_key = EXCLUDED.desired_release_key, evaluated_at = EXCLUDED.evaluated_at`,
		key, condition.Reason, since, openedAt, announceAs, observed.State, observedRelease, observed.ObservationID, desiredRelKey, now); err != nil {
		return fmt.Errorf("record release drift: %w", err)
	}
	if !publish {
		return nil
	}
	data := map[string]any{"engine_instance_id": key, "engine_id": engineCode, "drift_reason": condition.Reason,
		"desired_release_id": nil, "observed_state": observed.State, "detected_at": openedAt.UTC().Format(time.RFC3339Nano)}
	if desiredRelKey != "" {
		data["desired_release_id"] = desiredRelKey
	}
	if observedRelease != "" {
		data["observed_release_id"] = observedRelease
	}
	if observed.ObservationID != "" {
		data["observation_id"] = observed.ObservationID
	}
	env, err := events.New(events.Params{Type: EventReleaseDriftDetected, Source: r.eventSource(), Subject: key,
		DataSchema: schemaReleaseDriftDetected, CorrelationID: domain.NewUUIDv7(), Data: data})
	if err != nil {
		return err
	}
	return r.insertOutboxEvent(ctx, tx, "engine_instance_release", key, now.UnixMicro(), env)
}

func (r *PostgresRepository) SweepReleaseDrift(ctx context.Context, now time.Time) (int, error) {
	rows, err := r.pool.Query(ctx, `SELECT engine_instance_key FROM topology.engine_instance ei
		WHERE ei.desired_release_id IS NOT NULL
			OR EXISTS (SELECT 1 FROM topology.deployment_observation o WHERE o.engine_instance_key = ei.engine_instance_key)
			OR EXISTS (SELECT 1 FROM topology.engine_instance_release_drift d WHERE d.engine_instance_key = ei.engine_instance_key)
		ORDER BY ei.engine_instance_key`)
	if err != nil {
		return 0, fmt.Errorf("list instances for release drift: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	evaluated := 0
	var firstErr error
	for _, key := range keys {
		if err := r.EvaluateReleaseDrift(ctx, key, now); err != nil {
			slog.Error("release drift evaluation failed", "engine_instance_id", key, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		evaluated++
	}
	return evaluated, firstErr
}

func (r *PostgresRepository) ListReleaseDrift(ctx context.Context, tenantID string) ([]ReleaseDrift, error) {
	policy, err := releaseDriftPolicy()
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT d.engine_instance_key, d.reason_code, d.opened_at, d.observed_state,
			COALESCE(d.observed_release_key, ''), COALESCE(d.desired_release_key, '')
		FROM topology.engine_instance_release_drift d
		JOIN topology.engine_instance ei ON ei.engine_instance_key = d.engine_instance_key
		WHERE d.opened_at IS NOT NULL AND EXISTS (
			SELECT 1 FROM capability.capability_binding b JOIN capability.capability_scope s ON s.scope_id = b.scope_id
			WHERE b.engine_instance_id = ei.engine_instance_id AND s.tenant_id = $1 AND b.status = 'ACTIVE'
				AND b.effective_from <= now() AND (b.effective_to IS NULL OR b.effective_to > now()))
		ORDER BY d.engine_instance_key`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list release drift: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReleaseDrift, error) {
		var d ReleaseDrift
		if err := row.Scan(&d.EngineInstanceID, &d.Reason, &d.DetectedAt, &d.ObservedState, &d.ObservedRelease, &d.DesiredRelease); err != nil {
			return d, err
		}
		d.DetectedAt, d.Severity = d.DetectedAt.UTC(), policy[d.Reason].Severity
		return d, nil
	})
}

// ReleaseReadinessRepository judges a tenant's readiness by the release drift
// of the instances behind its capabilities (ADR-BCP-025 section 2.8).
type ReleaseReadinessRepository interface {
	// TenantReleaseReadiness is the readiness consequence of open release
	// drift for a tenant. Drift blocks only through a mandatory dependency
	// whose every usable binding is affected, and degrades otherwise; it is
	// reported, never acted on (resolution is unchanged, ER-06 is not accepted).
	TenantReleaseReadiness(ctx context.Context, tenantID string, now time.Time) (release.TenantReleaseReadiness, error)
}

var _ ReleaseReadinessRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) TenantReleaseReadiness(ctx context.Context, tenantID string, now time.Time) (release.TenantReleaseReadiness, error) {
	policy, err := releaseDriftPolicy()
	if err != nil {
		return release.TenantReleaseReadiness{}, err
	}
	// Mandatory capabilities: MANDATORY members, unconditioned, of the
	// ACTIVE composition of each product the tenant is subscribed to now.
	rows, err := r.pool.Query(ctx, `
		WITH subscribed AS (
			SELECT DISTINCT pv.composition_key
			FROM product.product_subscription s JOIN product.product_version pv ON pv.product_version_id = s.product_version_id
			WHERE s.tenant_id = $1 AND s.status = 'ACTIVE' AND s.effective_from <= $2 AND (s.effective_to IS NULL OR s.effective_to > $2)),
		latest AS (
			SELECT DISTINCT ON (c.composition_key) c.composition_id
			FROM capability.capability_composition c
			WHERE c.composition_key IN (SELECT composition_key FROM subscribed) AND c.lifecycle = 'ACTIVE'
			ORDER BY c.composition_key, string_to_array(c.version, '.')::int[] DESC)
		SELECT DISTINCT m.capability_key FROM capability.capability_composition_member m
		WHERE m.composition_id IN (SELECT composition_id FROM latest) AND m.criticality = 'MANDATORY'
			AND COALESCE(m.activation_condition, '') = ''`, tenantID, now)
	if err != nil {
		return release.TenantReleaseReadiness{}, fmt.Errorf("list mandatory capabilities: %w", err)
	}
	mandatory := map[string]bool{}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return release.TenantReleaseReadiness{}, err
	}
	for _, k := range keys {
		mandatory[k] = true
	}
	// The tenant's usable bindings: ACTIVE, in date, serving (not a
	// migration source or target, a shadow or a disabled binding).
	rows, err = r.pool.Query(ctx, `SELECT cap.code, ei.engine_instance_key, COALESCE(d.reason_code, '')
		FROM capability.capability_binding b
		JOIN capability.capability_scope s ON s.scope_id = b.scope_id
		JOIN capability.capability cap ON cap.capability_id = b.capability_id
		JOIN topology.engine_instance ei ON ei.engine_instance_id = b.engine_instance_id
		LEFT JOIN topology.engine_instance_release_drift d ON d.engine_instance_key = ei.engine_instance_key AND d.opened_at IS NOT NULL
		WHERE s.tenant_id = $1 AND b.status = 'ACTIVE' AND b.effective_from <= $2 AND (b.effective_to IS NULL OR b.effective_to > $2)
			AND b.binding_mode IN ('PRIMARY', 'SECONDARY', 'FALLBACK', 'READ_ONLY')
		ORDER BY cap.code, ei.engine_instance_key`, tenantID, now)
	if err != nil {
		return release.TenantReleaseReadiness{}, fmt.Errorf("list tenant bindings: %w", err)
	}
	defer rows.Close()
	byCapability := map[string][]release.BoundInstance{}
	var order []string
	for rows.Next() {
		var capability, instance, reason string
		if err := rows.Scan(&capability, &instance, &reason); err != nil {
			return release.TenantReleaseReadiness{}, err
		}
		if _, seen := byCapability[capability]; !seen {
			order = append(order, capability)
		}
		bound := release.BoundInstance{EngineInstanceID: instance, DriftReason: reason}
		if reason != "" {
			bound.DriftEffect = policy[reason].ReadinessEffect
		}
		byCapability[capability] = append(byCapability[capability], bound)
	}
	if err := rows.Err(); err != nil {
		return release.TenantReleaseReadiness{}, err
	}
	capabilities := make([]release.CapabilityReadiness, 0, len(order))
	for _, capability := range order {
		capabilities = append(capabilities, release.ReleaseReadinessOf(capability, mandatory[capability], byCapability[capability]))
	}
	return release.Aggregate(capabilities), nil
}
