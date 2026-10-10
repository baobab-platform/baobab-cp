// PEO-02S: separately authenticated, nonproduction human review/appeals.
// Neither applicant input nor a review outcome grants provider/legal authority.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

type documentaryReviewWriter interface {
	ProposeDocumentaryReview(context.Context, string, basestore.RequestMetadata,
		string, string, int, []byte) (postgres.ReviewReceipt, error)
	DecideDocumentaryReview(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ReviewDecisionInput) (postgres.ReviewReceipt, error)
	FileDocumentaryReviewAppeal(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ReviewAppealInput) (postgres.ReviewReceipt, error)
	DecideDocumentaryReviewAppeal(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ReviewAppealDecisionInput) (postgres.ReviewReceipt, error)
}
type documentaryReviewHandler struct {
	api  *API
	repo documentaryReviewWriter
}

func (h documentaryReviewHandler) caller(w http.ResponseWriter, r *http.Request) (string, basestore.RequestMetadata, bool) {
	return (foundingGovernanceHandler{api: h.api}).caller(w, r)
}
func (h documentaryReviewHandler) fail(w http.ResponseWriter, r *http.Request, e error) {
	if errors.Is(e, postgres.ErrDocumentaryReviewDenied) {
		problem(w, r, http.StatusConflict, "DOCUMENTARY_REVIEW_AUTHORITY_DENIED",
			"require independently reviewed, matching and current documentary evidence", false)
		return
	}
	problem(w, r, http.StatusInternalServerError, "DOCUMENTARY_REVIEW_UNAVAILABLE",
		"governed documentary review could not be committed", true)
}
func decodeReviewBody(w http.ResponseWriter, r *http.Request, target any) bool {
	raw, ok := readBody(w, r)
	if !ok {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(target) != nil || dec.Decode(new(any)) != io.EOF {
		problem(w, r, http.StatusBadRequest, "INVALID_DOCUMENTARY_REVIEW",
			"exactly one valid documentary review command is required", false)
		return false
	}
	return true
}
func (h documentaryReviewHandler) propose(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	month, err := strconv.Atoi(chi.URLParam(r, "month"))
	if err != nil || (month != 6 && month != 12 && month != 18 && month != 24) {
		problem(w, r, http.StatusBadRequest, "INVALID_REVIEW_MILESTONE", "unrecognised documentary review month", false)
		return
	}
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	result, err := h.repo.ProposeDocumentaryReview(r.Context(), key, meta, actor,
		chi.URLParam(r, "deferralID"), month, raw)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}
func (h documentaryReviewHandler) decide(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var in postgres.ReviewDecisionInput
	if !decodeReviewBody(w, r, &in) {
		return
	}
	result, err := h.repo.DecideDocumentaryReview(r.Context(), key, meta, actor, chi.URLParam(r, "reviewID"), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h documentaryReviewHandler) appeal(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var in postgres.ReviewAppealInput
	if !decodeReviewBody(w, r, &in) {
		return
	}
	result, err := h.repo.FileDocumentaryReviewAppeal(r.Context(), key, meta, actor, chi.URLParam(r, "decisionID"), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}
func (h documentaryReviewHandler) appealDecision(w http.ResponseWriter, r *http.Request) {
	actor, meta, ok := h.caller(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	var in postgres.ReviewAppealDecisionInput
	if !decodeReviewBody(w, r, &in) {
		return
	}
	result, err := h.repo.DecideDocumentaryReviewAppeal(r.Context(), key, meta, actor, chi.URLParam(r, "appealID"), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
