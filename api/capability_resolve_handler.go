package api

import (
	"encoding/json"
	"errors"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"net/http"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// CapabilityResolveHandler exposes ADR-BCP-003's capability resolution
// ("POST /v1/capabilities/resolve") atop an already-resolved Context,
// redeemed by context_id (baobab-platform/shared's resolutionRequest contract)
// rather than accepting an inline context -- the caller is expected to have
// called PlatformContextHandler.Resolve first.
type CapabilityResolveHandler struct {
	Contexts repository.ContextRepository
	Service  service.ResolutionService
}

type capabilityResolveRequest struct {
	ContextID         string `json:"context_id"`
	CanonicalEntityID string `json:"canonical_entity_id"`
	CapabilityKey     string `json:"capability_key"`
}

func (h CapabilityResolveHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST only", false)
		return
	}
	var req capabilityResolveRequest
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
	if !ok || principal.ActorType != "workload" || !principal.HasScope("context:resolve") {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
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
	// A resolved Context is bound to the tenant that produced it
	// (ADR-BCP-004 §71, Context Immutability). A workload token for a
	// different tenant redeeming someone else's context_id is exactly the
	// cross-tenant leakage this check exists to prevent -- it must fail
	// closed rather than silently resolve against the wrong tenant. Uses
	// resolveWorkloadTenant, not a bare equality check, because a workload
	// token with no tenant_id claim of its own (every real workload client
	// today) has nothing to compare against; the context it is redeeming
	// was itself already tenant-validated when PlatformContextHandler
	// created it, so that is what's trusted here instead.
	if _, ok := resolveWorkloadTenant(principal.TenantID, trustedContext.TenantID); !ok {
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "the referenced context does not belong to the authenticated tenant", false)
		return
	}

	result, err := h.Service.Resolve(r.Context(), service.ResolutionRequest{
		TenantID:          trustedContext.TenantID,
		CanonicalEntityID: req.CanonicalEntityID,
		CapabilityKey:     req.CapabilityKey,
		Context:           trustedContext,
	})
	if err != nil {
		// ADR-0008 §45 ("Denial Model"): reason codes SHALL not expose
		// sensitive information indiscriminately to external clients,
		// mirroring ResolverHandler.Resolve's identical handling of the same
		// underlying *resolver.ResolutionError.
		problem(w, r, http.StatusBadRequest, "RESOLUTION_FAILED", "the request could not be resolved to an authorized routing decision", false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"context_id":     trustedContext.ID,
		"tenant_id":      result.Context.TenantID,
		"capability_key": req.CapabilityKey,
		"mapping": map[string]any{
			"id":     result.Mapping.Mapping.ID,
			"status": result.Mapping.Mapping.Status,
		},
		"capability": map[string]any{
			"binding_mode":       result.Capability.BindingMode,
			"engine_instance_id": domain.EngineInstanceKey(result.Capability.EngineInstanceID),
		},
		"policy": map[string]any{
			"allowed": result.Policy.Allowed,
			"reason":  result.Policy.Reason,
		},
		"topology": map[string]any{
			"id":          domain.EngineInstanceKey(result.Topology.ID),
			"environment": result.Topology.Environment,
		},
	})
}
