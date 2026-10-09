package repository

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/events"
    "github.com/jackc/pgx/v5"
)

// EnsureFirstPartyOperatingIdentity recognises a *business Organisation* in
// the source-controlled Shared registry. Inclusion establishes neither an
// incorporated LegalEntityProfile nor statutory verification. The same
// canonical Organisation is reused on replay; incompatible declarations
// become blocking drift rather than rewritten history.
func (r *PostgresRepository) EnsureFirstPartyOperatingIdentity(
    ctx context.Context,firstPartyID,name,identityClass,incorporationClaim,evidenceReference string,
    at time.Time, actor AuditActor,
) (GovernanceOutcome,error) {
    var outcome GovernanceOutcome
    if !domain.IsCanonicalLegalEntityID(firstPartyID)||name==""||identityClass==""||
       incorporationClaim==""||evidenceReference==""||at.IsZero() {
        return outcome,errors.New("invalid governed first-party operating identity")
    }
    err:=r.inTx(ctx,actor,func(tx pgx.Tx) error {
        // The id comes from the *trusted registry*, never an applicant claim.
        if _,err:=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtext($1))`,
            "first-party-operating/"+firstPartyID);err!=nil{return err}

        var existingClass, existingClaim, orgID string
        err:=tx.QueryRow(ctx,`SELECT organisation_id::text,identity_class,incorporation_claim
            FROM registry.first_party_organisation_identity
            WHERE first_party_id=$1 FOR UPDATE`,firstPartyID).
            Scan(&orgID,&existingClass,&existingClaim)
        switch {
        case err==nil:
            outcome.OrganisationID=orgID
            if existingClass!=identityClass||existingClaim!=incorporationClaim {
                outcome.Drift=append(outcome.Drift,GovernanceDrift{
                    Field:"first-party/"+firstPartyID+".incorporation_claim",
                    Observed:existingClass+"/"+existingClaim,
                    Governed:identityClass+"/"+incorporationClaim,Blocking:true,
                    Reason:"identity form changed; requires governed evidence and migration review",
                })
            }
        case errors.Is(err,pgx.ErrNoRows):
            // If a previous reconciler created an Organisation behind a
            // legacy LegalEntityProfile, reuse its operating identity but
            // NEVER take its VERIFIED company status as authoritative.
            err=tx.QueryRow(ctx,`SELECT organisation_id::text FROM registry.legal_entity_profile
                WHERE legal_entity_id=$1`,firstPartyID).Scan(&orgID)
            if err!=nil&&!errors.Is(err,pgx.ErrNoRows){return err}
            if errors.Is(err,pgx.ErrNoRows) {
                orgID=domain.NewUUIDv7()
                _,err=tx.Exec(ctx,`INSERT INTO registry.canonical_entity
                   (canonical_entity_id,entity_type,status)
                   VALUES($1::uuid,'ORGANISATION','active')`,orgID)
                if err!=nil{return err}
                _,err=tx.Exec(ctx,`INSERT INTO registry.organisation_profile
                   (canonical_entity_id,display_name,verification_state,
                    source_authority,status,effective_from,evidence_references)
                   VALUES($1::uuid,$2,'UNVERIFIED','shared-first-party-identity',
                     'ACTIVE',$3,jsonb_build_array($4::text))`,
                     orgID,name,at,evidenceReference)
                if err!=nil{return err}
                outcome.Created=true
                outcome.Changes=append(outcome.Changes,"operating Organisation identity created without LegalEntityProfile")
                if err=r.recordOrganisationChange(ctx,tx,actor,events.OrganisationChange{
                    AuditAction:"organisation.created",Target:"organisation/"+orgID,
                    AggregateType:"organisation",AggregateID:orgID,
                    EventType:events.OrganisationCreated,
                    Data:map[string]any{"organisation_id":orgID,
                        "verification_state":"UNVERIFIED","source_authority":"shared-first-party-identity",
                        "effective_from":events.Timestamp(at)},
                    AuditPayload:map[string]any{"first_party_id":firstPartyID,
                        "evidence_reference":evidenceReference,"incorporation_claim":incorporationClaim},
                });err!=nil{return err}
            }
            _,err=tx.Exec(ctx,`INSERT INTO registry.first_party_organisation_identity
                 (first_party_id,organisation_id,identity_class,incorporation_claim,registry_evidence_reference)
                 VALUES($1,$2::uuid,$3,$4,$5)`,
                 firstPartyID,orgID,identityClass,incorporationClaim,evidenceReference)
            if err!=nil{return err}
            outcome.OrganisationID=orgID
        default:
            return err
        }

        var oldStatus,oldSource string
        err=tx.QueryRow(ctx,`SELECT verification_state,source_authority
            FROM registry.legal_entity_profile WHERE legal_entity_id=$1`,firstPartyID).
            Scan(&oldStatus,&oldSource)
        if err!=nil&&!errors.Is(err,pgx.ErrNoRows){return err}
        if err==nil && oldStatus=="VERIFIED" && oldSource=="shared-governance" {
            outcome.Drift=append(outcome.Drift,GovernanceDrift{
                Field:"legal_entity_profile/"+firstPartyID+".verification_state",
                Observed:"VERIFIED from Shared registry digest",
                Governed:"independently evidenced verification",
                Blocking:true,
                Reason:"registry identity does not prove incorporation; review the historic verification without deleting its audit",
            })
        }
        if err==nil && identityClass=="OPERATING_BUSINESS" {
            outcome.Drift=append(outcome.Drift,GovernanceDrift{
                Field:"legal_entity_profile/"+firstPartyID+".existence",
                Observed:"legacy company-shaped profile",
                Governed:"separately unincorporated operating business",
                Blocking:true,
                Reason:"historic legal profile requires correction review; do not auto-delete or rewrite records",
            })
        }
        // No automatic verification, no mapping from DEFAULT LegalEntity,
        // no implicit parent-as-seller mandate, no IAM capability grants.
        return nil
    })
    if err!=nil{return GovernanceOutcome{},fmt.Errorf("recognise first-party %s: %w",firstPartyID,err)}
    return outcome,nil
}
