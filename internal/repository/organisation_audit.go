package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/events"
)

var correlationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// validateActor fails closed: an organisation change nobody can be held
// accountable for is not written (ADR-BCP-018 section 131).
func validateActor(actor AuditActor) error {
	if actor.ActorID == "" {
		return errors.New("organisation change requires an authenticated actor")
	}
	if actor.ActorType != "human" && actor.ActorType != "workload" {
		return fmt.Errorf("organisation change: actor_type %q must be human or workload", actor.ActorType)
	}
	if !correlationPattern.MatchString(actor.CorrelationID) {
		return errors.New("organisation change requires a uuid correlation_id")
	}
	return nil
}

// recordOrganisationChange writes the audit row and, when the change has a
// section 124 event, the outbox event, both on tx so they commit atomically
// with the state change itself.
func (r *PostgresRepository) recordOrganisationChange(ctx context.Context, tx pgx.Tx, actor AuditActor, change events.OrganisationChange) error {
	payload, err := json.Marshal(change.AuditPayload)
	if err != nil {
		return fmt.Errorf("marshal audit payload for %s: %w", change.AuditAction, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_id, actor_type, client_id, token_id, correlation_id,
			action, target, result, payload)
		VALUES ($1, $2, $3, $4, $5, $6::uuid, $7, $8, 'accepted', $9::jsonb)`,
		nullable(change.TenantID), actor.ActorID, actor.ActorType, nullable(actor.ClientID), nullable(actor.TokenID),
		actor.CorrelationID, change.AuditAction, change.Target, payload); err != nil {
		return fmt.Errorf("audit %s: %w", change.AuditAction, err)
	}
	if change.EventType == "" {
		return nil
	}
	env, err := events.NewOrganisationEnvelope(change, r.eventSource(), actor.CorrelationID)
	if err != nil {
		return fmt.Errorf("build %s event: %w", change.EventType, err)
	}
	// The outbox orders an aggregate's events by aggregate_version; the
	// organisation tables carry no row version, so commit time is used.
	return r.insertOutboxEvent(ctx, tx, change.AggregateType, change.AggregateID, time.Now().UnixMicro(), env)
}
