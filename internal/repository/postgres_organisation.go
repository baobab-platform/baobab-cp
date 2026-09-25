// ADR-BCP-018 organisation persistence on PostgresRepository (pgx pool).
// Schema: registry.* tables from migration 000045. Rows are keyed by uuid;
// this file converts to and from the opaque contract ids at the boundary.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
)

var _ OrganisationRepository = (*PostgresRepository)(nil)

// liveStatuses must match the partial unique indexes in migration 000045.
const liveStatuses = `('PENDING','ACTIVE','SUSPENDED')`

func jsonOrDefault(v any, empty string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []byte(empty), nil
	}
	return b, nil
}

func decodeJSON(raw []byte, into any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, into)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// rowID converts an optional contract id into a row uuid (nil when empty).
func rowID(prefix, id string) (any, error) {
	if id == "" {
		return nil, nil
	}
	return domain.ParseResourceID(prefix, id)
}

func formatIDs(prefix string, uuids []string) ([]string, error) {
	out := make([]string, 0, len(uuids))
	for _, u := range uuids {
		id, err := domain.FormatResourceID(prefix, u)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func parseIDs(prefix string, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		u, err := domain.ParseResourceID(prefix, id)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func validateEvidence(ev Evidence) error {
	if len(ev.References) == 0 || ev.VerifiedAt.IsZero() {
		return errors.New("verification requires evidence references and verified_at")
	}
	return nil
}

// ensureByNaturalKey inserts a row and, when the live natural key already
// exists, returns the existing row's uuid instead. The existing row is never
// modified, so replays converge.
func (r *PostgresRepository) GetOrganisation(ctx context.Context, canonicalEntityID string) (*domain.Organisation, error) {
	var o domain.Organisation
	var vs, form string
	var trading, identifiers, addresses, evidence, meta []byte
	err := r.pool.QueryRow(ctx, `
		SELECT canonical_entity_id::text, display_name, COALESCE(official_name,''), trading_names,
			COALESCE(organisation_form,''), COALESCE(jurisdiction,''), verification_state,
			source_authority, status, effective_from, effective_to, identifiers, addresses,
			evidence_references, metadata
		FROM registry.organisation_profile WHERE canonical_entity_id=$1::uuid`, canonicalEntityID,
	).Scan(&o.CanonicalEntityID, &o.DisplayName, &o.OfficialName, &trading, &form, &o.Jurisdiction,
		&vs, &o.SourceAuthority, &o.Status, &o.EffectiveFrom, &o.EffectiveTo, &identifiers, &addresses,
		&evidence, &meta)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.VerificationState = domain.VerificationState(vs)
	o.OrganisationForm = domain.OrganisationForm(form)
	if err := errors.Join(decodeJSON(trading, &o.TradingNames), decodeJSON(identifiers, &o.Identifiers),
		decodeJSON(addresses, &o.Addresses), decodeJSON(evidence, &o.EvidenceReferences), decodeJSON(meta, &o.Metadata)); err != nil {
		return nil, err
	}
	return &o, nil
}

// --- LegalEntityProfile --------------------------------------------------

const legalEntityProfileColumns = `legal_entity_id, organisation_id::text, legal_name,
	COALESCE(jurisdiction_of_incorporation,''), registration_identifiers, incorporation_date,
	legal_status, source_authority, verification_state, effective_from, effective_to,
	evidence_references, COALESCE(verified_by,''), verified_at, metadata`

func scanLegalEntityProfile(row pgx.Row) (domain.LegalEntityProfile, error) {
	var p domain.LegalEntityProfile
	var vs string
	var regIDs, evidence, meta []byte
	if err := row.Scan(&p.LegalEntityID, &p.OrganisationID, &p.LegalName, &p.JurisdictionOfIncorporation,
		&regIDs, &p.IncorporationDate, &p.LegalStatus, &p.SourceAuthority, &vs, &p.EffectiveFrom,
		&p.EffectiveTo, &evidence, &p.VerifiedBy, &p.VerifiedAt, &meta); err != nil {
		return p, err
	}
	p.VerificationState = domain.VerificationState(vs)
	return p, errors.Join(decodeJSON(regIDs, &p.RegistrationIdentifiers),
		decodeJSON(evidence, &p.EvidenceReferences), decodeJSON(meta, &p.Metadata))
}

func (r *PostgresRepository) GetLegalEntityProfile(ctx context.Context, legalEntityID string) (*domain.LegalEntityProfile, error) {
	p, err := scanLegalEntityProfile(r.pool.QueryRow(ctx,
		`SELECT `+legalEntityProfileColumns+` FROM registry.legal_entity_profile WHERE legal_entity_id=$1`, legalEntityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PostgresRepository) ListLegalEntityProfilesByOrganisation(ctx context.Context, organisationID string) ([]domain.LegalEntityProfile, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+legalEntityProfileColumns+`
		FROM registry.legal_entity_profile WHERE organisation_id=$1::uuid ORDER BY legal_entity_id`, organisationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LegalEntityProfile
	for rows.Next() {
		p, err := scanLegalEntityProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- CorporateRelationship -----------------------------------------------

const corporateRelationshipColumns = `corporate_relationship_id::text, source_organisation_id::text,
	target_organisation_id::text, relationship_type, ownership_percentage::float8,
	COALESCE(control_basis,''), direct_or_derived, basis_relationship_ids::text[], derived_at,
	COALESCE(derivation_version,''), verification_state, status, effective_from, effective_to,
	source_authority, evidence_references, COALESCE(verified_by,''), verified_at,
	COALESCE(classification,''), metadata`

func scanCorporateRelationship(row pgx.Row) (domain.CorporateRelationship, error) {
	var c domain.CorporateRelationship
	var id, rt, vs string
	var basis []string
	var evidence, meta []byte
	if err := row.Scan(&id, &c.SourceOrganisationID, &c.TargetOrganisationID, &rt, &c.OwnershipPercentage,
		&c.ControlBasis, &c.DirectOrDerived, &basis, &c.DerivedAt, &c.DerivationVersion, &vs, &c.Status,
		&c.EffectiveFrom, &c.EffectiveTo, &c.SourceAuthority, &evidence, &c.VerifiedBy, &c.VerifiedAt,
		&c.Classification, &meta); err != nil {
		return c, err
	}
	c.RelationshipType = domain.CorporateRelationshipType(rt)
	c.VerificationState = domain.VerificationState(vs)
	var err error
	if c.ID, err = domain.FormatResourceID(domain.CorporateRelationshipIDPrefix, id); err != nil {
		return c, err
	}
	if len(basis) > 0 {
		if c.BasisRelationshipIDs, err = formatIDs(domain.CorporateRelationshipIDPrefix, basis); err != nil {
			return c, err
		}
	}
	return c, errors.Join(decodeJSON(evidence, &c.EvidenceReferences), decodeJSON(meta, &c.Metadata))
}

func (r *PostgresRepository) queryCorporateRelationships(ctx context.Context, sql string, args ...any) ([]domain.CorporateRelationship, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorporateRelationship
	for rows.Next() {
		c, err := scanCorporateRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListCorporateRelationshipsByOrganisation(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	return r.queryCorporateRelationships(ctx, `SELECT `+corporateRelationshipColumns+`
		FROM registry.corporate_relationship
		WHERE (source_organisation_id=$1::uuid OR target_organisation_id=$1::uuid)
		  AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, corporate_relationship_id`, organisationID, at)
}

// consequentialControlSQL selects, on alias cr, verified OWNS/CONTROLS facts
// in force at $2 (an ENDED fact still counts for dates before its end).
const consequentialControlSQL = `cr.relationship_type IN ('OWNS','CONTROLS') AND cr.verification_state = 'VERIFIED'
	AND (cr.status = 'ACTIVE' OR (cr.status = 'ENDED' AND cr.effective_to IS NOT NULL))
	AND cr.effective_from <= $2 AND (cr.effective_to IS NULL OR cr.effective_to > $2)`

// corporateControlWalkSQL walks consequential control from $1 along
// corporate relationships, matching "from" to the organisation reached so
// far and continuing at "next". Each step is an index lookup
// (corporate_relationship_target_idx or _source_idx), so the cost follows
// the organisation's own ancestry or descendants, not the size of the
// platform's corporate graph (section 173). UNION (not UNION ALL) plus the
// depth bound $3 terminates on cycles. The rows are then fetched by primary
// key: "= ANY (ARRAY(...))" keeps the planner from hash-joining the whole
// table on its (poor) estimate of a recursive CTE's size.
func corporateControlWalkSQL(from, next string) string {
	return `
		WITH RECURSIVE walk(id, reached, depth) AS (
			SELECT cr.corporate_relationship_id, cr.` + next + `, 1
			FROM registry.corporate_relationship cr
			WHERE cr.` + from + ` = $1::uuid AND ` + consequentialControlSQL + `
			UNION
			SELECT cr.corporate_relationship_id, cr.` + next + `, w.depth + 1
			FROM walk w JOIN registry.corporate_relationship cr ON cr.` + from + ` = w.reached
			WHERE w.depth < $3 AND ` + consequentialControlSQL + `
		)
		SELECT ` + corporateRelationshipColumns + `
		FROM registry.corporate_relationship
		WHERE corporate_relationship_id = ANY (ARRAY(SELECT id FROM walk))
		ORDER BY effective_from, corporate_relationship_id`
}

var (
	corporateControlAncestrySQL    = corporateControlWalkSQL("target_organisation_id", "source_organisation_id")
	corporateControlDescendantsSQL = corporateControlWalkSQL("source_organisation_id", "target_organisation_id")
)

func (r *PostgresRepository) ListCorporateControlAncestry(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	return r.queryCorporateRelationships(ctx, corporateControlAncestrySQL, organisationID, at, domain.MaxCorporateControlDepth)
}

// --- CorporateGroup ------------------------------------------------------

const corporateGroupMembershipColumns = `corporate_group_membership_id::text, corporate_group_id::text,
	organisation_id::text, COALESCE(group_role,''), basis_relationship_ids::text[],
	COALESCE(manual_basis_reference,''), status, effective_from, effective_to, derived_at,
	derivation_version, metadata`

func (r *PostgresRepository) ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error) {
	return r.queryCorporateGroupMemberships(ctx, `SELECT `+corporateGroupMembershipColumns+`
		FROM registry.corporate_group_membership
		WHERE organisation_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, corporate_group_membership_id`, organisationID, at)
}

func (r *PostgresRepository) queryCorporateGroupMemberships(ctx context.Context, sql string, args ...any) ([]domain.CorporateGroupMembership, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorporateGroupMembership
	for rows.Next() {
		var m domain.CorporateGroupMembership
		var id, group string
		var basis []string
		var meta []byte
		if err := rows.Scan(&id, &group, &m.OrganisationID, &m.GroupRole, &basis, &m.ManualBasisReference,
			&m.Status, &m.EffectiveFrom, &m.EffectiveTo, &m.DerivedAt, &m.DerivationVersion, &meta); err != nil {
			return nil, err
		}
		if m.ID, err = domain.FormatResourceID(domain.CorporateGroupMembershipIDPrefix, id); err != nil {
			return nil, err
		}
		if m.CorporateGroupID, err = domain.FormatResourceID(domain.CorporateGroupIDPrefix, group); err != nil {
			return nil, err
		}
		if m.BasisRelationshipIDs, err = formatIDs(domain.CorporateRelationshipIDPrefix, basis); err != nil {
			return nil, err
		}
		if err := decodeJSON(meta, &m.Metadata); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- PlatformRelationship ------------------------------------------------

const platformRelationshipColumns = `platform_relationship_id::text, platform_id, organisation_id::text, relationship_type,
			verification_state, status, effective_from, effective_to,
			COALESCE(basis_relationship_id::text,''), COALESCE(admission_decision_id,''), source_authority,
			evidence_references, COALESCE(verified_by,''), verified_at, COALESCE(classification,''), metadata`

func (r *PostgresRepository) ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error) {
	return r.queryPlatformRelationships(ctx, `
		SELECT `+platformRelationshipColumns+`
		FROM registry.platform_relationship
		WHERE organisation_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, platform_relationship_id`, organisationID, at)
}

func (r *PostgresRepository) ListLivePlatformRelationshipsByBasis(ctx context.Context, corporateRelationshipID string) ([]domain.PlatformRelationship, error) {
	basis, err := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, corporateRelationshipID)
	if err != nil {
		return nil, err
	}
	return r.queryPlatformRelationships(ctx, `
		SELECT `+platformRelationshipColumns+`
		FROM registry.platform_relationship
		WHERE basis_relationship_id=$1::uuid AND status IN `+liveStatuses+`
		ORDER BY effective_from, platform_relationship_id`, basis)
}

func (r *PostgresRepository) GetPlatformRelationship(ctx context.Context, id string) (*domain.PlatformRelationship, error) {
	row, err := domain.ParseResourceID(domain.PlatformRelationshipIDPrefix, id)
	if err != nil {
		return nil, err
	}
	got, err := r.queryPlatformRelationships(ctx, `SELECT `+platformRelationshipColumns+`
		FROM registry.platform_relationship WHERE platform_relationship_id=$1::uuid`, row)
	if err != nil || len(got) == 0 {
		return nil, err
	}
	return &got[0], nil
}

func (r *PostgresRepository) queryPlatformRelationships(ctx context.Context, sql string, args ...any) ([]domain.PlatformRelationship, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlatformRelationship
	for rows.Next() {
		var p domain.PlatformRelationship
		var id, rt, vs, basis string
		var evidence, meta []byte
		if err := rows.Scan(&id, &p.PlatformID, &p.OrganisationID, &rt, &vs, &p.Status, &p.EffectiveFrom,
			&p.EffectiveTo, &basis, &p.AdmissionDecisionID, &p.SourceAuthority, &evidence, &p.VerifiedBy,
			&p.VerifiedAt, &p.Classification, &meta); err != nil {
			return nil, err
		}
		p.RelationshipType = domain.PlatformRelationshipType(rt)
		p.VerificationState = domain.VerificationState(vs)
		if p.ID, err = domain.FormatResourceID(domain.PlatformRelationshipIDPrefix, id); err != nil {
			return nil, err
		}
		if basis != "" {
			if p.BasisRelationshipID, err = domain.FormatResourceID(domain.CorporateRelationshipIDPrefix, basis); err != nil {
				return nil, err
			}
		}
		if err := errors.Join(decodeJSON(evidence, &p.EvidenceReferences), decodeJSON(meta, &p.Metadata)); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListPlatformOwnerOrganisations(ctx context.Context, platformID string, at time.Time) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT organisation_id::text FROM registry.platform_relationship
		WHERE platform_id=$1 AND relationship_type='PLATFORM_OWNER'
		  AND verification_state='VERIFIED' AND (status='ACTIVE' OR (status='ENDED' AND effective_to IS NOT NULL))
		  AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, platformID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// --- PlatformAccount -----------------------------------------------------

func (r *PostgresRepository) ListPlatformAccountMemberships(ctx context.Context, accountID string, at time.Time) ([]domain.PlatformAccountMembership, error) {
	account, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, accountID)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT platform_account_membership_id::text, organisation_id::text, account_role, status,
			effective_from, effective_to, COALESCE(evidence_reference,''), metadata
		FROM registry.platform_account_membership
		WHERE platform_account_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, platform_account_membership_id`, account, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlatformAccountMembership
	for rows.Next() {
		m := domain.PlatformAccountMembership{PlatformAccountID: accountID}
		var id string
		var meta []byte
		if err := rows.Scan(&id, &m.OrganisationID, &m.AccountRole, &m.Status, &m.EffectiveFrom,
			&m.EffectiveTo, &m.EvidenceReference, &meta); err != nil {
			return nil, err
		}
		if m.ID, err = domain.FormatResourceID(domain.PlatformAccountMembershipIDPrefix, id); err != nil {
			return nil, err
		}
		if err := decodeJSON(meta, &m.Metadata); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Tenant mappings -----------------------------------------------------

func (r *PostgresRepository) ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error) {
	return r.queryTenantOrganisationMappings(ctx, `
		SELECT tenant_organisation_mapping_id::text, tenant_id, organisation_id::text, mapping_role,
			status, effective_from, effective_to, provenance, metadata
		FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, tenant_organisation_mapping_id`, tenantID, at)
}

func (r *PostgresRepository) ListLiveTenantOrganisationMappings(ctx context.Context, tenantID string) ([]domain.TenantOrganisationMapping, error) {
	return r.queryTenantOrganisationMappings(ctx, `
		SELECT tenant_organisation_mapping_id::text, tenant_id, organisation_id::text, mapping_role,
			status, effective_from, effective_to, provenance, metadata
		FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND status IN `+liveStatuses+`
		ORDER BY effective_from, tenant_organisation_mapping_id`, tenantID)
}

func (r *PostgresRepository) queryTenantOrganisationMappings(ctx context.Context, sql string, args ...any) ([]domain.TenantOrganisationMapping, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TenantOrganisationMapping
	for rows.Next() {
		var m domain.TenantOrganisationMapping
		var id string
		var meta []byte
		if err := rows.Scan(&id, &m.TenantID, &m.OrganisationID, &m.MappingRole, &m.Status,
			&m.EffectiveFrom, &m.EffectiveTo, &m.Provenance, &meta); err != nil {
			return nil, err
		}
		if m.ID, err = domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, id); err != nil {
			return nil, err
		}
		if err := decodeJSON(meta, &m.Metadata); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListTenantLegalEntityMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantLegalEntityMapping, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_legal_entity_mapping_id::text, tenant_id, legal_entity_id, mapping_role,
			status, effective_from, effective_to, provenance, metadata
		FROM registry.tenant_legal_entity_mapping
		WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, tenant_legal_entity_mapping_id`, tenantID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TenantLegalEntityMapping
	for rows.Next() {
		var m domain.TenantLegalEntityMapping
		var id string
		var meta []byte
		if err := rows.Scan(&id, &m.TenantID, &m.LegalEntityID, &m.MappingRole, &m.Status,
			&m.EffectiveFrom, &m.EffectiveTo, &m.Provenance, &meta); err != nil {
			return nil, err
		}
		if m.ID, err = domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, id); err != nil {
			return nil, err
		}
		if err := decodeJSON(meta, &m.Metadata); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
