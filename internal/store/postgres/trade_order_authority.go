// P0-04B Trade-0 (ADR-SHARED-039): trusted, CP-internal persistence for Trade
// order authority. Not an API and not a grant of authority.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TradeOrderAuthorityDecision is one immutable decision as CP records it.
// Optional fields are empty or nil unless the outcome is AUTHORIZED, except
// the CP-derived scope (tenant, Organisation, market, role, activity), which is
// recorded whenever CP established it.
type TradeOrderAuthorityDecision struct {
	ClientID                  string
	ContextID                 string
	EngineInstanceID          string
	OperationClass            string
	OperationReference        string
	BindingID                 string
	BindingVersion            int
	NativeConfigurationDigest string
	Currency                  string
	Outcome                   string
	TenantID                  string
	OperatingOrganisationID   string
	Market                    string
	LegalActorRole            string
	Activity                  string
	MandateID                 string
	SupersedesMandateID       string
	ResponsibleLegalEntityID  string
	EvidenceReferences        []string
	MandateEffectiveFrom      *time.Time
	MandateEffectiveTo        *time.Time
	EvaluatedAt               time.Time
	ExpiresAt                 *time.Time
	PolicyReference           string
	CorrelationID             string
}

// TradeEngineInstanceBound reports whether the canonical engine instance is a
// live baobab-trade instance with an ACTIVE, in-effect capability binding in a
// scope owned by the tenant. No other engine ever qualifies.
func (s *Store) TradeEngineInstanceBound(ctx context.Context, tenantID, engineInstanceKey string, at time.Time) (bool, error) {
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
		       AND e.code = 'baobab-trade'
		       AND ei.status = 'ACTIVE'
		       AND ei.valid_period @> $3::timestamptz
		       AND cs.tenant_id = $1
		       AND b.status = 'ACTIVE'
		       AND b.valid_period @> $3::timestamptz)
	`, tenantID, engineInstanceKey, at.UTC()).Scan(&bound)
	if err != nil {
		return false, fmt.Errorf("check Trade engine-instance binding: %w", err)
	}
	return bound, nil
}

// RecordTradeOrderAuthorityDecision appends the decision and returns its
// decision_id. A decision that cannot be recorded must never be returned.
func (s *Store) RecordTradeOrderAuthorityDecision(ctx context.Context, d TradeOrderAuthorityDecision) (string, error) {
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
		INSERT INTO registry.trade_order_authority_decision (
		    client_id, context_id, engine_instance_id, operation_class, operation_reference,
		    binding_id, binding_version, native_configuration_digest, currency, outcome,
		    tenant_id, operating_organisation_id, market, legal_actor_role, activity,
		    mandate_id, supersedes_mandate_id, responsible_legal_entity_id,
		    evidence_references, mandate_effective_from, mandate_effective_to,
		    evaluated_at, expires_at, policy_reference, correlation_id)
		VALUES ($1, $2::uuid, $3, $4, $5, $6::uuid, $7, $8, $9, $10,
		        NULLIF($11, ''), NULLIF($12, '')::uuid, NULLIF($13, ''), NULLIF($14, ''), $15,
		        NULLIF($16, '')::uuid, NULLIF($17, '')::uuid, NULLIF($18, ''),
		        $19::jsonb, $20, $21, $22, $23, $24, NULLIF($25, ''))
		RETURNING decision_id::text
	`, d.ClientID, d.ContextID, d.EngineInstanceID, d.OperationClass, d.OperationReference,
		d.BindingID, d.BindingVersion, d.NativeConfigurationDigest, d.Currency, d.Outcome,
		d.TenantID, d.OperatingOrganisationID, d.Market, d.LegalActorRole, d.Activity,
		d.MandateID, d.SupersedesMandateID, d.ResponsibleLegalEntityID,
		evidence, d.MandateEffectiveFrom, d.MandateEffectiveTo,
		d.EvaluatedAt.UTC(), d.ExpiresAt, d.PolicyReference, d.CorrelationID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("record Trade order-authority decision: %w", err)
	}
	return id, nil
}
