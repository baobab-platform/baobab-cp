package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReadFederationGovernanceTargetRegistration proves, in one repeatable read,
// that the native target reference and the federation provider binding are
// current in the same CP environment/scope. It never interprets IAM-owned
// trust bytes and never treats registration as approval.
func (r *PostgresRepository) ReadFederationGovernanceTargetRegistration(
	ctx context.Context,
	q FederationGovernanceTargetQuery,
	now time.Time,
) (FederationGovernanceTargetRegistration, error) {
	var out FederationGovernanceTargetRegistration
	if r == nil || r.pool == nil || ctx == nil || ctx.Err() != nil ||
		q.ReferenceID == "" || q.Kind == "" || q.SystemNamespace == "" ||
		q.EngineCode == "" || q.NativeEntityType == "" || q.Kind != q.NativeEntityType || q.ProviderID == "" ||
		q.EngineInstanceID == "" || q.OrganisationID == "" ||
		q.DigitalEstateID == "" || q.Environment == "" {
		return out, ErrFederationGovernanceTargetNotFound
	}
	now = now.UTC()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `
		SELECT ref.fingerprint, ref.external_reference_id, ref.environment, scope.tenant_id
		FROM mapping.external_reference ref
		JOIN topology.engine_instance ref_instance
		  ON ref_instance.engine_instance_key = ref.engine_instance_id
		 AND ref_instance.environment = $9
		 AND UPPER(ref_instance.status) = 'ACTIVE'
		JOIN topology.engine ref_engine
		  ON ref_engine.engine_id = ref_instance.engine_id
		 AND ref_engine.code = $3
		JOIN capability.capability_provider cp
		  ON cp.canonical_provider_id = $5
		 AND UPPER(cp.status) = 'ACTIVE'
		JOIN topology.engine_instance provider_instance
		  ON provider_instance.engine_instance_key = $6
		 AND provider_instance.engine_id = cp.engine_id
		 AND provider_instance.environment = $9
		 AND UPPER(provider_instance.status) = 'ACTIVE'
		JOIN capability.capability cap
		  ON cap.code = 'identity.authentication.perform'
		JOIN capability.provider_capability_support pcs
		  ON pcs.provider_id = cp.provider_id
		 AND pcs.capability_id = cap.capability_id
		 AND pcs.status = 'ACTIVE'
		 AND pcs.effective_from <= $10
		 AND (pcs.effective_to IS NULL OR pcs.effective_to > $10)
		JOIN capability.capability_binding cb
		  ON cb.provider_id = cp.provider_id
		 AND cb.engine_instance_id = provider_instance.engine_instance_id
		 AND cb.capability_id = cap.capability_id
		 AND cb.binding_mode = 'PRIMARY'
		 AND cb.status = 'ACTIVE'
		 AND cb.effective_from <= $10
		 AND (cb.effective_to IS NULL OR cb.effective_to > $10)
		JOIN capability.capability_scope scope
		  ON scope.scope_id = cb.scope_id
		JOIN registry.tenant_organisation_mapping tom
		  ON tom.tenant_id = scope.tenant_id
		 AND tom.organisation_id::text = $7
		 AND tom.status = 'ACTIVE'
		 AND tom.effective_from <= $10
		 AND (tom.effective_to IS NULL OR tom.effective_to > $10)
		WHERE ref.external_reference_id = $1
		  AND ref.system_namespace = $2
		  AND ref.engine_id = $3
		  AND ref.native_entity_type = $4
		  AND ref.environment = $9
		  AND ref.engine_instance_id IS NOT NULL
		  AND ref.status = 'active'
		  AND ref.source_authority <> 'manual-import'
		  AND ref.fingerprint ~ '^sha256:[0-9a-f]{64}$'
		  AND scope.organisation_id = $7
		  AND scope.digital_estate_id = $8
		  AND (scope.environment IS NULL OR scope.environment = $9)
		LIMIT 1`,
		q.ReferenceID,
		q.SystemNamespace,
		q.EngineCode,
		q.NativeEntityType,
		q.ProviderID,
		q.EngineInstanceID,
		q.OrganisationID,
		q.DigitalEstateID,
		q.Environment,
		now,
	).Scan(&out.Digest, &out.ReferenceID, &out.Environment, &out.TenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FederationGovernanceTargetRegistration{}, ErrFederationGovernanceTargetNotFound
	}
	if err != nil {
		return FederationGovernanceTargetRegistration{}, fmt.Errorf("read federation governance target registration: %w", err)
	}
	return out, nil
}

var _ FederationGovernanceTargetReader = (*PostgresRepository)(nil)
