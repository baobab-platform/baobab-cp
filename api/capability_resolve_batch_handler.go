package api

import (
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// CapabilityResolveBatchHandler serves POST /v1/capabilities/resolve-batch:
// up to 50 capabilities for one context, each decided and recorded on its
// own (a batch has no atomicity across its members).
type CapabilityResolveBatchHandler struct {
	Contexts repository.ContextRepository
	Service  service.CapabilityResolutionService
}

type batchResolutionRequest struct {
	ContextID     string   `json:"context_id"`
	Capabilities  []string `json:"capabilities"`
	CorrelationID string   `json:"correlation_id"`
}

type batchResolutionBody struct {
	ContextID  string           `json:"context_id"`
	Decisions  []resolutionBody `json:"decisions"`
	ResolvedAt time.Time        `json:"resolved_at"`
}

func (h CapabilityResolveBatchHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req batchResolutionRequest
	if !decodeRaw(w, r, batchResolutionRequestSchema, raw, &req) {
		return
	}
	trusted, ok := redeemContext(w, r, h.Contexts, req.ContextID)
	if !ok {
		return
	}
	if h.Service.Store == nil {
		problem(w, r, http.StatusServiceUnavailable, "CAPABILITY_RESOLUTION_UNAVAILABLE", "capability resolution is temporarily unavailable", true)
		return
	}
	out := batchResolutionBody{ContextID: trusted.ID, Decisions: make([]resolutionBody, 0, len(req.Capabilities))}
	for _, key := range req.Capabilities {
		rec, err := h.Service.Resolve(r.Context(), service.CapabilityResolutionRequest{Context: trusted, CapabilityKey: key, CorrelationID: req.CorrelationID})
		if err != nil {
			problem(w, r, http.StatusServiceUnavailable, "CAPABILITY_RESOLUTION_UNAVAILABLE", "the capabilities could not be resolved", true)
			return
		}
		body, err := resolutionOf(rec)
		if err != nil {
			problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the resolution could not be rendered", true)
			return
		}
		out.Decisions = append(out.Decisions, body)
		if out.ResolvedAt.Before(rec.ResolvedAt) {
			out.ResolvedAt = rec.ResolvedAt
		}
	}
	writeJSON(w, http.StatusOK, out)
}
