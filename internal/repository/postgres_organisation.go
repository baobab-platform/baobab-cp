// Target path: internal/repository/postgres_organisation.go
//
// ADR-BCP-018 organisation persistence on PostgresRepository (pgx pool).
// Schema: registry.* tables from migration 000045.

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
)

// Compile-time assertion once the methods are attached to PostgresRepository.
var _ OrganisationRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) UpsertOrganisation(ctx context.Context, org domain.Organisation) error {
	trading, _ := json.Marshal(org.TradingNames)
	if trading == nil {
		trading = []byte("[]")
	}
	meta, _ := json.Marshal(org.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.organisation_profile (
			canonical_entity_id, display_name, official_name, trading_names,
			organisation_form, jurisdiction, verification_state, source_authority,
			status, effective_from, effective_to, metadata, updated_at
		) VALUES (
			$1::uuid, $2, NULLIF($3,''), $4::jsonb, NULLIF($5,''), NULLIF($6,''),
			$7, $8, $9, $10, $11, $12::jsonb, now()
		)
		ON CONFLICT (canonical_entity_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			official_name = EXCLUDED.official_name,
			trading_names = EXCLUDED.trading_names,
			organisation_form = EXCLUDED.organisation_form,
			jurisdiction = EXCLUDED.jurisdiction,
			verification_state = EXCLUDED.verification_state,
			source_authority = EXCLUDED.source_authority,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		org.CanonicalEntityID, org.DisplayName, org.OfficialName, trading,
		string(org.OrganisationForm), org.Jurisdiction, string(org.VerificationState),
		org.SourceAuthority, org.Status, org.EffectiveFrom, org.EffectiveTo, meta,
	)
	return err
}

func (r *PostgresRepository) GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT canonical_entity_id::text, display_name, COALESCE(official_name,''),
			verification_state, source_authority, status, effective_from, effective_to
		FROM registry.organisation_profile WHERE canonical_entity_id=$1::uuid`, canonicalEntityID)
	var o domain.Organisation
	var vs string
	if err := row.Scan(&o.CanonicalEntityID, &o.DisplayName, &o.OfficialName,
		&vs, &o.SourceAuthority, &o.Status, &o.EffectiveFrom, &o.EffectiveTo); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	o.VerificationState = domain.VerificationState(vs)
	return &o, nil
}

func (r *PostgresRepository) UpsertLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) error {
	// Ensure legal_entities row exists (RegisterTenant also does this).
	if _, err := r.pool.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, lep.LegalEntityID); err != nil {
		return err
	}
	evidence, _ := json.Marshal(lep.EvidenceReferences)
	if evidence == nil {
		evidence = []byte("[]")
	}
	meta, _ := json.Marshal(lep.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.legal_entity_profile (
			legal_entity_id, organisation_id, legal_name, jurisdiction_of_incorporation,
			legal_status, source_authority, verification_state, effective_from, effective_to,
			evidence_references, metadata, updated_at
		) VALUES (
			$1, $2::uuid, $3, NULLIF($4,''), $5, $6, $7, $8, $9, $10::jsonb, $11::jsonb, now()
		)
		ON CONFLICT (legal_entity_id) DO UPDATE SET
			organisation_id = EXCLUDED.organisation_id,
			legal_name = EXCLUDED.legal_name,
			jurisdiction_of_incorporation = EXCLUDED.jurisdiction_of_incorporation,
			legal_status = EXCLUDED.legal_status,
			source_authority = EXCLUDED.source_authority,
			verification_state = EXCLUDED.verification_state,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			evidence_references = EXCLUDED.evidence_references,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		lep.LegalEntityID, lep.OrganisationID, lep.LegalName, lep.JurisdictionOfIncorporation,
		lep.LegalStatus, lep.SourceAuthority, string(lep.VerificationState),
		lep.EffectiveFrom, lep.EffectiveTo, evidence, meta,
	)
	return err
}

func (r *PostgresRepository) GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT legal_entity_id, organisation_id::text, legal_name, COALESCE(jurisdiction_of_incorporation,''),
			legal_status, source_authority, verification_state, effective_from, effective_to
		FROM registry.legal_entity_profile WHERE legal_entity_id=$1`, legalEntityID)
	var lep domain.LegalEntityProfile
	var vs string
	if err := row.Scan(&lep.LegalEntityID, &lep.OrganisationID, &lep.LegalName, &lep.JurisdictionOfIncorporation,
		&lep.LegalStatus, &lep.SourceAuthority, &vs, &lep.EffectiveFrom, &lep.EffectiveTo); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	lep.VerificationState = domain.VerificationState(vs)
	return &lep, nil
}

func (r *PostgresRepository) ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT legal_entity_id, organisation_id::text, legal_name, COALESCE(jurisdiction_of_incorporation,''),
			legal_status, source_authority, verification_state, effective_from, effective_to
		FROM registry.legal_entity_profile WHERE organisation_id=$1::uuid`, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LegalEntityProfile
	for rows.Next() {
		var lep domain.LegalEntityProfile
		var vs string
		if err := rows.Scan(&lep.LegalEntityID, &lep.OrganisationID, &lep.LegalName, &lep.JurisdictionOfIncorporation,
			&lep.LegalStatus, &lep.SourceAuthority, &vs, &lep.EffectiveFrom, &lep.EffectiveTo); err != nil {
			return nil, err
		}
		lep.VerificationState = domain.VerificationState(vs)
		out = append(out, lep)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) error {
	id := rel.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	evidence, _ := json.Marshal(rel.EvidenceReferences)
	if evidence == nil {
		evidence = []byte("[]")
	}
	meta, _ := json.Marshal(rel.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.corporate_relationship (
			corporate_relationship_id, source_organisation_id, target_organisation_id,
			relationship_type, ownership_percentage, control_basis, direct_or_derived,
			verification_state, status, effective_from, effective_to, source_authority,
			evidence_references, verified_by, verified_at, classification, metadata, updated_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, NULLIF($6,''), NULLIF($7,''),
			$8, $9, $10, $11, $12, $13::jsonb, NULLIF($14,''), $15, NULLIF($16,''), $17::jsonb, now()
		)
		ON CONFLICT (corporate_relationship_id) DO UPDATE SET
			relationship_type = EXCLUDED.relationship_type,
			ownership_percentage = EXCLUDED.ownership_percentage,
			verification_state = EXCLUDED.verification_state,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			source_authority = EXCLUDED.source_authority,
			evidence_references = EXCLUDED.evidence_references,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, rel.SourceOrganisationID, rel.TargetOrganisationID, string(rel.RelationshipType),
		rel.OwnershipPercentage, rel.ControlBasis, rel.DirectOrDerived,
		string(rel.VerificationState), rel.Status, rel.EffectiveFrom, rel.EffectiveTo,
		rel.SourceAuthority, evidence, rel.VerifiedBy, rel.VerifiedAt, rel.Classification, meta,
	)
	return err
}

func (r *PostgresRepository) ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT corporate_relationship_id::text, source_organisation_id::text, target_organisation_id::text,
			relationship_type, ownership_percentage, COALESCE(control_basis,''), COALESCE(direct_or_derived,''),
			verification_state, status, effective_from, effective_to, source_authority
		FROM registry.corporate_relationship
		WHERE (source_organisation_id=$1::uuid OR target_organisation_id=$1::uuid)
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)`, organisationID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorporateRelationship
	for rows.Next() {
		var rel domain.CorporateRelationship
		var rt, vs string
		if err := rows.Scan(&rel.ID, &rel.SourceOrganisationID, &rel.TargetOrganisationID,
			&rt, &rel.OwnershipPercentage, &rel.ControlBasis, &rel.DirectOrDerived,
			&vs, &rel.Status, &rel.EffectiveFrom, &rel.EffectiveTo, &rel.SourceAuthority); err != nil {
			return nil, err
		}
		rel.RelationshipType = domain.CorporateRelationshipType(rt)
		rel.VerificationState = domain.VerificationState(vs)
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertCorporateGroup(ctx context.Context, g domain.CorporateGroup) error {
	id := g.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	meta, _ := json.Marshal(g.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	var root any
	if g.RootOrganisationID != "" {
		root = g.RootOrganisationID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.corporate_group (
			corporate_group_id, display_name, root_organisation_id, status,
			effective_from, effective_to, metadata, updated_at
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7::jsonb, now())
		ON CONFLICT (corporate_group_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			root_organisation_id = EXCLUDED.root_organisation_id,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, g.DisplayName, root, g.Status, g.EffectiveFrom, g.EffectiveTo, meta,
	)
	return err
}

func (r *PostgresRepository) UpsertCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership) error {
	if len(m.BasisRelationshipIDs) == 0 {
		return fmt.Errorf("corporate group membership requires basis_relationship_ids")
	}
	id := m.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	meta, _ := json.Marshal(m.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.corporate_group_membership (
			corporate_group_membership_id, corporate_group_id, organisation_id,
			membership_type, status, effective_from, effective_to,
			basis_relationship_ids, derived_at, derivation_version, metadata, updated_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7,
			$8::uuid[], $9, $10, $11::jsonb, now()
		)
		ON CONFLICT (corporate_group_membership_id) DO UPDATE SET
			membership_type = EXCLUDED.membership_type,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			basis_relationship_ids = EXCLUDED.basis_relationship_ids,
			derived_at = EXCLUDED.derived_at,
			derivation_version = EXCLUDED.derivation_version,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, m.CorporateGroupID, m.OrganisationID, m.MembershipType, m.Status,
		m.EffectiveFrom, m.EffectiveTo, m.BasisRelationshipIDs, m.DerivedAt, m.DerivationVersion, meta,
	)
	return err
}

func (r *PostgresRepository) ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT corporate_group_membership_id::text, corporate_group_id::text, organisation_id::text,
			membership_type, status, effective_from, effective_to,
			basis_relationship_ids::text[], derived_at, derivation_version
		FROM registry.corporate_group_membership
		WHERE organisation_id=$1::uuid
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)`, organisationID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorporateGroupMembership
	for rows.Next() {
		var m domain.CorporateGroupMembership
		if err := rows.Scan(&m.ID, &m.CorporateGroupID, &m.OrganisationID, &m.MembershipType,
			&m.Status, &m.EffectiveFrom, &m.EffectiveTo, &m.BasisRelationshipIDs, &m.DerivedAt, &m.DerivationVersion); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertPlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) error {
	id := rel.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	evidence, _ := json.Marshal(rel.EvidenceReferences)
	if evidence == nil {
		evidence = []byte("[]")
	}
	meta, _ := json.Marshal(rel.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	var basis any
	if rel.BasisRelationshipID != "" {
		basis = rel.BasisRelationshipID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.platform_relationship (
			platform_relationship_id, platform_id, organisation_id, relationship_type,
			verification_state, status, effective_from, effective_to,
			basis_relationship_id, admission_decision_id, source_authority,
			evidence_references, verified_by, verified_at, metadata, updated_at
		) VALUES (
			$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8,
			$9::uuid, NULLIF($10,''), $11, $12::jsonb, NULLIF($13,''), $14, $15::jsonb, now()
		)
		ON CONFLICT (platform_relationship_id) DO UPDATE SET
			verification_state = EXCLUDED.verification_state,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			basis_relationship_id = EXCLUDED.basis_relationship_id,
			admission_decision_id = EXCLUDED.admission_decision_id,
			source_authority = EXCLUDED.source_authority,
			evidence_references = EXCLUDED.evidence_references,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, rel.PlatformID, rel.OrganisationID, string(rel.RelationshipType),
		string(rel.VerificationState), rel.Status, rel.EffectiveFrom, rel.EffectiveTo,
		basis, rel.AdmissionDecisionID, rel.SourceAuthority, evidence, rel.VerifiedBy, rel.VerifiedAt, meta,
	)
	return err
}

func (r *PostgresRepository) ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT platform_relationship_id::text, platform_id, organisation_id::text, relationship_type,
			verification_state, status, effective_from, effective_to,
			COALESCE(basis_relationship_id::text,''), COALESCE(admission_decision_id,''), source_authority
		FROM registry.platform_relationship
		WHERE organisation_id=$1::uuid
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)`, organisationID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlatformRelationship
	for rows.Next() {
		var pr domain.PlatformRelationship
		var rt, vs string
		if err := rows.Scan(&pr.ID, &pr.PlatformID, &pr.OrganisationID, &rt,
			&vs, &pr.Status, &pr.EffectiveFrom, &pr.EffectiveTo,
			&pr.BasisRelationshipID, &pr.AdmissionDecisionID, &pr.SourceAuthority); err != nil {
			return nil, err
		}
		pr.RelationshipType = domain.PlatformRelationshipType(rt)
		pr.VerificationState = domain.VerificationState(vs)
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertPlatformAccount(ctx context.Context, acct domain.PlatformAccount) error {
	id := acct.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	meta, _ := json.Marshal(acct.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	var primary any
	if acct.PrimaryOrganisationID != "" {
		primary = acct.PrimaryOrganisationID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.platform_account (
			platform_account_id, display_name, primary_organisation_id, status,
			effective_from, effective_to, billing_reference, metadata, updated_at
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, NULLIF($7,''), $8::jsonb, now())
		ON CONFLICT (platform_account_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			primary_organisation_id = EXCLUDED.primary_organisation_id,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			billing_reference = EXCLUDED.billing_reference,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, acct.DisplayName, primary, acct.Status, acct.EffectiveFrom, acct.EffectiveTo, acct.BillingReference, meta,
	)
	return err
}

func (r *PostgresRepository) UpsertPlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership) error {
	id := m.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	meta, _ := json.Marshal(m.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.platform_account_membership (
			platform_account_membership_id, platform_account_id, member_type, member_id,
			role, status, effective_from, effective_to, metadata, updated_at
		) VALUES ($1::uuid, $2::uuid, $3, $4, NULLIF($5,''), $6, $7, $8, $9::jsonb, now())
		ON CONFLICT (platform_account_membership_id) DO UPDATE SET
			role = EXCLUDED.role,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, m.PlatformAccountID, m.MemberType, m.MemberID, m.Role, m.Status, m.EffectiveFrom, m.EffectiveTo, meta,
	)
	return err
}

func (r *PostgresRepository) ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT platform_account_membership_id::text, platform_account_id::text, member_type, member_id,
			COALESCE(role,''), status, effective_from, effective_to
		FROM registry.platform_account_membership
		WHERE platform_account_id=$1::uuid
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)`, accountID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlatformAccountMembership
	for rows.Next() {
		var m domain.PlatformAccountMembership
		if err := rows.Scan(&m.ID, &m.PlatformAccountID, &m.MemberType, &m.MemberID,
			&m.Role, &m.Status, &m.EffectiveFrom, &m.EffectiveTo); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping) error {
	id := m.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	meta, _ := json.Marshal(m.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.tenant_organisation_mapping (
			tenant_organisation_mapping_id, tenant_id, organisation_id, mapping_role,
			is_default, status, effective_from, effective_to, provenance, metadata, updated_at
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10::jsonb, now())
		ON CONFLICT (tenant_organisation_mapping_id) DO UPDATE SET
			mapping_role = EXCLUDED.mapping_role,
			is_default = EXCLUDED.is_default,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			provenance = EXCLUDED.provenance,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, m.TenantID, m.OrganisationID, m.MappingRole, m.IsDefault, m.Status,
		m.EffectiveFrom, m.EffectiveTo, m.Provenance, meta,
	)
	return err
}

func (r *PostgresRepository) ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_organisation_mapping_id::text, tenant_id, organisation_id::text, mapping_role,
			is_default, status, effective_from, effective_to, provenance
		FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)`, tenantID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TenantOrganisationMapping
	for rows.Next() {
		var m domain.TenantOrganisationMapping
		if err := rows.Scan(&m.ID, &m.TenantID, &m.OrganisationID, &m.MappingRole,
			&m.IsDefault, &m.Status, &m.EffectiveFrom, &m.EffectiveTo, &m.Provenance); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertTenantLegalEntityMapping(ctx context.Context, m domain.TenantLegalEntityMapping) error {
	id := m.ID
	if id == "" {
		id = domain.NewUUIDv7()
	}
	meta, _ := json.Marshal(m.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO registry.tenant_legal_entity_mapping (
			tenant_legal_entity_mapping_id, tenant_id, legal_entity_id, mapping_role,
			is_default, status, effective_from, effective_to, provenance, metadata, updated_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, now())
		ON CONFLICT (tenant_legal_entity_mapping_id) DO UPDATE SET
			mapping_role = EXCLUDED.mapping_role,
			is_default = EXCLUDED.is_default,
			status = EXCLUDED.status,
			effective_from = EXCLUDED.effective_from,
			effective_to = EXCLUDED.effective_to,
			provenance = EXCLUDED.provenance,
			metadata = EXCLUDED.metadata,
			updated_at = now()`,
		id, m.TenantID, m.LegalEntityID, m.MappingRole, m.IsDefault, m.Status,
		m.EffectiveFrom, m.EffectiveTo, m.Provenance, meta,
	)
	return err
}

func (r *PostgresRepository) ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_legal_entity_mapping_id::text, tenant_id, legal_entity_id, mapping_role,
			is_default, status, effective_from, effective_to, provenance
		FROM registry.tenant_legal_entity_mapping
		WHERE tenant_id=$1
		  AND effective_from <= $2
		  AND (effective_to IS NULL OR effective_to > $2)`, tenantID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TenantLegalEntityMapping
	for rows.Next() {
		var m domain.TenantLegalEntityMapping
		if err := rows.Scan(&m.ID, &m.TenantID, &m.LegalEntityID, &m.MappingRole,
			&m.IsDefault, &m.Status, &m.EffectiveFrom, &m.EffectiveTo, &m.Provenance); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EnsureDefaultTenantLegalEntityMapping is idempotent: upserts the default
// mapping for (tenant, legal entity) and keeps tenants.legal_entity_id in sync.
func (r *PostgresRepository) EnsureDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, legalEntityID, provenance string, at time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, legalEntityID); err != nil {
		return err
	}

	// Clear other defaults for this tenant
	if _, err := tx.Exec(ctx, `
		UPDATE registry.tenant_legal_entity_mapping
		SET is_default = false, updated_at = now()
		WHERE tenant_id = $1 AND is_default = true`, tenantID); err != nil {
		return err
	}

	// Upsert by natural key: one active default per tenant — insert if missing
	var existing string
	err = tx.QueryRow(ctx, `
		SELECT tenant_legal_entity_mapping_id::text
		FROM registry.tenant_legal_entity_mapping
		WHERE tenant_id=$1 AND legal_entity_id=$2 AND status='ACTIVE'
		LIMIT 1`, tenantID, legalEntityID).Scan(&existing)
	if err == pgx.ErrNoRows {
		if _, err = tx.Exec(ctx, `
			INSERT INTO registry.tenant_legal_entity_mapping (
				tenant_id, legal_entity_id, mapping_role, is_default, status,
				effective_from, provenance
			) VALUES ($1, $2, 'DEFAULT', true, 'ACTIVE', $3, $4)`,
			tenantID, legalEntityID, at, provenance); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if _, err = tx.Exec(ctx, `
			UPDATE registry.tenant_legal_entity_mapping
			SET is_default = true, mapping_role = 'DEFAULT', status = 'ACTIVE',
			    provenance = $2, updated_at = now()
			WHERE tenant_legal_entity_mapping_id = $1::uuid`, existing, provenance); err != nil {
			return err
		}
	}

	// Compatibility projection
	if _, err = tx.Exec(ctx, `
		UPDATE tenants SET legal_entity_id = $2, updated_at = now() WHERE tenant_id = $1`,
		tenantID, legalEntityID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
