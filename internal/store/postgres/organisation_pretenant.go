package postgres

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/events"
    basestore "github.com/baobab-platform/baobab-cp/internal/store"
    "github.com/jackc/pgx/v5"
)

// PrepareOnboardingOrganisation resolves a reviewed, pre-tenant Organisation
// for an AUTHORISED request. It must be called by a privileged, authenticated
// reviewer distinct from both requester and authoriser, with case evidence.
// It neither invents a LegalEntityProfile nor admits a new tenant. Replay of
// exactly the same binding is idempotent.
func (s *Store) PrepareOnboardingOrganisation(ctx context.Context, key string, metadata basestore.RequestMetadata, requestID, policyReference, evidenceReference string) (string, error) {
    requestUUID, err := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, requestID)
    if err != nil { return "", err }
    if !domain.IsUUID(metadata.ActorID) || strings.TrimSpace(policyReference)=="" ||
       strings.TrimSpace(evidenceReference)=="" || strings.TrimSpace(key)=="" {
        return "", errors.New("independent reviewer, identity policy, evidence and idempotency key are mandatory")
    }
    tx, err := s.pool.Begin(ctx)
    if err != nil { return "", err }
    defer tx.Rollback(ctx)

    var decision, requested, authorised, name, status string
    err = tx.QueryRow(ctx, `SELECT admission_decision_id::text, requested_by::text,
      authorised_by::text, display_name, status
      FROM admission.tenant_onboarding_request WHERE tenant_onboarding_request_id=$1::uuid
      FOR UPDATE`, requestUUID).Scan(&decision,&requested,&authorised,&name,&status)
    if err != nil { return "", fmt.Errorf("load authorised onboarding request: %w",err) }
    if metadata.ActorID==requested || metadata.ActorID==authorised {
        return "", errors.New("Organisation reviewer must be independent of requester and authoriser")
    }

    var existing, existingPolicy, existingEvidence, existingReviewer string
    err=tx.QueryRow(ctx,`SELECT organisation_id::text, identity_resolution_policy_reference,
      evidence_reference, reviewed_by::text
      FROM admission.tenant_onboarding_organisation
      WHERE tenant_onboarding_request_id=$1::uuid`,requestUUID).
      Scan(&existing,&existingPolicy,&existingEvidence,&existingReviewer)
    if err==nil {
        if existingPolicy!=policyReference || existingEvidence!=evidenceReference ||
            existingReviewer!=metadata.ActorID {
            return "", basestore.ErrIdempotencyConflict
        }
        if err=tx.Commit(ctx);err!=nil{return "",err}
        return existing,nil
    }
    if !errors.Is(err,pgx.ErrNoRows){return "",err}
    if status!="AUTHORISED" { return "", errors.New("Organisation can be bound only to AUTHORISED onboarding") }

    orgID:=domain.NewUUIDv7()
    // A recognised applicant name is a claim, not independent incorporation
    // evidence. Organisation owns no Tenant or legal-entity reference yet.
    _,err=tx.Exec(ctx,`INSERT INTO registry.canonical_entity
      (canonical_entity_id,entity_type,status)
      VALUES($1::uuid,'ORGANISATION','active')`,orgID)
    if err!=nil{return "",err}
    _,err=tx.Exec(ctx,`INSERT INTO registry.organisation_profile
      (canonical_entity_id,display_name,verification_state,source_authority,status,effective_from,
       evidence_references)
      VALUES($1::uuid,$2,'UNVERIFIED','control-plane-approved-admission','ACTIVE',$3,
        jsonb_build_array($4::text))`,orgID,name,time.Now().UTC(),evidenceReference)
    if err!=nil{return "",err}
    _,err=tx.Exec(ctx,`INSERT INTO admission.tenant_onboarding_organisation
      (tenant_onboarding_request_id,organisation_id,admission_decision_id,
       reviewed_by,identity_resolution_policy_reference,evidence_reference)
      VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6)`,
        requestUUID,orgID,decision,metadata.ActorID,policyReference,evidenceReference)
    if err!=nil{return "",err}
    if err=s.recordOrganisationChange(ctx,tx,metadata,key,events.OrganisationChange{
        AuditAction:"organisation.created",
        Target:"organisation/"+orgID,AggregateType:"organisation",AggregateID:orgID,
        EventType:events.OrganisationCreated,
        Data:map[string]any{"organisation_id":orgID,"verification_state":"UNVERIFIED",
            "source_authority":"control-plane-approved-admission","effective_from":events.Timestamp(time.Now().UTC())},
        AuditPayload:map[string]any{"onboarding_request_id":requestID,
            "identity_resolution_policy_reference":policyReference,"evidence_reference":evidenceReference},
    });err!=nil{return "",err}
    if err=tx.Commit(ctx);err!=nil{return "",err}
    return orgID,nil
}
