package api

// PEO-03B: four separate, staff-only commands. No v1 admission fallback.
// Each route is independently role/scope checked in router.go; the store also
// enforces four distinct authenticated humans and an immutable evidence chain.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

type progressiveBridgeWriter interface {
	ReviewProgressiveAdmission(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ProgressiveAdmissionReviewInput) (postgres.ProgressiveBridgeReceipt, error)
	DecideProgressiveAdmission(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ProgressiveAdmissionDecisionInput) (postgres.ProgressiveBridgeReceipt, error)
	RequestProgressiveOnboarding(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ProgressiveOnboardingInput) (postgres.ProgressiveBridgeReceipt, error)
	AuthoriseProgressiveOnboarding(context.Context, string, basestore.RequestMetadata,
		string, string, postgres.ProgressiveAuthorisationInput) (postgres.ProgressiveBridgeReceipt, error)
}

type progressiveBridgeHandler struct {
	repo progressiveBridgeWriter
	api  *API
}

func (h progressiveBridgeHandler) caller(w http.ResponseWriter, r *http.Request) (string, basestore.RequestMetadata, bool) {
	actor, _, ok := resolveActor(w, r, h.api.identities, false)
	if !ok {
		return "", basestore.RequestMetadata{}, false
	}
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "authenticated human required", false)
		return "", basestore.RequestMetadata{}, false
	}
	return actor, requestMetadata(r, p), true
}

func (h progressiveBridgeHandler) action(w http.ResponseWriter, r *http.Request, stage string) {
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
	var result postgres.ProgressiveBridgeReceipt
	var err error
	decode := func(v any) bool {
		if dec.Decode(v) != nil || dec.Decode(new(any)) != io.EOF {
			problem(w, r, http.StatusBadRequest, "INVALID_PEO03_BRIDGE_COMMAND",
				"provide exactly one reviewed v2 admission command", false)
			return false
		}
		return true
	}
	switch stage {
	case "review":
		var v postgres.ProgressiveAdmissionReviewInput
		if !decode(&v) { return }
		result, err = h.repo.ReviewProgressiveAdmission(r.Context(), key, meta, actor, chi.URLParam(r, "applicationID"), v)
	case "decide":
		var v postgres.ProgressiveAdmissionDecisionInput
		if !decode(&v) { return }
		result, err = h.repo.DecideProgressiveAdmission(r.Context(), key, meta, actor, chi.URLParam(r, "reviewID"), v)
	case "request":
		var v postgres.ProgressiveOnboardingInput
		if !decode(&v) { return }
		result, err = h.repo.RequestProgressiveOnboarding(r.Context(), key, meta, actor, chi.URLParam(r, "decisionID"), v)
	case "authorise":
		var v postgres.ProgressiveAuthorisationInput
		if !decode(&v) { return }
		result, err = h.repo.AuthoriseProgressiveOnboarding(r.Context(), key, meta, actor, chi.URLParam(r, "requestID"), v)
	default:
		problem(w, r, http.StatusNotFound, "UNKNOWN_BRIDGE_STAGE", "unknown admission stage", false)
		return
	}
	if err != nil {
		if errors.Is(err, postgres.ErrProgressiveBridgeDenied) {
			problem(w, r, http.StatusConflict, "PROGRESSIVE_ADMISSION_DENIED",
				"the required independent evidence, active sponsorship or separation of duties is missing", false)
		} else {
			problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
				"the governed admission command could not be completed", true)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, result)
}
func (h progressiveBridgeHandler) review(w http.ResponseWriter, r *http.Request) { h.action(w, r, "review") }
func (h progressiveBridgeHandler) decide(w http.ResponseWriter, r *http.Request) { h.action(w, r, "decide") }
func (h progressiveBridgeHandler) request(w http.ResponseWriter, r *http.Request) { h.action(w, r, "request") }
func (h progressiveBridgeHandler) authorise(w http.ResponseWriter, r *http.Request) { h.action(w, r, "authorise") }
