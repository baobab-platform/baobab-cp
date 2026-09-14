package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/resolver"
	"github.com/nabhold/baobab-cp/internal/service"
)

// CapabilityExplainHandler exposes ADR-BCP-004 §77 ("Context Explainability")
// and ADR-BCP-003 §80 ("Explainability") as a privileged diagnostic endpoint:
// POST /v1/capabilities/explain. Unlike CapabilityResolveHandler and
// CapabilityResolveBatchHandler -- which deliberately collapse every failure
// to a generic RESOLUTION_FAILED / DENIED per ADR-0008 §45's "reason codes
// SHALL NOT expose sensitive information indiscriminately to external
// clients" -- this endpoint's entire purpose is the opposite: reveal exactly
// which stage of resolver.ResolutionPipeline a request reached and why it
// stopped there (resolver.ResolutionTrace), for the support/audit/debugging/
// operations use named in §76's "Context Inspection". It is therefore gated
// on the admin actor type and a distinct "capabilities:explain" scope, never
// the workload "context:resolve" scope the resolve endpoints use, and (like
// the existing admin-only GET /v1/entitlements) is not restricted to the
// calling principal's own tenant -- an operator explaining a customer's
// failed resolution is exactly this endpoint's purpose.
//
// §80's own example additionally shows per-candidate detail ("Grant H
// rejected: market mismatch", "Binding B2 rejected: contract mismatch").
// resolver.ResolutionPipeline does not track rejected candidates today --
// CapabilityResolverImpl.Resolve and EntitlementResolverImpl.Resolve each
// return only the winning candidate or a terminal error, never a
// per-candidate trail. Building that out is a resolver-internal change
// independent of this API surface, not something this handler can fabricate
// from data the pipeline doesn't produce. What this handler explains instead
// is the resolver.ResolutionTrace the pipeline already builds for every
// request: which record matched at each stage it reached (mapping, grant,
// binding, engine instance), and its terminal outcome and reason.
type CapabilityExplainHandler struct {
	Contexts repository.ContextRepository
	Service  service.ResolutionService
}

type capabilityExplainRequest struct {
	ContextID         string `json:"context_id"`
	CanonicalEntityID string `json:"canonical_entity_id"`
	CapabilityKey     string `json:"capability_key"`
}

func (h CapabilityExplainHandler) Explain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST only", false)
		return
	}
	var req capabilityExplainRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false)
		return
	}
	if req.ContextID == "" || req.CanonicalEntityID == "" || req.CapabilityKey == "" {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "context_id, canonical_entity_id and capability_key are required", false)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "human" || !principal.HasScope("capabilities:explain") {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified admin identity is required", false)
		return
	}
	if h.Contexts == nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context persistence is temporarily unavailable", true)
		return
	}
	trustedContext, err := h.Contexts.GetContext(r.Context(), req.ContextID)
	if errors.Is(err, repository.ErrContextNotFound) {
		problem(w, r, http.StatusNotFound, "CONTEXT_NOT_FOUND", "the referenced context_id does not exist or has expired", false)
		return
	}
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "context lookup failed", true)
		return
	}

	result, resolveErr := h.Service.Resolve(r.Context(), service.ResolutionRequest{
		TenantID:          trustedContext.TenantID,
		CanonicalEntityID: req.CanonicalEntityID,
		CapabilityKey:     req.CapabilityKey,
		Context:           trustedContext,
	})

	// A failure the pipeline itself attributed to a stage (mapping, grant,
	// binding, ...) carries a resolver.ResolutionTrace via *resolver.
	// ResolutionError. A failure before the pipeline ever ran -- e.g.
	// ResolutionService.Resolve's own repository lookups finding no
	// candidates for this canonical_entity_id at all -- has no such trace,
	// but its error message ("no mappings for X") is itself a perfectly
	// good explanation and is reported the same way rather than collapsed
	// to a generic 500: this endpoint's whole purpose is to say why, not to
	// hide it.
	trace := result.Trace
	var resolutionErr *resolver.ResolutionError
	switch {
	case resolveErr == nil:
	case errors.As(resolveErr, &resolutionErr):
		trace = resolutionErr.Trace
	default:
		trace.Outcome = "FAILED"
		trace.Reason = resolveErr.Error()
	}

	response := map[string]any{
		"context_id":          trustedContext.ID,
		"tenant_id":           trustedContext.TenantID,
		"canonical_entity_id": req.CanonicalEntityID,
		"capability_key":       req.CapabilityKey,
	}
	response["outcome"] = trace.Outcome
	response["reason"] = trace.Reason
	response["mapping_id"] = trace.MappingID
	response["grant_id"] = trace.GrantID
	response["binding_id"] = trace.BindingID
	response["engine_instance_id"] = trace.EngineInstanceID
	if resolveErr == nil {
		response["policy"] = map[string]any{
			"allowed": result.Policy.Allowed,
			"reason":  result.Policy.Reason,
		}
		response["topology"] = map[string]any{
			"id":          result.Topology.ID,
			"environment": result.Topology.Environment,
		}
	}

	writeJSON(w, http.StatusOK, response)
}
