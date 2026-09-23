// Target path: baobab-platform/baobab-cp/internal/repository/organisation_postgres.go
//
// ADR-BCP-018 — Postgres implementation skeleton for OrganisationRepository.
//
// Wire this into the existing registry/postgres repository struct (same *sql.DB
// or pgx pool). Method bodies below are intentionally minimal and must be
// completed against live column names and the project's query helpers.
//
// Fail-closed rule: Get* methods return (nil, nil) when not found so callers
// treat absence as non-authoritative evidence.

package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// OrganisationPostgres implements OrganisationRepository using the registry schema.
// Embed or hold the same DB handle as the rest of the Control Plane registry.
type OrganisationPostgres struct {
	DB *sql.DB
}

// Compile-time check.
var _ OrganisationRepository = (*OrganisationPostgres)(nil)

func (r *OrganisationPostgres) UpsertOrganisation(ctx context.Context, org domain.Organisation) error {
	const q = `
INSERT INTO registry.organisation_profile (
    canonical_entity_id, display_name, official_name, trading_names,
    organisation_form, jurisdiction, verification_state, source_authority,
    status, effective_from, effective_to, metadata, updated_at
) VALUES (
    $1, $2, $3, COALESCE($4::jsonb, '[]'::jsonb),
    $5, $6, $7, $8,
    $9, $10, $11, COALESCE($12::jsonb, '{}'::jsonb), now()
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
    updated_at = now()`
	// trading_names / metadata JSON encoding left to project helpers
	_, err := r.DB.ExecContext(ctx, q,
		org.CanonicalEntityID, org.DisplayName, nullString(org.OfficialName), nil,
		nullString(string(org.OrganisationForm)), nullString(org.Jurisdiction),
		string(org.VerificationState), org.SourceAuthority,
		org.Status, org.EffectiveFrom, org.EffectiveTo, nil,
	)
	return err
}

func (r *OrganisationPostgres) GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error) {
	const q = `
SELECT canonical_entity_id, display_name, COALESCE(official_name, ''),
       organisation_form, jurisdiction, verification_state, source_authority,
       status, effective_from, effective_to
FROM registry.organisation_profile
WHERE canonical_entity_id = $1`
	var org domain.Organisation
	var form, jur sql.NullString
	var effectiveTo sql.NullTime
	err := r.DB.QueryRowContext(ctx, q, canonicalEntityID).Scan(
		&org.CanonicalEntityID, &org.DisplayName, &org.OfficialName,
		&form, &jur, &org.VerificationState, &org.SourceAuthority,
		&org.Status, &org.EffectiveFrom, &effectiveTo,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if form.Valid {
		org.OrganisationForm = domain.OrganisationForm(form.String)
	}
	if jur.Valid {
		org.Jurisdiction = jur.String
	}
	if effectiveTo.Valid {
		t := effectiveTo.Time
		org.EffectiveTo = &t
	}
	return &org, nil
}

func (r *OrganisationPostgres) UpsertLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) error {
	const q = `
INSERT INTO registry.legal_entity_profile (
    legal_entity_id, organisation_id, legal_name, jurisdiction_of_incorporation,
    legal_status, source_authority, verification_state,
    effective_from, effective_to, evidence_references, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10::jsonb, '[]'::jsonb), COALESCE($11::jsonb, '{}'::jsonb), now()
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
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		lep.LegalEntityID, lep.OrganisationID, lep.LegalName, nullString(lep.JurisdictionOfIncorporation),
		lep.LegalStatus, lep.SourceAuthority, string(lep.VerificationState),
		lep.EffectiveFrom, lep.EffectiveTo, nil, nil,
	)
	return err
}

func (r *OrganisationPostgres) GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error) {
	const q = `
SELECT legal_entity_id, organisation_id, legal_name,
       COALESCE(jurisdiction_of_incorporation, ''), legal_status,
       source_authority, verification_state, effective_from, effective_to
FROM registry.legal_entity_profile
WHERE legal_entity_id = $1`
	var lep domain.LegalEntityProfile
	var effectiveTo sql.NullTime
	err := r.DB.QueryRowContext(ctx, q, legalEntityID).Scan(
		&lep.LegalEntityID, &lep.OrganisationID, &lep.LegalName,
		&lep.JurisdictionOfIncorporation, &lep.LegalStatus,
		&lep.SourceAuthority, &lep.VerificationState, &lep.EffectiveFrom, &effectiveTo,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if effectiveTo.Valid {
		t := effectiveTo.Time
		lep.EffectiveTo = &t
	}
	return &lep, nil
}

func (r *OrganisationPostgres) ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error) {
	const q = `
SELECT legal_entity_id, organisation_id, legal_name,
       COALESCE(jurisdiction_of_incorporation, ''), legal_status,
       source_authority, verification_state, effective_from, effective_to
FROM registry.legal_entity_profile
WHERE organisation_id = $1
ORDER BY legal_entity_id`
	rows, err := r.DB.QueryContext(ctx, q, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LegalEntityProfile
	for rows.Next() {
		var lep domain.LegalEntityProfile
		var effectiveTo sql.NullTime
		if err := rows.Scan(
			&lep.LegalEntityID, &lep.OrganisationID, &lep.LegalName,
			&lep.JurisdictionOfIncorporation, &lep.LegalStatus,
			&lep.SourceAuthority, &lep.VerificationState, &lep.EffectiveFrom, &effectiveTo,
		); err != nil {
			return nil, err
		}
		if effectiveTo.Valid {
			t := effectiveTo.Time
			lep.EffectiveTo = &t
		}
		out = append(out, lep)
	}
	return out, rows.Err()
}

func (r *OrganisationPostgres) UpsertCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) error {
	const q = `
INSERT INTO registry.corporate_relationship (
    id, source_organisation_id, target_organisation_id, relationship_type,
    ownership_percentage, verification_state, status,
    effective_from, effective_to, evidence_references, source_authority, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10::jsonb, '[]'::jsonb), $11, COALESCE($12::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    source_organisation_id = EXCLUDED.source_organisation_id,
    target_organisation_id = EXCLUDED.target_organisation_id,
    relationship_type = EXCLUDED.relationship_type,
    ownership_percentage = EXCLUDED.ownership_percentage,
    verification_state = EXCLUDED.verification_state,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    evidence_references = EXCLUDED.evidence_references,
    source_authority = EXCLUDED.source_authority,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		rel.ID, rel.SourceOrganisationID, rel.TargetOrganisationID, string(rel.RelationshipType),
		rel.OwnershipPercentage, string(rel.VerificationState), rel.Status,
		rel.EffectiveFrom, rel.EffectiveTo, nil, rel.SourceAuthority, nil,
	)
	return err
}

func (r *OrganisationPostgres) ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	const q = `
SELECT id, source_organisation_id, target_organisation_id, relationship_type,
       ownership_percentage, verification_state, status,
       effective_from, effective_to, source_authority
FROM registry.corporate_relationship
WHERE (source_organisation_id = $1 OR target_organisation_id = $1)
  AND effective_from <= $2
  AND (effective_to IS NULL OR effective_to > $2)`
	rows, err := r.DB.QueryContext(ctx, q, organisationID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorporateRelationship
	for rows.Next() {
		var rel domain.CorporateRelationship
		var ownership sql.NullFloat64
		var effectiveTo sql.NullTime
		if err := rows.Scan(
			&rel.ID, &rel.SourceOrganisationID, &rel.TargetOrganisationID, &rel.RelationshipType,
			&ownership, &rel.VerificationState, &rel.Status,
			&rel.EffectiveFrom, &effectiveTo, &rel.SourceAuthority,
		); err != nil {
			return nil, err
		}
		if ownership.Valid {
			v := ownership.Float64
			rel.OwnershipPercentage = &v
		}
		if effectiveTo.Valid {
			t := effectiveTo.Time
			rel.EffectiveTo = &t
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (r *OrganisationPostgres) UpsertCorporateGroup(ctx context.Context, g domain.CorporateGroup) error {
	const q = `
INSERT INTO registry.corporate_group (
    id, display_name, root_organisation_id, status,
    effective_from, effective_to, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, COALESCE($7::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    root_organisation_id = EXCLUDED.root_organisation_id,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		g.ID, g.DisplayName, g.RootOrganisationID, g.Status,
		g.EffectiveFrom, g.EffectiveTo, nil,
	)
	return err
}

func (r *OrganisationPostgres) UpsertCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership) error {
	const q = `
INSERT INTO registry.corporate_group_membership (
    id, corporate_group_id, organisation_id, membership_type, status,
    effective_from, effective_to, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, COALESCE($8::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    corporate_group_id = EXCLUDED.corporate_group_id,
    organisation_id = EXCLUDED.organisation_id,
    membership_type = EXCLUDED.membership_type,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		m.ID, m.CorporateGroupID, m.OrganisationID, m.MembershipType, m.Status,
		m.EffectiveFrom, m.EffectiveTo, nil,
	)
	return err
}

func (r *OrganisationPostgres) ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error) {
	const q = `
SELECT id, corporate_group_id, organisation_id, membership_type, status,
       effective_from, effective_to
FROM registry.corporate_group_membership
WHERE organisation_id = $1
  AND effective_from <= $2
  AND (effective_to IS NULL OR effective_to > $2)`
	rows, err := r.DB.QueryContext(ctx, q, organisationID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorporateGroupMembership
	for rows.Next() {
		var m domain.CorporateGroupMembership
		var effectiveTo sql.NullTime
		if err := rows.Scan(
			&m.ID, &m.CorporateGroupID, &m.OrganisationID, &m.MembershipType, &m.Status,
			&m.EffectiveFrom, &effectiveTo,
		); err != nil {
			return nil, err
		}
		if effectiveTo.Valid {
			t := effectiveTo.Time
			m.EffectiveTo = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *OrganisationPostgres) UpsertPlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) error {
	const q = `
INSERT INTO registry.platform_relationship (
    id, organisation_id, relationship_type, verification_state, status,
    effective_from, effective_to, evidence_references, source_authority, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, COALESCE($8::jsonb, '[]'::jsonb), $9, COALESCE($10::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    organisation_id = EXCLUDED.organisation_id,
    relationship_type = EXCLUDED.relationship_type,
    verification_state = EXCLUDED.verification_state,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    evidence_references = EXCLUDED.evidence_references,
    source_authority = EXCLUDED.source_authority,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		rel.ID, rel.OrganisationID, string(rel.RelationshipType), string(rel.VerificationState), rel.Status,
		rel.EffectiveFrom, rel.EffectiveTo, nil, rel.SourceAuthority, nil,
	)
	return err
}

func (r *OrganisationPostgres) GetPlatformRelationship(ctx context.Context, organisationID string, at time.Time) (*domain.PlatformRelationship, error) {
	const q = `
SELECT id, organisation_id, relationship_type, verification_state, status,
       effective_from, effective_to, source_authority
FROM registry.platform_relationship
WHERE organisation_id = $1
  AND effective_from <= $2
  AND (effective_to IS NULL OR effective_to > $2)
ORDER BY effective_from DESC
LIMIT 1`
	var rel domain.PlatformRelationship
	var effectiveTo sql.NullTime
	err := r.DB.QueryRowContext(ctx, q, organisationID, at).Scan(
		&rel.ID, &rel.OrganisationID, &rel.RelationshipType, &rel.VerificationState, &rel.Status,
		&rel.EffectiveFrom, &effectiveTo, &rel.SourceAuthority,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if effectiveTo.Valid {
		t := effectiveTo.Time
		rel.EffectiveTo = &t
	}
	return &rel, nil
}

func (r *OrganisationPostgres) UpsertPlatformAccount(ctx context.Context, acct domain.PlatformAccount) error {
	const q = `
INSERT INTO registry.platform_account (
    id, display_name, primary_organisation_id, status,
    effective_from, effective_to, billing_reference, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, COALESCE($8::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    primary_organisation_id = EXCLUDED.primary_organisation_id,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    billing_reference = EXCLUDED.billing_reference,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		acct.ID, acct.DisplayName, nullString(acct.PrimaryOrganisationID), acct.Status,
		acct.EffectiveFrom, acct.EffectiveTo, nullString(acct.BillingReference), nil,
	)
	return err
}

func (r *OrganisationPostgres) UpsertPlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership) error {
	const q = `
INSERT INTO registry.platform_account_membership (
    id, platform_account_id, member_type, member_id, role, status,
    effective_from, effective_to, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    platform_account_id = EXCLUDED.platform_account_id,
    member_type = EXCLUDED.member_type,
    member_id = EXCLUDED.member_id,
    role = EXCLUDED.role,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		m.ID, m.PlatformAccountID, m.MemberType, m.MemberID, nullString(m.Role), m.Status,
		m.EffectiveFrom, m.EffectiveTo, nil,
	)
	return err
}

func (r *OrganisationPostgres) ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error) {
	const q = `
SELECT id, platform_account_id, member_type, member_id, COALESCE(role, ''), status,
       effective_from, effective_to
FROM registry.platform_account_membership
WHERE platform_account_id = $1
  AND effective_from <= $2
  AND (effective_to IS NULL OR effective_to > $2)`
	rows, err := r.DB.QueryContext(ctx, q, accountID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlatformAccountMembership
	for rows.Next() {
		var m domain.PlatformAccountMembership
		var effectiveTo sql.NullTime
		if err := rows.Scan(
			&m.ID, &m.PlatformAccountID, &m.MemberType, &m.MemberID, &m.Role, &m.Status,
			&m.EffectiveFrom, &effectiveTo,
		); err != nil {
			return nil, err
		}
		if effectiveTo.Valid {
			t := effectiveTo.Time
			m.EffectiveTo = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *OrganisationPostgres) UpsertTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping) error {
	const q = `
INSERT INTO registry.tenant_organisation_mapping (
    id, tenant_id, organisation_id, is_default, status,
    effective_from, effective_to, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, COALESCE($8::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    tenant_id = EXCLUDED.tenant_id,
    organisation_id = EXCLUDED.organisation_id,
    is_default = EXCLUDED.is_default,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		m.ID, m.TenantID, m.OrganisationID, m.IsDefault, m.Status,
		m.EffectiveFrom, m.EffectiveTo, nil,
	)
	return err
}

func (r *OrganisationPostgres) ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error) {
	const q = `
SELECT id, tenant_id, organisation_id, is_default, status, effective_from, effective_to
FROM registry.tenant_organisation_mapping
WHERE tenant_id = $1
  AND effective_from <= $2
  AND (effective_to IS NULL OR effective_to > $2)`
	rows, err := r.DB.QueryContext(ctx, q, tenantID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TenantOrganisationMapping
	for rows.Next() {
		var m domain.TenantOrganisationMapping
		var effectiveTo sql.NullTime
		if err := rows.Scan(&m.ID, &m.TenantID, &m.OrganisationID, &m.IsDefault, &m.Status, &m.EffectiveFrom, &effectiveTo); err != nil {
			return nil, err
		}
		if effectiveTo.Valid {
			t := effectiveTo.Time
			m.EffectiveTo = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *OrganisationPostgres) UpsertTenantLegalEntityMapping(ctx context.Context, m domain.TenantLegalEntityMapping) error {
	const q = `
INSERT INTO registry.tenant_legal_entity_mapping (
    id, tenant_id, legal_entity_id, is_default, status,
    effective_from, effective_to, metadata, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, COALESCE($8::jsonb, '{}'::jsonb), now()
)
ON CONFLICT (id) DO UPDATE SET
    tenant_id = EXCLUDED.tenant_id,
    legal_entity_id = EXCLUDED.legal_entity_id,
    is_default = EXCLUDED.is_default,
    status = EXCLUDED.status,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    metadata = EXCLUDED.metadata,
    updated_at = now()`
	_, err := r.DB.ExecContext(ctx, q,
		m.ID, m.TenantID, m.LegalEntityID, m.IsDefault, m.Status,
		m.EffectiveFrom, m.EffectiveTo, nil,
	)
	return err
}

func (r *OrganisationPostgres) ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error) {
	const q = `
SELECT id, tenant_id, legal_entity_id, is_default, status, effective_from, effective_to
FROM registry.tenant_legal_entity_mapping
WHERE tenant_id = $1
  AND effective_from <= $2
  AND (effective_to IS NULL OR effective_to > $2)`
	rows, err := r.DB.QueryContext(ctx, q, tenantID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TenantLegalEntityMapping
	for rows.Next() {
		var m domain.TenantLegalEntityMapping
		var effectiveTo sql.NullTime
		if err := rows.Scan(&m.ID, &m.TenantID, &m.LegalEntityID, &m.IsDefault, &m.Status, &m.EffectiveFrom, &effectiveTo); err != nil {
			return nil, err
		}
		if effectiveTo.Valid {
			t := effectiveTo.Time
			m.EffectiveTo = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetDefaultTenantLegalEntityMapping clears other defaults and updates the
// tenant compatibility projection. Must run in a transaction with the tenant update.
func (r *OrganisationPostgres) SetDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, mappingID string) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Clear existing defaults for tenant
	if _, err := tx.ExecContext(ctx, `
UPDATE registry.tenant_legal_entity_mapping
SET is_default = false, updated_at = now()
WHERE tenant_id = $1 AND is_default = true`, tenantID); err != nil {
		return err
	}

	// Set new default and read legal_entity_id
	var legalEntityID string
	if err := tx.QueryRowContext(ctx, `
UPDATE registry.tenant_legal_entity_mapping
SET is_default = true, updated_at = now()
WHERE id = $1 AND tenant_id = $2
RETURNING legal_entity_id`, mappingID, tenantID).Scan(&legalEntityID); err != nil {
		return err
	}

	// Compatibility projection: keep Tenant.LegalEntityID in sync.
	// Adjust table/column names to match live schema if different.
	if _, err := tx.ExecContext(ctx, `
UPDATE registry.tenant
SET legal_entity_id = $1
WHERE tenant_id = $2`, legalEntityID, tenantID); err != nil {
		return err
	}

	return tx.Commit()
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
