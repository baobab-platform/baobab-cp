// ADR-SHARED-042 CAA-04 (Shared control-plane/v1 estate-client-binding):
// trusted, CP-internal persistence for estate client bindings and the
// immutable record of every resolution answer. An attestation is context,
// never buyer, membership or purchasing authority.
package postgres

import (
	"context"
	"fmt"
	"time"
)

// EstateClientBinding is one governed binding as CP holds it.
type EstateClientBinding struct {
	BindingID           string
	Issuer              string
	BFFClientID         string
	DigitalEstateID     string
	TenantID            string
	EngineInstanceID    string
	Environment         string
	DeploymentReference string
	Status              string
	Revision            int
	EffectiveFrom       time.Time
	EffectiveUntil      *time.Time
	EvidenceReference   string
}

// EstateClientBindingResolution is one immutable resolution answer. The
// binding and its revision are named only when the outcome is ATTESTED.
type EstateClientBindingResolution struct {
	CallerClientID           string
	Issuer                   string
	BFFClientID              string
	EngineInstanceID         string
	Environment              string
	RequestedDigitalEstateID string
	Outcome                  string
	BindingID                string
	Revision                 int
	CandidateCount           int
	ResolvedAt               time.Time
	CorrelationID            string
}

// EstateClientBindings returns every binding, in any status and environment,
// of the exact (issuer, client). The caller applies the resolution rules.
func (s *Store) EstateClientBindings(ctx context.Context, issuer, bffClientID string) ([]EstateClientBinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT binding_id::text, issuer, bff_client_id, digital_estate_id, tenant_id, engine_instance_id,
		       environment, deployment_reference, status, revision, effective_from, effective_until,
		       evidence_reference
		FROM registry.estate_client_binding
		WHERE issuer = $1 AND bff_client_id = $2
		ORDER BY binding_id`, issuer, bffClientID)
	if err != nil {
		return nil, fmt.Errorf("read estate client bindings: %w", err)
	}
	defer rows.Close()
	var out []EstateClientBinding
	for rows.Next() {
		var b EstateClientBinding
		if err := rows.Scan(&b.BindingID, &b.Issuer, &b.BFFClientID, &b.DigitalEstateID, &b.TenantID,
			&b.EngineInstanceID, &b.Environment, &b.DeploymentReference, &b.Status, &b.Revision,
			&b.EffectiveFrom, &b.EffectiveUntil, &b.EvidenceReference); err != nil {
			return nil, fmt.Errorf("scan estate client binding: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read estate client bindings: %w", err)
	}
	return out, nil
}

// RecordEstateClientBindingResolution appends one answer. An attestation that
// cannot be recorded must never be returned.
func (s *Store) RecordEstateClientBindingResolution(ctx context.Context, r EstateClientBindingResolution) error {
	var revision any
	if r.Revision > 0 {
		revision = r.Revision
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO registry.estate_client_binding_resolution (
		    caller_client_id, issuer, bff_client_id, engine_instance_id, environment,
		    requested_digital_estate_id, outcome, binding_id, revision, candidate_count, resolved_at, correlation_id)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5, NULLIF($6, ''), $7, NULLIF($8, '')::uuid, $9, $10, $11,
		        NULLIF($12, ''))`,
		r.CallerClientID, r.Issuer, r.BFFClientID, r.EngineInstanceID, r.Environment, r.RequestedDigitalEstateID,
		r.Outcome, r.BindingID, revision, r.CandidateCount, r.ResolvedAt, r.CorrelationID)
	if err != nil {
		return fmt.Errorf("record estate client binding resolution: %w", err)
	}
	return nil
}
