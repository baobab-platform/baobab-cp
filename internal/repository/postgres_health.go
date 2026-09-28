package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/baobab-platform/baobab-cp/internal/health"
	"github.com/jackc/pgx/v5"
)

// HealthReader loads the health observations eligibility needs
// (ADR-BCP-006 sections 72-73). A repository without it holds no
// observations, so every engine instance is UNKNOWN.
type HealthReader interface {
	// HealthLevels returns the newest observation of the engine instance
	// and, when providerID is set, of the provider and of the provider's
	// capabilityKey. A level with none is nil.
	HealthLevels(ctx context.Context, engineInstanceID, providerID, capabilityKey string) (health.Levels, error)
}

// RecordHealthObservation keeps the observation as its subject's newest,
// unless the Control Plane already holds a later one, or one made at the
// same instant whose status is at least as severe (health-policy.yaml
// equal_time_precedence, so a tie never resolves toward health). Health is
// ephemeral,
// so an older observation is replaced rather than kept (ADR-BCP-006
// sections 20 and 117). The subject's identifiers are this database's own
// engine_instance_id and provider_id, and the capability's key.
func (r *PostgresRepository) RecordHealthObservation(ctx context.Context, o health.Observation) error {
	if r == nil || r.pool == nil {
		return errors.New("repository is not initialized")
	}
	if err := o.Validate(); err != nil {
		return fmt.Errorf("validate health observation: %w", err)
	}
	level, _ := o.Subject.Level()
	policy, err := health.DefaultPolicy()
	if err != nil {
		return err
	}
	var precedence []string
	for _, s := range policy.EqualTimePrecedence() {
		precedence = append(precedence, string(s))
	}
	reasons := o.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	var conflict string
	switch level {
	case health.LevelEngineInstance:
		conflict = "(engine_instance_id) WHERE engine_instance_id IS NOT NULL"
	case health.LevelProvider:
		conflict = "(provider_id) WHERE provider_id IS NOT NULL AND capability_id IS NULL"
	default:
		conflict = "(provider_id, capability_id) WHERE capability_id IS NOT NULL"
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO topology.health_observation
			(engine_instance_id, provider_id, capability_id, status, observed_at, expires_at, source, reasons)
		SELECT NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, c.capability_id, $4, $5, $6, $7, $8
		FROM (SELECT 1) one
		LEFT JOIN capability.capability c ON c.code = $3
		WHERE $3 = '' OR c.capability_id IS NOT NULL
		ON CONFLICT `+conflict+` DO UPDATE SET
			status = EXCLUDED.status, observed_at = EXCLUDED.observed_at, expires_at = EXCLUDED.expires_at,
			source = EXCLUDED.source, reasons = EXCLUDED.reasons, recorded_at = now()
		WHERE topology.health_observation.observed_at < EXCLUDED.observed_at
			OR (topology.health_observation.observed_at = EXCLUDED.observed_at
				AND array_position($9::text[], EXCLUDED.status) < array_position($9::text[], topology.health_observation.status))`,
		o.Subject.EngineInstanceID, o.Subject.ProviderID, o.Subject.CapabilityKey,
		string(o.Status), o.ObservedAt, o.ExpiresAt, string(o.Source), reasons, precedence)
	if err != nil {
		return fmt.Errorf("record health observation: %w", err)
	}
	if tag.RowsAffected() == 0 && o.Subject.CapabilityKey != "" {
		// Either the capability is unknown, or a later observation is held.
		var known bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM capability.capability WHERE code = $1)`,
			o.Subject.CapabilityKey).Scan(&known); err != nil {
			return err
		}
		if !known {
			return fmt.Errorf("record health observation: capability %q is not registered", o.Subject.CapabilityKey)
		}
	}
	return nil
}

// HealthLevels implements HealthReader.
func (r *PostgresRepository) HealthLevels(ctx context.Context, engineInstanceID, providerID, capabilityKey string) (health.Levels, error) {
	if r == nil || r.pool == nil {
		return health.Levels{}, errors.New("repository is not initialized")
	}
	return healthLevelsOn(ctx, r.pool, engineInstanceID, providerID, capabilityKey)
}

// healthLevelsOn reads the health levels through a pool or a transaction.
func healthLevelsOn(ctx context.Context, q migrationQuerier, engineInstanceID, providerID, capabilityKey string) (health.Levels, error) {
	rows, err := q.Query(ctx, `
		SELECT COALESCE(h.engine_instance_id::text, ''), COALESCE(h.provider_id::text, ''), COALESCE(c.code, ''),
			h.status, h.observed_at, h.expires_at, h.source, h.reasons
		FROM topology.health_observation h
		LEFT JOIN capability.capability c ON c.capability_id = h.capability_id
		WHERE (h.engine_instance_id = NULLIF($1, '')::uuid)
			OR (h.provider_id = NULLIF($2, '')::uuid AND (h.capability_id IS NULL OR c.code = $3))`,
		engineInstanceID, providerID, capabilityKey)
	if err != nil {
		return health.Levels{}, fmt.Errorf("load health observations: %w", err)
	}
	observations, err := pgx.CollectRows(rows, scanHealthObservation)
	if err != nil {
		return health.Levels{}, fmt.Errorf("load health observations: %w", err)
	}
	var levels health.Levels
	for i := range observations {
		o := &observations[i]
		switch level, _ := o.Subject.Level(); level {
		case health.LevelEngineInstance:
			levels.EngineInstance = o
		case health.LevelProvider:
			levels.Provider = o
		case health.LevelProviderCapability:
			levels.ProviderCapability = o
		}
	}
	return levels, nil
}

func scanHealthObservation(row pgx.CollectableRow) (health.Observation, error) {
	var o health.Observation
	var status, source string
	err := row.Scan(&o.Subject.EngineInstanceID, &o.Subject.ProviderID, &o.Subject.CapabilityKey,
		&status, &o.ObservedAt, &o.ExpiresAt, &source, &o.Reasons)
	o.Status, o.Source = health.Status(status), health.Source(source)
	o.ObservedAt, o.ExpiresAt = o.ObservedAt.UTC(), o.ExpiresAt.UTC()
	return o, err
}
