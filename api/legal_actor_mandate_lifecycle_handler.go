package api

import (
	"context"
	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/go-chi/chi/v5"
	"net/http"
)

type legalActorLifecycleStore interface {
	TransitionOperatingLegalActorMandate(context.Context, string, basestore.RequestMetadata,
		string, string, legalactor.LifecycleCommand) (legalactor.LifecycleReceipt, error)
}

var mandateLifecycleSchema = contracts.MustSchema(
	"organisation/v2/legal-actor-mandate-commands.schema.json#/$defs/MandateLifecycleRequest")

type legalActorLifecycleHandler struct {
	store legalActorLifecycleStore
	api   *API
}

func (h legalActorLifecycleHandler) activate(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "ACTIVATE")
}
func (h legalActorLifecycleHandler) terminate(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, "")
}
func (h legalActorLifecycleHandler) transition(w http.ResponseWriter, r *http.Request, only string) {
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var body legalactor.LifecycleCommand
	if !decodeContract(w, r, mandateLifecycleSchema, &body) {
		return
	}
	if only != "" && body.Action != only {
		problem(w, r, http.StatusBadRequest, "INVALID_LIFECYCLE_ACTION", "activation endpoint accepts ACTIVATE only", false)
		return
	}
	if only == "" && body.Action == "ACTIVATE" {
		problem(w, r, http.StatusBadRequest, "INVALID_LIFECYCLE_ACTION", "termination endpoint cannot activate", false)
		return
	}
	actorID, _, ok := resolveActor(w, r, h.api.identities, false)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	receipt, err := h.store.TransitionOperatingLegalActorMandate(
		r.Context(), key, requestMetadata(r, principal), actorID, chi.URLParam(r, "mandateID"), body)
	if err != nil {
		(legalActorMandateHandler{}).fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, receipt)
}
