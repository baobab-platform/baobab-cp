// ADR-BCP-018 organisation persistence on PostgresRepository (pgx pool).
// Schema: registry.* tables from migration 000045. Rows are keyed by uuid;
// this file converts to and from the opaque contract ids at the boundary.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
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
	if len(ev.References) == 0 || ev.VerifiedBy == "" || ev.VerifiedAt.IsZero() {
		return errors.New("verification requires evidence references, verified_by and verified_at")
	}
	return nil
}

// ensureByNaturalKey inserts a row and, when the live natural key already
// exists, returns the existing row's uuid instead. The existing row is never
// modified, so replays converge.
func (r *PostgresRepository) ensureByNaturalKey(ctx context.Context, insert string, insertArgs []any, lookup string, lookupArgs []any) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, insert, insertArgs...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = r.pool.QueryRow(ctx, lookup, lookupArgs...).Scan(&id)
	}
	return id, err
}

// --- Organisation --------------------------------------------------------

func (r *PostgresRepository) EnsureOrganisation(ctx context.Context, org domain.Organisation) (bool, error) {
	if err := org.Validate(); err != nil {
		return false, err
	}
	trading, err := jsonOrDefault(org.TradingNames, "[]")
	if err != nil {
		return false, err
	}
	identifiers, err := jsonOrDefault(org.Identifiers, "[]")
	if err != nil {
		return false, err
	}
	addresses, err := jsonOrDefault(org.Addresses, "[]")
	if err != nil {
		return false, err
	}
	evidence, err := jsonOrDefault(org.EvidenceReferences, "[]")
	if err != nil {
		return false, err
	}
	meta, err := jsonOrDefault(org.Metadata, "{}")
	if err != nil {
		return false, err
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO registry.organisation_profile (
			canonical_entity_id, display_name, official_name, trading_names,
			organisation_form, jurisdiction, verification_state, source_authority,
			status, effective_from, effective_to, identifiers, addresses,
			evidence_references, metadata
		) VALUES (
			$1::uuid, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11,
			$12::jsonb, $13::jsonb, $14::jsonb, $15::jsonb
		)
		ON CONFLICT (canonical_entity_id) DO NOTHING`,
		org.CanonicalEntityID, org.DisplayName, nullable(org.OfficialName), trading,
		nullable(string(org.OrganisationForm)), nullable(org.Jurisdiction), string(org.VerificationState),
		org.SourceAuthority, org.Status, org.EffectiveFrom, org.EffectiveTo,
		identifiers, addresses, evidence, meta,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

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

func (r *PostgresRepository) EnsureLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile) (bool, error) {
	if err := lep.Validate(); err != nil {
		return false, err
	}
	regIDs, err := jsonOrDefault(lep.RegistrationIdentifiers, "[]")
	if err != nil {
		return false, err
	}
	evidence, err := jsonOrDefault(lep.EvidenceReferences, "[]")
	if err != nil {
		return false, err
	}
	meta, err := jsonOrDefault(lep.Metadata, "{}")
	if err != nil {
		return false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, lep.LegalEntityID); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO registry.legal_entity_profile (
			legal_entity_id, organisation_id, legal_name, jurisdiction_of_incorporation,
			registration_identifiers, incorporation_date, legal_status, source_authority,
			verification_state, effective_from, effective_to, evidence_references,
			verified_by, verified_at, metadata
		) VALUES ($1, $2::uuid, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15::jsonb)
		ON CONFLICT (legal_entity_id) DO NOTHING`,
		lep.LegalEntityID, lep.OrganisationID, lep.LegalName, nullable(lep.JurisdictionOfIncorporation),
		regIDs, lep.IncorporationDate, lep.LegalStatus, lep.SourceAuthority,
		string(lep.VerificationState), lep.EffectiveFrom, lep.EffectiveTo, evidence,
		nullable(lep.VerifiedBy), lep.VerifiedAt, meta,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT organisation_id::text FROM registry.legal_entity_profile WHERE legal_entity_id=$1`, lep.LegalEntityID).Scan(&owner); err != nil {
			return false, err
		}
		if owner != lep.OrganisationID {
			return false, fmt.Errorf("%w: legal entity %s belongs to organisation %s", ErrOrganisationConflict, lep.LegalEntityID, owner)
		}
	}
	return tag.RowsAffected() == 1, tx.Commit(ctx)
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

func (r *PostgresRepository) VerifyLegalEntityProfile(ctx context.Context, legalEntityID string, ev Evidence) error {
	if err := validateEvidence(ev); err != nil {
		return err
	}
	evidence, err := json.Marshal(ev.References)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE registry.legal_entity_profile
		SET verification_state='VERIFIED', evidence_references=$2::jsonb,
			verified_by=$3, verified_at=$4, updated_at=now()
		WHERE legal_entity_id=$1
		  AND verification_state IN ('UNVERIFIED','PENDING_REVIEW','CONFLICTED')`,
		legalEntityID, evidence, ev.VerifiedBy, ev.VerifiedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("legal entity profile %s is not awaiting verification", legalEntityID)
	}
	return nil
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

func (r *PostgresRepository) EnsureCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship) (string, error) {
	if rel.ID == "" {
		rel.ID = domain.NewResourceID(domain.CorporateRelationshipIDPrefix)
	}
	if err := rel.Validate(); err != nil {
		return "", err
	}
	id, err := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, rel.ID)
	if err != nil {
		return "", err
	}
	basis, err := parseIDs(domain.CorporateRelationshipIDPrefix, rel.BasisRelationshipIDs)
	if err != nil {
		return "", err
	}
	evidence, err := jsonOrDefault(rel.EvidenceReferences, "[]")
	if err != nil {
		return "", err
	}
	meta, err := jsonOrDefault(rel.Metadata, "{}")
	if err != nil {
		return "", err
	}
	got, err := r.ensureByNaturalKey(ctx, `
		INSERT INTO registry.corporate_relationship (
			corporate_relationship_id, source_organisation_id, target_organisation_id,
			relationship_type, ownership_percentage, control_basis, direct_or_derived,
			basis_relationship_ids, derived_at, derivation_version, verification_state, status,
			effective_from, effective_to, source_authority, evidence_references, verified_by,
			verified_at, classification, metadata
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8::uuid[], $9, $10, $11, $12,
			$13, $14, $15, $16::jsonb, $17, $18, $19, $20::jsonb
		)
		ON CONFLICT (source_organisation_id, target_organisation_id, relationship_type)
			WHERE status IN `+liveStatuses+` DO NOTHING
		RETURNING corporate_relationship_id::text`,
		[]any{id, rel.SourceOrganisationID, rel.TargetOrganisationID, string(rel.RelationshipType),
			rel.OwnershipPercentage, nullable(rel.ControlBasis), rel.DirectOrDerived, basis, rel.DerivedAt,
			nullable(rel.DerivationVersion), string(rel.VerificationState), rel.Status, rel.EffectiveFrom,
			rel.EffectiveTo, rel.SourceAuthority, evidence, nullable(rel.VerifiedBy), rel.VerifiedAt,
			nullable(rel.Classification), meta},
		`SELECT corporate_relationship_id::text FROM registry.corporate_relationship
		 WHERE source_organisation_id=$1::uuid AND target_organisation_id=$2::uuid
		   AND relationship_type=$3 AND status IN `+liveStatuses,
		[]any{rel.SourceOrganisationID, rel.TargetOrganisationID, string(rel.RelationshipType)},
	)
	if err != nil {
		return "", err
	}
	return domain.FormatResourceID(domain.CorporateRelationshipIDPrefix, got)
}

func (r *PostgresRepository) VerifyCorporateRelationship(ctx context.Context, id string, ev Evidence) error {
	if err := validateEvidence(ev); err != nil {
		return err
	}
	row, err := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, id)
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(ev.References)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE registry.corporate_relationship
		SET verification_state='VERIFIED', status='ACTIVE', evidence_references=$2::jsonb,
			verified_by=$3, verified_at=$4, updated_at=now()
		WHERE corporate_relationship_id=$1::uuid
		  AND status IN ('PENDING','ACTIVE')
		  AND verification_state IN ('UNVERIFIED','PENDING_REVIEW','CONFLICTED')`,
		row, evidence, ev.VerifiedBy, ev.VerifiedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("corporate relationship %s is not awaiting verification", id)
	}
	return nil
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

func (r *PostgresRepository) ListCorporateControlAncestry(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateRelationship, error) {
	// UNION (not UNION ALL) plus the depth bound terminates on cycles.
	return r.queryCorporateRelationships(ctx, `
		WITH RECURSIVE consequential AS (
			SELECT corporate_relationship_id, source_organisation_id, target_organisation_id
			FROM registry.corporate_relationship
			WHERE relationship_type IN ('OWNS','CONTROLS') AND verification_state='VERIFIED'
			  AND status='ACTIVE' AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		), ancestry(id, source, depth) AS (
			SELECT corporate_relationship_id, source_organisation_id, 1
			FROM consequential WHERE target_organisation_id = $1::uuid
			UNION
			SELECT c.corporate_relationship_id, c.source_organisation_id, a.depth + 1
			FROM consequential c JOIN ancestry a ON c.target_organisation_id = a.source
			WHERE a.depth < $3
		)
		SELECT `+corporateRelationshipColumns+`
		FROM registry.corporate_relationship
		WHERE corporate_relationship_id IN (SELECT id FROM ancestry)
		ORDER BY effective_from, corporate_relationship_id`,
		organisationID, at, domain.MaxCorporateControlDepth)
}

// --- CorporateGroup ------------------------------------------------------

func (r *PostgresRepository) CreateCorporateGroup(ctx context.Context, g domain.CorporateGroup) error {
	id, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, g.ID)
	if err != nil {
		return err
	}
	if g.DisplayName == "" || g.GroupingPolicy == "" || g.Status == "" || g.EffectiveFrom.IsZero() {
		return errors.New("corporate group: display_name, grouping_policy, status and effective_from are required")
	}
	meta, err := jsonOrDefault(g.Metadata, "{}")
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO registry.corporate_group (
			corporate_group_id, display_name, root_organisation_id, status, grouping_policy,
			effective_from, effective_to, classification, metadata
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT (corporate_group_id) DO NOTHING`,
		id, g.DisplayName, nullable(g.RootOrganisationID), g.Status, g.GroupingPolicy,
		g.EffectiveFrom, g.EffectiveTo, nullable(g.Classification), meta)
	return err
}

func (r *PostgresRepository) EnsureCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership) (string, error) {
	if m.ID == "" {
		m.ID = domain.NewResourceID(domain.CorporateGroupMembershipIDPrefix)
	}
	if err := m.Validate(); err != nil {
		return "", err
	}
	id, err := domain.ParseResourceID(domain.CorporateGroupMembershipIDPrefix, m.ID)
	if err != nil {
		return "", err
	}
	group, err := domain.ParseResourceID(domain.CorporateGroupIDPrefix, m.CorporateGroupID)
	if err != nil {
		return "", err
	}
	basis, err := parseIDs(domain.CorporateRelationshipIDPrefix, m.BasisRelationshipIDs)
	if err != nil {
		return "", err
	}
	meta, err := jsonOrDefault(m.Metadata, "{}")
	if err != nil {
		return "", err
	}
	got, err := r.ensureByNaturalKey(ctx, `
		INSERT INTO registry.corporate_group_membership (
			corporate_group_membership_id, corporate_group_id, organisation_id, group_role,
			basis_relationship_ids, manual_basis_reference, status, effective_from, effective_to,
			derived_at, derivation_version, metadata
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid[], $6, $7, $8, $9, $10, $11, $12::jsonb)
		ON CONFLICT (corporate_group_id, organisation_id) WHERE status IN `+liveStatuses+` DO NOTHING
		RETURNING corporate_group_membership_id::text`,
		[]any{id, group, m.OrganisationID, nullable(m.GroupRole), basis, nullable(m.ManualBasisReference),
			m.Status, m.EffectiveFrom, m.EffectiveTo, m.DerivedAt, m.DerivationVersion, meta},
		`SELECT corporate_group_membership_id::text FROM registry.corporate_group_membership
		 WHERE corporate_group_id=$1::uuid AND organisation_id=$2::uuid AND status IN `+liveStatuses,
		[]any{group, m.OrganisationID},
	)
	if err != nil {
		return "", err
	}
	return domain.FormatResourceID(domain.CorporateGroupMembershipIDPrefix, got)
}

func (r *PostgresRepository) ListCorporateGroupMemberships(ctx context.Context, organisationID string, at time.Time) ([]domain.CorporateGroupMembership, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT corporate_group_membership_id::text, corporate_group_id::text, organisation_id::text,
			COALESCE(group_role,''), basis_relationship_ids::text[], COALESCE(manual_basis_reference,''),
			status, effective_from, effective_to, derived_at, derivation_version, metadata
		FROM registry.corporate_group_membership
		WHERE organisation_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, corporate_group_membership_id`, organisationID, at)
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

func (r *PostgresRepository) EnsurePlatformRelationship(ctx context.Context, rel domain.PlatformRelationship) (string, error) {
	if rel.ID == "" {
		rel.ID = domain.NewResourceID(domain.PlatformRelationshipIDPrefix)
	}
	if err := rel.Validate(); err != nil {
		return "", err
	}
	id, err := domain.ParseResourceID(domain.PlatformRelationshipIDPrefix, rel.ID)
	if err != nil {
		return "", err
	}
	basis, err := rowID(domain.CorporateRelationshipIDPrefix, rel.BasisRelationshipID)
	if err != nil {
		return "", err
	}
	evidence, err := jsonOrDefault(rel.EvidenceReferences, "[]")
	if err != nil {
		return "", err
	}
	meta, err := jsonOrDefault(rel.Metadata, "{}")
	if err != nil {
		return "", err
	}
	got, err := r.ensureByNaturalKey(ctx, `
		INSERT INTO registry.platform_relationship (
			platform_relationship_id, platform_id, organisation_id, relationship_type,
			verification_state, status, effective_from, effective_to, basis_relationship_id,
			admission_decision_id, source_authority, evidence_references, verified_by, verified_at,
			classification, metadata
		) VALUES (
			$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9::uuid, $10, $11, $12::jsonb, $13, $14, $15, $16::jsonb
		)
		ON CONFLICT (organisation_id, platform_id, relationship_type) WHERE status IN `+liveStatuses+` DO NOTHING
		RETURNING platform_relationship_id::text`,
		[]any{id, rel.PlatformID, rel.OrganisationID, string(rel.RelationshipType),
			string(rel.VerificationState), rel.Status, rel.EffectiveFrom, rel.EffectiveTo, basis,
			nullable(rel.AdmissionDecisionID), rel.SourceAuthority, evidence, nullable(rel.VerifiedBy),
			rel.VerifiedAt, nullable(rel.Classification), meta},
		`SELECT platform_relationship_id::text FROM registry.platform_relationship
		 WHERE organisation_id=$1::uuid AND platform_id=$2 AND relationship_type=$3 AND status IN `+liveStatuses,
		[]any{rel.OrganisationID, rel.PlatformID, string(rel.RelationshipType)},
	)
	if err != nil {
		return "", err
	}
	return domain.FormatResourceID(domain.PlatformRelationshipIDPrefix, got)
}

func (r *PostgresRepository) VerifyPlatformRelationship(ctx context.Context, id string, ev Evidence) error {
	if err := validateEvidence(ev); err != nil {
		return err
	}
	row, err := domain.ParseResourceID(domain.PlatformRelationshipIDPrefix, id)
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(ev.References)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE registry.platform_relationship
		SET verification_state='VERIFIED', status='ACTIVE', evidence_references=$2::jsonb,
			verified_by=$3, verified_at=$4, updated_at=now()
		WHERE platform_relationship_id=$1::uuid
		  AND status IN ('PENDING','ACTIVE')
		  AND verification_state IN ('UNVERIFIED','PENDING_REVIEW','CONFLICTED')`,
		row, evidence, ev.VerifiedBy, ev.VerifiedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("platform relationship %s is not awaiting verification", id)
	}
	return nil
}

func (r *PostgresRepository) ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT platform_relationship_id::text, platform_id, organisation_id::text, relationship_type,
			verification_state, status, effective_from, effective_to,
			COALESCE(basis_relationship_id::text,''), COALESCE(admission_decision_id,''), source_authority,
			evidence_references, COALESCE(verified_by,''), verified_at, COALESCE(classification,''), metadata
		FROM registry.platform_relationship
		WHERE organisation_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, platform_relationship_id`, organisationID, at)
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
		  AND verification_state='VERIFIED' AND status='ACTIVE'
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

func (r *PostgresRepository) CreatePlatformAccount(ctx context.Context, acct domain.PlatformAccount) error {
	id, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, acct.ID)
	if err != nil {
		return err
	}
	if acct.DisplayName == "" || acct.Status == "" || acct.EffectiveFrom.IsZero() {
		return errors.New("platform account: display_name, status and effective_from are required")
	}
	contracts, err := jsonOrDefault(acct.ContractReferences, "[]")
	if err != nil {
		return err
	}
	meta, err := jsonOrDefault(acct.Metadata, "{}")
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO registry.platform_account (
			platform_account_id, display_name, primary_organisation_id, status, contract_references,
			billing_profile_reference, support_profile_reference, effective_from, effective_to, metadata
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5::jsonb, $6, $7, $8, $9, $10::jsonb)
		ON CONFLICT (platform_account_id) DO NOTHING`,
		id, acct.DisplayName, nullable(acct.PrimaryOrganisationID), acct.Status, contracts,
		nullable(acct.BillingProfileReference), nullable(acct.SupportProfileReference),
		acct.EffectiveFrom, acct.EffectiveTo, meta)
	return err
}

func (r *PostgresRepository) EnsurePlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership) (string, error) {
	if m.ID == "" {
		m.ID = domain.NewResourceID(domain.PlatformAccountMembershipIDPrefix)
	}
	id, err := domain.ParseResourceID(domain.PlatformAccountMembershipIDPrefix, m.ID)
	if err != nil {
		return "", err
	}
	account, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, m.PlatformAccountID)
	if err != nil {
		return "", err
	}
	if m.OrganisationID == "" || m.AccountRole == "" || m.Status == "" || m.EffectiveFrom.IsZero() {
		return "", errors.New("platform account membership: organisation_id, account_role, status and effective_from are required")
	}
	meta, err := jsonOrDefault(m.Metadata, "{}")
	if err != nil {
		return "", err
	}
	got, err := r.ensureByNaturalKey(ctx, `
		INSERT INTO registry.platform_account_membership (
			platform_account_membership_id, platform_account_id, organisation_id, account_role,
			status, effective_from, effective_to, evidence_reference, metadata
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT (platform_account_id, organisation_id, account_role) WHERE status IN `+liveStatuses+` DO NOTHING
		RETURNING platform_account_membership_id::text`,
		[]any{id, account, m.OrganisationID, m.AccountRole, m.Status, m.EffectiveFrom, m.EffectiveTo,
			nullable(m.EvidenceReference), meta},
		`SELECT platform_account_membership_id::text FROM registry.platform_account_membership
		 WHERE platform_account_id=$1::uuid AND organisation_id=$2::uuid AND account_role=$3 AND status IN `+liveStatuses,
		[]any{account, m.OrganisationID, m.AccountRole},
	)
	if err != nil {
		return "", err
	}
	return domain.FormatResourceID(domain.PlatformAccountMembershipIDPrefix, got)
}

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

func (r *PostgresRepository) EnsureTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping) (string, error) {
	if m.ID == "" {
		m.ID = domain.NewResourceID(domain.TenantOrganisationMappingIDPrefix)
	}
	id, err := domain.ParseResourceID(domain.TenantOrganisationMappingIDPrefix, m.ID)
	if err != nil {
		return "", err
	}
	if m.TenantID == "" || m.OrganisationID == "" || m.MappingRole == "" || m.Status == "" || m.Provenance == "" || m.EffectiveFrom.IsZero() {
		return "", errors.New("tenant organisation mapping: tenant_id, organisation_id, mapping_role, status, provenance and effective_from are required")
	}
	meta, err := jsonOrDefault(m.Metadata, "{}")
	if err != nil {
		return "", err
	}
	got, err := r.ensureByNaturalKey(ctx, `
		INSERT INTO registry.tenant_organisation_mapping (
			tenant_organisation_mapping_id, tenant_id, organisation_id, mapping_role,
			status, effective_from, effective_to, provenance, metadata
		) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT (tenant_id, organisation_id, mapping_role) WHERE status IN `+liveStatuses+` DO NOTHING
		RETURNING tenant_organisation_mapping_id::text`,
		[]any{id, m.TenantID, m.OrganisationID, m.MappingRole, m.Status, m.EffectiveFrom, m.EffectiveTo, m.Provenance, meta},
		`SELECT tenant_organisation_mapping_id::text FROM registry.tenant_organisation_mapping
		 WHERE tenant_id=$1 AND organisation_id=$2::uuid AND mapping_role=$3 AND status IN `+liveStatuses,
		[]any{m.TenantID, m.OrganisationID, m.MappingRole},
	)
	if err != nil {
		return "", err
	}
	return domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, got)
}

func (r *PostgresRepository) ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_organisation_mapping_id::text, tenant_id, organisation_id::text, mapping_role,
			status, effective_from, effective_to, provenance, metadata
		FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)
		ORDER BY effective_from, tenant_organisation_mapping_id`, tenantID, at)
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

func (r *PostgresRepository) EnsureDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, legalEntityID, provenance string, at time.Time) (string, error) {
	if tenantID == "" || legalEntityID == "" || provenance == "" {
		return "", errors.New("default tenant legal entity mapping: tenant_id, legal_entity_id and provenance are required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Serialise concurrent callers for this tenant.
	var current string
	if err := tx.QueryRow(ctx, `SELECT legal_entity_id FROM tenants WHERE tenant_id=$1 FOR UPDATE`, tenantID).Scan(&current); err != nil {
		return "", fmt.Errorf("lock tenant %s: %w", tenantID, err)
	}
	var id, defaultLE string
	err = tx.QueryRow(ctx, `
		SELECT tenant_legal_entity_mapping_id::text, legal_entity_id
		FROM registry.tenant_legal_entity_mapping
		WHERE tenant_id=$1 AND mapping_role='DEFAULT' AND status IN `+liveStatuses, tenantID).Scan(&id, &defaultLE)
	switch {
	case err == nil && defaultLE == legalEntityID && current == legalEntityID:
		// Replay: already the default and already projected.
		return domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, id)
	case err == nil && defaultLE != legalEntityID:
		// End the previous default; history stays queryable.
		if _, err := tx.Exec(ctx, `
			UPDATE registry.tenant_legal_entity_mapping
			SET status='ENDED', effective_to=GREATEST($2, effective_from), updated_at=now()
			WHERE tenant_legal_entity_mapping_id=$1::uuid`, id, at); err != nil {
			return "", err
		}
		id = ""
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return "", err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, legalEntityID); err != nil {
		return "", err
	}
	if id == "" {
		// An ADDITIONAL mapping for the same legal entity is promoted rather
		// than duplicated; otherwise a new DEFAULT row is inserted.
		err = tx.QueryRow(ctx, `
			UPDATE registry.tenant_legal_entity_mapping
			SET mapping_role='DEFAULT', updated_at=now()
			WHERE tenant_id=$1 AND legal_entity_id=$2 AND status IN `+liveStatuses+`
			RETURNING tenant_legal_entity_mapping_id::text`, tenantID, legalEntityID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `
				INSERT INTO registry.tenant_legal_entity_mapping (
					tenant_id, legal_entity_id, mapping_role, status, effective_from, provenance
				) VALUES ($1, $2, 'DEFAULT', 'ACTIVE', $3, $4)
				RETURNING tenant_legal_entity_mapping_id::text`, tenantID, legalEntityID, at, provenance).Scan(&id)
		}
		if err != nil {
			return "", err
		}
	}
	if current != legalEntityID {
		if _, err := tx.Exec(ctx, `UPDATE tenants SET legal_entity_id=$2, updated_at=now() WHERE tenant_id=$1`, tenantID, legalEntityID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, id)
}
