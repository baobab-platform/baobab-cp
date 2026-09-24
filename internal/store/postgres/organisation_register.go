// ADR-BCP-018 — organisation structure written inside RegisterTenant's transaction.

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
)

// registrationSourceAuthority marks facts that exist only because a tenant
// was registered. Registration input is a claim, never verification.
const registrationSourceAuthority = "control-plane-registration"

// insertOrganisationOnRegister links a newly registered tenant to the
// organisation structure:
//
//   - if a LegalEntityProfile already exists for the legal entity, the tenant
//     is mapped to that profile's Organisation and nothing about the existing
//     Organisation or profile is changed;
//   - otherwise a new ORGANISATION CanonicalEntity (owned by this tenant, so
//     ADR-BCP-016 organisation_id attestation keeps working), organisation
//     profile and legal-entity profile are created as UNVERIFIED claims with
//     legal_status UNKNOWN. Only an explicit verification transition may
//     promote them (ADR-BCP-018 section 69, ADR-BCP-023);
//   - the tenant's DEFAULT legal-entity mapping and PRIMARY organisation
//     mapping are created if absent.
//
// Platform relationships are not assigned here; admission policy does that
// after review, because privileged types must never be applicant-assigned.
func insertOrganisationOnRegister(ctx context.Context, tx pgx.Tx, c domain.RegisterTenant) error {
	at := time.Now().UTC()
	var orgID string
	err := tx.QueryRow(ctx, `
		SELECT organisation_id::text FROM registry.legal_entity_profile
		WHERE legal_entity_id = $1`, c.LegalEntityID).Scan(&orgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err = tx.QueryRow(ctx, `
			INSERT INTO registry.canonical_entity (entity_type, legal_entity_id, tenant_id, status)
			VALUES ($1, $2, $3, 'active')
			RETURNING canonical_entity_id::text`,
			domain.EntityTypeOrganisation, c.LegalEntityID, c.TenantID).Scan(&orgID); err != nil {
			return fmt.Errorf("create organisation canonical entity: %w", err)
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO registry.organisation_profile (
				canonical_entity_id, display_name, verification_state, source_authority,
				status, effective_from
			) VALUES ($1::uuid, $2, 'UNVERIFIED', $3, 'ACTIVE', $4)`,
			orgID, c.DisplayName, registrationSourceAuthority, at); err != nil {
			return fmt.Errorf("create organisation profile: %w", err)
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO registry.legal_entity_profile (
				legal_entity_id, organisation_id, legal_name, legal_status,
				source_authority, verification_state, effective_from
			) VALUES ($1, $2::uuid, $3, 'UNKNOWN', $4, 'UNVERIFIED', $5)`,
			c.LegalEntityID, orgID, c.DisplayName, registrationSourceAuthority, at); err != nil {
			return fmt.Errorf("create legal entity profile: %w", err)
		}
	case err != nil:
		return fmt.Errorf("lookup legal entity profile: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO registry.tenant_legal_entity_mapping (
			tenant_id, legal_entity_id, mapping_role, status, effective_from, provenance
		) VALUES ($1, $2, 'DEFAULT', 'ACTIVE', $3, $4)
		ON CONFLICT (tenant_id) WHERE mapping_role = 'DEFAULT' AND status IN ('PENDING','ACTIVE','SUSPENDED')
		DO NOTHING`,
		c.TenantID, c.LegalEntityID, at, registrationSourceAuthority); err != nil {
		return fmt.Errorf("tenant legal entity mapping: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO registry.tenant_organisation_mapping (
			tenant_id, organisation_id, mapping_role, status, effective_from, provenance
		) VALUES ($1, $2::uuid, 'PRIMARY_ORGANISATION', 'ACTIVE', $3, $4)
		ON CONFLICT (tenant_id) WHERE mapping_role = 'PRIMARY_ORGANISATION' AND status IN ('PENDING','ACTIVE','SUSPENDED')
		DO NOTHING`,
		c.TenantID, orgID, at, registrationSourceAuthority); err != nil {
		return fmt.Errorf("tenant organisation mapping: %w", err)
	}
	return nil
}
