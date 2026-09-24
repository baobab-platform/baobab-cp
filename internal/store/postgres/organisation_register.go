// Target path: internal/store/postgres/organisation_register.go
//
// ADR-BCP-018 — organisation structure written inside RegisterTenant's transaction.

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
)

// insertOrganisationOnRegister creates (idempotently) a CanonicalEntity of type
// ORGANISATION, organisation_profile, legal_entity_profile, default
// tenant_legal_entity_mapping, and tenant_organisation_mapping.
// Platform relationships are not assigned here — admission policy does that
// after review (privileged types must not be applicant-assigned).
func insertOrganisationOnRegister(ctx context.Context, tx pgx.Tx, c domain.RegisterTenant) error {
	at := time.Now().UTC()
	var orgID string
	err := tx.QueryRow(ctx, `
		SELECT organisation_id::text FROM registry.legal_entity_profile
		WHERE legal_entity_id = $1 LIMIT 1`, c.LegalEntityID).Scan(&orgID)
	if err == pgx.ErrNoRows {
		if err = tx.QueryRow(ctx, `
			INSERT INTO registry.canonical_entity (entity_type, legal_entity_id, tenant_id, status)
			VALUES ($1, $2, $3, 'active')
			RETURNING canonical_entity_id::text`,
			domain.EntityTypeOrganisation, c.LegalEntityID, c.TenantID).Scan(&orgID); err != nil {
			return fmt.Errorf("create organisation canonical entity: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("lookup legal entity profile: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO registry.organisation_profile (
			canonical_entity_id, display_name, verification_state, source_authority,
			status, effective_from
		) VALUES ($1::uuid, $2, 'VERIFIED', 'control-plane-admission', 'ACTIVE', $3)
		ON CONFLICT (canonical_entity_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			updated_at = now()`,
		orgID, c.DisplayName, at); err != nil {
		return fmt.Errorf("upsert organisation profile: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO registry.legal_entity_profile (
			legal_entity_id, organisation_id, legal_name, legal_status,
			source_authority, verification_state, effective_from
		) VALUES ($1, $2::uuid, $3, 'ACTIVE', 'control-plane-admission', 'VERIFIED', $4)
		ON CONFLICT (legal_entity_id) DO UPDATE SET
			organisation_id = EXCLUDED.organisation_id,
			legal_name = EXCLUDED.legal_name,
			updated_at = now()`,
		c.LegalEntityID, orgID, c.DisplayName, at); err != nil {
		return fmt.Errorf("upsert legal entity profile: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		UPDATE registry.tenant_legal_entity_mapping
		SET is_default = false, updated_at = now()
		WHERE tenant_id = $1 AND is_default = true`, c.TenantID); err != nil {
		return err
	}
	var mapID string
	err = tx.QueryRow(ctx, `
		SELECT tenant_legal_entity_mapping_id::text
		FROM registry.tenant_legal_entity_mapping
		WHERE tenant_id=$1 AND legal_entity_id=$2 AND status='ACTIVE' LIMIT 1`,
		c.TenantID, c.LegalEntityID).Scan(&mapID)
	if err == pgx.ErrNoRows {
		if _, err = tx.Exec(ctx, `
			INSERT INTO registry.tenant_legal_entity_mapping (
				tenant_id, legal_entity_id, mapping_role, is_default, status,
				effective_from, provenance
			) VALUES ($1, $2, 'DEFAULT', true, 'ACTIVE', $3, 'control-plane-admission')`,
			c.TenantID, c.LegalEntityID, at); err != nil {
			return fmt.Errorf("insert tenant legal entity mapping: %w", err)
		}
	} else if err != nil {
		return err
	} else {
		if _, err = tx.Exec(ctx, `
			UPDATE registry.tenant_legal_entity_mapping
			SET is_default = true, mapping_role = 'DEFAULT', updated_at = now()
			WHERE tenant_legal_entity_mapping_id = $1::uuid`, mapID); err != nil {
			return err
		}
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO registry.tenant_organisation_mapping (
			tenant_id, organisation_id, mapping_role, is_default, status,
			effective_from, provenance
		)
		SELECT $1, $2::uuid, 'PRIMARY_ORGANISATION', true, 'ACTIVE', $3, 'control-plane-admission'
		WHERE NOT EXISTS (
			SELECT 1 FROM registry.tenant_organisation_mapping
			WHERE tenant_id=$1 AND organisation_id=$2::uuid AND status='ACTIVE'
		)`, c.TenantID, orgID, at); err != nil {
		return fmt.Errorf("tenant organisation mapping: %w", err)
	}
	return nil
}
