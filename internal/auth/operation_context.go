package auth

import (
	"context"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type operationContextKey struct{}

// NewOperationContext derives the identity portion of Context from a
// verified Principal plus its resolved canonical identity. Business
// dimensions are enriched later from authoritative Control Plane
// registries, never from request headers/body.
//
// principalID is the durable domain.Principal.ID resolved from the
// Principal's (issuer, subject) via service.IdentityService -- ADR-0004
// §11/§39: the trusted Context is keyed by the canonical identity, not the
// raw token subject, and callers are expected to have already performed
// that resolution before calling this function (Gate IAM-3 phase 4,
// docs/governance/gate-iam-3-canonical-identity-scope.md). This package
// does not import internal/service or internal/repository itself, to keep
// token verification/context derivation decoupled from persistence.
func NewOperationContext(parent context.Context, principal Principal, principalID, correlationID string, now time.Time) (context.Context, domain.Context, error) {
	if parent == nil {
		return nil, domain.Context{}, errors.New("parent context is required")
	}
	if principal.Subject == "" || principal.TenantID == "" || principal.TokenID == "" {
		return nil, domain.Context{}, errors.New("verified workload principal is required")
	}
	if principalID == "" {
		return nil, domain.Context{}, errors.New("resolved canonical principal id is required")
	}
	resolved := domain.Context{
		// ADR-BCP-004 §70: "Every successfully resolved context SHALL
		// receive: context_id" -- it enables audit, traceability,
		// resolution correlation, incident investigation and cross-engine
		// observability. This was previously never set, leaving every
		// trusted Context this function ever produced (the real trusted
		// context every /v1/resolve call uses) with an empty ID.
		ID:            domain.NewUUIDv7(),
		PrincipalID:   principalID,
		TenantID:      principal.TenantID,
		CorrelationID: correlationID,
		ResolvedAt:    now.UTC(),
		Provenance: map[string]domain.ContextSource{
			"principal_id": {Source: "resolved_canonical_identity", TrustLevel: domain.TrustVerified, Evidence: principal.TokenID},
			"tenant_id":    {Source: "verified_access_token", TrustLevel: domain.TrustVerified, Evidence: principal.TokenID},
		},
	}
	if err := resolved.Validate(); err != nil {
		return nil, domain.Context{}, err
	}
	return context.WithValue(parent, operationContextKey{}, resolved), resolved, nil
}

// WithOperationContext re-attaches an already-resolved Context to parent,
// exactly as NewOperationContext's own return value does. It exists so a
// higher layer that -- unlike this package -- may depend on persistence
// (internal/service.ContextResolutionService, which enriches the Context
// NewOperationContext produced with an authoritative tenant record's
// legal_entity_id) can re-propagate an enriched Context without this
// package needing to know about tenant storage itself.
func WithOperationContext(parent context.Context, resolved domain.Context) context.Context {
	return context.WithValue(parent, operationContextKey{}, resolved)
}

// OperationContextFromContext supports immutable propagation to workers and
// internal calls. Callers receive a value copy; provenance is copied as well.
func OperationContextFromContext(ctx context.Context) (domain.Context, bool) {
	resolved, ok := ctx.Value(operationContextKey{}).(domain.Context)
	if !ok {
		return domain.Context{}, false
	}
	resolved.Provenance = cloneProvenance(resolved.Provenance)
	return resolved, true
}

func cloneProvenance(input map[string]domain.ContextSource) map[string]domain.ContextSource {
	output := make(map[string]domain.ContextSource, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
