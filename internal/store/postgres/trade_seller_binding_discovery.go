// P0-04B Trade-A TA-02 (ADR-SHARED-039 §5): trusted, CP-internal persistence
// for Seller Operating Binding discovery. A resolution identifies a candidate
// binding version; it is never authority.
package postgres

import (
	"context"
	"fmt"
	"time"
)

// SellerBindingResolution is one immutable discovery answer as CP records it.
// The CP-derived scope is empty only when the context was not usable, and the
// binding is named only when the outcome is RESOLVED.
type SellerBindingResolution struct {
	ClientID                     string
	ContextID                    string
	EngineInstanceID             string
	NativeConfigurationReference string
	NativeConfigurationDigest    string
	Currency                     string
	Outcome                      string
	TenantID                     string
	OperatingOrganisationID      string
	Market                       string
	BindingID                    string
	BindingVersion               int
	CandidateCount               int
	ResolvedAt                   time.Time
	CorrelationID                string
}

// SellerBindingCandidates returns every binding version, in any status, whose
// scope matches the context-derived tenant, Organisation and market, the
// engine instance and the native-configuration reference, and whose currency
// scope contains the currency. The caller applies the discovery precedence.
func (s *Store) SellerBindingCandidates(ctx context.Context, tenantID, organisationID, market,
	engineInstanceID, reference, currency string) ([]SellerOperatingBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+bindingColumns+`
		FROM registry.seller_operating_binding
		WHERE tenant_id = $1 AND operating_organisation_id = $2::uuid AND market = $3
		  AND trade_engine_instance_id = $4 AND native_configuration_reference = $5
		  AND $6 = ANY (currency_scope)
		ORDER BY binding_id, binding_version`,
		tenantID, organisationID, market, engineInstanceID, reference, currency)
	if err != nil {
		return nil, fmt.Errorf("read seller binding candidates: %w", err)
	}
	defer rows.Close()
	var out []SellerOperatingBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan seller binding candidate: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read seller binding candidates: %w", err)
	}
	return out, nil
}

// RecordSellerBindingResolution appends the resolution and returns its id. A
// resolution that cannot be recorded must never be returned.
func (s *Store) RecordSellerBindingResolution(ctx context.Context, r SellerBindingResolution) (string, error) {
	var bindingVersion any
	if r.BindingVersion > 0 {
		bindingVersion = r.BindingVersion
	}
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO registry.trade_seller_binding_resolution (
		    client_id, context_id, engine_instance_id, native_configuration_reference,
		    native_configuration_digest, currency, outcome, tenant_id, operating_organisation_id,
		    market, binding_id, binding_version, candidate_count, resolved_at, correlation_id)
		VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, '')::uuid,
		        NULLIF($10, ''), NULLIF($11, '')::uuid, $12, $13, $14, NULLIF($15, ''))
		RETURNING resolution_id::text
	`, r.ClientID, r.ContextID, r.EngineInstanceID, r.NativeConfigurationReference,
		r.NativeConfigurationDigest, r.Currency, r.Outcome, r.TenantID, r.OperatingOrganisationID,
		r.Market, r.BindingID, bindingVersion, r.CandidateCount, r.ResolvedAt.UTC(), r.CorrelationID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("record seller binding resolution: %w", err)
	}
	return id, nil
}
