// ADR-BCP-018 — organisation structure written inside RegisterTenant's transaction.

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
	basestore "github.com/nabhold/baobab-cp/internal/store"
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
// Every change is audited and each ADR-BCP-018 section 124 transition is
// written to the outbox, on the registration transaction.
//
// Platform relationships are not assigned here; admission policy does that
// after review, because privileged types must never be applicant-assigned.
func (s *Store) insertOrganisationOnRegister(ctx context.Context, tx pgx.Tx, c domain.RegisterTenant, metadata basestore.RequestMetadata, key string) error {
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
		if err = s.recordOrganisationChange(ctx, tx, metadata, key, events.OrganisationChange{
			AuditAction: "organisation.created", Target: "organisation/" + orgID,
			AggregateType: "organisation", AggregateID: orgID, EventType: events.OrganisationCreated,
			Data: map[string]any{"organisation_id": orgID, "verification_state": string(domain.VerificationUnverified),
				"source_authority": registrationSourceAuthority, "effective_from": events.Timestamp(at)},
			AuditPayload: map[string]any{"verification_state": string(domain.VerificationUnverified), "source_authority": registrationSourceAuthority},
		}); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO registry.legal_entity_profile (
				legal_entity_id, organisation_id, legal_name, legal_status,
				source_authority, verification_state, effective_from
			) VALUES ($1, $2::uuid, $3, 'UNKNOWN', $4, 'UNVERIFIED', $5)`,
			c.LegalEntityID, orgID, c.DisplayName, registrationSourceAuthority, at); err != nil {
			return fmt.Errorf("create legal entity profile: %w", err)
		}
		if err = s.recordOrganisationChange(ctx, tx, metadata, key, events.OrganisationChange{
			AuditAction: "legal_entity_profile.created", Target: "legal-entity/" + c.LegalEntityID,
			AuditPayload: map[string]any{"organisation_id": orgID, "verification_state": string(domain.VerificationUnverified), "source_authority": registrationSourceAuthority},
		}); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("lookup legal entity profile: %w", err)
	}

	var mappingID string
	err = tx.QueryRow(ctx, `
		INSERT INTO registry.tenant_legal_entity_mapping (
			tenant_id, legal_entity_id, mapping_role, status, effective_from, provenance
		) VALUES ($1, $2, 'DEFAULT', 'ACTIVE', $3, $4)
		ON CONFLICT (tenant_id) WHERE mapping_role = 'DEFAULT' AND status IN ('PENDING','ACTIVE','SUSPENDED')
		DO NOTHING
		RETURNING tenant_legal_entity_mapping_id::text`,
		c.TenantID, c.LegalEntityID, at, registrationSourceAuthority).Scan(&mappingID)
	switch {
	case err == nil:
		id, err := domain.FormatResourceID(domain.TenantLegalEntityMappingIDPrefix, mappingID)
		if err != nil {
			return err
		}
		if err = s.recordOrganisationChange(ctx, tx, metadata, key, events.OrganisationChange{
			AuditAction: "tenant_legal_entity_mapping.activated", Target: "tenant-legal-entity-mapping/" + id,
			AggregateType: "tenant", AggregateID: mappingID, TenantID: c.TenantID,
			EventType: events.TenantLegalEntityMappingActivated,
			Data: map[string]any{"tenant_legal_entity_mapping_id": id, "tenant_id": c.TenantID, "legal_entity_id": c.LegalEntityID,
				"mapping_role": domain.TenantLegalEntityRoleDefault, "effective_from": events.Timestamp(at)},
			AuditPayload: map[string]any{"legal_entity_id": c.LegalEntityID, "mapping_role": domain.TenantLegalEntityRoleDefault, "provenance": registrationSourceAuthority},
		}); err != nil {
			return err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("tenant legal entity mapping: %w", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO registry.tenant_organisation_mapping (
			tenant_id, organisation_id, mapping_role, status, effective_from, provenance
		) VALUES ($1, $2::uuid, 'PRIMARY_ORGANISATION', 'ACTIVE', $3, $4)
		ON CONFLICT (tenant_id) WHERE mapping_role = 'PRIMARY_ORGANISATION' AND status IN ('PENDING','ACTIVE','SUSPENDED')
		DO NOTHING
		RETURNING tenant_organisation_mapping_id::text`,
		c.TenantID, orgID, at, registrationSourceAuthority).Scan(&mappingID)
	switch {
	case err == nil:
		id, err := domain.FormatResourceID(domain.TenantOrganisationMappingIDPrefix, mappingID)
		if err != nil {
			return err
		}
		return s.recordOrganisationChange(ctx, tx, metadata, key, events.OrganisationChange{
			AuditAction: "tenant_organisation_mapping.activated", Target: "tenant-organisation-mapping/" + id,
			AggregateType: "tenant", AggregateID: mappingID, TenantID: c.TenantID,
			EventType: events.TenantOrganisationMappingActivated,
			Data: map[string]any{"tenant_organisation_mapping_id": id, "tenant_id": c.TenantID, "organisation_id": orgID,
				"mapping_role": domain.TenantOrgRolePrimary, "effective_from": events.Timestamp(at)},
			AuditPayload: map[string]any{"organisation_id": orgID, "mapping_role": domain.TenantOrgRolePrimary, "provenance": registrationSourceAuthority},
		})
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("tenant organisation mapping: %w", err)
	}
	return nil
}

// recordOrganisationChange audits change and, when it has a section 124
// event, writes the envelope to the canonical outbox, both on tx.
func (s *Store) recordOrganisationChange(ctx context.Context, tx pgx.Tx, metadata basestore.RequestMetadata, key string, change events.OrganisationChange) error {
	payload, err := json.Marshal(change.AuditPayload)
	if err != nil {
		return err
	}
	// Platform-scoped changes carry no tenant: stored as NULL, like the
	// repository's organisation audit rows.
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,actor_type,client_id,token_id,correlation_id,idempotency_key,action,target,result,payload) VALUES(NULLIF($1,''),$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,'accepted',$10)`,
		change.TenantID, metadata.ActorID, metadata.ActorType, metadata.ClientID, metadata.TokenID, metadata.CorrelationID, key, change.AuditAction, change.Target, payload); err != nil {
		return fmt.Errorf("audit %s: %w", change.AuditAction, err)
	}
	if change.EventType == "" {
		return nil
	}
	env, err := events.NewOrganisationEnvelope(change, s.eventSource(), metadata.CorrelationID)
	if err != nil {
		return fmt.Errorf("build %s event: %w", change.EventType, err)
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	var tenant any
	if change.TenantID != "" {
		tenant = change.TenantID
	}
	_, err = tx.Exec(ctx, `INSERT INTO messaging.outbox(aggregate_type,aggregate_id,aggregate_version,event_type,tenant_id,correlation_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		change.AggregateType, change.AggregateID, time.Now().UnixMicro(), env.Type, tenant, metadata.CorrelationID, body)
	return err
}
