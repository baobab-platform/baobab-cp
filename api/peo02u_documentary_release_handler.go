// PEO-02U — human maker/checker release of scoped documentary holds.
// Never accept applicant attestations, automatic appeal release or missing
// OEV-03 source-backed verification. Default off in all environments.
package api

import (
	"context"
	"net/http"

	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

type documentaryReleaseWriter interface {
	ProposeDocumentaryRelease(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.DocumentaryReleaseProposal) (postgres.ReviewReceipt, error)
	DecideDocumentaryRelease(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.DocumentaryReleaseDecision) (postgres.ReviewReceipt, error)
}

type documentaryReleaseHandler struct {
	api  *API
	repo documentaryReleaseWriter
}

func (h documentaryReleaseHandler) propose(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := (foundingGovernanceHandler{api: h.api}).caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var in postgres.DocumentaryReleaseProposal
	if !decodeReviewBody(w, r, &in) {
		return
	}
	receipt, err := h.repo.ProposeDocumentaryRelease(r.Context(), key, meta,
		actor, chi.URLParam(r, "restrictionID"), in)
	if err != nil {
		(documentaryReviewHandler{api: h.api}).fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, receipt)
}

func (h documentaryReleaseHandler) decide(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := (foundingGovernanceHandler{api: h.api}).caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var in postgres.DocumentaryReleaseDecision
	if !decodeReviewBody(w, r, &in) {
		return
	}
	receipt, err := h.repo.DecideDocumentaryRelease(r.Context(), key, meta,
		actor, chi.URLParam(r, "intentID"), in)
	if err != nil {
		(documentaryReviewHandler{api: h.api}).fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}
