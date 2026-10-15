// P0-04B Payments (ADR-SHARED-035): trusted, CP-internal persistence for
// Payments money-movement authority. Not an API and not a grant of authority.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// PaymentsMoneyMovementDecision is one immutable decision as CP records it.
// Optional fields are empty or nil unless the outcome is AUTHORIZED, except
// the CP-derived scope (tenant, Organisation, market, role, activity,
// capability), which is recorded whenever CP established it.
type PaymentsMoneyMovementDecision struct {
	ClientID                 string
	ContextID                string
	EngineInstanceID         string
	OperationClass           string
	OperationReference       string
	Attempt                  int
	InstructionFingerprint   string
	Outcome                  string
	TenantID                 string
	OperatingOrganisationID  string
	Market                   string
	LegalActorRole           string
	Activity                 string
	CapabilityKey            string
	MandateID                string
	SupersedesMandateID      string
	ResponsibleLegalEntityID string
	EvidenceReferences       []string
	MandateEffectiveFrom     *time.Time
	MandateEffectiveTo       *time.Time
	EvaluatedAt              time.Time
	ExpiresAt                *time.Time
	PolicyReference          string
	CorrelationID            string
}

// PaymentsEngineInstanceBound reports whether the canonical engine instance is
// a live baobab-payments instance with an ACTIVE, in-effect capability binding
// in a scope owned by the tenant. A binding of any other engine, including the
// ERP that records payments in the ledger, never qualifies.
func (s *Store) PaymentsEngineInstanceBound(ctx context.Context, tenantID, engineInstanceKey string, at time.Time) (bool, error) {
	if tenantID == "" || engineInstanceKey == "" || at.IsZero() {
		return false, nil
	}
	var bound bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		      FROM capability.capability_binding b
		      JOIN topology.engine_instance ei ON ei.engine_instance_id = b.engine_instance_id
		      JOIN topology.engine e ON e.engine_id = ei.engine_id
		      JOIN capability.capability_scope cs ON cs.scope_id = b.scope_id
		     WHERE ei.engine_instance_key = $2
		       AND e.code = 'baobab-payments'
		       AND ei.status = 'ACTIVE'
		       AND ei.valid_period @> $3::timestamptz
		       AND cs.tenant_id = $1
		       AND b.status = 'ACTIVE'
		       AND b.valid_period @> $3::timestamptz)
	`, tenantID, engineInstanceKey, at.UTC()).Scan(&bound)
	if err != nil {
		return false, fmt.Errorf("check Payments engine-instance binding: %w", err)
	}
	return bound, nil
}

// RecordPaymentsMoneyMovementDecision appends the decision and returns its
// decision_id. A decision that cannot be recorded must not be returned as
// AUTHORIZED: callers treat an error as unavailable.
func (s *Store) RecordPaymentsMoneyMovementDecision(ctx context.Context, d PaymentsMoneyMovementDecision) (string, error) {
	var evidence any
	if len(d.EvidenceReferences) > 0 {
		raw, err := json.Marshal(d.EvidenceReferences)
		if err != nil {
			return "", fmt.Errorf("encode evidence references: %w", err)
		}
		evidence = raw
	}
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO registry.payments_money_movement_decision (
		    client_id, context_id, engine_instance_id, operation_class,
		    operation_reference, attempt, instruction_fingerprint, outcome,
		    tenant_id, operating_organisation_id, market, legal_actor_role, activity, capability_key,
		    mandate_id, supersedes_mandate_id, responsible_legal_entity_id,
		    evidence_references, mandate_effective_from, mandate_effective_to,
		    evaluated_at, expires_at, policy_reference, correlation_id)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8,
		        NULLIF($9, ''), NULLIF($10, '')::uuid, NULLIF($11, ''), NULLIF($12, ''), $13, NULLIF($14, ''),
		        NULLIF($15, '')::uuid, NULLIF($16, '')::uuid, NULLIF($17, ''),
		        $18::jsonb, $19, $20, $21, $22, $23, NULLIF($24, ''))
		RETURNING decision_id::text
	`, d.ClientID, d.ContextID, d.EngineInstanceID, d.OperationClass,
		d.OperationReference, d.Attempt, d.InstructionFingerprint, d.Outcome,
		d.TenantID, d.OperatingOrganisationID, d.Market, d.LegalActorRole, d.Activity, d.CapabilityKey,
		d.MandateID, d.SupersedesMandateID, d.ResponsibleLegalEntityID,
		evidence, d.MandateEffectiveFrom, d.MandateEffectiveTo,
		d.EvaluatedAt.UTC(), d.ExpiresAt, d.PolicyReference, d.CorrelationID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("record Payments money-movement decision: %w", err)
	}
	return id, nil
}
