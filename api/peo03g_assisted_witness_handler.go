package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/go-chi/chi/v5"
)

type assistedWitnessWriter interface {
	ProposeAssistedWitness(context.Context,authority.AssistedFinding,string,time.Time,repository.AuditActor)(authority.AssistedFinding,error)
	DecideAssistedWitness(context.Context,string,string,string,string,time.Time,repository.AuditActor)(authority.AssistedFinding,error)
}

type assistedWitnessHandler struct {
	api *API
	repo assistedWitnessWriter
}

func (h assistedWitnessHandler) propose(w http.ResponseWriter,r *http.Request){
	w.Header().Set("Cache-Control","no-store")
	_,actor,ok:=resolveActor(w,r,h.api.identities,false)
	if !ok{return}
	key:=r.Header.Get("Idempotency-Key")
	if len(key)<16||len(key)>128{
		problem(w,r,http.StatusBadRequest,"INVALID_IDEMPOTENCY_KEY","request key required",false)
		return
	}
	var req struct {
		CaseID string `json:"verification_case_id"`
		ClaimID string `json:"claim_id"`
		SourceID string `json:"source_id"`
		EvidenceID string `json:"evidence_id"`
		OfficialReference string `json:"official_reference"`
		ResponseDigest string `json:"response_digest"`
		ObservedAt time.Time `json:"observed_at"`
		FreshnessUntil time.Time `json:"freshness_until"`
	}
	body:=http.MaxBytesReader(w,r.Body,4096)
	defer body.Close()
	dec:=json.NewDecoder(body);dec.DisallowUnknownFields()
	if dec.Decode(&req)!=nil||dec.Decode(new(any))!=io.EOF {
		problem(w,r,http.StatusBadRequest,"INVALID_OFFICIAL_WITNESS",
			"sanitized source witness only; credentials and PINs forbidden",false)
		return
	}
	f:=authority.AssistedFinding{IntentID: "asrs_"+newRequestID(),
		CaseID:req.CaseID,ClaimID:req.ClaimID,SourceID:req.SourceID,
		EvidenceID:req.EvidenceID,OfficialReference:req.OfficialReference,
		ObservedAt:req.ObservedAt,FreshnessUntil:req.FreshnessUntil,
		ResponseDigest:req.ResponseDigest,ProvenanceRef:"evidence:"+req.EvidenceID,
		Witness:actor.ActorID,Status:"PENDING"}
	created,err:=h.repo.ProposeAssistedWitness(r.Context(),f,key,time.Now().UTC(),actor)
	if err!=nil{
		problem(w,r,http.StatusUnprocessableEntity,"OFFICIAL_WITNESS_DENIED",
			"source, case, evidence or independent witness not accepted",false)
		return
	}
	writeJSON(w,http.StatusAccepted,created)
}

func (h assistedWitnessHandler) decide(w http.ResponseWriter,r *http.Request){
	w.Header().Set("Cache-Control","no-store")
	_,actor,ok:=resolveActor(w,r,h.api.identities,false)
	if !ok{return}
	key:=r.Header.Get("Idempotency-Key")
	if len(key)<16||len(key)>128{
		problem(w,r,http.StatusBadRequest,"INVALID_IDEMPOTENCY_KEY","request key required",false)
		return
	}
	var req struct {
		Outcome string `json:"outcome"`
		ReasonCode string `json:"reason_code"`
	}
	body:=http.MaxBytesReader(w,r.Body,1024)
	defer body.Close()
	dec:=json.NewDecoder(body);dec.DisallowUnknownFields()
	if dec.Decode(&req)!=nil||dec.Decode(new(any))!=io.EOF||
		(req.Outcome!="ATTESTED"&&req.Outcome!="REJECTED")||
		(req.ReasonCode!="OFFICIAL_PORTAL_REVIEWED"&&
			req.ReasonCode!="OFFICIAL_SOURCE_MISMATCH"&&
			req.ReasonCode!="EVIDENCE_INSUFFICIENT"){
		problem(w,r,http.StatusBadRequest,"INVALID_OFFICIAL_WITNESS_DECISION",
			"bounded outcome and reason code required",false)
		return
	}
	out,err:=h.repo.DecideAssistedWitness(r.Context(),chi.URLParam(r,"intentID"),
		key,req.Outcome,req.ReasonCode,time.Now().UTC(),actor)
	if err!=nil{
		problem(w,r,http.StatusForbidden,"OFFICIAL_WITNESS_DECISION_DENIED",
			"independent current human authorisation required",false)
		return
	}
	writeJSON(w,http.StatusOK,out)
}
