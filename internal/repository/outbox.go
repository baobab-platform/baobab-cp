package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/events"
)

// defaultEventSource mirrors internal/store/postgres.Store's own constant
// (ADR-0004, contracts/events/v1/envelope.schema.json's "source" field):
// every event this repository emits shares one producer identity unless
// PostgresRepository.EventSource overrides it.
const defaultEventSource = "urn:baobab-platform:service:baobab-cp"

// Gate ZB-02 event types/schemas. Scope note: this pass wires outbox
// publishing only for the write paths Gate ZB-02 itself introduced
// (MarketAssignment governance, TradeLane, TenantProvisioning phase
// transitions) -- not the pre-existing CapabilityGrant/CapabilityBinding
// write paths (CreateGrant/RevokeGrant/CreateBinding), which predate this
// gate, are shared by callers well beyond ZB-02, and were not touched by
// this pass. Wiring events into those is a separate, larger change with a
// wider blast radius; see the ZB-02 spec cross-check for the full list.
const (
	// Registered in baobab-platform/shared contracts/control-plane/v1/asyncapi.yaml
	// and contracts/trade-lane/v1/asyncapi.yaml (ADR-SHARED-008).
	eventTypeMarketParticipationCreated = "com.baobab-platform.control-plane.market-participation.created.v1"
	eventTypeMarketParticipationUpdated = "com.baobab-platform.control-plane.market-participation.updated.v1"
	eventTypeTradeLaneActivated         = "com.baobab-platform.market.trade-lane.activated.v1"
	eventTypeTenantProvisioningReady    = "com.baobab-platform.control-plane.tenant.provisioning-ready.v1"
	eventTypeTenantProvisioningActive   = "com.baobab-platform.control-plane.tenant.provisioning-active.v1"
	eventTypeTenantProvisioningFailed   = "com.baobab-platform.control-plane.tenant.provisioning-failed.v1"

	schemaMarketParticipationCreated = "https://contracts.baobab-platform.com/control-plane/v1/market-participation-created.schema.json"
	schemaMarketParticipationUpdated = "https://contracts.baobab-platform.com/control-plane/v1/market-participation-updated.schema.json"
	schemaTradeLaneActivated         = "https://contracts.baobab-platform.com/trade-lane/v1/events.schema.json#/$defs/tradeLaneActivatedEventData"
	schemaTenantProvisioningReady    = "https://contracts.baobab-platform.com/control-plane/v1/tenant-provisioning-milestone.schema.json"
	schemaTenantProvisioningActive   = schemaTenantProvisioningReady
	schemaTenantProvisioningFailed   = schemaTenantProvisioningReady
)

func (r *PostgresRepository) eventSource() string {
	if r.EventSource != "" {
		return r.EventSource
	}
	return defaultEventSource
}

// insertOutboxEvent writes env into the canonical messaging.outbox
// (migration 000015) on tx, so the event commits atomically with the
// domain write that produced it -- never a separate, unsafe dual write.
// aggregateType/aggregateID/aggregateVersion identify the row per
// messaging.outbox's own schema.
func (r *PostgresRepository) insertOutboxEvent(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID string, aggregateVersion int64, env events.Envelope) error {
	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", env.Type, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messaging.outbox(aggregate_type, aggregate_id, aggregate_version, event_type, tenant_id, correlation_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		aggregateType, aggregateID, aggregateVersion, env.Type, env.TenantID, env.CorrelationID, payload,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", env.Type, err)
	}
	return nil
}
