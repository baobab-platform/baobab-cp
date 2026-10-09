// LA-04C privileged mandate governance. These handlers can record an
// independently reviewed APPROVE/REJECT decision but CANNOT activate an actor.
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/go-chi/chi/v5"
)

type legalActorMandateStore interface {
	ProposeOperatingLegalActorMandate(context.Context,string,basestore.RequestMetadata,string,legalactor.ProposeCommand)(legalactor.CommandReceipt,error)
	DecideOperatingLegalActorMandate(context.Context,string,basestore.RequestMetadata,string,string,legalactor.DecideCommand)(legalactor.CommandReceipt,error)
}

var (
	mandateProposeSchema = contracts.MustSchema("organisation/v2/legal-actor-mandate-commands.schema.json#/$defs/ProposeMandateRequest")
	mandateDecideSchema = contracts.MustSchema("organisation/v2/legal-actor-mandate-commands.schema.json#/$defs/DecideMandateRequest")
)

type legalActorMandateHandler struct {
	store legalActorMandateStore
	api *API
}

func (h legalActorMandateHandler) propose(w http.ResponseWriter,r *http.Request) {
	key,ok:=idempotencyKey(w,r)
	if !ok{return}
	var body legalactor.ProposeCommand
	if !decodeContract(w,r,mandateProposeSchema,&body){return}
	makerID,_,ok:=resolveActor(w,r,h.api.identities,false)
	if !ok{return}
	principal,_:=auth.PrincipalFromContext(r.Context())
	receipt,err:=h.store.ProposeOperatingLegalActorMandate(r.Context(),key,requestMetadata(r,principal),makerID,body)
	if err!=nil {h.fail(w,r,err);return}
	writeJSON(w,http.StatusAccepted,receipt)
}

func (h legalActorMandateHandler) decide(w http.ResponseWriter,r *http.Request) {
	key,ok:=idempotencyKey(w,r)
	if !ok{return}
	var body legalactor.DecideCommand
	if !decodeContract(w,r,mandateDecideSchema,&body){return}
	checkerID,_,ok:=resolveActor(w,r,h.api.identities,false)
	if !ok{return}
	principal,_:=auth.PrincipalFromContext(r.Context())
	receipt,err:=h.store.DecideOperatingLegalActorMandate(r.Context(),key,
		requestMetadata(r,principal),checkerID,chi.URLParam(r,"mandateID"),body)
	if err!=nil{h.fail(w,r,err);return}
	writeJSON(w,http.StatusAccepted,receipt)
}

func (h legalActorMandateHandler) fail(w http.ResponseWriter,r *http.Request,err error){
	switch {
	case errors.Is(err,basestore.ErrIdempotencyConflict):
		problem(w,r,http.StatusConflict,"IDEMPOTENCY_KEY_REUSED","a different mandate command used this key",false)
	default:
		// Fail closed: do not leak whether another tenant's legal entity,
		// reviewer, evidence, mandate or corporate relationship exists.
		problem(w,r,http.StatusConflict,"LEGAL_ACTOR_MANDATE_NOT_AUTHORISED",
			"independent reviewer, scoped identity or evidence prerequisite not satisfied",false)
	}
}
