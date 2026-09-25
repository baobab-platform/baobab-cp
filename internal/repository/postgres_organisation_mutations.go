// ADR-BCP-018 organisation mutations. Each runs in one transaction that
// also writes the audit row and, for ADR-BCP-018 section 124 transitions,
// the outbox event (see organisation_audit.go). Replays change nothing and
// record nothing.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/jackc/pgx/v5"
)

// ensureByNaturalKey inserts a row on tx and, when the live natural key
// already exists, returns the existing row's uuid with created=false. The
// existing row is never modified, so replays converge.
func ensureByNaturalKey(ctx context.Context, tx pgx.Tx, insert string, insertArgs []any, lookup string, lookupArgs []any) (id string, created bool, err error) {
	err = tx.QueryRow(ctx, insert, insertArgs...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, lookup, lookupArgs...).Scan(&id)
		return id, false, err
	}
	return id, err == nil, err
}

// inTx runs fn in a transaction after validating the actor.
func (r *PostgresRepository) inTx(ctx context.Context, actor AuditActor, fn func(pgx.Tx) error) error {
	if err := validateActor(actor); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func activated(state domain.VerificationState, status string) bool {
	return state == domain.VerificationVerified && status == domain.RelationshipStatusActive
}

// --- Organisation --------------------------------------------------------

func (r *PostgresRepository) EnsureOrganisation(ctx context.Context, org domain.Organisation, actor AuditActor) (bool, error) {
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
	created := false
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
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
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		created = true
		return r.recordOrganisationChange(ctx, tx, actor, organisationCreatedChange(org))
	})
	return created, err
}

func organisationCreatedChange(org domain.Organisation) events.OrganisationChange {
	return events.OrganisationChange{
		AuditAction: "organisation.created", Target: "organisation/" + org.CanonicalEntityID,
		AggregateType: "organisation", AggregateID: org.CanonicalEntityID,
		EventType: events.OrganisationCreated,
		Data: map[string]any{
			"organisation_id": org.CanonicalEntityID, "verification_state": string(org.VerificationState),
			"source_authority": org.SourceAuthority, "effective_from": events.Timestamp(org.EffectiveFrom),
		},
		AuditPayload: map[string]any{"verification_state": string(org.VerificationState), "source_authority": org.SourceAuthority},
	}
}

// --- LegalEntityProfile --------------------------------------------------

func (r *PostgresRepository) EnsureLegalEntityProfile(ctx context.Context, lep domain.LegalEntityProfile, actor AuditActor) (bool, error) {
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
	created := false
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, lep.LegalEntityID); err != nil {
			return err
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
			return err
		}
		if tag.RowsAffected() == 0 {
			var owner string
			if err := tx.QueryRow(ctx, `SELECT organisation_id::text FROM registry.legal_entity_profile WHERE legal_entity_id=$1`, lep.LegalEntityID).Scan(&owner); err != nil {
				return err
			}
			if owner != lep.OrganisationID {
				return fmt.Errorf("%w: legal entity %s belongs to organisation %s", ErrOrganisationConflict, lep.LegalEntityID, owner)
			}
			return nil
		}
		created = true
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "legal_entity_profile.created", Target: "legal-entity/" + lep.LegalEntityID,
			AuditPayload: map[string]any{"organisation_id": lep.OrganisationID, "verification_state": string(lep.VerificationState), "source_authority": lep.SourceAuthority},
		})
	})
	return created, err
}

func (r *PostgresRepository) VerifyLegalEntityProfile(ctx context.Context, legalEntityID string, ev Evidence, actor AuditActor) error {
	if err := validateEvidence(ev); err != nil {
		return err
	}
	evidence, err := json.Marshal(ev.References)
	if err != nil {
		return err
	}
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var organisationID string
		err := tx.QueryRow(ctx, `
			UPDATE registry.legal_entity_profile
			SET verification_state='VERIFIED', evidence_references=$2::jsonb,
				verified_by=$3, verified_at=$4, updated_at=now()
			WHERE legal_entity_id=$1
			  AND verification_state IN ('UNVERIFIED','PENDING_REVIEW','CONFLICTED')
			RETURNING organisation_id::text`,
			legalEntityID, evidence, actor.ActorID, ev.VerifiedAt).Scan(&organisationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("legal entity profile %s is not awaiting verification", legalEntityID)
		}
		if err != nil {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "legal_entity_profile.verified", Target: "legal-entity/" + legalEntityID,
			AggregateType: "legal_entity", AggregateID: organisationID, EventType: events.LegalEntityVerified,
			Data: map[string]any{"legal_entity_id": legalEntityID, "organisation_id": organisationID,
				"verified_at": events.Timestamp(ev.VerifiedAt), "evidence_reference_count": len(ev.References)},
			AuditPayload: map[string]any{"evidence_references": ev.References, "reason": ev.Reason},
		})
	})
}

// --- CorporateRelationship -----------------------------------------------

func corporateRelationshipActivatedChange(rowID, id string, rel domain.CorporateRelationship, verifiedAt time.Time, evidence []string, reason string) events.OrganisationChange {
	return events.OrganisationChange{
		AuditAction: "corporate_relationship.verified", Target: "corporate-relationship/" + id,
		AggregateType: "corporate_relationship", AggregateID: rowID, EventType: events.CorporateRelationshipActivated,
		Data: map[string]any{"corporate_relationship_id": id, "source_organisation_id": rel.SourceOrganisationID,
			"target_organisation_id": rel.TargetOrganisationID, "relationship_type": string(rel.RelationshipType),
			"effective_from": events.Timestamp(rel.EffectiveFrom), "verified_at": events.Timestamp(verifiedAt),
			"evidence_reference_count": len(evidence)},
		AuditPayload: map[string]any{"evidence_references": evidence, "reason": reason},
	}
}

func (r *PostgresRepository) EnsureCorporateRelationship(ctx context.Context, rel domain.CorporateRelationship, actor AuditActor) (string, error) {
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
	var result string
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		got, created, err := ensureByNaturalKey(ctx, tx, `
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
			return err
		}
		if result, err = domain.FormatResourceID(domain.CorporateRelationshipIDPrefix, got); err != nil || !created {
			return err
		}
		change := events.OrganisationChange{
			AuditAction: "corporate_relationship.recorded", Target: "corporate-relationship/" + result,
			AuditPayload: map[string]any{"source_organisation_id": rel.SourceOrganisationID, "target_organisation_id": rel.TargetOrganisationID,
				"relationship_type": string(rel.RelationshipType), "verification_state": string(rel.VerificationState),
				"status": rel.Status, "source_authority": rel.SourceAuthority},
		}
		if activated(rel.VerificationState, rel.Status) {
			change = corporateRelationshipActivatedChange(got, result, rel, *rel.VerifiedAt, rel.EvidenceReferences, "recorded as verified")
		}
		return r.recordOrganisationChange(ctx, tx, actor, change)
	})
	return result, err
}

func (r *PostgresRepository) VerifyCorporateRelationship(ctx context.Context, id string, ev Evidence, actor AuditActor) error {
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
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var rel domain.CorporateRelationship
		var rt string
		err := tx.QueryRow(ctx, `
			UPDATE registry.corporate_relationship
			SET verification_state='VERIFIED', status='ACTIVE', evidence_references=$2::jsonb,
				verified_by=$3, verified_at=$4, updated_at=now()
			WHERE corporate_relationship_id=$1::uuid
			  AND status IN ('PENDING','ACTIVE')
			  AND verification_state IN ('UNVERIFIED','PENDING_REVIEW','CONFLICTED')
			RETURNING source_organisation_id::text, target_organisation_id::text, relationship_type, effective_from`,
			row, evidence, actor.ActorID, ev.VerifiedAt).Scan(&rel.SourceOrganisationID, &rel.TargetOrganisationID, &rt, &rel.EffectiveFrom)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("corporate relationship %s is not awaiting verification", id)
		}
		if err != nil {
			return err
		}
		rel.RelationshipType = domain.CorporateRelationshipType(rt)
		return r.recordOrganisationChange(ctx, tx, actor, corporateRelationshipActivatedChange(row, id, rel, ev.VerifiedAt, ev.References, ev.Reason))
	})
}

// --- CorporateGroup ------------------------------------------------------

func (r *PostgresRepository) CreateCorporateGroup(ctx context.Context, g domain.CorporateGroup, actor AuditActor) error {
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
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO registry.corporate_group (
				corporate_group_id, display_name, root_organisation_id, status, grouping_policy,
				effective_from, effective_to, classification, metadata
			) VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb)
			ON CONFLICT (corporate_group_id) DO NOTHING`,
			id, g.DisplayName, nullable(g.RootOrganisationID), g.Status, g.GroupingPolicy,
			g.EffectiveFrom, g.EffectiveTo, nullable(g.Classification), meta)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "corporate_group.created", Target: "corporate-group/" + g.ID,
			AuditPayload: map[string]any{"root_organisation_id": g.RootOrganisationID, "grouping_policy": g.GroupingPolicy, "status": g.Status},
		})
	})
}

func (r *PostgresRepository) EnsureCorporateGroupMembership(ctx context.Context, m domain.CorporateGroupMembership, actor AuditActor) (string, error) {
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
	var result string
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		got, created, err := ensureByNaturalKey(ctx, tx, `
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
			return err
		}
		if result, err = domain.FormatResourceID(domain.CorporateGroupMembershipIDPrefix, got); err != nil || !created {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "corporate_group_membership.recorded", Target: "corporate-group-membership/" + result,
			AuditPayload: map[string]any{"corporate_group_id": m.CorporateGroupID, "organisation_id": m.OrganisationID,
				"basis_relationship_ids": m.BasisRelationshipIDs, "manual_basis_reference": m.ManualBasisReference,
				"derivation_version": m.DerivationVersion},
		})
	})
	return result, err
}

// --- PlatformRelationship ------------------------------------------------

func platformRelationshipActivatedChange(rowID, id string, rel domain.PlatformRelationship, verifiedAt time.Time, evidence []string, reason string) events.OrganisationChange {
	data := map[string]any{"platform_relationship_id": id, "platform_id": rel.PlatformID, "organisation_id": rel.OrganisationID,
		"relationship_type": string(rel.RelationshipType), "effective_from": events.Timestamp(rel.EffectiveFrom),
		"verified_at": events.Timestamp(verifiedAt), "evidence_reference_count": len(evidence)}
	if rel.BasisRelationshipID != "" {
		data["basis_relationship_id"] = rel.BasisRelationshipID
	}
	return events.OrganisationChange{
		AuditAction: "platform_relationship.verified", Target: "platform-relationship/" + id,
		AggregateType: "platform_relationship", AggregateID: rowID, EventType: events.PlatformRelationshipActivated,
		Data: data, AuditPayload: map[string]any{"evidence_references": evidence, "reason": reason},
	}
}

func (r *PostgresRepository) EnsurePlatformRelationship(ctx context.Context, rel domain.PlatformRelationship, actor AuditActor) (string, error) {
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
	var result string
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		got, created, err := ensureByNaturalKey(ctx, tx, `
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
			return err
		}
		if result, err = domain.FormatResourceID(domain.PlatformRelationshipIDPrefix, got); err != nil || !created {
			return err
		}
		change := events.OrganisationChange{
			AuditAction: "platform_relationship.recorded", Target: "platform-relationship/" + result,
			AuditPayload: map[string]any{"platform_id": rel.PlatformID, "organisation_id": rel.OrganisationID,
				"relationship_type": string(rel.RelationshipType), "verification_state": string(rel.VerificationState),
				"status": rel.Status, "source_authority": rel.SourceAuthority, "admission_decision_id": rel.AdmissionDecisionID,
				"basis_relationship_id": rel.BasisRelationshipID},
		}
		if activated(rel.VerificationState, rel.Status) {
			change = platformRelationshipActivatedChange(got, result, rel, *rel.VerifiedAt, rel.EvidenceReferences, "recorded as verified")
		}
		return r.recordOrganisationChange(ctx, tx, actor, change)
	})
	return result, err
}

func (r *PostgresRepository) VerifyPlatformRelationship(ctx context.Context, id string, ev Evidence, actor AuditActor) error {
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
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		var rel domain.PlatformRelationship
		var rt, basis string
		err := tx.QueryRow(ctx, `
			UPDATE registry.platform_relationship
			SET verification_state='VERIFIED', status='ACTIVE', evidence_references=$2::jsonb,
				verified_by=$3, verified_at=$4, updated_at=now()
			WHERE platform_relationship_id=$1::uuid
			  AND status IN ('PENDING','ACTIVE')
			  AND verification_state IN ('UNVERIFIED','PENDING_REVIEW','CONFLICTED')
			RETURNING platform_id, organisation_id::text, relationship_type, effective_from,
				COALESCE(basis_relationship_id::text,'')`,
			row, evidence, actor.ActorID, ev.VerifiedAt).Scan(&rel.PlatformID, &rel.OrganisationID, &rt, &rel.EffectiveFrom, &basis)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("platform relationship %s is not awaiting verification", id)
		}
		if err != nil {
			return err
		}
		rel.RelationshipType = domain.PlatformRelationshipType(rt)
		if basis != "" {
			if rel.BasisRelationshipID, err = domain.FormatResourceID(domain.CorporateRelationshipIDPrefix, basis); err != nil {
				return err
			}
		}
		return r.recordOrganisationChange(ctx, tx, actor, platformRelationshipActivatedChange(row, id, rel, ev.VerifiedAt, ev.References, ev.Reason))
	})
}

// --- PlatformAccount -----------------------------------------------------

func (r *PostgresRepository) CreatePlatformAccount(ctx context.Context, acct domain.PlatformAccount, actor AuditActor) error {
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
	return r.inTx(ctx, actor, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO registry.platform_account (
				platform_account_id, display_name, primary_organisation_id, status, contract_references,
				billing_profile_reference, support_profile_reference, effective_from, effective_to, metadata
			) VALUES ($1::uuid, $2, $3::uuid, $4, $5::jsonb, $6, $7, $8, $9, $10::jsonb)
			ON CONFLICT (platform_account_id) DO NOTHING`,
			id, acct.DisplayName, nullable(acct.PrimaryOrganisationID), acct.Status, contracts,
			nullable(acct.BillingProfileReference), nullable(acct.SupportProfileReference),
			acct.EffectiveFrom, acct.EffectiveTo, meta)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		data := map[string]any{"platform_account_id": acct.ID, "status": acct.Status, "effective_from": events.Timestamp(acct.EffectiveFrom)}
		if acct.PrimaryOrganisationID != "" {
			data["primary_organisation_id"] = acct.PrimaryOrganisationID
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "platform_account.created", Target: "platform-account/" + acct.ID,
			AggregateType: "platform_account", AggregateID: id, EventType: events.PlatformAccountCreated, Data: data,
			AuditPayload: map[string]any{"primary_organisation_id": acct.PrimaryOrganisationID, "status": acct.Status},
		})
	})
}

func (r *PostgresRepository) EnsurePlatformAccountMembership(ctx context.Context, m domain.PlatformAccountMembership, actor AuditActor) (string, error) {
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
	var result string
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		got, created, err := ensureByNaturalKey(ctx, tx, `
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
			return err
		}
		if result, err = domain.FormatResourceID(domain.PlatformAccountMembershipIDPrefix, got); err != nil || !created {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, events.OrganisationChange{
			AuditAction: "platform_account_membership.added", Target: "platform-account-membership/" + result,
			AggregateType: "platform_account", AggregateID: account, EventType: events.PlatformAccountMembershipChanged,
			Data: map[string]any{"platform_account_membership_id": result, "platform_account_id": m.PlatformAccountID,
				"organisation_id": m.OrganisationID, "account_role": m.AccountRole, "change": "ADDED", "status": m.Status,
				"changed_at": events.Timestamp(m.EffectiveFrom)},
			AuditPayload: map[string]any{"evidence_reference": m.EvidenceReference},
		})
	})
	return result, err
}

// --- Tenant mappings -----------------------------------------------------

func (r *PostgresRepository) EnsureTenantOrganisationMapping(ctx context.Context, m domain.TenantOrganisationMapping, actor AuditActor) (string, error) {
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
	var result string
	err = r.inTx(ctx, actor, func(tx pgx.Tx) error {
		got, created, err := ensureByNaturalKey(ctx, tx, `
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
			return err
		}
		if result, err = domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, got); err != nil || !created {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor, TenantOrganisationMappingChange(got, result, m))
	})
	return result, err
}

// TenantOrganisationMappingChange describes a recorded tenant-organisation
// mapping; it publishes the activation event only when the mapping is ACTIVE.
func TenantOrganisationMappingChange(rowID, id string, m domain.TenantOrganisationMapping) events.OrganisationChange {
	change := events.OrganisationChange{
		AuditAction: "tenant_organisation_mapping.recorded", Target: "tenant-organisation-mapping/" + id,
		AggregateType: "tenant", AggregateID: rowID, TenantID: m.TenantID,
		AuditPayload: map[string]any{"organisation_id": m.OrganisationID, "mapping_role": m.MappingRole, "status": m.Status, "provenance": m.Provenance},
	}
	if m.Status == domain.RelationshipStatusActive {
		change.AuditAction = "tenant_organisation_mapping.activated"
		change.EventType = events.TenantOrganisationMappingActivated
		change.Data = map[string]any{"tenant_organisation_mapping_id": id, "tenant_id": m.TenantID, "organisation_id": m.OrganisationID,
			"mapping_role": m.MappingRole, "effective_from": events.Timestamp(m.EffectiveFrom)}
	}
	return change
}

// TenantLegalEntityMappingActivatedChange describes a tenant's new active
// legal-entity mapping. replacesID names the DEFAULT it ended, if any.
func TenantLegalEntityMappingActivatedChange(rowID, id, tenantID, legalEntityID, role, replacesID, provenance string, at time.Time) events.OrganisationChange {
	data := map[string]any{"tenant_legal_entity_mapping_id": id, "tenant_id": tenantID, "legal_entity_id": legalEntityID,
		"mapping_role": role, "effective_from": events.Timestamp(at)}
	if replacesID != "" {
		data["replaces_mapping_id"] = replacesID
	}
	return events.OrganisationChange{
		AuditAction: "tenant_legal_entity_mapping.activated", Target: "tenant-legal-entity-mapping/" + id,
		AggregateType: "tenant", AggregateID: rowID, TenantID: tenantID,
		EventType: events.TenantLegalEntityMappingActivated, Data: data,
		AuditPayload: map[string]any{"legal_entity_id": legalEntityID, "mapping_role": role, "replaces_mapping_id": replacesID, "provenance": provenance},
	}
}

func (r *PostgresRepository) EnsureDefaultTenantLegalEntityMapping(ctx context.Context, tenantID, legalEntityID, provenance string, at time.Time, actor AuditActor) (string, error) {
	if tenantID == "" || legalEntityID == "" || provenance == "" {
		return "", errors.New("default tenant legal entity mapping: tenant_id, legal_entity_id and provenance are required")
	}
	var result string
	err := r.inTx(ctx, actor, func(tx pgx.Tx) error {
		// Serialise concurrent callers for this tenant.
		var current string
		if err := tx.QueryRow(ctx, `SELECT legal_entity_id FROM tenants WHERE tenant_id=$1 FOR UPDATE`, tenantID).Scan(&current); err != nil {
			return fmt.Errorf("lock tenant %s: %w", tenantID, err)
		}
		var id, defaultLE, replaced string
		err := tx.QueryRow(ctx, `
			SELECT tenant_legal_entity_mapping_id::text, legal_entity_id
			FROM registry.tenant_legal_entity_mapping
			WHERE tenant_id=$1 AND mapping_role='DEFAULT' AND status IN `+liveStatuses, tenantID).Scan(&id, &defaultLE)
		switch {
		case err == nil && defaultLE == legalEntityID && current == legalEntityID:
			// Replay: already the default and already projected. Nothing changes.
			result, err = domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, id)
			return err
		case err == nil && defaultLE != legalEntityID:
			// End the previous default; history stays queryable.
			if _, err := tx.Exec(ctx, `
				UPDATE registry.tenant_legal_entity_mapping
				SET status='ENDED', effective_to=GREATEST($2, effective_from), updated_at=now()
				WHERE tenant_legal_entity_mapping_id=$1::uuid`, id, at); err != nil {
				return err
			}
			if replaced, err = domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, id); err != nil {
				return err
			}
			id = ""
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		if _, err := tx.Exec(ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES($1) ON CONFLICT DO NOTHING`, legalEntityID); err != nil {
			return err
		}
		changed := false
		if id == "" {
			changed = true
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
				return err
			}
		}
		if current != legalEntityID {
			changed = true
			if _, err := tx.Exec(ctx, `UPDATE tenants SET legal_entity_id=$2, updated_at=now() WHERE tenant_id=$1`, tenantID, legalEntityID); err != nil {
				return err
			}
		}
		if result, err = domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, id); err != nil || !changed {
			return err
		}
		return r.recordOrganisationChange(ctx, tx, actor,
			TenantLegalEntityMappingActivatedChange(id, result, tenantID, legalEntityID, domain.TenantLegalEntityRoleDefault, replaced, provenance, at))
	})
	return result, err
}
