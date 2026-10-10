// PEO-02 founding governance routes: platform staff only, distinct approval.
// None are self-service or grant provider, legal-actor or IAM authority.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

type foundingGovernanceWriter interface {
	ProposeFoundingGovernance(context.Context, string, basestore.RequestMetadata,
		string, string, []byte) (postgres.FoundingCommandReceipt, error)
	DecideFoundingGovernance(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.FoundingDecisionInput) (postgres.FoundingCommandReceipt, error)
}
type foundingGovernanceHandler struct {
	repo foundingGovernanceWriter
	api  *API
}

func (h foundingGovernanceHandler) caller(w http.ResponseWriter, r *http.Request) (string, basestore.RequestMetadata, bool) {
	actorID, _, ok := resolveActor(w, r, h.api.identities, false)
	if !ok {
		return "", basestore.RequestMetadata{}, false
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "authenticated human required", false)
		return "", basestore.RequestMetadata{}, false
	}
	return actorID, requestMetadata(r, principal), true
}
func (h foundingGovernanceHandler) propose(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, meta, ok := h.caller(w, r)
		if !ok {
			return
		}
		key, ok := idempotencyKey(w, r)
		if !ok {
			return
		}
		raw, ok := readBody(w, r)
		if !ok {
			return
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if kind == "SPONSORSHIP" {
			var body postgres.FoundingSponsorshipInput
			if dec.Decode(&body) != nil {
				problem(w, r, http.StatusBadRequest, "INVALID_FOUNDING_INTENT", "unexpected sponsorship input", false)
				return
			}
		} else {
			var body postgres.FoundingDeferralInput
			if dec.Decode(&body) != nil {
				problem(w, r, http.StatusBadRequest, "INVALID_FOUNDING_INTENT", "unexpected documentary intent", false)
				return
			}
		}
		receipt, err := h.repo.ProposeFoundingGovernance(r.Context(), key, meta, actor, kind, raw)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, receipt)
	}
}
func (h foundingGovernanceHandler) decide(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var input postgres.FoundingDecisionInput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REVIEW", "invalid decision input", false)
		return
	}
	receipt, err := h.repo.DecideFoundingGovernance(r.Context(), key, meta, actor, chi.URLParam(r, "intentID"), input)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, receipt)
}
func (h foundingGovernanceHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, postgres.ErrFoundingAuthority) {
		// A storage or infrastructure fault is not a missing authority: do not report it as one.
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the governance command could not be processed", true)
		return
	}
	problem(w, r, http.StatusConflict, "FOUNDING_AUTHORITY_NOT_ESTABLISHED",
		"independent platform reviewer, current verified sponsorship, admission decision or named platform documentary policy is missing", false)
}
