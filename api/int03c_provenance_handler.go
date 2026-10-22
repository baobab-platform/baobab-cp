package api

// Privileged, CP-owned attestation of an already existing synthetic Principal.
// The maker submits an exact run/source and original creation-proof digest;
// a distinct current human administrator checks the CP issuer mapping before
// the deactivation service may cite this immutable provenance.
import (
 "net/http"

 "github.com/go-chi/chi/v5"
)

func (h int03CPrincipalHandler) proposeProvenance(w http.ResponseWriter,r *http.Request){
 var input struct {
  PrincipalID string `json:"principal_id"`
  Issuer string `json:"issuer"`
  Subject string `json:"subject"`
  RunID string `json:"run_id"`
  SourceSHA string `json:"source_sha"`
  CreationDigest string `json:"source_evidence_digest"`
 }
 if !int03CDecode(w,r,&input){return}
 if !int03CUUID.MatchString(input.PrincipalID)||!int03CHex.MatchString(input.RunID)||
  !int03CSHA.MatchString(input.SourceSHA)||!int03CDigest.MatchString(input.CreationDigest){
  h.deny(w,r);return
 }
 maker,_,ok:=resolveActor(w,r,h.identities,false)
 if !ok{return}
 out,err:=h.store.ProposeInt03CProvenance(r.Context(),input.PrincipalID,input.Issuer,
  input.Subject,input.RunID,input.SourceSHA,input.CreationDigest,maker)
 if err!=nil{h.deny(w,r);return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusAccepted,out)
}

func (h int03CPrincipalHandler) approveProvenance(w http.ResponseWriter,r *http.Request){
 var input struct{ SourceEvidenceDigest string `json:"source_evidence_digest"` }
 if !int03CDecode(w,r,&input){return}
 id:=chi.URLParam(r,"provenanceID")
 if !int03CUUID.MatchString(id)||!int03CDigest.MatchString(input.SourceEvidenceDigest){
  h.deny(w,r);return
 }
 checker,_,ok:=resolveActor(w,r,h.identities,false)
 if !ok{return}
 out,err:=h.store.ApproveInt03CProvenance(r.Context(),id,checker)
 if err!=nil||out.SourceEvidenceDigest!=input.SourceEvidenceDigest||
  out.Status!="INDEPENDENTLY_APPROVED_UNATTESTED"{
  h.deny(w,r);return
 }
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,out)
}
