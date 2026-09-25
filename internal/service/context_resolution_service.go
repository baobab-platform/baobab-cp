package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store"
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

// ErrOrganisationNotResolved is returned when IAM organisation evidence does
// not resolve, through an active link, to exactly one canonical Organisation
// (ADR-BCP-018 section 66). Resolution fails closed on it.
var ErrOrganisationNotResolved = errors.New("iam organisation evidence did not resolve to a canonical organisation")

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
	// IamOrganisations resolves IAM organisation evidence to a canonical
	// Organisation (ADR-BCP-018 gate ORG-10). It is only consulted by
	// ResolveWithIamOrganisation, which fails closed when it is unset.
	IamOrganisations IamOrganisationResolver
	// CounterpartyRoles lets a generic ORGANISATION satisfy an expected
	// BUYER_ORGANISATION or SUPPLIER_ORGANISATION kind by holding the
	// matching counterparty role for the tenant (ADR-BCP-024 clause 8,
	// ADR-BCP-018 gate ORG-13). Nil keeps exact-kind attestation strict.
	CounterpartyRoles CounterpartyRoleReader
}

// CounterpartyRoleReader reports whether an organisation holds an ACTIVE,
// in-effect counterparty role for a tenant.
type CounterpartyRoleReader interface {
	HoldsCounterpartyRole(ctx context.Context, organisationID, tenantID, role string, at time.Time) (bool, error)
}

// IamOrganisationResolver resolves IAM organisation evidence through an
// active IamOrganisationReference.
type IamOrganisationResolver interface {
	ResolveIamOrganisation(ctx context.Context, ev domain.IamOrganisationEvidence, at time.Time) (string, error)
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
	return s.resolve(ctx, principal, tenantID, organisationID, "", nil, correlationID, now)
}

// ResolveExpectedOrganisationKind is Resolve with ADR-BCP-024 exact-kind
// attestation: when expectedOrganisationType is non-empty, organisationID is
// required and the attested organisation's EntityType must equal it. An
// empty expectedOrganisationType preserves Resolve's generic ADR-BCP-016
// behaviour.
func (s ContextResolutionService) ResolveExpectedOrganisationKind(ctx context.Context, principal auth.Principal, tenantID string, organisationID string, expectedOrganisationType string, correlationID string, now time.Time) (context.Context, domain.Context, error) {
	return s.resolve(ctx, principal, tenantID, organisationID, expectedOrganisationType, nil, correlationID, now)
}

// ResolveWithIamOrganisation resolves Context for IAM organisation evidence
// (ADR-BCP-018 section 66): the evidence is resolved through an active
// IamOrganisationReference to a canonical Organisation, which is then
// attested against the tenant exactly like an organisation_id passed to
// Resolve. The evidence is never copied into the Context; its provenance
// records how the Organisation was found.
func (s ContextResolutionService) ResolveWithIamOrganisation(ctx context.Context, principal auth.Principal, tenantID string, ev domain.IamOrganisationEvidence, correlationID string, now time.Time) (context.Context, domain.Context, error) {
	return s.ResolveWithIamOrganisationKind(ctx, principal, tenantID, ev, "", correlationID, now)
}

// ResolveWithIamOrganisationKind is ResolveWithIamOrganisation with
// ADR-BCP-024 exact-kind attestation of the resolved organisation.
func (s ContextResolutionService) ResolveWithIamOrganisationKind(ctx context.Context, principal auth.Principal, tenantID string, ev domain.IamOrganisationEvidence, expectedOrganisationType string, correlationID string, now time.Time) (context.Context, domain.Context, error) {
	if s.IamOrganisations == nil {
		return nil, domain.Context{}, fmt.Errorf("%w: no IAM organisation resolver is configured", ErrOrganisationNotResolved)
	}
	organisationID, err := s.IamOrganisations.ResolveIamOrganisation(ctx, ev, now)
	if err != nil {
		metrics.RelationshipResolutionFailures.Inc(metrics.OutcomeIamNotLinked)
		return nil, domain.Context{}, fmt.Errorf("%w: %v", ErrOrganisationNotResolved, err)
	}
	source := domain.ContextSource{
		Source: "baobab-cp:iam-organisation-reference", TrustLevel: domain.TrustSystem,
		Evidence: ev.Provider + ":" + ev.Issuer + "#" + ev.ProviderOrganisationID,
	}
	return s.resolve(ctx, principal, tenantID, organisationID, expectedOrganisationType, &source, correlationID, now)
}

func (s ContextResolutionService) resolve(ctx context.Context, principal auth.Principal, tenantID string, organisationID string, expectedOrganisationType string, organisationSource *domain.ContextSource, correlationID string, now time.Time) (context.Context, domain.Context, error) {
	// ADR-BCP-024: an exact-kind request is checked before any lookup so a
	// malformed request costs nothing and never reaches the registry.
	if expectedOrganisationType != "" {
		if organisationID == "" {
			return nil, domain.Context{}, recordOrganisationFailure(organisationFailure(metrics.OutcomeInvalidRequest,
				errors.New("organisation_id is required when expected_organisation_type is supplied")))
		}
		if !domain.OrganisationEntityTypes[expectedOrganisationType] {
			return nil, domain.Context{}, recordOrganisationFailure(organisationFailure(metrics.OutcomeInvalidRequest,
				errors.New("expected_organisation_type is not a registered organisation entity type")))
		}
	}
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
		organisation, err := s.attestRequestedOrganisation(ctx, tenantID, organisationID, expectedOrganisationType, now)
		if err != nil {
			return nil, domain.Context{}, recordOrganisationFailure(err)
		}
		trustedContext.OrganisationID = organisation.ID
		trustedContext.Provenance["organisation_id"] = domain.ContextSource{
			Source: "baobab-cp:canonical-registry", TrustLevel: domain.TrustSystem,
			Evidence: organisation.ID,
		}
		if organisationSource != nil {
			trustedContext.Provenance["organisation_id"] = *organisationSource
		}
	}
	if err := trustedContext.Validate(); err != nil {
		return nil, domain.Context{}, err
	}
	return auth.WithOperationContext(opCtx, trustedContext), trustedContext, nil
}

// attestKind enforces ADR-BCP-024's exact kind. A legacy ADR-BCP-016 kind
// must match exactly, so a supplier record can never stand in for a buyer.
// A generic ORGANISATION has no commercial kind of its own (ADR-BCP-018
// section 7); it satisfies BUYER_ORGANISATION or SUPPLIER_ORGANISATION only
// while it holds the matching ACTIVE role for this tenant (clause 8).
func (s ContextResolutionService) attestKind(ctx context.Context, organisation domain.CanonicalEntity, tenantID, expected string, now time.Time) error {
	if organisation.EntityType == expected {
		return nil
	}
	role := domain.LegacyOrganisationRole[expected]
	if organisation.EntityType == domain.EntityTypeOrganisation && role != "" && s.CounterpartyRoles != nil {
		held, err := s.CounterpartyRoles.HoldsCounterpartyRole(ctx, organisation.ID, tenantID, role, now)
		if err != nil {
			return fmt.Errorf("resolve counterparty role: %w", err)
		}
		if held {
			return nil
		}
	}
	return ErrOrganisationKindMismatch
}

// ErrOrganisationKindMismatch: the attested organisation is not of the
// expected kind (ADR-BCP-024).
var ErrOrganisationKindMismatch = errors.New("requested organisation does not match expected_organisation_type")

// attestRequestedOrganisation is the organisation stage of resolution
// (ADR-BCP-016, ADR-BCP-018 ORG-14, ADR-BCP-024). Every failure carries the
// outcome relationship_resolution_failure_total counts it under.
func (s ContextResolutionService) attestRequestedOrganisation(ctx context.Context, tenantID, organisationID, expected string, now time.Time) (domain.CanonicalEntity, error) {
	if s.Canonical == nil {
		return domain.CanonicalEntity{}, organisationFailure(metrics.OutcomeLookupFailed, errors.New("canonical entity repository is required to verify organisation_id"))
	}
	if s.Mappings == nil {
		return domain.CanonicalEntity{}, organisationFailure(metrics.OutcomeLookupFailed, errors.New("tenant organisation mappings are required to verify organisation_id"))
	}
	organisation, err := s.Canonical.GetCanonicalEntity(ctx, organisationID)
	if err != nil {
		return domain.CanonicalEntity{}, organisationFailure(metrics.OutcomeNotFound, fmt.Errorf("resolve organisation: %w", err))
	}
	mappings, err := s.Mappings.ListTenantOrganisationMappings(ctx, tenantID, now)
	if err != nil {
		return domain.CanonicalEntity{}, organisationFailure(metrics.OutcomeLookupFailed, fmt.Errorf("resolve tenant organisation mappings: %w", err))
	}
	if err := domain.AttestOrganisation(organisation, tenantID, mappings, now); err != nil {
		outcome := metrics.OutcomeLookupFailed
		switch {
		case errors.Is(err, domain.ErrOrganisationNotMappedToTenant):
			outcome = metrics.OutcomeNotMapped
		case errors.Is(err, domain.ErrOrganisationNotActive):
			outcome = metrics.OutcomeInactive
		case errors.Is(err, domain.ErrOrganisationNotOrganisationKind):
			outcome = metrics.OutcomeNotOrganisation
		}
		return domain.CanonicalEntity{}, organisationFailure(outcome, err)
	}
	// ADR-BCP-024: the kind is compared only after attestation, so a caller
	// learns nothing about an organisation its tenant is not attested for.
	if expected != "" {
		if err := s.attestKind(ctx, organisation, tenantID, expected, now); err != nil {
			outcome := metrics.OutcomeWrongKind
			if !errors.Is(err, ErrOrganisationKindMismatch) {
				outcome = metrics.OutcomeLookupFailed
			}
			return domain.CanonicalEntity{}, organisationFailure(outcome, err)
		}
	}
	return organisation, nil
}

// outcomeError labels a failure with its relationship_resolution_failure_total
// outcome; it unwraps to the underlying error.
type outcomeError struct {
	outcome string
	err     error
}

func (e outcomeError) Error() string { return e.err.Error() }
func (e outcomeError) Unwrap() error { return e.err }

func organisationFailure(outcome string, err error) error {
	return outcomeError{outcome: outcome, err: err}
}

// recordOrganisationFailure counts err under its outcome, and as a denied
// cross-tenant access when the organisation is not attested for the tenant.
func recordOrganisationFailure(err error) error {
	var oe outcomeError
	if errors.As(err, &oe) {
		metrics.RelationshipResolutionFailures.Inc(oe.outcome)
		if oe.outcome == metrics.OutcomeNotMapped {
			metrics.CrossTenantGroupAccessDenied.Inc()
		}
	}
	return err
}
