// ADR-BCP-018 section 13 first-party governance reconciliation: Shared's
// legal-entity registry is the governance source for first-party identities,
// and the Control Plane seeds and corrects its runtime records from it.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
)

// firstPartySourceAuthority marks facts established by Shared governance.
const firstPartySourceAuthority = "shared-governance"

func (r *PostgresRepository) ListTenantsByDefaultLegalEntity(ctx context.Context, legalEntityID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_id FROM registry.tenant_legal_entity_mapping
		WHERE legal_entity_id=$1 AND mapping_role='DEFAULT' AND status IN `+liveStatuses+`
		ORDER BY tenant_id`, legalEntityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ApplyFirstPartyGovernance(ctx context.Context, g FirstPartyGovernance, actor AuditActor) (GovernanceOutcome, error) {
	var out GovernanceOutcome
	if g.LegalEntityID == "" || g.LegalName == "" || g.EvidenceReference == "" || g.At.IsZero() {
		return out, errors.New("first-party governance: legal_entity_id, legal_name, evidence_reference and at are required")
	}
	evidence := []string{g.EvidenceReference}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return out, err
	}
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var legalName, source, state string
		var evidenceRaw []byte
		err := tx.QueryRow(ctx, `
			SELECT organisation_id::text, legal_name, source_authority, verification_state, evidence_references
			FROM registry.legal_entity_profile WHERE legal_entity_id=$1 FOR UPDATE`, g.LegalEntityID,
		).Scan(&out.OrganisationID, &legalName, &source, &state, &evidenceRaw)
		if errors.Is(err, pgx.ErrNoRows) {
			return r.seedFirstParty(ctx, tx, g, evidenceJSON, actor, &out)
		}
		if err != nil {
			return err
		}
		if state == string(domain.VerificationRejected) || state == string(domain.VerificationExpired) {
			out.Drift = append(out.Drift, GovernanceDrift{Field: "legal_entity_profile.verification_state", Observed: state,
				Governed: string(domain.VerificationVerified), Blocking: true,
				Reason: "a rejected or expired legal-entity verification is never overturned automatically; it needs governed review"})
			return r.recordDrift(ctx, tx, actor, g, out)
		}

		if legalName != g.LegalName {
			out.Drift = append(out.Drift, GovernanceDrift{Field: "legal_entity_profile.legal_name", Observed: legalName,
				Governed: g.LegalName, Reason: "Shared governance is authoritative for first-party legal names"})
			if _, err := tx.Exec(ctx, `UPDATE registry.legal_entity_profile SET legal_name=$2, updated_at=now() WHERE legal_entity_id=$1`,
				g.LegalEntityID, g.LegalName); err != nil {
				return err
			}
			out.Changes = append(out.Changes, "legal_entity_profile.legal_name corrected")
		}
		var current []string
		if err := json.Unmarshal(evidenceRaw, &current); err != nil {
			return err
		}
		switch {
		case state != string(domain.VerificationVerified):
			if _, err := tx.Exec(ctx, `
				UPDATE registry.legal_entity_profile
				SET verification_state='VERIFIED', source_authority=$2, evidence_references=$3::jsonb,
					verified_by=$4, verified_at=$5, updated_at=now()
				WHERE legal_entity_id=$1`, g.LegalEntityID, firstPartySourceAuthority, evidenceJSON, actor.ActorID, g.At); err != nil {
				return err
			}
			out.Changes = append(out.Changes, "legal_entity_profile verified from "+state)
			if err := r.recordOrganisationChange(ctx, tx, actor, legalEntityVerifiedChange(g, out.OrganisationID, evidence)); err != nil {
				return err
			}
		case source != firstPartySourceAuthority || !slices.Contains(current, g.EvidenceReference):
			// Already verified, but from another source or an older registry
			// revision: re-anchor it to the current governance record.
			if _, err := tx.Exec(ctx, `
				UPDATE registry.legal_entity_profile
				SET source_authority=$2, evidence_references=$3::jsonb, verified_by=$4, verified_at=$5, updated_at=now()
				WHERE legal_entity_id=$1`, g.LegalEntityID, firstPartySourceAuthority, evidenceJSON, actor.ActorID, g.At); err != nil {
				return err
			}
			out.Changes = append(out.Changes, "legal_entity_profile evidence re-anchored to the current registry revision")
		}

		if err := r.reconcileFirstPartyOrganisation(ctx, tx, g, evidenceJSON, actor, &out); err != nil {
			return err
		}
		if len(out.Changes) == 0 && len(out.Drift) == 0 {
			return nil
		}
		return r.recordDrift(ctx, tx, actor, g, out)
	})
	return out, err
}

// seedFirstParty creates the Organisation and LegalEntityProfile for a
// first-party entity the Control Plane has never seen.
func (r *PostgresRepository) seedFirstParty(ctx context.Context, tx pgx.Tx, g FirstPartyGovernance, evidenceJSON []byte, actor AuditActor, out *GovernanceOutcome) error {
	// ADR-BCP-016 organisation_id attestation compares the entity's owner
	// tenant with the requesting tenant, so the owner is set only when
	// exactly one tenant already defaults to this legal entity.
	var owner any
	rows, err := tx.Query(ctx, `
		SELECT tenant_id FROM registry.tenant_legal_entity_mapping
		WHERE legal_entity_id=$1 AND mapping_role='DEFAULT' AND status IN `+liveStatuses, g.LegalEntityID)
	if err != nil {
		return err
	}
	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(tenants) == 1 {
		owner = tenants[0]
	}
	if _, err := tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, g.LegalEntityID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO registry.canonical_entity (entity_type, legal_entity_id, tenant_id, status)
		VALUES ($1, $2, $3, 'active') RETURNING canonical_entity_id::text`,
		domain.EntityTypeOrganisation, g.LegalEntityID, owner).Scan(&out.OrganisationID); err != nil {
		return fmt.Errorf("create first-party canonical entity: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO registry.organisation_profile (
			canonical_entity_id, display_name, official_name, verification_state, source_authority,
			status, effective_from, evidence_references
		) VALUES ($1::uuid, $2, $2, 'VERIFIED', $3, 'ACTIVE', $4, $5::jsonb)`,
		out.OrganisationID, g.LegalName, firstPartySourceAuthority, g.At, evidenceJSON); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO registry.legal_entity_profile (
			legal_entity_id, organisation_id, legal_name, legal_status, source_authority,
			verification_state, effective_from, evidence_references, verified_by, verified_at
		) VALUES ($1, $2::uuid, $3, 'UNKNOWN', $4, 'VERIFIED', $5, $6::jsonb, $7, $5)`,
		g.LegalEntityID, out.OrganisationID, g.LegalName, firstPartySourceAuthority, g.At, evidenceJSON, actor.ActorID); err != nil {
		return err
	}
	out.Created = true
	out.Changes = append(out.Changes, "organisation and legal_entity_profile seeded from Shared governance")
	evidence := []string{g.EvidenceReference}
	if err := r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
		AuditAction: "organisation.created", Target: "organisation/" + out.OrganisationID,
		AggregateType: "organisation", AggregateID: out.OrganisationID, EventType: events.OrganisationCreated,
		Data: map[string]any{"organisation_id": out.OrganisationID, "verification_state": string(domain.VerificationVerified),
			"source_authority": firstPartySourceAuthority, "effective_from": events.Timestamp(g.At)},
		AuditPayload: map[string]any{"legal_entity_id": g.LegalEntityID, "evidence_references": evidence, "source_authority": firstPartySourceAuthority},
	}); err != nil {
		return err
	}
	return r.recordOrganisationChange(ctx, tx, actor, legalEntityVerifiedChange(g, out.OrganisationID, evidence))
}

// reconcileFirstPartyOrganisation aligns the Organisation profile behind a
// first-party legal entity with Shared governance.
func (r *PostgresRepository) reconcileFirstPartyOrganisation(ctx context.Context, tx pgx.Tx, g FirstPartyGovernance, evidenceJSON []byte, actor AuditActor, out *GovernanceOutcome) error {
	var official, state string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(official_name,''), verification_state FROM registry.organisation_profile
		WHERE canonical_entity_id=$1::uuid FOR UPDATE`, out.OrganisationID).Scan(&official, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO registry.organisation_profile (
				canonical_entity_id, display_name, official_name, verification_state, source_authority,
				status, effective_from, evidence_references
			) VALUES ($1::uuid, $2, $2, 'VERIFIED', $3, 'ACTIVE', $4, $5::jsonb)`,
			out.OrganisationID, g.LegalName, firstPartySourceAuthority, g.At, evidenceJSON); err != nil {
			return err
		}
		out.Changes = append(out.Changes, "organisation profile created from Shared governance")
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "organisation.created", Target: "organisation/" + out.OrganisationID,
			AggregateType: "organisation", AggregateID: out.OrganisationID, EventType: events.OrganisationCreated,
			Data: map[string]any{"organisation_id": out.OrganisationID, "verification_state": string(domain.VerificationVerified),
				"source_authority": firstPartySourceAuthority, "effective_from": events.Timestamp(g.At)},
			AuditPayload: map[string]any{"legal_entity_id": g.LegalEntityID, "source_authority": firstPartySourceAuthority},
		})
	}
	if err != nil {
		return err
	}
	if state == string(domain.VerificationRejected) || state == string(domain.VerificationExpired) {
		out.Drift = append(out.Drift, GovernanceDrift{Field: "organisation_profile.verification_state", Observed: state,
			Governed: string(domain.VerificationVerified), Blocking: true,
			Reason: "a rejected or expired organisation verification is never overturned automatically; it needs governed review"})
		return nil
	}
	if official != g.LegalName {
		if official != "" {
			out.Drift = append(out.Drift, GovernanceDrift{Field: "organisation_profile.official_name", Observed: official,
				Governed: g.LegalName, Reason: "Shared governance is authoritative for first-party official names"})
		}
		if _, err := tx.Exec(ctx, `UPDATE registry.organisation_profile SET official_name=$2, updated_at=now() WHERE canonical_entity_id=$1::uuid`,
			out.OrganisationID, g.LegalName); err != nil {
			return err
		}
		out.Changes = append(out.Changes, "organisation_profile.official_name set from Shared governance")
	}
	if state == string(domain.VerificationVerified) {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE registry.organisation_profile
		SET verification_state='VERIFIED', source_authority=$2, evidence_references=$3::jsonb, updated_at=now()
		WHERE canonical_entity_id=$1::uuid`, out.OrganisationID, firstPartySourceAuthority, evidenceJSON); err != nil {
		return err
	}
	out.Changes = append(out.Changes, "organisation_profile verified from "+state)
	return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
		AuditAction: "organisation.verified", Target: "organisation/" + out.OrganisationID,
		AggregateType: "organisation", AggregateID: out.OrganisationID, EventType: events.OrganisationVerified,
		Data:         map[string]any{"organisation_id": out.OrganisationID, "verified_at": events.Timestamp(g.At), "evidence_reference_count": 1},
		AuditPayload: map[string]any{"legal_entity_id": g.LegalEntityID, "evidence_references": []string{g.EvidenceReference}, "previous_state": state},
	})
}

func legalEntityVerifiedChange(g FirstPartyGovernance, organisationID string, evidence []string) events.OrganisationChange {
	return events.OrganisationChange{
		AuditAction: "legal_entity_profile.verified", Target: "legal-entity/" + g.LegalEntityID,
		AggregateType: "legal_entity", AggregateID: organisationID, EventType: events.LegalEntityVerified,
		Data: map[string]any{"legal_entity_id": g.LegalEntityID, "organisation_id": organisationID,
			"verified_at": events.Timestamp(g.At), "evidence_reference_count": len(evidence)},
		AuditPayload: map[string]any{"evidence_references": evidence, "reason": "Shared first-party governance"},
	}
}

// recordDrift audits the reconciliation's corrections and drift together,
// including every previous value, so section 131's "who changed this and
// from what" stays answerable.
func (r *PostgresRepository) recordDrift(ctx context.Context, tx pgx.Tx, actor AuditActor, g FirstPartyGovernance, out GovernanceOutcome) error {
	return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
		AuditAction: "first_party.reconciled", Target: "legal-entity/" + g.LegalEntityID,
		AuditPayload: map[string]any{"organisation_id": out.OrganisationID, "evidence_reference": g.EvidenceReference,
			"changes": out.Changes, "drift": out.Drift},
	})
}
