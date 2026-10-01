// ADR-BCP-025 section 2.10 events, published through the transactional
// outbox in the same transaction as the change: a release recorded, a release
// changing status, and an engine instance's desired release changing. The
// drift event is published by the drift evaluation. Every one is
// platform-scoped and carries identifiers, versions, digests and states only
// (topology/v1 events.schema.json).

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

const (
	EventEngineReleaseRecorded       = "com.baobab-platform.control-plane.engine-release.recorded.v1"
	EventEngineReleaseStatusChanged  = "com.baobab-platform.control-plane.engine-release.status-changed.v1"
	EventDesiredReleaseChanged       = "com.baobab-platform.control-plane.engine-instance.desired-release-changed.v1"
	schemaEngineReleaseRecorded      = "https://contracts.baobab-platform.com/topology/v1/events.schema.json#/$defs/EngineReleaseRecorded"
	schemaEngineReleaseStatusChanged = "https://contracts.baobab-platform.com/topology/v1/events.schema.json#/$defs/EngineReleaseStatusChanged"
	schemaDesiredReleaseChanged      = "https://contracts.baobab-platform.com/topology/v1/events.schema.json#/$defs/EngineInstanceDesiredReleaseChanged"
	topologyReleaseAggregate         = "engine_release"
	topologyInstanceReleaseAggregate = "engine_instance_release"
)

func publishTopologyEvent(ctx context.Context, tx pgx.Tx, source, eventType, schema, subject, aggregateType string, version int64, data map[string]any) error {
	env, err := events.New(events.Params{Type: eventType, Source: source, Subject: subject, DataSchema: schema,
		CorrelationID: domain.NewUUIDv7(), Data: data})
	if err != nil {
		return err
	}
	return insertOutbox(ctx, tx, aggregateType, subject, version, env)
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// publishEngineReleaseRecorded: a new release was recorded as CANDIDATE. A
// replay records nothing and publishes nothing.
func publishEngineReleaseRecorded(ctx context.Context, tx pgx.Tx, source string, rel release.Release) error {
	return publishTopologyEvent(ctx, tx, source, EventEngineReleaseRecorded, schemaEngineReleaseRecorded, rel.ReleaseID, topologyReleaseAggregate, 1,
		map[string]any{"release_id": rel.ReleaseID, "engine_id": rel.EngineID, "release_version": rel.ReleaseVersion,
			"source_revision": rel.SourceRevision, "status": rel.Status, "recorded_at": timestamp(rel.RecordedAt)})
}

// publishEngineReleaseStatusChanged: a release moved along the policy's
// status transitions (approval, deprecation or revocation).
func publishEngineReleaseStatusChanged(ctx context.Context, tx pgx.Tx, source, releaseKey, previous, status string, at time.Time) error {
	var engineCode, version string
	if err := tx.QueryRow(ctx, `SELECT e.code, r.release_version FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id
		WHERE r.release_key = $1`, releaseKey).Scan(&engineCode, &version); err != nil {
		return fmt.Errorf("read release for status event: %w", err)
	}
	return publishTopologyEvent(ctx, tx, source, EventEngineReleaseStatusChanged, schemaEngineReleaseStatusChanged, releaseKey, topologyReleaseAggregate, at.UnixMicro(),
		map[string]any{"release_id": releaseKey, "engine_id": engineCode, "release_version": version,
			"previous_status": previous, "status": status, "changed_at": timestamp(at)})
}

// publishDesiredReleaseChanged: an engine instance's desired release changed.
// When a release is desired the payload carries its artifacts, which is
// everything infrastructure tooling needs to execute the change.
func publishDesiredReleaseChanged(ctx context.Context, tx pgx.Tx, source, instanceKey, previousKey, desiredKey, changesetID string, version int64, at time.Time) error {
	var engineCode string
	if err := tx.QueryRow(ctx, `SELECT e.code FROM topology.engine_instance ei JOIN topology.engine e ON e.engine_id = ei.engine_id
		WHERE ei.engine_instance_key = $1`, instanceKey).Scan(&engineCode); err != nil {
		return fmt.Errorf("read engine instance for desired-release event: %w", err)
	}
	data := map[string]any{"engine_instance_id": instanceKey, "engine_id": engineCode, "previous_release_id": nil, "desired_release_id": nil,
		"version": version, "changed_at": timestamp(at)}
	if previousKey != "" {
		data["previous_release_id"] = previousKey
	}
	if changesetID != "" {
		data["changeset_id"] = changesetID
	}
	if desiredKey != "" {
		desired, err := readEngineRelease(ctx, tx, desiredKey)
		if err != nil {
			return fmt.Errorf("read desired release for event: %w", err)
		}
		raw, err := json.Marshal(desired.Artifacts)
		if err != nil {
			return err
		}
		var artifacts []any
		if err := json.Unmarshal(raw, &artifacts); err != nil {
			return err
		}
		data["desired_release_id"], data["desired_release_version"], data["desired_artifacts"] = desiredKey, desired.ReleaseVersion, artifacts
	}
	return publishTopologyEvent(ctx, tx, source, EventDesiredReleaseChanged, schemaDesiredReleaseChanged, instanceKey, topologyInstanceReleaseAggregate+"_desired", version, data)
}
