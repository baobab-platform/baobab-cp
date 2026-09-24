// ADR-BCP-018 gate ORG-09 — persistence for organisation admission.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
)

// OrganisationAdmissionRepository is what organisation admission needs
// beyond OrganisationRepository.
type OrganisationAdmissionRepository interface {
	OrganisationRepository
	// FindOrganisationsByIdentifiers returns the organisations, other than
	// exclude, whose organisation or legal-entity profile carries any of the
	// governed identifiers, whatever their verification state. Names never
	// match (ADR-BCP-018 sections 99-100).
	FindOrganisationsByIdentifiers(ctx context.Context, ids []domain.OrganisationIdentifier, exclude string) ([]string, error)
	// RecordLegalEntityClaims records applicant evidence on a legal-entity
	// profile that is not yet VERIFIED: the legal name, jurisdiction and
	// identifiers (all unverified). A VERIFIED profile is never overwritten
	// by applicant claims; recorded is false then.
	RecordLegalEntityClaims(ctx context.Context, legalEntityID string, claims LegalEntityClaims, actor AuditActor) (recorded bool, err error)
	// VerifyOrganisation verifies an organisation profile with evidence.
	// Verifying an already VERIFIED profile changes nothing.
	VerifyOrganisation(ctx context.Context, organisationID string, ev Evidence, actor AuditActor) error
	// PlatformAccountExists reports whether the PlatformAccount exists.
	PlatformAccountExists(ctx context.Context, id string) (bool, error)
	// ReplacePrimaryTenantOrganisation makes organisationID the tenant's
	// PRIMARY organisation, ending (never deleting) a live PRIMARY mapping
	// to another organisation. It is a governed re-point: the previous
	// organisation is left untouched, never merged.
	ReplacePrimaryTenantOrganisation(ctx context.Context, tenantID, organisationID string, at time.Time, provenance string, actor AuditActor) (id string, err error)
}

// LegalEntityClaims is applicant evidence about a legal entity.
type LegalEntityClaims struct {
	LegalName    string
	Jurisdiction string
	Identifiers  []domain.OrganisationIdentifier
}

var _ OrganisationAdmissionRepository = (*PostgresRepository)(nil)

func (r *PostgresRepository) FindOrganisationsByIdentifiers(ctx context.Context, ids []domain.OrganisationIdentifier, exclude string) ([]string, error) {
	governed := domain.GovernedIdentifiers(ids)
	if len(governed) == 0 {
		return nil, nil
	}
	found := map[string]bool{}
	for _, id := range governed {
		// Stored values are normalised in SQL the same way
		// domain.NormaliseIdentifierValue does (normalisedIdentifierSQL);
		// jurisdictions must agree when both sides state one.
		rows, err := r.pool.Query(ctx, `
			WITH candidates AS (
				SELECT organisation_id AS org, registration_identifiers AS ids FROM registry.legal_entity_profile
				UNION ALL
				SELECT canonical_entity_id, identifiers FROM registry.organisation_profile
			)
			SELECT DISTINCT org::text FROM candidates c, jsonb_array_elements(c.ids) e
			WHERE e->>'type' = $1
			  AND `+normalisedIdentifierSQL+` = $2
			  AND ($3 = '' OR COALESCE(upper(e->>'issuing_jurisdiction'), '') IN ('', $3))
			  AND ($4 = '' OR org::text <> $4)`,
			id.Type, id.Value, id.IssuingJurisdiction, exclude)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var org string
			if err := rows.Scan(&org); err != nil {
				rows.Close()
				return nil, err
			}
			found[org] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(found))
	for org := range found {
		out = append(out, org)
	}
	return out, nil
}

func (r *PostgresRepository) RecordLegalEntityClaims(ctx context.Context, legalEntityID string, claims LegalEntityClaims, actor AuditActor) (bool, error) {
	if strings.TrimSpace(claims.LegalName) == "" {
		return false, errors.New("legal entity claims require a legal name")
	}
	ids, err := json.Marshal(domain.AsClaims(claims.Identifiers))
	if err != nil {
		return false, err
	}
	recorded := false
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var state, name, jurisdiction string
		var current []byte
		err := tx.QueryRow(ctx, `SELECT verification_state, legal_name, COALESCE(jurisdiction_of_incorporation,''), registration_identifiers
			FROM registry.legal_entity_profile WHERE legal_entity_id=$1 FOR UPDATE`, legalEntityID).Scan(&state, &name, &jurisdiction, &current)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("legal entity profile %s not found", legalEntityID)
		}
		if err != nil || state == string(domain.VerificationVerified) {
			return err
		}
		var existing []domain.OrganisationIdentifier
		if err := decodeJSON(current, &existing); err != nil {
			return err
		}
		sameIDs, err := jsonEqual(current, ids)
		if err != nil {
			return err
		}
		if name == claims.LegalName && jurisdiction == claims.Jurisdiction && sameIDs {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE registry.legal_entity_profile
			SET legal_name=$2, jurisdiction_of_incorporation=NULLIF($3,''), registration_identifiers=$4::jsonb, updated_at=now()
			WHERE legal_entity_id=$1`, legalEntityID, claims.LegalName, claims.Jurisdiction, ids); err != nil {
			return err
		}
		recorded = true
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "legal_entity_profile.claims_recorded", Target: "legal-entity/" + legalEntityID,
			AuditPayload: map[string]any{"verification_state": state, "legal_name": claims.LegalName,
				"previous_legal_name": name, "jurisdiction_of_incorporation": claims.Jurisdiction,
				"registration_identifiers": domain.AsClaims(claims.Identifiers), "previous_registration_identifiers": existing},
		})
	})
	return recorded, err
}

func jsonEqual(a, b []byte) (bool, error) {
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false, err
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya), nil
}

func (r *PostgresRepository) VerifyOrganisation(ctx context.Context, organisationID string, ev Evidence, actor AuditActor) error {
	if err := validateEvidence(ev); err != nil {
		return err
	}
	evidence, err := json.Marshal(ev.References)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var state string
		err := tx.QueryRow(ctx, `SELECT verification_state FROM registry.organisation_profile WHERE canonical_entity_id=$1::uuid FOR UPDATE`,
			organisationID).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("organisation %s has no profile", organisationID)
		}
		if err != nil || state == string(domain.VerificationVerified) {
			return err
		}
		switch domain.VerificationState(state) {
		case domain.VerificationRejected, domain.VerificationExpired:
			return fmt.Errorf("organisation %s is %s; verification cannot overturn it", organisationID, state)
		}
		if _, err := tx.Exec(ctx, `UPDATE registry.organisation_profile SET verification_state='VERIFIED', evidence_references=$2::jsonb, updated_at=now()
			WHERE canonical_entity_id=$1::uuid`, organisationID, evidence); err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation.verified", Target: "organisation/" + organisationID,
			AggregateType: "organisation", AggregateID: organisationID, EventType: events.OrganisationVerified,
			Data: map[string]any{"organisation_id": organisationID, "verified_at": events.Timestamp(ev.VerifiedAt),
				"evidence_reference_count": len(ev.References)},
			AuditPayload: map[string]any{"evidence_references": ev.References, "reason": ev.Reason, "previous_state": state},
		})
	})
}

func (r *PostgresRepository) PlatformAccountExists(ctx context.Context, id string) (bool, error) {
	row, err := domain.ParseResourceID(domain.PlatformAccountIDPrefix, id)
	if err != nil {
		return false, err
	}
	var exists bool
	err = r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM registry.platform_account WHERE platform_account_id=$1::uuid)`, row).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) ReplacePrimaryTenantOrganisation(ctx context.Context, tenantID, organisationID string, at time.Time, provenance string, actor AuditActor) (string, error) {
	if tenantID == "" || organisationID == "" || provenance == "" || at.IsZero() {
		return "", errors.New("replacing a primary organisation requires tenant, organisation, provenance and time")
	}
	var result string
	err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var row, org string
		err := tx.QueryRow(ctx, `
			SELECT tenant_organisation_mapping_id::text, organisation_id::text FROM registry.tenant_organisation_mapping
			WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION' AND status IN `+liveStatuses+` FOR UPDATE`, tenantID).Scan(&row, &org)
		switch {
		case err == nil && org == organisationID:
			result, err = domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, row)
			return err
		case err == nil:
			if _, err := tx.Exec(ctx, `UPDATE registry.tenant_organisation_mapping SET status='ENDED', effective_to=GREATEST(effective_from, $2), updated_at=now()
				WHERE tenant_organisation_mapping_id=$1::uuid`, row, at); err != nil {
				return err
			}
			ended, err := domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, row)
			if err != nil {
				return err
			}
			if err := r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
				AuditAction: "tenant_organisation_mapping.ended", Target: "tenant-organisation-mapping/" + ended, TenantID: tenantID,
				AuditPayload: map[string]any{"organisation_id": org, "replaced_by_organisation_id": organisationID, "provenance": provenance},
			}); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		m := domain.TenantOrganisationMapping{TenantID: tenantID, OrganisationID: organisationID, MappingRole: domain.TenantOrgRolePrimary,
			Status: domain.RelationshipStatusActive, EffectiveFrom: at, Provenance: provenance}
		var got string
		if err := tx.QueryRow(ctx, `
			INSERT INTO registry.tenant_organisation_mapping (tenant_id, organisation_id, mapping_role, status, effective_from, provenance)
			VALUES ($1, $2::uuid, 'PRIMARY_ORGANISATION', 'ACTIVE', $3, $4)
			RETURNING tenant_organisation_mapping_id::text`, tenantID, organisationID, at, provenance).Scan(&got); err != nil {
			return err
		}
		if result, err = domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, got); err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, TenantOrganisationMappingChange(got, result, m))
	})
	return result, err
}
