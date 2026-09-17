package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/service"
)

// ResolverHandler exposes the composed resolution service over HTTP.
type ResolverHandler struct {
	Service           service.ResolutionService
	ContextResolution service.ContextResolutionService
}

type resolverRequest struct {
	TenantID          string `json:"tenant_id"`
	CanonicalEntityID string `json:"canonical_entity_id"`
	CapabilityKey     string `json:"capability_key"`
}

func (h ResolverHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST only", false)
		return
	}

	var req resolverRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" || !principal.HasScope("context:resolve") {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
		return
	}
	tenantID, ok := resolveWorkloadTenant(principal.TenantID, req.TenantID)
	if !ok {
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "requested tenant does not match verified workload identity", false)
		return
	}
	// contracts/control-plane/v1/canonical-mapping.schema.json's
	// resolutionRequest already requires canonical_entity_id -- this
	// repository just never accepted it (docs/governance/gate-iam-0-
	// discovery.md R-3), so nothing downstream could resolve an actual
	// entity, only the tenant itself.
	if req.CanonicalEntityID == "" || req.CapabilityKey == "" {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "canonical_entity_id and capability_key are required", false)
		return
	}
	// ADR-BCP-004 §52: resolve identity -> resolve tenant -> validate
	// principal<->tenant relationship -> resolve legal entity, all fail
	// closed. This endpoint does not accept organisation_id today -- "" skips
	// the ADR-BCP-016 stage entirely, matching this handler's pre-ZB-03.3
	// behavior; see PlatformContextHandler for the endpoint that does.
	operationCtx, trustedContext, err := h.ContextResolution.Resolve(r.Context(), principal, tenantID, "", correlationID(r), time.Now())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIdentityResolutionFailed):
			problem(w, r, http.StatusForbidden, "IDENTITY_RESOLUTION_FAILED", "the authenticated identity could not be resolved", false)
		case errors.Is(err, service.ErrTenantNotActive):
			problem(w, r, http.StatusForbidden, "TENANT_NOT_ACTIVE", "the tenant is not active", false)
		default:
			problem(w, r, http.StatusForbidden, "CONTEXT_DENIED", "trusted Context could not be constructed", false)
		}
		return
	}
	r = r.WithContext(operationCtx)

	result, err := h.Service.Resolve(operationCtx, service.ResolutionRequest{
		TenantID:          trustedContext.TenantID,
		CanonicalEntityID: req.CanonicalEntityID,
		CapabilityKey:     req.CapabilityKey,
		Context:           trustedContext,
	})
	if err != nil {
		// ADR-0008 §45 ("Denial Model"): reason codes SHALL not expose
		// sensitive information indiscriminately to external clients. err
		// here is a *resolver.ResolutionError wrapping the pipeline's raw
		// internal cause (mapping/capability/policy/topology internals,
		// e.g. "mapping not found", "engine instance missing") — that detail
		// is an operator diagnostic, not a client-facing message. Mirrors
		// how resolveContext (api/context.go) already treats
		// store.ErrContextDenied with a fixed, opaque detail string.
		problem(w, r, http.StatusBadRequest, "RESOLUTION_FAILED", "the request could not be resolved to an authorized routing decision", false)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tenant_id":      result.Context.TenantID,
		"capability_key": req.CapabilityKey,
		"mapping": map[string]any{
			"id":     result.Mapping.Mapping.ID,
			"status": result.Mapping.Mapping.Status,
		},
		"capability": map[string]any{
			"binding_mode":       result.Capability.BindingMode,
			"engine_instance_id": result.Capability.EngineInstanceID,
		},
		"policy": map[string]any{
			"allowed": result.Policy.Allowed,
			"reason":  result.Policy.Reason,
		},
		"topology": map[string]any{
			"id":          result.Topology.ID,
			"environment": result.Topology.Environment,
		},
	})
}
