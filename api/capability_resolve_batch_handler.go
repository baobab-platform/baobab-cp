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

// maxBatchResolveEntities bounds how many canonical entities a single
// POST /v1/capabilities/resolve-batch request may resolve, to keep one
// request from doing unbounded resolution work.
const maxBatchResolveEntities = 50

// CapabilityResolveBatchHandler exposes ADR-BCP-003 §70's batch resolution
// ("Batch resolution SHALL NOT hide per-capability failures... Each SHALL
// receive an independent decision") atop an already-resolved Context,
// redeemed by context_id exactly as CapabilityResolveHandler does.
//
// This endpoint resolves one explicitly requested capability independently
// for each canonical entity. It never substitutes a provider or product key
// for the canonical capability requested by the consumer.
type CapabilityResolveBatchHandler struct {
	Contexts repository.ContextRepository
	Service  service.ResolutionService
}

type capabilityResolveBatchRequest struct {
	ContextID          string   `json:"context_id"`
	CapabilityKey      string   `json:"capability_key"`
	CanonicalEntityIDs []string `json:"canonical_entity_ids"`
}

type capabilityResolveBatchResultItem struct {
	CanonicalEntityID string         `json:"canonical_entity_id"`
	Status            string         `json:"status"`
	Mapping           map[string]any `json:"mapping,omitempty"`
	Capability        map[string]any `json:"capability,omitempty"`
	Policy            map[string]any `json:"policy,omitempty"`
	Topology          map[string]any `json:"topology,omitempty"`
}

func (h CapabilityResolveBatchHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST only", false)
		return
	}
	var req capabilityResolveBatchRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false)
		return
	}
	if req.ContextID == "" || req.CapabilityKey == "" || len(req.CanonicalEntityIDs) == 0 {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "context_id, capability_key and at least one canonical_entity_id are required", false)
		return
	}
	if len(req.CanonicalEntityIDs) > maxBatchResolveEntities {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "canonical_entity_ids may contain at most 50 entries", false)
		return
	}
	for _, id := range req.CanonicalEntityIDs {
		if id == "" {
			problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "canonical_entity_ids must not contain empty values", false)
			return
		}
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
	// Mirrors CapabilityResolveHandler's identical cross-tenant redemption
	// guard (ADR-BCP-004 §71) -- see resolveWorkloadTenant's doc comment
	// for why this isn't a bare equality check.
	if _, ok := resolveWorkloadTenant(principal.TenantID, trustedContext.TenantID); !ok {
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "the referenced context does not belong to the authenticated tenant", false)
		return
	}

	results := make([]capabilityResolveBatchResultItem, 0, len(req.CanonicalEntityIDs))
	for _, canonicalEntityID := range req.CanonicalEntityIDs {
		result, err := h.Service.Resolve(r.Context(), service.ResolutionRequest{
			TenantID:          trustedContext.TenantID,
			CanonicalEntityID: canonicalEntityID,
			CapabilityKey:     req.CapabilityKey,
			Context:           trustedContext,
		})
		if err != nil {
			// §70: a per-item failure is reported alongside the others, not
			// hidden by aborting the whole batch. ADR-0008 §45 still applies
			// per item: no internal resolver detail is exposed.
			results = append(results, capabilityResolveBatchResultItem{CanonicalEntityID: canonicalEntityID, Status: "DENIED"})
			continue
		}
		results = append(results, capabilityResolveBatchResultItem{
			CanonicalEntityID: canonicalEntityID,
			Status:            "RESOLVED",
			Mapping: map[string]any{
				"id":     result.Mapping.Mapping.ID,
				"status": result.Mapping.Mapping.Status,
			},
			Capability: map[string]any{
				"binding_mode":       result.Capability.BindingMode,
				"engine_instance_id": domain.EngineInstanceKey(result.Capability.EngineInstanceID),
			},
			Policy: map[string]any{
				"allowed": result.Policy.Allowed,
				"reason":  result.Policy.Reason,
			},
			Topology: map[string]any{
				"id":          domain.EngineInstanceKey(result.Topology.ID),
				"environment": result.Topology.Environment,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"context_id":     trustedContext.ID,
		"tenant_id":      trustedContext.TenantID,
		"capability_key": req.CapabilityKey,
		"results":        results,
	})
}
