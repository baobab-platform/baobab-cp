// Package organisation: PEO-04 / LA-06 controlled founding-group qualification.
// Source-controlled first-party business intent is NEVER an incorporation,
// corporate-control, mandate, IAM membership or provider-readiness decision.
package organisation

import (
 "context"
 "errors"
 "fmt"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

var ErrFoundingRosterIntegrity = errors.New("founding roster identity or tenant integrity denied")

type FoundingTenantSnapshot struct {
 OrganisationID string
 IdentityClass string
 IncorporationClaim string
 TenantIDs []string
 PrimaryEnforced bool
 LegalProfileCount int
 LegalProfileVerified bool
 SponsorshipEligible bool
 ActiveMandateCount int
}

type FoundingSnapshotReader interface {
 SnapshotFoundingEntity(context.Context,string,time.Time)(FoundingTenantSnapshot,error)
}

// PostgresFoundingSnapshot reads authoritative state only. It does not create
// tenants or legal profiles or turn unreviewed founding claims into VERIFIED.
type PostgresFoundingSnapshot struct { Pool *pgxpool.Pool }

func (p PostgresFoundingSnapshot) SnapshotFoundingEntity(ctx context.Context, firstPartyID string, at time.Time)(FoundingTenantSnapshot,error){
 var row FoundingTenantSnapshot
 if p.Pool==nil||ctx==nil||ctx.Err()!=nil||firstPartyID==""||at.IsZero(){return row,ErrFoundingRosterIntegrity}
 err:=p.Pool.QueryRow(ctx,`SELECT organisation_id::text,identity_class,incorporation_claim
 FROM registry.first_party_organisation_identity WHERE first_party_id=$1`,firstPartyID).
 Scan(&row.OrganisationID,&row.IdentityClass,&row.IncorporationClaim)
 if errors.Is(err,pgx.ErrNoRows){return row,nil}
 if err!=nil{return row,err}
 tenants,err:=p.Pool.Query(ctx,`SELECT t.tenant_id,t.primary_organisation_enforced
 FROM tenants t
 JOIN registry.tenant_organisation_mapping m ON m.tenant_id=t.tenant_id
 WHERE m.organisation_id=$1::uuid AND m.mapping_role='PRIMARY_ORGANISATION'
 AND m.status='ACTIVE' AND m.effective_from<=$2
 AND (m.effective_to IS NULL OR m.effective_to>$2)
 ORDER BY t.tenant_id LIMIT 3`,row.OrganisationID,at)
 if err!=nil{return row,err}
 for tenants.Next(){
  var tenantID string
  var enforced bool
  if err=tenants.Scan(&tenantID,&enforced);err!=nil{break}
  row.TenantIDs=append(row.TenantIDs,tenantID)
  row.PrimaryEnforced=enforced
 }
 if closeErr:=tenants.Err();err==nil {err=closeErr}
 tenants.Close()
 if err!=nil{return row,err}
 err=p.Pool.QueryRow(ctx,`SELECT count(*),
 COALESCE(bool_or(verification_state='VERIFIED' AND legal_status='ACTIVE'
 AND source_authority NOT IN('shared-governance','control-plane-registration')
 AND jsonb_array_length(evidence_references)>0),false)
 FROM registry.legal_entity_profile WHERE organisation_id=$1::uuid`,row.OrganisationID).
 Scan(&row.LegalProfileCount,&row.LegalProfileVerified)
 if err!=nil{return row,err}
 if len(row.TenantIDs)!=1 || !row.PrimaryEnforced{return row,nil}
 err=p.Pool.QueryRow(ctx,`SELECT EXISTS(
 SELECT 1 FROM admission.founding_group_sponsorship s
 JOIN registry.first_party_organisation_identity fp ON fp.organisation_id=s.sponsor_organisation_id
 JOIN registry.legal_entity_profile lp ON lp.organisation_id=s.sponsor_organisation_id
 WHERE s.operating_organisation_id=$1::uuid
 AND s.scope='INTERNAL_GROUP_ADMISSION' AND s.status='ACTIVE'
 AND s.effective_from<=$2 AND s.effective_to>$2
 AND fp.identity_class='LEGAL_PERSON'
 AND lp.verification_state='VERIFIED' AND lp.legal_status='ACTIVE'
 AND lp.source_authority NOT IN ('shared-governance','control-plane-registration')
 AND lp.effective_from<=$2 AND (lp.effective_to IS NULL OR lp.effective_to>$2)
 AND jsonb_array_length(lp.evidence_references)>0
 )`,row.OrganisationID,at).Scan(&row.SponsorshipEligible)
 if err!=nil{return row,err}
 err=p.Pool.QueryRow(ctx,`SELECT count(*) FROM registry.operating_legal_actor_mandate m
 JOIN registry.legal_entity_profile lp ON lp.legal_entity_id=m.responsible_legal_entity_id
 WHERE m.tenant_id=$1 AND m.operating_organisation_id=$2::uuid
 AND m.status='ACTIVE' AND m.effective_from<=$3
 AND (m.effective_to IS NULL OR m.effective_to>$3)
 AND m.approved_by IS NOT NULL AND m.approved_by<>m.created_by
 AND m.legal_actor_verification_reference IS NOT NULL
 AND lp.verification_state='VERIFIED' AND lp.legal_status='ACTIVE'
 AND lp.source_authority NOT IN ('shared-governance','control-plane-registration')
 AND lp.effective_from<=$3 AND (lp.effective_to IS NULL OR lp.effective_to>$3)
 `,row.TenantIDs[0],row.OrganisationID,at).Scan(&row.ActiveMandateCount)
 return row,err
}

// FoundingQualification describes whether a governed onboarding transition
// has been completed, not whether a specific Trade/ERP/PSP operation is legal.
type FoundingQualification struct {
 FirstPartyID string `json:"first_party_id"`
 OrganisationID string `json:"organisation_id,omitempty"`
 TenantID string `json:"tenant_id,omitempty"`
 IdentityClass string `json:"identity_class"`
 IncorporationClaim string `json:"incorporation_claim"`
 OrganisationRecognised bool `json:"organisation_recognised"`
 TenantPrimaryIsolated bool `json:"tenant_primary_isolated"`
 CompanyVerificationRecorded bool `json:"company_verification_recorded"`
 CurrentSponsorshipEligible bool `json:"current_sponsorship_eligible"`
 ActiveScopedMandateCount int `json:"active_scoped_mandate_count"`
 Readiness string `json:"readiness"`
 Missing []string `json:"missing,omitempty"`
}

// QualifyFoundingRoster uses the actual Shared first-party declarations, not
// applicant-supplied ids or a hard-coded set of privileged company names.
// Missing evidence yields PENDING, never an auto-approval or fabricated actor.
func QualifyFoundingRoster(ctx context.Context, reg FirstPartyRegistry, reader FoundingSnapshotReader, now time.Time)([]FoundingQualification,error){
 if ctx==nil||ctx.Err()!=nil||reader==nil||now.IsZero()||reg.Digest==""||len(reg.Entities)==0{return nil,ErrFoundingRosterIntegrity}
 out:=make([]FoundingQualification,0,len(reg.Entities))
 seenTenant:=map[string]string{}
 seenOrg:=map[string]string{}
 for _,entry:=range reg.Entities{
  if entry.IdentityClass==""||entry.IncorporationClaim==""{return nil,ErrFoundingRosterIntegrity}
  snapshot,err:=reader.SnapshotFoundingEntity(ctx,entry.ID,now)
  if err!=nil{return nil,fmt.Errorf("read first-party %s: %w",entry.ID,err)}
  result:=FoundingQualification{FirstPartyID:entry.ID,IdentityClass:entry.IdentityClass,
   IncorporationClaim:entry.IncorporationClaim,OrganisationID:snapshot.OrganisationID,
   CompanyVerificationRecorded:snapshot.LegalProfileVerified,
   CurrentSponsorshipEligible:snapshot.SponsorshipEligible,
   ActiveScopedMandateCount:snapshot.ActiveMandateCount,Readiness:"PENDING"}
  result.OrganisationRecognised=snapshot.OrganisationID!=""&&snapshot.IdentityClass==entry.IdentityClass&&
   snapshot.IncorporationClaim==entry.IncorporationClaim
  if !result.OrganisationRecognised{
   result.Missing=append(result.Missing,"GOVERNED_ORGANISATION_IDENTITY")
  }else if other:=seenOrg[snapshot.OrganisationID];other!=""&&other!=entry.ID {
   return nil,ErrFoundingRosterIntegrity
  }else{seenOrg[snapshot.OrganisationID]=entry.ID}
  if len(snapshot.TenantIDs)>1{return nil,ErrFoundingRosterIntegrity}
  if len(snapshot.TenantIDs)==1{
   result.TenantID=snapshot.TenantIDs[0]
   if other:=seenTenant[result.TenantID];other!=""&&other!=entry.ID{return nil,ErrFoundingRosterIntegrity}
   seenTenant[result.TenantID]=entry.ID
   result.TenantPrimaryIsolated=result.OrganisationRecognised&&snapshot.PrimaryEnforced
  }
  if !result.TenantPrimaryIsolated{result.Missing=append(result.Missing,"APPROVED_INDEPENDENT_TENANT_PRIMARY")}
  if entry.IdentityClass=="OPERATING_BUSINESS"{
   if entry.IncorporationClaim!="NOT_INCORPORATED"||snapshot.LegalProfileCount!=0{
    result.Missing=append(result.Missing,"FALSE_OR_LEGACY_INCORPORATION_PROFILE")
   }
   if !snapshot.SponsorshipEligible{result.Missing=append(result.Missing,"CURRENT_GOVERNED_FOUNDING_SPONSORSHIP")}
   // A live approved mandate is not a universal permission; review roles,
   // markets and providers independently at every commercial transaction.
   if snapshot.ActiveMandateCount==0{result.Missing=append(result.Missing,"SCOPED_LEGAL_ACTOR_MANDATE_FOR_RESTRICTED_OPERATIONS")}
  }else if entry.IdentityClass=="LEGAL_PERSON"{
   if entry.IncorporationClaim!="REGISTERED_EVIDENCED"&&entry.IncorporationClaim!="REGISTERED_CLAIMED"{
    result.Missing=append(result.Missing,"LEGAL_PERSON_DECLARATION")
   }
   if !snapshot.LegalProfileVerified{result.Missing=append(result.Missing,"INDEPENDENT_VERIFICATION_CASE_AND_REVIEW")}
  }else{return nil,ErrFoundingRosterIntegrity}
  if len(result.Missing)==0{
   // Even a qualified identity+tenant does not activate a capability: CP
   // legal-actor policy, IAM, subscriptions and provider readiness decide it.
   result.Readiness="IDENTITY_AND_TENANT_GATES_SATISFIED_NOT_PRODUCTION"
  }
  out=append(out,result)
 }
 return out,nil
}
