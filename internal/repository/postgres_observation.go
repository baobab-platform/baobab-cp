// ADR-BCP-025 gate ER-04: deployment observations. Registered reporters
// append what is running on an engine instance (section 2.6); the current
// observation and the observed release are derived on read, never stored.
// Observation stays observation: nothing here changes resolution
// (section 2.8, amendment A4).

package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// ErrObservationMalformedPageToken: a list page token this repository did
// not issue.
var ErrObservationMalformedPageToken = errors.New("malformed deployment observation page token")

// ObservationRepository appends and reads deployment observations.
type ObservationRepository interface {
	// RecordDeploymentObservation appends one observation, minting its id and
	// assigning recorded_at and ingestion_sequence. source is the reporting
	// workload's principal, taken from the verified caller. An unknown
	// instance is a *release.ErrInvalid coded
	// DEPLOYMENT_OBSERVATION_INSTANCE_UNKNOWN; a window outside the policy is
	// DEPLOYMENT_OBSERVATION_WINDOW_INVALID.
	RecordDeploymentObservation(ctx context.Context, req release.ObservationSubmission, source string, now time.Time) (release.Observation, error)
	ListDeploymentObservations(ctx context.Context, engineInstanceID, pageToken string, limit int) ([]release.Observation, string, error)
	// GetObservedRelease derives the observed release of the instance at now.
	GetObservedRelease(ctx context.Context, engineInstanceID string, now time.Time) (release.ObservedRelease, error)
}

var _ ObservationRepository = (*PostgresRepository)(nil)

func newObservationKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mint observation id: %w", err)
	}
	return "dob_" + hex.EncodeToString(b[:]), nil
}

// ValidateObservationWindow checks the validity window against
// release-policy.yaml observation.ttl_seconds: expires_at later than
// observed_at, by a time-to-live within the policy's bounds.
func ValidateObservationWindow(observedAt, expiresAt time.Time) error {
	doc, err := loadReleasePolicy()
	if err != nil {
		return err
	}
	ttl := expiresAt.Sub(observedAt)
	minTTL := time.Duration(doc.Observation.TTLSeconds.Minimum) * time.Second
	maxTTL := time.Duration(doc.Observation.TTLSeconds.Maximum) * time.Second
	if ttl < minTTL || ttl > maxTTL {
		return &release.ErrInvalid{Code: release.ReasonObservationWindowInvalid,
			Detail: fmt.Sprintf("the observation's validity window is %s; policy allows %s to %s", ttl, minTTL, maxTTL)}
	}
	return nil
}

func (r *PostgresRepository) RecordDeploymentObservation(ctx context.Context, req release.ObservationSubmission, source string, now time.Time) (release.Observation, error) {
	if err := ValidateObservationWindow(req.ObservedAt, req.ExpiresAt); err != nil {
		return release.Observation{}, err
	}
	key, err := newObservationKey()
	if err != nil {
		return release.Observation{}, err
	}
	artifacts, err := json.Marshal(req.Artifacts)
	if err != nil {
		return release.Observation{}, err
	}
	obs := release.Observation{
		ObservationID: key, EngineInstanceID: req.EngineInstanceID, Artifacts: req.Artifacts,
		Environment: req.Environment, Region: req.Region,
		ObservedAt: req.ObservedAt.UTC(), ExpiresAt: req.ExpiresAt.UTC(), RecordedAt: now.UTC(), Source: source,
	}
	err = r.pool.QueryRow(ctx, `INSERT INTO topology.deployment_observation
			(observation_key, engine_instance_key, artifacts, environment, region, observed_at, expires_at, recorded_at, source)
		SELECT $1, ei.engine_instance_key, $3::jsonb, $4, $5, $6, $7, $8, $9
		FROM topology.engine_instance ei WHERE ei.engine_instance_key = $2
		RETURNING ingestion_sequence`,
		key, req.EngineInstanceID, artifacts, req.Environment, req.Region, obs.ObservedAt, obs.ExpiresAt, obs.RecordedAt, source).
		Scan(&obs.IngestionSequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.Observation{}, &release.ErrInvalid{Code: release.ReasonObservationInstanceUnknown,
			Detail: fmt.Sprintf("engine instance %s is not known to the Control Plane", req.EngineInstanceID)}
	}
	if err != nil {
		return release.Observation{}, fmt.Errorf("record deployment observation: %w", err)
	}
	// Compare on every observation (section 2.7). The observation is
	// already recorded; if the comparison fails the periodic sweep makes it
	// again, so the reporter is not told its report failed.
	if err := r.EvaluateReleaseDrift(ctx, req.EngineInstanceID, now); err != nil {
		slog.Error("release drift evaluation after observation failed", "engine_instance_id", req.EngineInstanceID, "error", err)
	}
	return obs, nil
}

const observationColumns = `observation_key, ingestion_sequence, engine_instance_key, artifacts, environment, region,
	observed_at, expires_at, recorded_at, source`

func scanObservation(row pgx.Row) (release.Observation, error) {
	var (
		o         release.Observation
		artifacts []byte
	)
	if err := row.Scan(&o.ObservationID, &o.IngestionSequence, &o.EngineInstanceID, &artifacts, &o.Environment, &o.Region,
		&o.ObservedAt, &o.ExpiresAt, &o.RecordedAt, &o.Source); err != nil {
		return release.Observation{}, err
	}
	if err := json.Unmarshal(artifacts, &o.Artifacts); err != nil {
		return release.Observation{}, fmt.Errorf("decode observed artifacts: %w", err)
	}
	o.ObservedAt, o.ExpiresAt, o.RecordedAt = o.ObservedAt.UTC(), o.ExpiresAt.UTC(), o.RecordedAt.UTC()
	return o, nil
}

func (r *PostgresRepository) engineInstanceExists(ctx context.Context, id string) (bool, error) {
	if !domain.ValidEngineInstanceID(id) {
		return false, nil
	}
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM topology.engine_instance WHERE engine_instance_key = $1)`, id).Scan(&ok)
	return ok, err
}

func (r *PostgresRepository) ListDeploymentObservations(ctx context.Context, engineInstanceID, pageToken string, limit int) ([]release.Observation, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var afterAt *time.Time
	var afterSeq int64
	if pageToken != "" {
		nanos, seq, ok := strings.Cut(pageToken, ".")
		n, err1 := strconv.ParseInt(nanos, 10, 64)
		s, err2 := strconv.ParseInt(seq, 10, 64)
		if !ok || err1 != nil || err2 != nil || s < 1 {
			return nil, "", ErrObservationMalformedPageToken
		}
		at := time.Unix(0, n).UTC()
		afterAt, afterSeq = &at, s
	}
	exists, err := r.engineInstanceExists(ctx, engineInstanceID)
	if err != nil {
		return nil, "", fmt.Errorf("read engine instance: %w", err)
	}
	if !exists {
		return nil, "", ErrEngineInstanceNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT `+observationColumns+` FROM topology.deployment_observation
		WHERE engine_instance_key = $1 AND ($2::timestamptz IS NULL OR (observed_at, ingestion_sequence) < ($2, $3))
		ORDER BY observed_at DESC, ingestion_sequence DESC LIMIT $4`, engineInstanceID, afterAt, afterSeq, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list deployment observations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (release.Observation, error) { return scanObservation(row) })
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = strconv.FormatInt(last.ObservedAt.UnixNano(), 10) + "." + strconv.FormatInt(last.IngestionSequence, 10)
	}
	return items, next, nil
}

func (r *PostgresRepository) GetObservedRelease(ctx context.Context, engineInstanceID string, now time.Time) (release.ObservedRelease, error) {
	if !domain.ValidEngineInstanceID(engineInstanceID) {
		return release.ObservedRelease{}, ErrEngineInstanceNotFound
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return release.ObservedRelease{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only
	observed, _, err := observedReleaseOf(ctx, tx, engineInstanceID, now)
	return observed, err
}

// observedReleaseOf derives one instance's observed release. It is also
// what the drift sweep and the observation-time evaluation read (gate ER-05).
func observedReleaseOf(ctx context.Context, q rowsQuerier, engineInstanceID string, now time.Time) (release.ObservedRelease, *release.Observation, error) {
	var engineCode string
	err := q.QueryRow(ctx, `SELECT e.code FROM topology.engine_instance ei JOIN topology.engine e ON e.engine_id = ei.engine_id
		WHERE ei.engine_instance_key = $1`, engineInstanceID).Scan(&engineCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.ObservedRelease{}, nil, ErrEngineInstanceNotFound
	}
	if err != nil {
		return release.ObservedRelease{}, nil, fmt.Errorf("read engine instance: %w", err)
	}
	// Only the newest observation can be current; an older one never stands
	// in for an expired or future-dated newest (see release.CurrentObservation).
	row := q.QueryRow(ctx, `SELECT `+observationColumns+` FROM topology.deployment_observation
		WHERE engine_instance_key = $1
		ORDER BY observed_at DESC, ingestion_sequence DESC LIMIT 1`, engineInstanceID)
	obs, err := scanObservation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return release.Derive(engineInstanceID, engineCode, nil, nil, now), nil, nil
	}
	if err != nil {
		return release.ObservedRelease{}, nil, fmt.Errorf("read current observation: %w", err)
	}
	current, ok := release.CurrentObservation([]release.Observation{obs}, now)
	if !ok {
		return release.Derive(engineInstanceID, engineCode, nil, nil, now), nil, nil
	}
	digests := make([]string, 0, len(current.Artifacts))
	for _, a := range current.Artifacts {
		digests = append(digests, a.Digest)
	}
	rows, err := q.Query(ctx, `SELECT a.digest, r.release_key, e.code
		FROM topology.engine_release_artifact a
		JOIN topology.engine_release r ON r.engine_release_id = a.engine_release_id
		JOIN topology.engine e ON e.engine_id = r.engine_id
		WHERE a.digest = ANY($1)`, digests)
	if err != nil {
		return release.ObservedRelease{}, nil, fmt.Errorf("resolve observed digests: %w", err)
	}
	owners := map[string]release.Owner{}
	for rows.Next() {
		var digest string
		var owner release.Owner
		if err := rows.Scan(&digest, &owner.ReleaseID, &owner.EngineID); err != nil {
			rows.Close()
			return release.ObservedRelease{}, nil, err
		}
		owners[digest] = owner
	}
	if err := rows.Err(); err != nil {
		return release.ObservedRelease{}, nil, err
	}
	return release.Derive(engineInstanceID, engineCode, &current, owners, now), &current, nil
}
