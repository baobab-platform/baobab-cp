package repository

import (
	"context"
	"fmt"
	"time"
)

// CapabilityResolutionRecord is one capability resolution decision, as
// recorded (migration 000079). Grant, binding, provider and engine instance
// are the surrogate ids the decision named.
type CapabilityResolutionRecord struct {
	ResolutionID     string
	ContextID        string
	TenantID         string
	CapabilityKey    string
	Decision         string
	ReasonCode       string
	GrantID          string
	BindingID        string
	ProviderID       string
	EngineInstanceID string
	ContractVersion  int
	ServiceReference string
	Protocol         string
	CorrelationID    string
	ResolvedAt       time.Time
	ExpiresAt        *time.Time
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// RecordCapabilityResolution stores a decision. Decisions are never updated.
func (r *PostgresRepository) RecordCapabilityResolution(ctx context.Context, rec CapabilityResolutionRecord) error {
	var version *int
	if rec.ContractVersion > 0 {
		version = &rec.ContractVersion
	}
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO capability.capability_resolution (resolution_id, context_id, tenant_id, capability_key, decision, reason_code,
			grant_id, binding_id, provider_id, engine_instance_id, contract_version, service_reference, correlation_id, resolved_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::uuid, $8::uuid, $9::uuid, $10::uuid, $11, $12, $13::uuid, $14, $15)`,
		rec.ResolutionID, rec.ContextID, rec.TenantID, rec.CapabilityKey, rec.Decision, nullIfEmpty(rec.ReasonCode),
		nullIfEmpty(rec.GrantID), nullIfEmpty(rec.BindingID), nullIfEmpty(rec.ProviderID), nullIfEmpty(rec.EngineInstanceID),
		version, nullIfEmpty(rec.ServiceReference), rec.CorrelationID, rec.ResolvedAt, rec.ExpiresAt); err != nil {
		return fmt.Errorf("record capability resolution %s: %w", rec.ResolutionID, err)
	}
	return nil
}
