package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// TopologyMetricsCollector reports ADR-BCP-025 section 2.10's state gauges.
// Wrap it in metrics.CachedCollector: each collection reads releases, drift
// and the current observation of every observed instance.
//
// Labels are bounded: release_status, observed_state, drift_reason and
// environment, each from a closed vocabulary. deployment_observation_age_seconds
// is the age of the oldest current observation in each environment, because an
// instance label is not allowed.
type TopologyMetricsCollector struct {
	Repo *PostgresRepository
	Now  func() time.Time
}

func (c TopologyMetricsCollector) Collect(ctx context.Context) ([]metrics.Family, error) {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now()
	}
	releases := metrics.Family{Name: "engine_release_total", Help: "Recorded engine releases by status.", Kind: metrics.Gauge}
	rows, err := c.Repo.pool.Query(ctx, `SELECT status, count(*) FROM topology.engine_release GROUP BY status ORDER BY status`)
	if err != nil {
		return nil, fmt.Errorf("count releases: %w", err)
	}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return nil, err
		}
		releases.Samples = append(releases.Samples, metrics.Sample{Labels: map[string]string{"release_status": status}, Value: float64(n)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	drift := metrics.Family{Name: "engine_instance_release_drift_total", Help: "Engine instances with open release drift, by reason.", Kind: metrics.Gauge}
	if rows, err = c.Repo.pool.Query(ctx, `SELECT reason_code, count(*) FROM topology.engine_instance_release_drift
		WHERE opened_at IS NOT NULL GROUP BY reason_code ORDER BY reason_code`); err != nil {
		return nil, fmt.Errorf("count release drift: %w", err)
	}
	for rows.Next() {
		var reason string
		var n int64
		if err := rows.Scan(&reason, &n); err != nil {
			rows.Close()
			return nil, err
		}
		drift.Samples = append(drift.Samples, metrics.Sample{Labels: map[string]string{"drift_reason": reason}, Value: float64(n)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Every instance that desires a release or has been observed has an
	// observed state; derive it as a read does.
	rows, err = c.Repo.pool.Query(ctx, `SELECT engine_instance_key, COALESCE(environment, '') FROM topology.engine_instance ei
		WHERE desired_release_id IS NOT NULL OR EXISTS (SELECT 1 FROM topology.deployment_observation o WHERE o.engine_instance_key = ei.engine_instance_key)
		ORDER BY engine_instance_key`)
	if err != nil {
		return nil, fmt.Errorf("list observed instances: %w", err)
	}
	type instance struct{ key, environment string }
	var instances []instance
	for rows.Next() {
		var i instance
		if err := rows.Scan(&i.key, &i.environment); err != nil {
			rows.Close()
			return nil, err
		}
		instances = append(instances, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	states := metrics.Family{Name: "engine_instance_release_total",
		Help: "Engine instances by observed state and, for one observed release, that release's status.", Kind: metrics.Gauge}
	age := metrics.Family{Name: "deployment_observation_age_seconds",
		Help: "Age in seconds of the oldest current deployment observation, by environment.", Kind: metrics.Gauge}
	counts := map[[2]string]int64{}
	oldest := map[string]float64{}
	tx, err := c.Repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only
	for _, i := range instances {
		observed, current, err := observedReleaseOf(ctx, tx, i.key, now)
		if err != nil {
			return nil, err
		}
		status := ""
		if observed.State == release.StateRelease {
			if err := tx.QueryRow(ctx, `SELECT status FROM topology.engine_release WHERE release_key = $1`, observed.ReleaseID).Scan(&status); err != nil {
				return nil, fmt.Errorf("read observed release status: %w", err)
			}
		}
		counts[[2]string{observed.State, status}]++
		if current != nil {
			a := now.Sub(current.ObservedAt).Seconds()
			if previous, seen := oldest[i.environment]; !seen || a > previous {
				oldest[i.environment] = a
			}
		}
	}
	for key, n := range counts {
		labels := map[string]string{"observed_state": key[0]}
		if key[1] != "" {
			labels["release_status"] = key[1]
		}
		states.Samples = append(states.Samples, metrics.Sample{Labels: labels, Value: float64(n)})
	}
	for environment, seconds := range oldest {
		if environment == "" {
			continue
		}
		age.Samples = append(age.Samples, metrics.Sample{Labels: map[string]string{"environment": environment}, Value: seconds})
	}
	return []metrics.Family{releases, states, drift, age}, nil
}
