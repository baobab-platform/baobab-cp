// LA-05A: private, current and context-owned legal responsibility assessment.
// A resolved legal actor is a CP fact, not a provider/Trade/ERP/payment grant.
package api

import (
 "context"
 "log/slog"
 "net/http"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/auth"
 "github.com/baobab-platform/baobab-cp/internal/contracts"
 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/baobab-platform/baobab-cp/internal/service/legalactor"
 "github.com/baobab-platform/baobab-cp/internal/repository"
)

type legalActorAssessmentStore interface {
 ResolveOperatingLegalActor(context.Context,legalactor.Request)(legalactor.Resolution,error)
}

type assessLegalActorRequest struct {
 ContextID string `json:"context_id"`
 Role string `json:"role"`
 Activity string `json:"activity"`
 Market string `json:"market"`
 Capability string `json:"capability,omitempty"`
 OperationReference string `json:"operation_reference"`
}

type legalActorResolutionDTO struct {
 Outcome legalactor.Outcome `json:"outcome"`
 EvaluatedAt time.Time `json:"evaluated_at"`
 PolicyReference string `json:"policy_reference"`
 MandateID string `json:"mandate_id,omitempty"`
 ResponsibleLegalEntityID string `json:"responsible_legal_entity_id,omitempty"`
 EvidenceReferences []string `json:"evidence_references,omitempty"`
 ValidUntil *time.Time `json:"valid_until,omitempty"`
}

type assessLegalActorResponse struct {
 ContextID string `json:"context_id"`
 OperationReference string `json:"operation_reference"`
 ProviderPermissionsGranted bool `json:"provider_permissions_granted"`
 LegalActorResolution legalActorResolutionDTO `json:"legal_actor_resolution"`
}

var assessLegalActorSchema=contracts.MustSchema(
 "organisation/v2/legal-actor-enforcement.schema.json#/$defs/AssessLegalActorRequest")

type legalActorAssessmentHandler struct {
 api *API
 contexts repository.ContextRepository
 identities repository.IdentityRepository
 store legalActorAssessmentStore
}

func (h legalActorAssessmentHandler) assess(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Pragma","no-cache")
 p,ok:=auth.PrincipalFromContext(r.Context())
 if !ok||p.ActorType!="workload"||p.ClientID==""||
  !p.HasScope("legal-actor:assess")||h.api==nil||
  h.api.workloadRegistry==nil||!h.api.workloadRegistry.IsActive(p.ClientID) {
  problem(w,r,http.StatusForbidden,"LEGAL_ACTOR_ASSESSMENT_DENIED",
   "registered active legal actor assessor required",false)
  return
 }
 // Workload's registered scope must independently allow the JWT scope.
 scopes,ok:=h.api.workloadRegistry.(auth.WorkloadScopeRegistry)
 if !ok||!scopes.AllowsScope(p.ClientID,"legal-actor:assess"){
  problem(w,r,http.StatusForbidden,"LEGAL_ACTOR_ASSESSMENT_DENIED",
   "legal actor assessor scope not registered",false)
  return
 }
 raw,ok:=readBody(w,r)
 if !ok{return}
 var req assessLegalActorRequest
 if !decodeRaw(w,r,assessLegalActorSchema,raw,&req){return}
 if h.store==nil||h.contexts==nil||h.identities==nil||h.api.store==nil{
  problem(w,r,http.StatusServiceUnavailable,"LEGAL_ACTOR_ASSESSMENT_UNAVAILABLE",
   "legal actor assessment unavailable",true)
  return
 }
 // Reuse CP's canonical owner + authenticated tenant enforcement.
 trusted,ok:=redeemContext(w,r,h.contexts,h.identities,req.ContextID)
 if !ok{return}
 now:=time.Now().UTC()
 // No cross-service/legal authority from unbounded, expired, provisioning,
 // cross-market or organisation-less contexts.
 if !trusted.IsRuntime()||trusted.ExpiresAt==nil||
  !now.Before(*trusted.ExpiresAt)||trusted.ResolvedAt.After(now)||
  !domain.IsUUID(trusted.OrganisationID)||trusted.CountryCode!=req.Market {
  problem(w,r,http.StatusForbidden,"LEGAL_ACTOR_CONTEXT_DENIED",
   "current runtime operating Organisation context required",false)
  return
 }
 tenant,err:=h.api.store.GetTenant(r.Context(),trusted.TenantID)
 if err!=nil {
  problem(w,r,http.StatusServiceUnavailable,"LEGAL_ACTOR_ASSESSMENT_UNAVAILABLE",
   "tenant status could not be verified",true)
  return
 }
 if tenant.ObservedState!=string(domain.LifecycleActive){
  problem(w,r,http.StatusForbidden,"LEGAL_ACTOR_CONTEXT_DENIED",
   "tenant must be active",false)
  return
 }
 outcome,err:=h.store.ResolveOperatingLegalActor(r.Context(),legalactor.Request{
  TenantID:trusted.TenantID,OperatingOrganisationID:trusted.OrganisationID,
  Role:req.Role,Activity:req.Activity,Market:req.Market,
  Capability:req.Capability,EffectiveAt:now,
 })
 if err!=nil {
  problem(w,r,http.StatusServiceUnavailable,"LEGAL_ACTOR_ASSESSMENT_UNAVAILABLE",
   "current legal actor authority could not be verified",true)
  return
 }
 // Caller cannot select a MandateID, LegalEntity or its evidence.
 // A denied resolution MUST NOT disclose actor identity.
 decision:=legalActorResolutionDTO{
  Outcome:outcome.Outcome,EvaluatedAt:outcome.EvaluatedAt,
  PolicyReference:outcome.PolicyReference,
 }
 if outcome.Outcome==legalactor.Authorized&&outcome.ValidUntil!=nil&&
  outcome.ResponsibleLegalEntityID!=""&&outcome.MandateID!="" {
  until:=*outcome.ValidUntil
  if trusted.ExpiresAt.Before(until){until=*trusted.ExpiresAt}
  if !until.After(now) {
   decision.Outcome=legalactor.RevokedOrExpired
  } else {
   decision.MandateID=outcome.MandateID
   decision.ResponsibleLegalEntityID=outcome.ResponsibleLegalEntityID
   decision.EvidenceReferences=outcome.EvidenceReferences
   decision.ValidUntil=&until
  }
 }else if outcome.Outcome==legalactor.Authorized {
  decision.Outcome=legalactor.ActorNotVerified
 }
 // No request or response is a provider grant. Resource servers must
 // separately enforce their OWN provider/capability readiness.
 slog.InfoContext(r.Context(),"legal actor assessment",
  "client_id",p.ClientID,"tenant_id",trusted.TenantID,
  "context_id",trusted.ID,"operation_reference",req.OperationReference,
  "decision",decision.Outcome,"correlation_id",correlationID(r))
 writeJSON(w,http.StatusOK,assessLegalActorResponse{
  ContextID:trusted.ID,OperationReference:req.OperationReference,
  ProviderPermissionsGranted:false,LegalActorResolution:decision,
 })
}
