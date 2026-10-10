// PEO-02 independent maker/checker sponsorship and grace commands.
// These commands DO NOT issue company identities, legal mandates or access.
package postgres

import (
 "context"
 "crypto/sha256"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/domain"
 basestore "github.com/baobab-platform/baobab-cp/internal/store"
 "github.com/jackc/pgx/v5"
)

var ErrFoundingAuthority = errors.New("PEO-02 governed sponsorship or documentary authority denied")
type FoundingSponsorshipInput struct {
 OperatingOrganisationID string `json:"operating_organisation_id"`
 SponsorOrganisationID string `json:"sponsor_organisation_id"`
 PlatformID string `json:"platform_id"`
 AuthorityBasisReference string `json:"authority_basis_reference"`
 EvidenceReferences []string `json:"evidence_references"`
 EffectiveFrom time.Time `json:"effective_from"`
 EffectiveTo time.Time `json:"effective_to"`
}
type FoundingDeferralInput struct {
 OrganisationID string `json:"organisation_id"`
 SponsorshipID string `json:"sponsorship_id"`
 AdmissionDecisionID string `json:"admission_decision_id"`
 RequirementIDs []string `json:"requirement_ids"`
 PolicyReference string `json:"policy_reference"`
}
type FoundingCommandReceipt struct {
 IntentID string `json:"intent_id"`
 Kind string `json:"kind"`
 Status string `json:"status"`
 GrantID string `json:"grant_id,omitempty"`
}
type FoundingDecisionInput struct {
 ReviewReference string `json:"review_reference"`
 Decision string `json:"decision"`
}
func validateFoundingRequest(kind string, raw []byte)(string,error){
 switch kind {
 case "SPONSORSHIP":
  var x FoundingSponsorshipInput
  if err:=json.Unmarshal(raw,&x);err!=nil{return "",err}
  if !domain.IsUUID(x.OperatingOrganisationID)||!domain.IsUUID(x.SponsorOrganisationID)||
    x.OperatingOrganisationID==x.SponsorOrganisationID ||
    x.PlatformID==""||x.AuthorityBasisReference==""||len(x.EvidenceReferences)==0||
    x.EffectiveFrom.IsZero()||!x.EffectiveTo.After(x.EffectiveFrom)||
    x.EffectiveTo.Before(time.Now().UTC()) {return "",ErrFoundingAuthority}
  for _,e:=range x.EvidenceReferences{if strings.TrimSpace(e)==""{return "",ErrFoundingAuthority}}
  return x.OperatingOrganisationID,nil
 case "DOCUMENTARY_DEFERRAL":
  var x FoundingDeferralInput
  if err:=json.Unmarshal(raw,&x);err!=nil{return "",err}
  if !domain.IsUUID(x.OrganisationID)||!domain.IsUUID(x.SponsorshipID)||
    !domain.IsUUID(x.AdmissionDecisionID)||x.PolicyReference==""||len(x.RequirementIDs)==0{return "",ErrFoundingAuthority}
  seen:=map[string]bool{}
  for _,s:=range x.RequirementIDs{if len(s)<3||seen[s]{return "",ErrFoundingAuthority};seen[s]=true}
  return x.OrganisationID,nil
 }
 return "",ErrFoundingAuthority
}
func foundingAudit(ctx context.Context,tx pgx.Tx,meta basestore.RequestMetadata,
 action,grant,kind string,details map[string]any)error{
 data:=map[string]any{"grant_id":grant,"kind":kind,"operation":action,"details":details}
 raw,err:=json.Marshal(data);if err!=nil{return err}
 _,err=tx.Exec(ctx,`INSERT INTO audit_events(actor_id,actor_type,client_id,token_id,correlation_id,
 action,target,result,payload) VALUES($1::uuid,$2,NULLIF($3,''),NULLIF($4,''),$5::uuid,
 $6,$7,'accepted',$8::jsonb)`,
 meta.ActorID,meta.ActorType,meta.ClientID,meta.TokenID,meta.CorrelationID,
 "founding_governance."+action,"founding-governance/"+grant,raw)
 return err
}
// ProposeFoundingGovernance does not activate a grace period or sponsorship.
// It is a durable, digest-bound and audited human maker command.
func(s *Store)ProposeFoundingGovernance(ctx context.Context,key string,meta basestore.RequestMetadata,
 makerID,kind string,raw []byte)(FoundingCommandReceipt,error){
 var empty FoundingCommandReceipt
 if requireProgressiveHuman(meta,makerID)!=nil||len(key)<16||len(key)>128{return empty,ErrFoundingAuthority}
 orgID,err:=validateFoundingRequest(kind,raw);if err!=nil{return empty,err}
 // Canonicalize JSON so idempotent content-equivalent requests converge.
 var parsed any
 if err=json.Unmarshal(raw,&parsed);err!=nil{return empty,err}
 stable,err:=json.Marshal(parsed);if err!=nil{return empty,err}
 digest:=fmt.Sprintf("%x",sha256.Sum256(stable))
 tx,err:=s.pool.Begin(ctx);if err!=nil{return empty,err};defer tx.Rollback(ctx)
 if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,"peo02:"+kind+":"+makerID+":"+key);err!=nil{return empty,err}
 var receipt FoundingCommandReceipt
 var existingDigest string
 err=tx.QueryRow(ctx,`SELECT intent_id::text,kind,status,COALESCE(grant_id::text,''),request_digest
 FROM admission.founding_governance_intent
 WHERE kind=$1 AND proposed_by=$2::uuid AND idempotency_key=$3`,kind,makerID,key).
 Scan(&receipt.IntentID,&receipt.Kind,&receipt.Status,&receipt.GrantID,&existingDigest)
 if err==nil {
  if existingDigest!=digest{return empty,ErrFoundingAuthority}
  return receipt,nil
 }
 if !errors.Is(err,pgx.ErrNoRows){return empty,err}
 var human bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM identity.principal WHERE principal_id=$1::uuid AND actor_type='human')`,makerID).Scan(&human);err!=nil{return empty,err}
 if !human{return empty,ErrFoundingAuthority}
 intentID:=domain.NewUUIDv7()
 _,err=tx.Exec(ctx,`INSERT INTO admission.founding_governance_intent
 (intent_id,kind,operating_organisation_id,proposed_by,proposal,idempotency_key,request_digest)
 VALUES($1::uuid,$2,$3::uuid,$4::uuid,$5::jsonb,$6,$7)`,
 intentID,kind,orgID,makerID,string(stable),key,digest)
 if err!=nil{return empty,err}
 receipt=FoundingCommandReceipt{IntentID:intentID,Kind:kind,Status:"PENDING"}
 if err=foundingAudit(ctx,tx,meta,"proposed",intentID,kind,map[string]any{
 "operating_organisation_id":orgID,"request_digest":digest});err!=nil{return empty,err}
 if err=tx.Commit(ctx);err!=nil{return empty,err}
 return receipt,nil
}
// DecideFoundingGovernance independently attests authority. Empty/unknown
// documentary policy classifications and unverified sponsors ALWAYS deny.
func(s *Store)DecideFoundingGovernance(ctx context.Context,key string,meta basestore.RequestMetadata,
 checkerID,intentID string,decision FoundingDecisionInput)(FoundingCommandReceipt,error){
 var empty FoundingCommandReceipt
 if requireProgressiveHuman(meta,checkerID)!=nil||!domain.IsUUID(intentID)||
   len(key)<16||len(key)>128||strings.TrimSpace(decision.ReviewReference)==""||
   (decision.Decision!="APPROVE"&&decision.Decision!="REJECT"){return empty,ErrFoundingAuthority}
 tx,err:=s.pool.Begin(ctx);if err!=nil{return empty,err};defer tx.Rollback(ctx)
 var kind,org,maker,status,grant string
 var data []byte
 err=tx.QueryRow(ctx,`SELECT kind,operating_organisation_id::text,proposed_by::text,
 status,COALESCE(grant_id::text,''),proposal FROM admission.founding_governance_intent
 WHERE intent_id=$1::uuid FOR UPDATE`,intentID).Scan(&kind,&org,&maker,&status,&grant,&data)
 if err!=nil{return empty,ErrFoundingAuthority}
 if maker==checkerID{return empty,ErrFoundingAuthority}
 var checkerHuman bool
 if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM identity.principal
 WHERE principal_id=$1::uuid AND actor_type='human')`,checkerID).Scan(&checkerHuman);err!=nil{return empty,err}
 if !checkerHuman{return empty,ErrFoundingAuthority}
 if status!="PENDING"{
  // Never reuse an approved intent to grant again; idempotent callers may
  // read their receipt but must not forge a distinct second authorisation.
  return empty,ErrFoundingAuthority
 }
 now:=time.Now().UTC()
 next:="REJECTED"
 if decision.Decision=="APPROVE" {
  grant=domain.NewUUIDv7()
  switch kind{
  case "SPONSORSHIP":
   var p FoundingSponsorshipInput
   if err=json.Unmarshal(data,&p);err!=nil{return empty,err}
   var verified bool
   err=tx.QueryRow(ctx,`SELECT EXISTS(
    SELECT 1 FROM registry.first_party_organisation_identity fp
    JOIN registry.legal_entity_profile lp ON lp.organisation_id=fp.organisation_id
    WHERE fp.organisation_id=$1::uuid AND fp.identity_class='LEGAL_PERSON'
      AND fp.incorporation_claim='REGISTERED_EVIDENCED'
      AND lp.verification_state='VERIFIED' AND lp.legal_status='ACTIVE'
      AND lp.source_authority NOT IN ('shared-governance','control-plane-registration')
      AND lp.verified_at<=clock_timestamp()
      AND lp.effective_from<=clock_timestamp()
      AND (lp.effective_to IS NULL OR lp.effective_to>clock_timestamp())
      AND jsonb_array_length(lp.evidence_references)>0)`,p.SponsorOrganisationID).Scan(&verified)
   if err!=nil{return empty,err}
   if !verified||now.Before(p.EffectiveFrom)||!now.Before(p.EffectiveTo){return empty,ErrFoundingAuthority}
   grantResult, execErr := tx.Exec(ctx,`INSERT INTO admission.founding_group_sponsorship
   (sponsorship_id,sponsor_organisation_id,operating_organisation_id,platform_id,
    status,scope,authority_basis_reference,evidence_references,proposed_by,
    approved_by,approved_at,effective_from,effective_to,provenance)
   VALUES($1::uuid,$2::uuid,$3::uuid,$4,'ACTIVE','INTERNAL_GROUP_ADMISSION',
   $5,$6,$7::uuid,$8::uuid,$9,$10,$11,$12)`,
   grant,p.SponsorOrganisationID,org,p.PlatformID,p.AuthorityBasisReference,
   p.EvidenceReferences,maker,checkerID,now,p.EffectiveFrom,p.EffectiveTo,
   "governed-peo02:"+decision.ReviewReference)
  case "DOCUMENTARY_DEFERRAL":
   var p FoundingDeferralInput
   if err=json.Unmarshal(data,&p);err!=nil{return empty,err}
   var approvedCount int
   err=tx.QueryRow(ctx,`SELECT count(*) FROM admission.founding_documentary_policy_requirement
    WHERE policy_reference=$1 AND requirement_id=ANY($2::text[])
     AND requirement_authority='PLATFORM_DOCUMENTARY'
     AND approved_at<=clock_timestamp() AND effective_from<=clock_timestamp()
     AND effective_to>clock_timestamp()`,p.PolicyReference,p.RequirementIDs).Scan(&approvedCount)
   if err!=nil{return empty,err}
   if approvedCount!=len(p.RequirementIDs){return empty,ErrFoundingAuthority}
   // Admission decision is immutable. The DB trigger verifies exact
   // INTERNAL_GROUP, original approved date, 24-month expiry and SoD.
   grantResult, execErr := tx.Exec(ctx,`INSERT INTO admission.founding_documentary_deferral
   (deferral_id,organisation_id,sponsorship_id,admission_decision_id,
    requirement_ids,policy_reference,approval_reference,proposed_by,
    approved_by,approved_at,effective_from,expires_at,maximum_duration_months,status)
   SELECT $1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8::uuid,$9::uuid,$10,
    d.decided_at,d.decided_at+interval '24 months',24,'ACTIVE'
   FROM admission.admission_decision d WHERE d.admission_decision_id=$4::uuid`,
   grant,p.OrganisationID,p.SponsorshipID,p.AdmissionDecisionID,p.RequirementIDs,
   p.PolicyReference,decision.ReviewReference,maker,checkerID,now)
  default: return empty,ErrFoundingAuthority
  }
  if execErr != nil || grantResult.RowsAffected() != 1 { return empty, ErrFoundingAuthority }
  next="APPROVED"
 }
 _,err=tx.Exec(ctx,`UPDATE admission.founding_governance_intent
 SET status=$2,reviewed_by=$3::uuid,reviewed_at=$4,review_reference=$5,
 grant_id=NULLIF($6,'')::uuid WHERE intent_id=$1::uuid`,
 intentID,next,checkerID,now,decision.ReviewReference,grant)
 if err!=nil{return empty,err}
 if err=foundingAudit(ctx,tx,meta,"decided",intentID,kind,map[string]any{
 "decision":decision.Decision,"grant_id":grant,
 "review_reference":decision.ReviewReference});err!=nil{return empty,err}
 if err=tx.Commit(ctx);err!=nil{return empty,err}
 return FoundingCommandReceipt{IntentID:intentID,Kind:kind,Status:next,GrantID:grant},nil
}
