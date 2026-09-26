package api

import (
	"encoding/json"
	"errors"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/resolver"
	"github.com/baobab-platform/baobab-cp/internal/service"
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

// capabilityExplainRequest and capabilityExplanation are Shared's
// control-plane/v1 capability-explanation.schema.json CapabilityExplanationRequest
// and CapabilityExplanation.
type capabilityExplainRequest struct {
	ContextID         string `json:"context_id"`
	CanonicalEntityID string `json:"canonical_entity_id"`
	CapabilityKey     string `json:"capability_key"`
}

type capabilityExplanation struct {
	ContextID         string                     `json:"context_id"`
	TenantID          string                     `json:"tenant_id"`
	CanonicalEntityID string                     `json:"canonical_entity_id"`
	CapabilityKey     string                     `json:"capability_key"`
	Outcome           string                     `json:"outcome"`
	Reason            string                     `json:"reason"`
	MappingID         string                     `json:"mapping_id,omitempty"`
	GrantID           string                     `json:"grant_id,omitempty"`
	BindingID         string                     `json:"binding_id,omitempty"`
	EngineInstanceID  string                     `json:"engine_instance_id,omitempty"`
	Policy            *capabilityExplainPolicy   `json:"policy,omitempty"`
	Topology          *capabilityExplainTopology `json:"topology,omitempty"`
}

type capabilityExplainPolicy struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

type capabilityExplainTopology struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
}

var (
	// explainOpaqueIDPattern is the schema's opaqueId and, with a minimum
	// length of 3, domain.schema.json's canonicalEntityId.
	explainOpaqueIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
	// sharedCapabilityKeyPattern is capability/v1 domain.schema.json's
	// capabilityKey: <domain>.<resource>.<action>. The Control Plane's own
	// registry accepts a wider grammar; a key outside Shared's cannot name a
	// canonical capability, so it is refused rather than explained.
	sharedCapabilityKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*\.[a-z][a-z0-9]*(?:-[a-z0-9]+)*\.[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
)

// explainReasonLimit is the schema's maxLength for reasons.
const explainReasonLimit = 2048

func explainReason(reason string) string {
	if len(reason) <= explainReasonLimit {
		return reason
	}
	cut := explainReasonLimit
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return reason[:cut]
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
	if len(req.ContextID) > 128 || !explainOpaqueIDPattern.MatchString(req.ContextID) ||
		len(req.CanonicalEntityID) < 3 || len(req.CanonicalEntityID) > 128 || !explainOpaqueIDPattern.MatchString(req.CanonicalEntityID) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "context_id or canonical_entity_id is malformed", false)
		return
	}
	if len(req.CapabilityKey) < 5 || len(req.CapabilityKey) > 128 || !sharedCapabilityKeyPattern.MatchString(req.CapabilityKey) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", "capability_key must be a <domain>.<resource>.<action> capability key", false)
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

	response := capabilityExplanation{
		ContextID:         trustedContext.ID,
		TenantID:          trustedContext.TenantID,
		CanonicalEntityID: req.CanonicalEntityID,
		CapabilityKey:     req.CapabilityKey,
		Outcome:           "FAILED",
		Reason:            explainReason(trace.Reason),
		MappingID:         trace.MappingID,
		GrantID:           trace.GrantID,
		BindingID:         trace.BindingID,
		EngineInstanceID:  engineInstanceKey(trace.EngineInstanceID),
	}
	if resolveErr == nil {
		response.Outcome = "ROUTED"
		response.Policy = &capabilityExplainPolicy{Allowed: result.Policy.Allowed, Reason: explainReason(result.Policy.Reason)}
		response.Topology = &capabilityExplainTopology{ID: domain.EngineInstanceKey(result.Topology.ID), Environment: result.Topology.Environment}
	}

	writeJSON(w, http.StatusOK, response)
}

// engineInstanceKey is the canonical identifier of a traced engine instance,
// or empty when resolution never reached one.
func engineInstanceKey(id string) string {
	if id == "" {
		return ""
	}
	return domain.EngineInstanceKey(id)
}
