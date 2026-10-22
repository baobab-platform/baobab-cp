package api

// INT-03C default-off, nonproduction-only Principal disable. This is a
// separate governed CP identity mutation, NOT generic Principal self-service,
// IAM client revocation, or synthetic SQL fixture. Two existing, independent
// ACTIVE CP human administrators must authorise one digest-bound operation.
import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "regexp"

 "github.com/baobab-platform/baobab-cp/internal/repository"
 "github.com/go-chi/chi/v5"
)

type int03CPrincipalStore interface {
 ProposeInt03C(context.Context,string,string,string,string,string,string,string) (repository.Int03CDeactivation,error)
 ApproveInt03C(context.Context,string,string,string) (repository.Int03CDeactivation,error)
 ReadInt03CPrincipal(context.Context,string) (string,error)
}

type int03CPrincipalHandler struct {
 store int03CPrincipalStore
 identities repository.IdentityRepository
}

var (
 int03CUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
 int03CHex = regexp.MustCompile(`^[0-9a-f]{32}$`)
 int03CSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
 int03CDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func int03CDecode(w http.ResponseWriter,r *http.Request, target any) bool {
 r.Body=http.MaxBytesReader(w,r.Body,4096)
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(target);err!=nil{
  problem(w,r,http.StatusBadRequest,"INT03C_INVALID_REQUEST","closed governance request required",false)
  return false
 }
 var trailing any
 if err:=decoder.Decode(&trailing);err!=io.EOF{
  problem(w,r,http.StatusBadRequest,"INT03C_INVALID_REQUEST","trailing request data denied",false)
  return false
 }
 return true
}

func (h int03CPrincipalHandler) deny(w http.ResponseWriter,r *http.Request) {
 // Do not leak whether the Principal belongs to another Organisation,
 // lacks an issuer mapping, or was already disabled by a competing checker.
 problem(w,r,http.StatusConflict,"INT03C_GOVERNANCE_DENIED","independent current governed Principal authority is not satisfied",false)
}

func (h int03CPrincipalHandler) propose(w http.ResponseWriter,r *http.Request) {
 type input struct {
  Issuer string `json:"issuer"`
  Subject string `json:"subject"`
  RunID string `json:"run_id"`
  SourceSHA string `json:"source_sha"`
  IdempotencyKey string `json:"idempotency_key"`
 }
 var v input
 if !int03CDecode(w,r,&v){return}
 target:=chi.URLParam(r,"principalID")
 if !int03CUUID.MatchString(target) || !int03CUUID.MatchString(v.IdempotencyKey) ||
   !int03CHex.MatchString(v.RunID) || !int03CSHA.MatchString(v.SourceSHA) {
  h.deny(w,r);return
 }
 maker,_,ok:=resolveActor(w,r,h.identities,false)
 if !ok{return}
 out,err:=h.store.ProposeInt03C(r.Context(),target,v.Issuer,v.Subject,v.RunID,v.SourceSHA,maker,v.IdempotencyKey)
 if err!=nil{h.deny(w,r);return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusAccepted,out)
}

func (h int03CPrincipalHandler) approve(w http.ResponseWriter,r *http.Request) {
 var v struct { PlanDigest string `json:"plan_digest"` }
 if !int03CDecode(w,r,&v){return}
 requestID:=chi.URLParam(r,"requestID")
 if !int03CUUID.MatchString(requestID) || !int03CDigest.MatchString(v.PlanDigest){
  h.deny(w,r);return
 }
 checker,_,ok:=resolveActor(w,r,h.identities,false)
 if !ok{return}
 out,err:=h.store.ApproveInt03C(r.Context(),requestID,v.PlanDigest,checker)
 if err!=nil || out.Decision!="COMPLETED"{h.deny(w,r);return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,out)
}

func (h int03CPrincipalHandler) read(w http.ResponseWriter,r *http.Request) {
 target:=chi.URLParam(r,"principalID")
 if !int03CUUID.MatchString(target){h.deny(w,r);return}
 checker,_,ok:=resolveActor(w,r,h.identities,false)
 if !ok{return}
 // Independent checker may read only while its own CP identity is ACTIVE.
 human,err:=h.identities.GetPrincipal(r.Context(),checker)
 if err!=nil || human.ActorType!="human" || human.Status!="ACTIVE" {h.deny(w,r);return}
 status,err:=h.store.ReadInt03CPrincipal(r.Context(),target)
 if err!=nil{h.deny(w,r);return}
 w.Header().Set("Cache-Control","private, no-store")
 writeJSON(w,http.StatusOK,map[string]string{"principal_id":target,"status":status})
}

// Closed body type must be kept strict when an operator's audit trail is
// independently rechecked, without accepting double-decoded JSON.
var _ = bytes.Equal
var _ = errors.Is
