package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
)

// PlatformContextHandler exposes ADR-BCP-004's context resolution as its own
// step (§75 recommends "POST /v1/context/resolve" for this) -- deliberately
// mounted at a different path here because /v1/context/resolve already
// exists (api/context.go's resolveContext) and resolves an unrelated, older
// concept: tenant+product entitlement (domain.ResolveContext/
// ResolvedContext, predating the capability-centric rebuild), not the
// PlatformContext this handler produces. The two are not renamed or merged
// by this change.
//
// A resolved Context is persisted via repository.ContextWriter so its
// context_id can be redeemed later by CapabilityResolveHandler, per
// nabhold/shared's resolutionRequest contract requiring a pre-resolved
// context_id rather than an inline context.
type PlatformContextHandler struct {
	ContextResolution service.ContextResolutionService
	Contexts          repository.ContextWriter
	// TTL bounds the persisted context's lifetime (ADR-BCP-004 §72). Zero
	// (the default) leaves it unbounded, mirroring resolver.
	// ResolutionEvidence.TTL's identical zero-value semantics.
	TTL time.Duration
}

type platformContextResolveRequest struct {
	// TenantID is optional: required only when the workload token carries
	// no tenant_id claim of its own (every real workload client today --
	// see resolveWorkloadTenant's doc comment); must equal the claim if the
	// token does carry one.
	TenantID string `json:"tenant_id"`
	// OrganisationID, when supplied, must name a real, ACTIVE,
	// organisation-kind CanonicalEntity owned by the resolved tenant
	// (ADR-BCP-016) -- verified by ContextResolutionService.Resolve, never
	// trusted merely because it is well-formed. The calling workload (e.g.
	// baobab-trade, resolving its own authenticated buyer's organisation)
	// asserts this value itself; CP never parses it out of the workload's
	// own token, per ADR-0010 §36 ("No Buyer Context by Header Alone").
	OrganisationID string `json:"organisation_id,omitempty"`
	// ExpectedOrganisationType requests ADR-BCP-018 exact-kind attestation.
	// It is optional for backwards compatibility, but commands granting
	// kind-specific authority must supply it.
	ExpectedOrganisationType string `json:"expected_organisation_type,omitempty"`
}

type platformContextResolveResponse struct {
	ContextID        string     `json:"context_id"`
	TenantID         string     `json:"tenant_id"`
	ResolvedAt       time.Time  `json:"resolved_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	OrganisationID   string     `json:"organisation_id,omitempty"`
	OrganisationType string     `json:"organisation_type,omitempty"`
}

func (h PlatformContextHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		problem(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST only", false)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.ActorType != "workload" || !principal.HasScope("context:resolve") {
		problem(w, r, http.StatusUnauthorized, "AUTH_TOKEN_REQUIRED", "verified workload identity is required", false)
		return
	}
	// An empty body is valid here (pre-existing callers, and any workload
	// whose token already carries its own tenant_id, never need to send
	// one) -- only a malformed non-empty body is rejected.
	var req platformContextResolveRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		problem(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false)
		return
	}
	tenantID, ok := resolveWorkloadTenant(principal.TenantID, req.TenantID)
	if !ok {
		problem(w, r, http.StatusForbidden, "TENANT_CONTEXT_MISMATCH", "requested tenant does not match verified workload identity", false)
		return
	}
	// ADR-BCP-004 §52: resolve identity -> resolve tenant -> validate
	// principal<->tenant relationship -> resolve legal entity, all fail
	// closed, mirroring ResolverHandler.Resolve's identical step. Unlike
	// that handler, this one also verifies req.OrganisationID when supplied
	// (ADR-BCP-016) -- the only real HTTP path that reaches the ZB-03.2
	// OrganisationID verification stage today.
	_, trustedContext, err := h.ContextResolution.ResolveExpectedOrganisationKind(r.Context(), principal, tenantID, req.OrganisationID, req.ExpectedOrganisationType, correlationID(r), time.Now())
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
	if h.TTL > 0 {
		expiresAt := trustedContext.ResolvedAt.Add(h.TTL)
		trustedContext.ExpiresAt = &expiresAt
	}
	if h.Contexts == nil {
		problem(w, r, http.StatusServiceUnavailable, "CONTEXT_STORE_UNAVAILABLE", "context persistence is temporarily unavailable", true)
		return
	}
	if err := h.Contexts.CreateContext(r.Context(), trustedContext); err != nil {
		problem(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "resolved context could not be persisted", true)
		return
	}
	writeJSON(w, http.StatusOK, platformContextResolveResponse{
		ContextID:        trustedContext.ID,
		TenantID:         trustedContext.TenantID,
		ResolvedAt:       trustedContext.ResolvedAt,
		ExpiresAt:        trustedContext.ExpiresAt,
		OrganisationID:   trustedContext.OrganisationID,
		OrganisationType: req.ExpectedOrganisationType,
	})
}
