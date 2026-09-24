package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/store"
)

// ErrIdentityResolutionFailed wraps any error IdentityService.Resolve
// returns, so HTTP handlers can distinguish an identity failure from other
// ContextResolutionService.Resolve failures via errors.Is without string
// matching.
var ErrIdentityResolutionFailed = errors.New("identity resolution failed")

// ErrTenantNotActive is returned when the tenant record exists but is not
// in the ACTIVE lifecycle state (ADR-BCP-004 §52/§53: "Resolve tenant --
// unknown/inactive -> DENY").
var ErrTenantNotActive = errors.New("tenant is not active")

// ContextResolutionService implements the tenant and legal-entity stages of
// ADR-BCP-004 §52's Context Resolution Algorithm: authenticate (done by
// middleware before this is called) -> resolve principal -> resolve tenant
// -> validate principal<->tenant relationship -> resolve legal entity.
//
// This is deliberately not the full 13-stage algorithm. Digital Estate,
// Digital Property, channel, market/market participation, jurisdiction,
// cross-border derivation, currency, residency and deployment-region
// resolution are separate, larger increments (tracked in issue #74) that
// need their own registries and provisioning-time data, most of which does
// not exist yet even at the database level with real assigned rows.
//
// Before this type existed, every production Context (built directly by
// auth.NewOperationContext) never looked up the tenant record at all: it
// trusted the JWT's tenant_id string was valid without checking the tenant
// exists or is active, and never populated legal_entity_id even though the
// tenant record has always carried one. A workload token for a suspended or
// decommissioned tenant could still successfully build a Context and call
// every resolution endpoint. This closes that gap.
type ContextResolutionService struct {
	Identity IdentityService
	Tenants  store.TenantStore
	// Canonical backs the optional OrganisationID verification stage
	// (ADR-BCP-016). Nil is a valid zero value as long as no caller ever
	// passes a non-empty organisationID to Resolve (every existing caller
	// before this stage existed): Resolve fails closed with an explicit
	// error rather than silently dropping an unverified organisationID if
	// one is supplied while this is unset.
	Canonical repository.CanonicalEntityRepository
	// Mappings backs organisation attestation (ADR-BCP-018 gate ORG-14,
	// domain.AttestOrganisation). Like Canonical it is only consulted when
	// an organisationID is supplied, and Resolve fails closed if it is unset.
	Mappings TenantOrganisationMappingReader
}

// TenantOrganisationMappingReader lists the TenantOrganisationMappings of
// one tenant in effect at a point in time.
type TenantOrganisationMappingReader interface {
	ListTenantOrganisationMappings(ctx context.Context, tenantID string, at time.Time) ([]domain.TenantOrganisationMapping, error)
}

// Resolve mirrors auth.NewOperationContext's signature and return shape
// (context.Context, domain.Context, error), extended with an explicit
// tenantID parameter -- the effective tenant, already reconciled by the
// caller via resolveWorkloadTenant between the token's own (usually empty)
// tenant_id claim and any request-supplied tenant -- so call sites that
// already use NewOperationContext directly can switch to this with a
// like-for-like change, gaining the tenant/legal-entity stages this type
// adds on top.
func (s ContextResolutionService) Resolve(ctx context.Context, principal auth.Principal, tenantID string, organisationID string, correlationID string, now time.Time) (context.Context, domain.Context, error) {
	if s.Tenants == nil {
		return nil, domain.Context{}, errors.New("tenant store is required")
	}
	// ADR-0004 §11/§39: resolve the verified (issuer, subject) to a durable
	// canonical Principal before any business-domain operation proceeds.
	resolvedPrincipal, err := s.Identity.Resolve(ctx, principal.Issuer, principal.Subject, principal.ActorType)
	if err != nil {
		return nil, domain.Context{}, fmt.Errorf("%w: %v", ErrIdentityResolutionFailed, err)
	}
	principal.TenantID = tenantID
	opCtx, trustedContext, err := auth.NewOperationContext(ctx, principal, resolvedPrincipal.ID, correlationID, now)
	if err != nil {
		return nil, domain.Context{}, err
	}
	// ADR-BCP-004 §52 "Resolve tenant" / §53 "unknown tenant SHALL fail":
	// the effective tenant is authenticated (or, for a token with no
	// tenant_id claim, an authenticated workload's own explicit request --
	// see resolveWorkloadTenant) but not authoritative on its own -- it
	// must name a tenant that actually exists in the Control Plane's own
	// registry and is currently active.
	tenant, err := s.Tenants.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, domain.Context{}, fmt.Errorf("resolve tenant: %w", err)
	}
	if tenant.ObservedState != string(domain.LifecycleActive) {
		return nil, domain.Context{}, ErrTenantNotActive
	}
	// ADR-BCP-004 §55: legal entity is a child of tenant. Every tenant has
	// exactly one legal_entity_id today (set at registration) -- there is no
	// multi-legal-entity-per-tenant registry yet, so "resolve legal entity"
	// degenerates to "the tenant's own legal entity," always populated,
	// never caller-selectable.
	trustedContext.LegalEntityID = tenant.LegalEntityID
	// ADR-BCP-016: a caller-asserted organisationID is never trusted merely
	// because it is well-formed -- it must name a real, ACTIVE,
	// organisation-kind CanonicalEntity the resolved tenant is attested for
	// (ADR-BCP-018 ORG-14, domain.AttestOrganisation), mirroring AuthoritativeContextResolver.Resolve's identical
	// OrganisationID stage (internal/provisioning/context_resolver.go).
	if organisationID != "" {
		if s.Canonical == nil {
			return nil, domain.Context{}, errors.New("canonical entity repository is required to verify organisation_id")
		}
		if s.Mappings == nil {
			return nil, domain.Context{}, errors.New("tenant organisation mappings are required to verify organisation_id")
		}
		organisation, err := s.Canonical.GetCanonicalEntity(ctx, organisationID)
		if err != nil {
			return nil, domain.Context{}, fmt.Errorf("resolve organisation: %w", err)
		}
		mappings, err := s.Mappings.ListTenantOrganisationMappings(ctx, tenantID, now)
		if err != nil {
			return nil, domain.Context{}, fmt.Errorf("resolve tenant organisation mappings: %w", err)
		}
		if err := domain.AttestOrganisation(organisation, tenantID, mappings, now); err != nil {
			return nil, domain.Context{}, err
		}
		trustedContext.OrganisationID = organisation.ID
		trustedContext.Provenance["organisation_id"] = domain.ContextSource{
			Source: "baobab-cp:canonical-registry", TrustLevel: domain.TrustSystem,
			Evidence: organisation.ID,
		}
	}
	if err := trustedContext.Validate(); err != nil {
		return nil, domain.Context{}, err
	}
	return auth.WithOperationContext(opCtx, trustedContext), trustedContext, nil
}
