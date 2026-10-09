package api

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strings"

    "github.com/baobab-platform/baobab-cp/internal/auth"
    "github.com/baobab-platform/baobab-cp/internal/contracts"
    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/service/onboarding"
    "github.com/baobab-platform/baobab-cp/internal/store"
    "github.com/go-chi/chi/v5"
)

// LA-03's v2 routes are intentionally disabled by default. They validate
// against byte-for-byte Shared LA-01 schemas pinned in contracts.lock.yaml
// and must never bypass existing privileged human authorisation.
type organisationFirstRegistrationStore interface {
    PrepareOnboardingOrganisation(context.Context,string,store.RequestMetadata,string,string,string,string)(string,error)
    RegisterTenantV2(context.Context,string,store.RequestMetadata,domain.RegisterTenantV2,store.RegistrationStep)(domain.Operation,error)
}
var registrationV2Schema=contracts.MustSchema("control-plane/v2/tenant-registration.schema.json")

func (a *API) prepareOrganisationV2(w http.ResponseWriter,r *http.Request){
    if a.onboarding==nil{
        problem(w,r,http.StatusServiceUnavailable,"ONBOARDING_UNAVAILABLE","onboarding is unavailable",true)
        return
    }
    db,ok:=a.store.(organisationFirstRegistrationStore)
    if !ok{
        problem(w,r,http.StatusServiceUnavailable,"ORGANISATION_FIRST_UNAVAILABLE","Organisation-first persistence is unavailable",true)
        return
    }
    key,ok:=idempotencyKey(w,r);if !ok{return}
    var body struct {
        PolicyReference string `json:"identity_resolution_policy_reference"`
        EvidenceReference string `json:"evidence_reference"`
    }
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,65536))
    decoder.DisallowUnknownFields()
    if err:=decoder.Decode(&body);err!=nil{
        problem(w,r,http.StatusBadRequest,"VALIDATION_FAILED","invalid Organisation review request",false)
        return
    }
    var tail any
    if err:=decoder.Decode(&tail);!errors.Is(err,io.EOF){
        problem(w,r,http.StatusBadRequest,"VALIDATION_FAILED","only one JSON object is permitted",false)
        return
    }
    if strings.TrimSpace(body.PolicyReference)==""||strings.TrimSpace(body.EvidenceReference)==""{
        problem(w,r,http.StatusBadRequest,"VALIDATION_FAILED","review policy and evidence are required",false)
        return
    }
    _,_,ok=resolveActor(w,r,a.identities,false);if !ok{return}
    principal,_:=auth.PrincipalFromContext(r.Context())
    id,err:=db.PrepareOnboardingOrganisation(r.Context(),key,requestMetadata(r,principal),
        chi.URLParam(r,"requestID"),body.PolicyReference,body.EvidenceReference)
    if err!=nil{
        if errors.Is(err,store.ErrIdempotencyConflict){
            problem(w,r,http.StatusConflict,"ORGANISATION_IDENTITY_CONFLICT","the approved identity binding differs from the reviewed source",false)
        }else{
            problem(w,r,http.StatusConflict,"ORGANISATION_REVIEW_REQUIRED","independent reviewed Organisation binding could not be established",false)
        }
        return
    }
    writeJSON(w,http.StatusOK,map[string]string{"organisation_id":id,"tenant_onboarding_request_id":chi.URLParam(r,"requestID")})
}

func (a *API) registerV2(w http.ResponseWriter,r *http.Request){
    if a.onboarding==nil{
        problem(w,r,http.StatusServiceUnavailable,"ONBOARDING_UNAVAILABLE","onboarding is unavailable",true)
        return
    }
    db,ok:=a.store.(organisationFirstRegistrationStore)
    if !ok{
        problem(w,r,http.StatusServiceUnavailable,"ORGANISATION_FIRST_UNAVAILABLE","Organisation-first persistence is unavailable",true)
        return
    }
    key,ok:=idempotencyKey(w,r);if !ok{return}
    var command domain.RegisterTenantV2
    if !decodeContract(w,r,registrationV2Schema,&command){return}
    command.Basis=domain.RegistrationOnboarding
    command.TenantID=domain.NewTenantID()
    if err:=command.Validate();err!=nil{
        problem(w,r,http.StatusBadRequest,"VALIDATION_FAILED",err.Error(),false)
        return
    }
    principalID,auditActor,ok:=resolveActor(w,r,a.identities,false);if !ok{return}
    principal,_:=auth.PrincipalFromContext(r.Context())
    step:=a.onboarding.RegistrationStepV2(onboarding.Actor{
        PrincipalID:principalID,Audit:auditActor,
    },command)
    operation,err:=db.RegisterTenantV2(r.Context(),key,requestMetadata(r,principal),command,step)
    switch{
    case err==nil:
        w.Header().Set("Location","/v1/operations/"+operation.OperationID)
        writeJSON(w,http.StatusAccepted,operation)
    case errors.Is(err,store.ErrIdempotencyConflict):
        problem(w,r,http.StatusConflict,"IDEMPOTENCY_KEY_REUSED","idempotency key has different authority or identity",false)
    case errors.Is(err,onboarding.ErrTransition):
        problem(w,r,http.StatusConflict,"TENANT_ONBOARDING_REQUEST_NOT_AUTHORISED","onboarding request is not authorised",false)
    case errors.Is(err,onboarding.ErrDesiredStateMismatch):
        problem(w,r,http.StatusUnprocessableEntity,"DESIRED_STATE_MISMATCH","registration does not match authorised desired state",false)
    default:
        problem(w,r,http.StatusConflict,"ORGANISATION_AUTHORITY_NOT_SATISFIED",
          "governed identity, binding or provider prerequisites are not satisfied",false)
    }
}
