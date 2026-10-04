package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// Capability resolution by context_id (Shared control-plane/v1
// resolveCapability and resolveCapabilityBatch; capability/v1
// resolution.schema.json). The caller redeems a PlatformContext it resolved
// earlier; it never states a tenant, provider or engine instance.
var (
	resolutionRequestSchema      = contracts.MustSchema("capability/v1/resolution.schema.json#/$defs/resolutionRequest")
	batchResolutionRequestSchema = contracts.MustSchema("capability/v1/resolution.schema.json#/$defs/batchResolutionRequest")
)

// CapabilityResolveHandler serves POST /v1/capabilities/resolve.
type CapabilityResolveHandler struct {
	Contexts repository.ContextRepository
	// Identities is the read-only canonical-principal lookup the ownership
	// rule needs; without it no context can be consumed.
	Identities repository.IdentityRepository
	Service    service.CapabilityResolutionService
}

type resolutionRequest struct {
	CapabilityKey           string `json:"capability_key"`
	RequiredContractVersion int    `json:"required_contract_version"`
	ContextID               string `json:"context_id"`
	CorrelationID           string `json:"correlation_id"`
}

type invocationDescriptor struct {
	ServiceReference string `json:"service_reference"`
	Protocol         string `json:"protocol"`
	ContractVersion  int    `json:"contract_version"`
	ProviderID       string `json:"provider_id"`
	EngineInstanceID string `json:"engine_instance_id"`
}

type resolutionBody struct {
	ResolutionID    string                `json:"resolution_id"`
	ContextID       string                `json:"context_id"`
	CapabilityKey   string                `json:"capability_key"`
	ContractVersion int                   `json:"contract_version,omitempty"`
	Decision        string                `json:"decision"`
	ReasonCode      string                `json:"reason_code,omitempty"`
	GrantID         string                `json:"grant_id,omitempty"`
	BindingID       string                `json:"binding_id,omitempty"`
	Invocation      *invocationDescriptor `json:"invocation,omitempty"`
	ResolvedAt      time.Time             `json:"resolved_at"`
	ExpiresAt       *time.Time            `json:"expires_at,omitempty"`
	CorrelationID   string                `json:"correlation_id"`
}

// resolutionOf renders a recorded decision in the capability/v1 grammar.
// Only a RESOLVED decision names what it resolved to.
func resolutionOf(rec repository.CapabilityResolutionRecord) (resolutionBody, error) {
	body := resolutionBody{ResolutionID: rec.ResolutionID, ContextID: rec.ContextID, CapabilityKey: rec.CapabilityKey,
		Decision: rec.Decision, ReasonCode: rec.ReasonCode, ResolvedAt: rec.ResolvedAt, ExpiresAt: rec.ExpiresAt, CorrelationID: rec.CorrelationID}
	if rec.Decision != service.DecisionResolved {
		return body, nil
	}
	grant, err := domain.FormatResourceID("grant", rec.GrantID)
	if err != nil {
		return body, err
	}
	binding, err := domain.FormatResourceID("bind", rec.BindingID)
	if err != nil {
		return body, err
	}
	provider, err := domain.FormatResourceID("provider", rec.ProviderID)
	if err != nil {
		return body, err
	}
	body.ContractVersion, body.GrantID, body.BindingID = rec.ContractVersion, grant, binding
	body.Invocation = &invocationDescriptor{ServiceReference: rec.ServiceReference, Protocol: rec.Protocol, ContractVersion: rec.ContractVersion,
		ProviderID: provider, EngineInstanceID: domain.EngineInstanceKey(rec.EngineInstanceID)}
	return body, nil
}

// redeemContext returns the caller's resolved context: a workload holding
// context:resolve, redeeming a context it resolved itself. A context another
// principal resolved is indistinguishable from an unknown or expired one.
func redeemContext(w http.ResponseWriter, r *http.Request, contexts repository.ContextRepository, identities repository.IdentityRepository, contextID string) (domain.Context, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" || !principal.HasScope("context:resolve") {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
		return domain.Context{}, false
	}
	if contexts == nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context persistence is temporarily unavailable", true)
		return domain.Context{}, false
	}
	trusted, err := contexts.GetContext(r.Context(), contextID)
	if errors.Is(err, repository.ErrContextNotFound) {
		problem(w, r, http.StatusNotFound, "CONTEXT_NOT_FOUND", "the referenced context_id does not exist or has expired", false)
		return domain.Context{}, false
	}
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context lookup failed", true)
		return domain.Context{}, false
	}
	// A context is bound to the principal that resolved it: it is a handle,
	// not a bearer credential. Not owning it answers exactly like not
	// finding it, so a context_id cannot be probed for existence.
	owned, err := callerOwnsContext(r.Context(), identities, principal, trusted)
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context lookup failed", true)
		return domain.Context{}, false
	}
	if !owned {
		problem(w, r, http.StatusNotFound, "CONTEXT_NOT_FOUND", "the referenced context_id does not exist or has expired", false)
		return domain.Context{}, false
	}
	// A context is bound to the tenant that produced it (ADR-BCP-004 §71):
	// a token whose own tenant_id differs fails closed.
	if _, ok := resolveWorkloadTenant(principal.TenantID, trusted.TenantID); !ok {
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "the referenced context does not belong to the authenticated tenant", false)
		return domain.Context{}, false
	}
	return trusted, true
}

func (h CapabilityResolveHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	raw, ok := readBody(w, r)
	if !ok {
		return
	}
	var req resolutionRequest
	if !decodeRaw(w, r, resolutionRequestSchema, raw, &req) {
		return
	}
	trusted, ok := redeemContext(w, r, h.Contexts, h.Identities, req.ContextID)
	if !ok {
		return
	}
	if h.Service.Store == nil {
		problem(w, r, http.StatusServiceUnavailable, "CAPABILITY_RESOLUTION_UNAVAILABLE", "capability resolution is temporarily unavailable", true)
		return
	}
	rec, err := h.Service.Resolve(r.Context(), service.CapabilityResolutionRequest{Context: trusted, CapabilityKey: req.CapabilityKey,
		RequiredContractVersion: req.RequiredContractVersion, CorrelationID: req.CorrelationID})
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "CAPABILITY_RESOLUTION_UNAVAILABLE", "the capability could not be resolved", true)
		return
	}
	body, err := resolutionOf(rec)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "the resolution could not be rendered", true)
		return
	}
	writeJSON(w, http.StatusOK, body)
}
