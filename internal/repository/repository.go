package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
	productdomain "github.com/nabhold/baobab-cp/internal/product/domain"
	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
	"github.com/nabhold/baobab-cp/internal/resolver"
)

// MappingRepository defines the contract for retrieving canonical mappings.
type MappingRepository interface {
	ListMappings(ctx context.Context, canonicalEntityID string) ([]domain.Mapping, error)
}

// CapabilityRepository defines the contract for retrieving capability bindings and engine instances.
type CapabilityRepository interface {
	ListBindings(ctx context.Context, capabilityKey string) ([]resolver.CapabilityBinding, error)
	ListActiveInstances(ctx context.Context, engineID string) ([]resolver.EngineInstance, error)
}

// CapabilityRegistryRepository is the read contract for the Capability
// registry itself (ADR-BCP-003 §4, "Capability Registry") -- distinct from
// CapabilityRepository above, which reads CapabilityBinding/EngineInstance
// routing data, not the capability aggregate's own identity and lifecycle.
type CapabilityRegistryRepository interface {
	GetCapability(ctx context.Context, capabilityKey string) (capabilitydomain.Capability, error)
	ListCapabilityDependencies(ctx context.Context, capabilityKey string) ([]capabilitydomain.CapabilityDependency, error)
}

// CapabilityRegistryWriter is the mutable Capability registry contract.
// CreateCapabilityDependency enforces ADR-BCP-003 §8's "Required
// dependency graphs SHALL be acyclic" at write time, across the whole
// REQUIRED-only graph, not merely the dependency being inserted.
type CapabilityRegistryWriter interface {
	CreateCapability(ctx context.Context, capability capabilitydomain.Capability) error
	CreateCapabilityDependency(ctx context.Context, dependency capabilitydomain.CapabilityDependency) error
}

// ResolverRepository combines the mapping and capability read contracts used by the resolution stack.
type ResolverRepository interface {
	MappingRepository
	CapabilityRepository
}

// MappingWriter is the mutable mapping contract implemented by repositories
// that support the current simplified mapping aggregate.
type MappingWriter interface {
	CreateMapping(ctx context.Context, mapping domain.Mapping) error
	GetMapping(ctx context.Context, mappingID string) (domain.Mapping, error)
	SaveMapping(ctx context.Context, mapping domain.Mapping, expectedVersion int64) error
}

// CapabilityWriter is the mutable capability-binding contract implemented by
// repositories that support the current simplified binding aggregate.
type CapabilityWriter interface {
	CreateBinding(ctx context.Context, binding resolver.CapabilityBinding) error
	SaveBinding(ctx context.Context, binding resolver.CapabilityBinding, expectedVersion int64) error
}

// MappingScopeWriter is the mutable MappingScope contract (Gate 2,
// docs/reconciliation/platform-resolution-spine-audit.md): the first
// repositories to support persisting the scope dimensions Gate 1 (#61)
// already gave domain.MappingScope Go fields for.
type MappingScopeWriter interface {
	CreateMappingScope(ctx context.Context, scope domain.MappingScope) error
	GetMappingScope(ctx context.Context, scopeID string) (domain.MappingScope, error)
	ListMappingScopes(ctx context.Context, tenantID string) ([]domain.MappingScope, error)
}

// CapabilityScopeWriter is the mutable CapabilityScope contract
// (ADR-BCP-003 §13, capability.capability_scope -- migration 000029).
// Deliberately separate from MappingScopeWriter: a CapabilityScope and a
// MappingScope share dimension vocabulary but are distinct aggregates
// evaluated by different resolvers (ADR-SHARED-007 §25).
type CapabilityScopeWriter interface {
	CreateCapabilityScope(ctx context.Context, scope capabilitydomain.CapabilityScope) error
	GetCapabilityScope(ctx context.Context, scopeID string) (capabilitydomain.CapabilityScope, error)
	ListCapabilityScopes(ctx context.Context, tenantID string) ([]capabilitydomain.CapabilityScope, error)
}

// CapabilityGrantRepository is the read contract for CapabilityGrant
// (ADR-BCP-003 §9-12, capability.capability_grant -- migration 000029),
// consumed by resolver.EntitlementResolverImpl.
type CapabilityGrantRepository interface {
	ListGrants(ctx context.Context, tenantID, capabilityKey string) ([]capabilitydomain.CapabilityGrant, error)
}

// CapabilityGrantWriter is the mutable CapabilityGrant contract. Revocation
// is explicit per §12 -- there is deliberately no delete: a revoked grant
// SHALL remain historically queryable.
type CapabilityGrantWriter interface {
	CreateGrant(ctx context.Context, grant capabilitydomain.CapabilityGrant) error
	RevokeGrant(ctx context.Context, grantID, revokedBy, reason string, expectedVersion int64) error
}

// CompositionRepository is the read/write contract for CapabilityComposition
// (ADR-BCP-005, capability.capability_composition -- migration 000036,
// Programme Gate P4 "Product and Composition Engine").
type CompositionRepository interface {
	CreateComposition(ctx context.Context, composition capabilitydomain.CapabilityComposition) error
	// GetActiveComposition returns the highest-semver ACTIVE composition
	// registered under compositionKey. service.CompositionExpansionService
	// fails closed through this method exactly as
	// CapabilityRegistryRepository.GetCapability fails closed for an
	// unknown/non-ACTIVE capability.
	GetActiveComposition(ctx context.Context, compositionKey string) (capabilitydomain.CapabilityComposition, error)
}

// ProductRepository is the read/write contract for Product and
// ProductVersion (Technical Specification §31, product.product /
// product.product_version -- migration 000033, Programme Gate P4). Deliberately
// Create+Get only for this Gate's "basics" cut -- List/Update and a real
// version-selection workflow remain future Gate P4 work (see migration
// 000033's own comment).
type ProductRepository interface {
	CreateProduct(ctx context.Context, product productdomain.Product) error
	GetProduct(ctx context.Context, productID string) (productdomain.Product, error)
	CreateProductVersion(ctx context.Context, version productdomain.ProductVersion) error
	GetProductVersion(ctx context.Context, productVersionID string) (productdomain.ProductVersion, error)
}

// EntitlementProjectionRepository is the read/write contract for the audit
// trail service.CompositionExpansionService writes as it expands a
// ProductSubscription's composition members into CapabilityGrant records
// (Technical Specification §91, product.entitlement_projection -- migration
// 000036).
type EntitlementProjectionRepository interface {
	CreateEntitlementProjection(ctx context.Context, projection productdomain.EntitlementProjection) error
	ListEntitlementProjections(ctx context.Context, subscriptionID string) ([]productdomain.EntitlementProjection, error)
}

// ErrTenantProvisioningVersionConflict is returned by
// UpdateTenantProvisioning when expectedVersion no longer matches the
// stored row -- the same optimistic-concurrency shape RevokeGrant already
// uses for capability.capability_grant.version.
var ErrTenantProvisioningVersionConflict = errors.New("tenant provisioning version conflict")

// TenantProvisioningRepository is the read/write contract for
// TenantProvisioning (Technical Specification SS21, Gate ZB-02 / Programme
// Gate P7 "basics" -- provisioning.tenant_provisioning, migration 000038).
type TenantProvisioningRepository interface {
	// CreateTenantProvisioning inserts a new TenantProvisioning row.
	// (tenant_id, idempotency_key) is UNIQUE (migration 000038) -- a
	// caller retrying the same onboarding request with the same key gets
	// ErrTenantProvisioningAlreadyExists rather than a silent duplicate,
	// mirroring how CreateGrant/CreateComposition fail closed on conflict.
	CreateTenantProvisioning(ctx context.Context, provisioning provisioningdomain.TenantProvisioning) error
	GetTenantProvisioning(ctx context.Context, id string) (provisioningdomain.TenantProvisioning, error)
	// GetTenantProvisioningByIdempotencyKey supports idempotent onboarding
	// retries: a caller re-submitting the same (tenant_id, idempotency_key)
	// pair gets back the existing aggregate instead of erroring.
	GetTenantProvisioningByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (provisioningdomain.TenantProvisioning, error)
	ListTenantProvisioningsForTenant(ctx context.Context, tenantID string) ([]provisioningdomain.TenantProvisioning, error)
	// UpdateTenantProvisioning persists provisioning's new state,
	// optimistically locked on expectedVersion (the version the caller
	// read provisioning at, before calling TenantProvisioning.Advance).
	UpdateTenantProvisioning(ctx context.Context, provisioning provisioningdomain.TenantProvisioning, expectedVersion int64) error
}

// ErrTenantProvisioningAlreadyExists is returned by CreateTenantProvisioning
// when (tenant_id, idempotency_key) already has a row.
var ErrTenantProvisioningAlreadyExists = errors.New("tenant provisioning already exists for this idempotency key")

// ErrContextNotFound is returned by GetContext when no resolved Context
// exists for the given context_id, or exists but is expired (ADR-BCP-004
// §72: a caller must not be able to distinguish "never existed" from
// "existed but expired" through a different error, since both mean the
// same thing to a caller -- re-resolve).
var ErrContextNotFound = errors.New("resolved context not found")

// ContextRepository is the read contract for resolved Context values
// (ADR-BCP-004 §70/§73, context.resolved_context -- migration 000031),
// the store the Runtime APIs' resolutionRequest.context_id lookup needs.
type ContextRepository interface {
	GetContext(ctx context.Context, contextID string) (domain.Context, error)
}

// ContextWriter is the mutable resolved-Context contract. There is
// deliberately no update method: a resolved Context is immutable
// (ADR-BCP-004 §71) and never rewritten after creation.
// DeleteContextsByTenant supports §74's cache-invalidation events
// (tenant.suspended, etc.) and returns the number of rows removed.
type ContextWriter interface {
	CreateContext(ctx context.Context, resolved domain.Context) error
	DeleteContextsByTenant(ctx context.Context, tenantID string) (int64, error)
}

// ContextStore combines the read and write resolved-Context contracts so a
// caller -- the Runtime API handlers, in particular -- can depend on one
// interface instead of importing ContextRepository and ContextWriter
// separately.
type ContextStore interface {
	ContextRepository
	ContextWriter
}

// DigitalEstateRepository is the read contract for digital estates
// (ADR-BCP-004 §4/§55, estate.digital_estate -- migration 000007). The
// table has existed since early in this codebase's history; this and
// DigitalEstateWriter are the first Go code to read or write it.
type DigitalEstateRepository interface {
	GetDigitalEstate(ctx context.Context, id string) (domain.DigitalEstate, error)
	ListDigitalEstatesForTenant(ctx context.Context, tenantID string) ([]domain.DigitalEstate, error)
}

// DigitalEstateWriter is the mutable digital-estate contract.
type DigitalEstateWriter interface {
	CreateDigitalEstate(ctx context.Context, estate domain.DigitalEstate) error
}

// ErrMarketAssignmentOverlap is returned when an AssignMarketToTenant insert
// is rejected by market.market_assignment's market_assignment_active_excl
// exclusion constraint (migration 000024): the same (tenant_id, market_id)
// pair already has an assignment with an overlapping validity period. A
// tenant MAY have concurrent assignments to different markets, just not two
// overlapping assignments to the same one.
var ErrMarketAssignmentOverlap = errors.New("overlapping active market assignment")

// MarketRepository is the read contract for markets and market assignments
// (ADR-BCP-004 §4/§55, market.market / market.market_assignment --
// migration 000006). The tables have existed since early in this
// codebase's history; this and MarketWriter are the first Go code to read
// or write them.
type MarketRepository interface {
	GetMarket(ctx context.Context, id string) (domain.Market, error)
	GetMarketByCode(ctx context.Context, code string) (domain.Market, error)
	// ListActiveMarketsForTenant returns every Market with an assignment
	// covering at (i.e. effective_from <= at < effective_to, or no
	// effective_to at all).
	ListActiveMarketsForTenant(ctx context.Context, tenantID string, at time.Time) ([]domain.Market, error)
	// ListMarketAssignmentsForTenant returns every MarketAssignment record
	// for tenantID (all markets, all periods, with Capabilities populated) --
	// unlike ListActiveMarketsForTenant, it exposes what the tenant is
	// authorised to do in each market, not just which markets it has any
	// assignment to (ADR-BCP-011 §6, Gate P0's market-participation
	// capability-flag remodel).
	ListMarketAssignmentsForTenant(ctx context.Context, tenantID string) ([]domain.MarketAssignment, error)
}

// MarketWriter is the mutable market/market-assignment contract.
type MarketWriter interface {
	CreateMarket(ctx context.Context, market domain.Market) error
	AssignMarketToTenant(ctx context.Context, assignment domain.MarketAssignment) error
}

// ErrTenantIsolationProfileOverlap is returned when an
// AssignIsolationProfileToTenant insert is rejected by policy.
// tenant_isolation_profile's tenant_isolation_profile_active_excl exclusion
// constraint (migration 000024): unlike ErrMarketAssignmentOverlap, this
// excludes on tenant_id alone, not tenant_id+profile_id -- a tenant SHALL
// have at most one active isolation profile assignment at any given time,
// regardless of which profile.
var ErrTenantIsolationProfileOverlap = errors.New("overlapping active tenant isolation profile assignment")

// IsolationProfileRepository is the read contract for isolation profiles
// and tenant assignments (ADR-BCP-004 §4/§55, policy.isolation_profile /
// policy.tenant_isolation_profile -- migration 000004). The tables have
// existed since early in this codebase's history; this and
// IsolationProfileWriter are the first Go code to read or write them.
type IsolationProfileRepository interface {
	GetIsolationProfile(ctx context.Context, id string) (domain.IsolationProfile, error)
	// GetCurrentIsolationProfileForTenant returns the IsolationProfile whose
	// assignment covers at (effective_from <= at, and either no
	// effective_to or effective_to > at).
	GetCurrentIsolationProfileForTenant(ctx context.Context, tenantID string, at time.Time) (domain.IsolationProfile, error)
}

// IsolationProfileWriter is the mutable isolation-profile contract.
type IsolationProfileWriter interface {
	CreateIsolationProfile(ctx context.Context, profile domain.IsolationProfile) error
	AssignIsolationProfileToTenant(ctx context.Context, assignment domain.TenantIsolationProfileAssignment) error
}

// ErrIdentityNotFound is returned by ResolveIdentity when no ExternalIdentity
// exists for the given (issuer, subject) pair -- ADR-0004 §11's "absent"
// branch of identity resolution, distinct from a repository failure, so
// Gate IAM-3 phase 3's first-authentication provisioning flow can branch on
// it via errors.Is instead of string-matching.
var ErrIdentityNotFound = errors.New("identity not found")

// IdentityRepository is the Gate IAM-3 contract
// (docs/governance/gate-iam-3-canonical-identity-scope.md, phase 2) for
// ADR-0004's CanonicalIdentity/ExternalIdentity(issuer, subject) layer.
// ResolveIdentity is the read side of ADR-0004 §11's resolution flow;
// CreateIdentity and LinkExternalIdentity are the write side a
// first-authentication provisioning flow (phase 3, ADR-0004 §12/§56) calls
// when ResolveIdentity returns ErrIdentityNotFound. Callers are expected to
// mint IDs via domain.NewPrincipalID()/NewExternalIdentityID() before
// calling, matching every other Create* method in this package.
type IdentityRepository interface {
	ResolveIdentity(ctx context.Context, issuer, subject string) (domain.Principal, error)
	// GetPrincipal returns the Principal identified by principalID.
	// ADR-0004 §15-22's linking/unlinking/merging flows (Gate IAM-3 phase
	// 6) all operate on an already-resolved Principal by ID, unlike
	// ResolveIdentity's (issuer, subject) lookup. Returns
	// ErrIdentityNotFound if no such Principal exists.
	GetPrincipal(ctx context.Context, principalID string) (domain.Principal, error)
	CreateIdentity(ctx context.Context, principal domain.Principal) error
	LinkExternalIdentity(ctx context.Context, external domain.ExternalIdentity) error
}

// ErrWorkforceMembershipNotFound is returned by GetWorkforceMembership when
// no membership row exists for the given (principalID, tenantID) pair --
// distinct from a repository failure so role-aware authorization (Gate
// IAM-5 phase 3, api/router.go) can fail closed on it via errors.Is rather
// than string-matching, matching ErrIdentityNotFound's own shape.
var ErrWorkforceMembershipNotFound = errors.New("workforce membership not found")

// WorkforceMembershipRepository is the Gate IAM-5 phase 3 contract
// (docs/governance/gate-iam-5-workforce-sso-scope.md) for ADR-0009 §27's
// WorkforceMembership relationship. It is deliberately read-and-lifecycle
// only, with no auto-provisioning path analogous to IdentityService.Resolve:
// per ADR-0009 §29 ("Privileged access SHALL not be assigned merely because
// identity provisioning succeeded") and Gate IAM-5 phase 1's "no privileged
// JIT provisioning" precedent, a WorkforceMembership is always created by an
// explicit joiner action (CreateWorkforceMembership), never materialized on
// first authentication.
type WorkforceMembershipRepository interface {
	// GetWorkforceMembership returns the membership row for principalID in
	// tenantID, or ErrWorkforceMembershipNotFound if none exists. Role-aware
	// authorization treats "not found" and "found but not ACTIVE" as the
	// same deny decision -- callers check Status themselves rather than
	// this method filtering by status, so an existing-but-DISABLED
	// membership can still be inspected (e.g. by an access-review tool)
	// without a second, status-agnostic lookup method.
	GetWorkforceMembership(ctx context.Context, principalID, tenantID string) (domain.WorkforceMembership, error)
	// ListWorkforceMemberships returns every membership row for
	// principalID, across all tenants -- the mover/leaver processes
	// (ADR-0009 §31-36) operate over a person's whole membership set, not
	// one tenant at a time.
	ListWorkforceMemberships(ctx context.Context, principalID string) ([]domain.WorkforceMembership, error)
	// CreateWorkforceMembership is the joiner action (ADR-0009 §29): an
	// explicit, deliberate grant, never implicit. Callers mint the ID via
	// domain.NewWorkforceMembershipID() before calling, matching every
	// other Create* method in this package.
	CreateWorkforceMembership(ctx context.Context, membership domain.WorkforceMembership) error
	// SetWorkforceMembershipStatus implements the mover process's "revoke
	// obsolete privileges" step and the leaver process's "workforce
	// membership disabled" step (ADR-0009 §31, §33) as a status-only
	// transition -- it never touches Roles.
	SetWorkforceMembershipStatus(ctx context.Context, membershipID, status string) error
	// SetWorkforceMembershipRoles implements the mover process's "grant new
	// privileges" step (ADR-0009 §31) as an explicit role replacement on
	// the existing membership row. A second CreateWorkforceMembership for
	// the same (principal_id, tenant_id) is not an option: UNIQUE(
	// principal_id, tenant_id) (migration 000032) means there is exactly
	// one membership row per tenant a principal belongs to, so a role
	// change updates that row in place rather than superseding it with a
	// second one that the constraint would reject. A caller that needs a
	// record of the specific change (not just the resulting roles)
	// captures the before state itself first (e.g. via
	// GetWorkforceMembership), the same pattern this package's other
	// audited operations (LinkExternalIdentityAudited) already use.
	SetWorkforceMembershipRoles(ctx context.Context, membershipID string, roles []string) error
}

// AuditActor identifies who performed a security-sensitive identity
// operation (linking, unlinking, merging), for the audit record ADR-0004
// §16/§21/§52 require alongside it. Mirrors internal/store's
// RequestMetadata shape -- callers building either already have this same
// set of fields off a verified auth.Principal -- but is defined locally
// rather than imported, to keep internal/repository (mapping/capability
// /identity) and internal/store (tenant/context) independent of each
// other's packages.
type AuditActor struct {
	ActorID       string
	ActorType     string
	ClientID      string
	TokenID       string
	CorrelationID string
}

// IdentityLinkingRepository is the Gate IAM-3 phase 6 contract
// (docs/governance/gate-iam-3-canonical-identity-scope.md) for ADR-0004
// §15-17's identity-linking flow: adding a second ExternalIdentity to an
// already-existing Principal. Unlike LinkExternalIdentity (phase 2, used by
// phase 3's first-authentication provisioning, which has no existing
// Principal to audit a link *against* yet), this SHALL always write an
// audit record alongside the link -- ADR-0004 §16, "Every successful link
// SHALL be auditable." Callers are responsible for having already
// established both factors §15 requires (an existing authenticated session
// for the target Principal, and a fresh authentication with the new
// provider yielding the (issuer, subject) being linked); this method does
// not itself re-verify either -- it is the audited persistence step, not
// the authentication protocol.
type IdentityLinkingRepository interface {
	LinkExternalIdentityAudited(ctx context.Context, external domain.ExternalIdentity, actor AuditActor, reason string) error
}

// ErrExternalIdentityNotLinked is returned by UnlinkExternalIdentityAudited
// when the requested (issuer, subject) is not an ACTIVE ExternalIdentity of
// the given Principal -- it doesn't exist, belongs to a different
// Principal, or has already been unlinked/disabled/revoked.
var ErrExternalIdentityNotLinked = errors.New("external identity is not an active credential of this principal")

// ErrLastCredentialDenied is returned when unlinking would leave a
// Principal with no remaining ACTIVE ExternalIdentity and the caller did
// not mark the operation administrative -- ADR-0004 §18: "Unlinking SHALL
// verify that the actor will not be left without a valid authentication
// path unless the operation is explicitly administrative."
var ErrLastCredentialDenied = errors.New("unlinking this identity would leave the principal without any active credential")

// IdentityUnlinkingRepository is the Gate IAM-3 phase 6 contract for
// ADR-0004 §18's identity-unlinking flow. UnlinkExternalIdentityAudited
// marks the (issuer, subject) ExternalIdentity belonging to principalID as
// UNLINKED (never deleted -- ADR-0004 §34/§21's "retired identity SHALL
// not simply disappear" spirit, though written for canonical identities,
// applies just as well to a single credential's history) rather than
// deleting the row. Denies the operation with ErrLastCredentialDenied
// unless administrative is true and the Principal would otherwise be left
// with zero ACTIVE ExternalIdentities -- callers set administrative for
// explicit administrative recovery flows (§18's own example: "establishing
// another credential first; administrative recovery; explicit account
// disablement"). Like LinkExternalIdentityAudited, always writes an
// audit_events row in the same transaction (ADR-0004 §52).
type IdentityUnlinkingRepository interface {
	UnlinkExternalIdentityAudited(ctx context.Context, principalID, issuer, subject string, administrative bool, actor AuditActor, reason string) error
}

// ErrMergeSourceEqualsTarget is returned when MergePrincipalsAudited is
// called with the same Principal as both source and target.
var ErrMergeSourceEqualsTarget = errors.New("merge source and target principal must differ")

// ErrMergeNotEligible is returned when either the source or target
// Principal of a merge is already ARCHIVED -- an archived Principal is
// either already the retired half of a previous merge (invalid as a
// source: it has nothing left to transfer) or not a valid consolidation
// target (an archived identity must not become canonical again).
var ErrMergeNotEligible = errors.New("merge source or target principal is not eligible for merge")

// IdentityMergeRepository is the Gate IAM-3 phase 6 contract for ADR-0004
// §19-21's identity merge: consolidating two Principals discovered to
// represent the same actor into one. MergePrincipalsAudited transfers every
// ExternalIdentity and IdentityReference row from sourcePrincipalID to
// targetPrincipalID (safe from collision by construction:
// UNIQUE(issuer, subject) and UNIQUE(engine, external_type, external_id)
// both exclude principal_id, so re-pointing ownership can never violate
// either constraint) and archives the source -- ADR-0004 §20: "One identity
// SHALL remain canonical. The retired identity SHALL not simply disappear."
// The source Principal row is archived (status ARCHIVED), never deleted,
// and remains resolvable via GetPrincipal.
//
// nabhold/shared's principal.schema.json is additionalProperties: false
// with no merged-into field, and ADR-0004 §21 only requires the retired
// identity remain resolvable "through historical audit records" (not via a
// live pointer) -- so provenance is carried entirely by the two
// audit_events rows this method writes (one targeting the source, one
// targeting the target, sharing one correlation ID), not by a schema
// change. Each row's payload captures everything §21 requires: source
// identity, target identity, actor, reason, timestamp (audit_events'
// occurred_at), and every transferred external identity and engine
// mapping.
//
// Merge/split of business relationships this repository doesn't model yet
// (tenant memberships, buyer/supplier relationships) is explicitly out of
// scope -- see docs/governance/gate-iam-3-canonical-identity-scope.md §4.
type IdentityMergeRepository interface {
	MergePrincipalsAudited(ctx context.Context, sourcePrincipalID, targetPrincipalID string, actor AuditActor, reason string) error
}

// ErrIdentityReferenceNotFound is returned by ResolveIdentityReference when
// no mapping exists for the given engine-native actor -- the "absent"
// branch of ADR-0004 §23-27's engine-reference resolution, mirroring
// ErrIdentityNotFound's role for issuer/subject resolution.
var ErrIdentityReferenceNotFound = errors.New("identity reference not found")

// ErrIdentityReferenceAlreadyMapped is returned when CreateIdentityReference
// is rejected by identity.identity_reference's UNIQUE(engine, external_type,
// external_id) constraint (migration 000027) -- ADR-0004 §29's collision
// -avoidance invariant: a given engine-native actor resolves to at most one
// Principal.
var ErrIdentityReferenceAlreadyMapped = errors.New("engine-native actor is already mapped to a principal")

// IdentityReferenceRepository is the Gate IAM-3 phase 5 contract
// (docs/governance/gate-iam-3-canonical-identity-scope.md) for ADR-0004
// §23-30's engine-reference mapping: a Principal to an engine-native actor
// (Medusa customer, iDempiere AD_User, Payload user, ...) and back.
type IdentityReferenceRepository interface {
	CreateIdentityReference(ctx context.Context, reference domain.IdentityReference) error
	ListIdentityReferences(ctx context.Context, principalID string) ([]domain.IdentityReference, error)
	ResolveIdentityReference(ctx context.Context, engine, externalType, externalID string) (domain.IdentityReference, error)
}

// Repository is a lightweight in-memory repository backing the resolver/service layer.
type Repository struct {
	Mappings               map[string][]domain.Mapping
	Bindings               map[string][]resolver.CapabilityBinding
	EngineInstances        map[string][]resolver.EngineInstance
	MappingScopes          map[string]domain.MappingScope                     // keyed by ScopeID
	CapabilityScopes       map[string]capabilitydomain.CapabilityScope        // keyed by ScopeID
	Grants                 map[string]capabilitydomain.CapabilityGrant        // keyed by ID
	Capabilities           map[string]capabilitydomain.Capability             // keyed by Key
	CapabilityDependencies map[string][]capabilitydomain.CapabilityDependency // keyed by owning CapabilityKey
	Principals             map[string]domain.Principal                        // keyed by ID
	ExternalIdentities     map[string]domain.ExternalIdentity                 // keyed by "issuer\x00subject", mirroring UNIQUE(issuer, subject)
	IdentityReferences     map[string]domain.IdentityReference                // keyed by "engine\x00external_type\x00external_id", mirroring UNIQUE(engine, external_type, external_id)
	Contexts               map[string]domain.Context                          // keyed by ID
	DigitalEstates         map[string]domain.DigitalEstate                    // keyed by ID
	Markets                map[string]domain.Market                           // keyed by ID
	MarketAssignments      map[string]domain.MarketAssignment                 // keyed by ID
	IsolationProfiles      map[string]domain.IsolationProfile                 // keyed by ID
	// TenantIsolationProfiles has no synthetic ID (mirroring the real
	// table's PRIMARY KEY (tenant_id, isolation_profile_id, effective_from));
	// keyed by "tenant_id\x00isolation_profile_id\x00effective_from".
	TenantIsolationProfiles map[string]domain.TenantIsolationProfileAssignment
	// WorkforceMemberships is keyed by ID, mirroring the real table's
	// primary key -- unlike ExternalIdentities' composite-key map, lookups
	// here (GetWorkforceMembership, ListWorkforceMemberships) filter by
	// scanning, matching this package's existing pattern for
	// multi-valued-per-owner collections (see Mappings, Bindings).
	WorkforceMemberships map[string]domain.WorkforceMembership
	// LinkAudit records every LinkExternalIdentityAudited call, purely in
	// memory (there is no real audit_events table to write to here) so
	// tests can assert an audit entry was actually produced.
	LinkAudit []LinkAuditRecord
	// UnlinkAudit is LinkAudit's counterpart for UnlinkExternalIdentityAudited.
	UnlinkAudit []UnlinkAuditRecord
	// MergeAudit is LinkAudit's counterpart for MergePrincipalsAudited.
	MergeAudit []MergeAuditRecord
	// Compositions is keyed by CompositionKey, holding every registered
	// version -- GetActiveComposition scans for the highest-semver ACTIVE
	// entry, mirroring how the real table has no "current version" column
	// either.
	Compositions    map[string][]capabilitydomain.CapabilityComposition
	Products        map[string]productdomain.Product        // keyed by ProductID
	ProductVersions map[string]productdomain.ProductVersion // keyed by ProductVersionID
	// EntitlementProjections is keyed by SubscriptionID, mirroring
	// ListEntitlementProjections' own lookup key.
	EntitlementProjections map[string][]productdomain.EntitlementProjection
	// TenantProvisionings is keyed by ID, mirroring the real table's
	// primary key.
	TenantProvisionings map[string]provisioningdomain.TenantProvisioning
}

// LinkAuditRecord is the in-memory equivalent of the audit_events row
// PostgresRepository.LinkExternalIdentityAudited writes.
type LinkAuditRecord struct {
	External domain.ExternalIdentity
	Actor    AuditActor
	Reason   string
}

// UnlinkAuditRecord is the in-memory equivalent of the audit_events row
// PostgresRepository.UnlinkExternalIdentityAudited writes.
type UnlinkAuditRecord struct {
	PrincipalID    string
	Issuer         string
	Subject        string
	Administrative bool
	Actor          AuditActor
	Reason         string
}

// MergeAuditRecord is the in-memory equivalent of the two audit_events rows
// PostgresRepository.MergePrincipalsAudited writes.
type MergeAuditRecord struct {
	SourcePrincipalID             string
	TargetPrincipalID             string
	TransferredExternalIdentities []domain.ExternalIdentity
	TransferredIdentityReferences []domain.IdentityReference
	// TransferredWorkforceMemberships lists the source's membership rows
	// reassigned to the target. A source membership whose tenant the
	// target already belongs to is not transferred here (that would
	// violate migration 000032's UNIQUE(principal_id, tenant_id)); it is
	// disabled in place instead -- see DisabledWorkforceMemberships.
	TransferredWorkforceMemberships []domain.WorkforceMembership
	// DisabledWorkforceMemberships lists source membership rows that
	// conflicted with an existing target membership for the same tenant
	// (both principals already belonged to it) and were therefore left on
	// the archived source principal with their status set to DISABLED,
	// rather than transferred -- the target's own pre-existing membership
	// for that tenant remains authoritative.
	DisabledWorkforceMemberships []domain.WorkforceMembership
	Actor                        AuditActor
	Reason                       string
}

var _ MappingRepository = (*Repository)(nil)
var _ CapabilityRepository = (*Repository)(nil)
var _ ResolverRepository = (*Repository)(nil)
var _ MappingWriter = (*Repository)(nil)
var _ CapabilityWriter = (*Repository)(nil)
var _ MappingScopeWriter = (*Repository)(nil)
var _ CapabilityScopeWriter = (*Repository)(nil)
var _ CapabilityGrantRepository = (*Repository)(nil)
var _ CapabilityGrantWriter = (*Repository)(nil)
var _ CapabilityRegistryRepository = (*Repository)(nil)
var _ CapabilityRegistryWriter = (*Repository)(nil)
var _ IdentityRepository = (*Repository)(nil)
var _ WorkforceMembershipRepository = (*Repository)(nil)
var _ IdentityReferenceRepository = (*Repository)(nil)
var _ IdentityLinkingRepository = (*Repository)(nil)
var _ IdentityUnlinkingRepository = (*Repository)(nil)
var _ IdentityMergeRepository = (*Repository)(nil)
var _ ContextRepository = (*Repository)(nil)
var _ ContextWriter = (*Repository)(nil)
var _ ContextStore = (*Repository)(nil)
var _ DigitalEstateRepository = (*Repository)(nil)
var _ DigitalEstateWriter = (*Repository)(nil)
var _ MarketRepository = (*Repository)(nil)
var _ MarketWriter = (*Repository)(nil)
var _ IsolationProfileRepository = (*Repository)(nil)
var _ IsolationProfileWriter = (*Repository)(nil)
var _ CompositionRepository = (*Repository)(nil)
var _ ProductRepository = (*Repository)(nil)
var _ EntitlementProjectionRepository = (*Repository)(nil)
var _ TenantProvisioningRepository = (*Repository)(nil)

func NewInMemoryRepository() *Repository {
	return &Repository{
		Mappings:                map[string][]domain.Mapping{},
		Bindings:                map[string][]resolver.CapabilityBinding{},
		EngineInstances:         map[string][]resolver.EngineInstance{},
		MappingScopes:           map[string]domain.MappingScope{},
		CapabilityScopes:        map[string]capabilitydomain.CapabilityScope{},
		Grants:                  map[string]capabilitydomain.CapabilityGrant{},
		Capabilities:            map[string]capabilitydomain.Capability{},
		CapabilityDependencies:  map[string][]capabilitydomain.CapabilityDependency{},
		Principals:              map[string]domain.Principal{},
		ExternalIdentities:      map[string]domain.ExternalIdentity{},
		IdentityReferences:      map[string]domain.IdentityReference{},
		Contexts:                map[string]domain.Context{},
		DigitalEstates:          map[string]domain.DigitalEstate{},
		Markets:                 map[string]domain.Market{},
		MarketAssignments:       map[string]domain.MarketAssignment{},
		IsolationProfiles:       map[string]domain.IsolationProfile{},
		TenantIsolationProfiles: map[string]domain.TenantIsolationProfileAssignment{},
		WorkforceMemberships:    map[string]domain.WorkforceMembership{},
		Compositions:            map[string][]capabilitydomain.CapabilityComposition{},
		Products:                map[string]productdomain.Product{},
		ProductVersions:         map[string]productdomain.ProductVersion{},
		EntitlementProjections:  map[string][]productdomain.EntitlementProjection{},
		TenantProvisionings:     map[string]provisioningdomain.TenantProvisioning{},
	}
}

func (r *Repository) ListMappings(_ context.Context, canonicalEntityID string) ([]domain.Mapping, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	mappings, ok := r.Mappings[canonicalEntityID]
	if !ok {
		return nil, fmt.Errorf("no mappings for %s", canonicalEntityID)
	}
	return mappings, nil
}

func (r *Repository) CreateMapping(_ context.Context, mapping domain.Mapping) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := mapping.Validate(); err != nil {
		return fmt.Errorf("validate mapping: %w", err)
	}
	if mapping.ID == "" {
		return errors.New("mapping id is required")
	}
	if _, err := r.GetMapping(context.Background(), mapping.ID); err == nil {
		return fmt.Errorf("mapping %s already exists", mapping.ID)
	}
	mapping.Revision = 1
	r.Mappings[mapping.CanonicalEntityID] = append(r.Mappings[mapping.CanonicalEntityID], mapping)
	return nil
}

func (r *Repository) GetMapping(_ context.Context, mappingID string) (domain.Mapping, error) {
	if r == nil {
		return domain.Mapping{}, errors.New("repository is nil")
	}
	for _, mappings := range r.Mappings {
		for _, mapping := range mappings {
			if mapping.ID == mappingID {
				return mapping, nil
			}
		}
	}
	return domain.Mapping{}, fmt.Errorf("mapping %s not found", mappingID)
}

func (r *Repository) SaveMapping(_ context.Context, mapping domain.Mapping, expectedVersion int64) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := mapping.Validate(); err != nil {
		return fmt.Errorf("validate mapping: %w", err)
	}
	for entityID, mappings := range r.Mappings {
		for index, current := range mappings {
			if current.ID != mapping.ID {
				continue
			}
			if current.Revision != expectedVersion {
				return fmt.Errorf("mapping %s version conflict: expected %d, got %d", mapping.ID, expectedVersion, current.Revision)
			}
			mapping.Revision = current.Revision + 1
			r.Mappings[entityID][index] = mapping
			return nil
		}
	}
	return fmt.Errorf("mapping %s not found", mapping.ID)
}

func (r *Repository) CreateMappingScope(_ context.Context, scope domain.MappingScope) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := scope.Validate(); err != nil {
		return fmt.Errorf("validate mapping scope: %w", err)
	}
	if _, exists := r.MappingScopes[scope.ScopeID]; exists {
		return fmt.Errorf("mapping scope %s already exists", scope.ScopeID)
	}
	r.MappingScopes[scope.ScopeID] = scope
	return nil
}

func (r *Repository) GetMappingScope(_ context.Context, scopeID string) (domain.MappingScope, error) {
	if r == nil {
		return domain.MappingScope{}, errors.New("repository is nil")
	}
	scope, ok := r.MappingScopes[scopeID]
	if !ok {
		return domain.MappingScope{}, fmt.Errorf("mapping scope %s not found", scopeID)
	}
	return scope, nil
}

func (r *Repository) ListMappingScopes(_ context.Context, tenantID string) ([]domain.MappingScope, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []domain.MappingScope
	for _, scope := range r.MappingScopes {
		if scope.TenantID == tenantID {
			out = append(out, scope)
		}
	}
	return out, nil
}

func (r *Repository) CreateCapabilityScope(_ context.Context, scope capabilitydomain.CapabilityScope) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := scope.Validate(); err != nil {
		return fmt.Errorf("validate capability scope: %w", err)
	}
	if _, exists := r.CapabilityScopes[scope.ScopeID]; exists {
		return fmt.Errorf("capability scope %s already exists", scope.ScopeID)
	}
	r.CapabilityScopes[scope.ScopeID] = scope
	return nil
}

func (r *Repository) GetCapabilityScope(_ context.Context, scopeID string) (capabilitydomain.CapabilityScope, error) {
	if r == nil {
		return capabilitydomain.CapabilityScope{}, errors.New("repository is nil")
	}
	scope, ok := r.CapabilityScopes[scopeID]
	if !ok {
		return capabilitydomain.CapabilityScope{}, fmt.Errorf("capability scope %s not found", scopeID)
	}
	return scope, nil
}

func (r *Repository) ListCapabilityScopes(_ context.Context, tenantID string) ([]capabilitydomain.CapabilityScope, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []capabilitydomain.CapabilityScope
	for _, scope := range r.CapabilityScopes {
		if scope.TenantID == tenantID {
			out = append(out, scope)
		}
	}
	return out, nil
}

func (r *Repository) CreateGrant(_ context.Context, grant capabilitydomain.CapabilityGrant) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := grant.Validate(); err != nil {
		return fmt.Errorf("validate capability grant: %w", err)
	}
	if _, exists := r.Grants[grant.ID]; exists {
		return fmt.Errorf("capability grant %s already exists", grant.ID)
	}
	grant.Version = 1
	r.Grants[grant.ID] = grant
	return nil
}

func (r *Repository) ListGrants(_ context.Context, tenantID, capabilityKey string) ([]capabilitydomain.CapabilityGrant, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []capabilitydomain.CapabilityGrant
	for _, grant := range r.Grants {
		if grant.TenantID == tenantID && grant.CapabilityKey == capabilityKey {
			out = append(out, grant)
		}
	}
	return out, nil
}

// RevokeGrant sets status=REVOKED and records revocation provenance (§12);
// it never deletes the row -- a revoked grant SHALL remain historically
// queryable.
func (r *Repository) RevokeGrant(_ context.Context, grantID, revokedBy, reason string, expectedVersion int64) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	grant, ok := r.Grants[grantID]
	if !ok {
		return fmt.Errorf("capability grant %s not found", grantID)
	}
	if grant.Version != expectedVersion {
		return fmt.Errorf("capability grant %s version conflict: expected %d, got %d", grantID, expectedVersion, grant.Version)
	}
	now := time.Now().UTC()
	grant.Status = capabilitydomain.GrantStatusRevoked
	grant.RevokedAt = &now
	grant.RevokedBy = revokedBy
	grant.RevocationReason = reason
	grant.Version = grant.Version + 1
	r.Grants[grantID] = grant
	return nil
}

func (r *Repository) CreateComposition(_ context.Context, composition capabilitydomain.CapabilityComposition) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := composition.Validate(); err != nil {
		return fmt.Errorf("validate capability composition: %w", err)
	}
	r.Compositions[composition.CompositionKey] = append(r.Compositions[composition.CompositionKey], composition)
	return nil
}

// GetActiveComposition returns the highest-semver ACTIVE composition
// registered under compositionKey, failing closed (an error, never a zero
// value) when none is found -- mirroring CapabilityRegistryRepository's
// fail-closed contract for an unknown or non-resolvable capability.
func (r *Repository) GetActiveComposition(_ context.Context, compositionKey string) (capabilitydomain.CapabilityComposition, error) {
	if r == nil {
		return capabilitydomain.CapabilityComposition{}, errors.New("repository is nil")
	}
	var best capabilitydomain.CapabilityComposition
	found := false
	for _, candidate := range r.Compositions[compositionKey] {
		if !candidate.IsResolvable() {
			continue
		}
		if !found || compareSemver(candidate.Version, best.Version) > 0 {
			best = candidate
			found = true
		}
	}
	if !found {
		return capabilitydomain.CapabilityComposition{}, fmt.Errorf("no ACTIVE composition registered for %q", compositionKey)
	}
	return best, nil
}

func (r *Repository) CreateProduct(_ context.Context, product productdomain.Product) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := product.Validate(); err != nil {
		return fmt.Errorf("validate product: %w", err)
	}
	if _, exists := r.Products[product.ID]; exists {
		return fmt.Errorf("product %s already exists", product.ID)
	}
	r.Products[product.ID] = product
	return nil
}

func (r *Repository) GetProduct(_ context.Context, productID string) (productdomain.Product, error) {
	if r == nil {
		return productdomain.Product{}, errors.New("repository is nil")
	}
	product, ok := r.Products[productID]
	if !ok {
		return productdomain.Product{}, fmt.Errorf("product %s not found", productID)
	}
	return product, nil
}

func (r *Repository) CreateProductVersion(_ context.Context, version productdomain.ProductVersion) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := version.Validate(); err != nil {
		return fmt.Errorf("validate product version: %w", err)
	}
	if _, exists := r.ProductVersions[version.ID]; exists {
		return fmt.Errorf("product version %s already exists", version.ID)
	}
	r.ProductVersions[version.ID] = version
	return nil
}

func (r *Repository) GetProductVersion(_ context.Context, productVersionID string) (productdomain.ProductVersion, error) {
	if r == nil {
		return productdomain.ProductVersion{}, errors.New("repository is nil")
	}
	version, ok := r.ProductVersions[productVersionID]
	if !ok {
		return productdomain.ProductVersion{}, fmt.Errorf("product version %s not found", productVersionID)
	}
	return version, nil
}

func (r *Repository) CreateEntitlementProjection(_ context.Context, projection productdomain.EntitlementProjection) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := projection.Validate(); err != nil {
		return fmt.Errorf("validate entitlement projection: %w", err)
	}
	r.EntitlementProjections[projection.SubscriptionID] = append(r.EntitlementProjections[projection.SubscriptionID], projection)
	return nil
}

func (r *Repository) ListEntitlementProjections(_ context.Context, subscriptionID string) ([]productdomain.EntitlementProjection, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	return r.EntitlementProjections[subscriptionID], nil
}

func (r *Repository) CreateTenantProvisioning(_ context.Context, provisioning provisioningdomain.TenantProvisioning) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if provisioning.ID == "" {
		return errors.New("tenant provisioning id is required")
	}
	if err := provisioning.Validate(); err != nil {
		return fmt.Errorf("validate tenant provisioning: %w", err)
	}
	if _, exists := r.TenantProvisionings[provisioning.ID]; exists {
		return fmt.Errorf("tenant provisioning %s already exists", provisioning.ID)
	}
	for _, existing := range r.TenantProvisionings {
		if existing.TenantID == provisioning.TenantID && existing.IdempotencyKey == provisioning.IdempotencyKey {
			return ErrTenantProvisioningAlreadyExists
		}
	}
	if provisioning.Version == 0 {
		provisioning.Version = 1
	}
	r.TenantProvisionings[provisioning.ID] = provisioning
	return nil
}

func (r *Repository) GetTenantProvisioning(_ context.Context, id string) (provisioningdomain.TenantProvisioning, error) {
	if r == nil {
		return provisioningdomain.TenantProvisioning{}, errors.New("repository is nil")
	}
	p, ok := r.TenantProvisionings[id]
	if !ok {
		return provisioningdomain.TenantProvisioning{}, fmt.Errorf("tenant provisioning %s not found", id)
	}
	return p, nil
}

func (r *Repository) GetTenantProvisioningByIdempotencyKey(_ context.Context, tenantID, idempotencyKey string) (provisioningdomain.TenantProvisioning, error) {
	if r == nil {
		return provisioningdomain.TenantProvisioning{}, errors.New("repository is nil")
	}
	for _, p := range r.TenantProvisionings {
		if p.TenantID == tenantID && p.IdempotencyKey == idempotencyKey {
			return p, nil
		}
	}
	return provisioningdomain.TenantProvisioning{}, fmt.Errorf("tenant provisioning for tenant %s, idempotency key %s not found", tenantID, idempotencyKey)
}

func (r *Repository) ListTenantProvisioningsForTenant(_ context.Context, tenantID string) ([]provisioningdomain.TenantProvisioning, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []provisioningdomain.TenantProvisioning
	for _, p := range r.TenantProvisionings {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *Repository) UpdateTenantProvisioning(_ context.Context, provisioning provisioningdomain.TenantProvisioning, expectedVersion int64) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	existing, ok := r.TenantProvisionings[provisioning.ID]
	if !ok {
		return fmt.Errorf("tenant provisioning %s not found", provisioning.ID)
	}
	if existing.Version != expectedVersion {
		return ErrTenantProvisioningVersionConflict
	}
	// Version is server-assigned from existing, not trusted from the
	// caller, mirroring RevokeGrant's identical existing.Version+1
	// contract and PostgresRepository.UpdateTenantProvisioning's
	// unconditional `version=version+1`.
	provisioning.Version = existing.Version + 1
	if err := provisioning.Validate(); err != nil {
		return fmt.Errorf("validate tenant provisioning: %w", err)
	}
	r.TenantProvisionings[provisioning.ID] = provisioning
	return nil
}

// compareSemver compares two "major.minor.patch" version strings
// numerically (not lexicographically, so "10.0.0" correctly orders after
// "9.0.0"), returning <0, 0 or >0. A malformed segment compares as 0 rather
// than erroring -- both GetActiveComposition callers (in-memory and
// Postgres) already constrain input to Validate()-passed compositions, whose
// Version field is regex-checked as \d+\.\d+\.\d+ before it can ever reach
// storage, so a malformed value here would indicate a storage-layer bug, not
// a valid input this function needs to reject itself.
func compareSemver(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			return av - bv
		}
	}
	return 0
}

func (r *Repository) GetCapability(_ context.Context, capabilityKey string) (capabilitydomain.Capability, error) {
	if r == nil {
		return capabilitydomain.Capability{}, errors.New("repository is nil")
	}
	capability, ok := r.Capabilities[capabilityKey]
	if !ok {
		return capabilitydomain.Capability{}, fmt.Errorf("capability %s not found", capabilityKey)
	}
	return capability, nil
}

func (r *Repository) CreateCapability(_ context.Context, capability capabilitydomain.Capability) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := capability.Validate(); err != nil {
		return fmt.Errorf("validate capability: %w", err)
	}
	if _, exists := r.Capabilities[capability.Key]; exists {
		return fmt.Errorf("capability %s already exists", capability.Key)
	}
	r.Capabilities[capability.Key] = capability
	return nil
}

func (r *Repository) ListCapabilityDependencies(_ context.Context, capabilityKey string) ([]capabilitydomain.CapabilityDependency, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	return r.CapabilityDependencies[capabilityKey], nil
}

// CreateCapabilityDependency enforces ADR-BCP-003 §8's acyclic requirement
// across the whole REQUIRED-only dependency graph, not merely the edge
// being inserted -- a per-edge check alone cannot see a cycle that closes
// through capabilities other than the two endpoints.
func (r *Repository) CreateCapabilityDependency(_ context.Context, dependency capabilitydomain.CapabilityDependency) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := dependency.Validate(); err != nil {
		return fmt.Errorf("validate capability dependency: %w", err)
	}
	if dependency.DependencyType == capabilitydomain.DependencyTypeRequired {
		all := make([]capabilitydomain.CapabilityDependency, 0)
		for _, deps := range r.CapabilityDependencies {
			all = append(all, deps...)
		}
		all = append(all, dependency)
		if capabilitydomain.HasCapabilityDependencyCycle(all) {
			return fmt.Errorf("adding %s -> %s would create a required-dependency cycle", dependency.CapabilityKey, dependency.DependsOnCapability)
		}
	}
	r.CapabilityDependencies[dependency.CapabilityKey] = append(r.CapabilityDependencies[dependency.CapabilityKey], dependency)
	return nil
}

func externalIdentityKey(issuer, subject string) string { return issuer + "\x00" + subject }

func (r *Repository) ResolveIdentity(_ context.Context, issuer, subject string) (domain.Principal, error) {
	if r == nil {
		return domain.Principal{}, errors.New("repository is nil")
	}
	external, ok := r.ExternalIdentities[externalIdentityKey(issuer, subject)]
	if !ok || external.Status != "ACTIVE" {
		// ADR-0004 §32: external identities may independently be UNLINKED,
		// DISABLED or REVOKED -- such a row must not resolve, otherwise
		// UnlinkExternalIdentityAudited (Gate IAM-3 phase 6) would leave a
		// still-usable authentication path behind it.
		return domain.Principal{}, ErrIdentityNotFound
	}
	principal, ok := r.Principals[external.PrincipalID]
	if !ok {
		return domain.Principal{}, fmt.Errorf("external identity %s references missing principal %s", external.ID, external.PrincipalID)
	}
	return principal, nil
}

func (r *Repository) GetPrincipal(_ context.Context, principalID string) (domain.Principal, error) {
	if r == nil {
		return domain.Principal{}, errors.New("repository is nil")
	}
	principal, ok := r.Principals[principalID]
	if !ok {
		return domain.Principal{}, ErrIdentityNotFound
	}
	return principal, nil
}

func (r *Repository) CreateIdentity(_ context.Context, principal domain.Principal) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := principal.Validate(); err != nil {
		return fmt.Errorf("validate principal: %w", err)
	}
	if principal.ID == "" {
		return errors.New("principal id is required")
	}
	if _, exists := r.Principals[principal.ID]; exists {
		return fmt.Errorf("principal %s already exists", principal.ID)
	}
	r.Principals[principal.ID] = principal
	return nil
}

func (r *Repository) LinkExternalIdentity(_ context.Context, external domain.ExternalIdentity) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := external.Validate(); err != nil {
		return fmt.Errorf("validate external identity: %w", err)
	}
	if external.ID == "" {
		return errors.New("external identity id is required")
	}
	if _, exists := r.Principals[external.PrincipalID]; !exists {
		return fmt.Errorf("principal %s does not exist", external.PrincipalID)
	}
	key := externalIdentityKey(external.Issuer, external.Subject)
	if _, exists := r.ExternalIdentities[key]; exists {
		return fmt.Errorf("issuer %s subject already linked to a principal", external.Issuer)
	}
	r.ExternalIdentities[key] = external
	return nil
}

func (r *Repository) GetWorkforceMembership(_ context.Context, principalID, tenantID string) (domain.WorkforceMembership, error) {
	if r == nil {
		return domain.WorkforceMembership{}, errors.New("repository is nil")
	}
	for _, m := range r.WorkforceMemberships {
		if m.PrincipalID == principalID && m.TenantID == tenantID {
			return m, nil
		}
	}
	return domain.WorkforceMembership{}, ErrWorkforceMembershipNotFound
}

func (r *Repository) ListWorkforceMemberships(_ context.Context, principalID string) ([]domain.WorkforceMembership, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var memberships []domain.WorkforceMembership
	for _, m := range r.WorkforceMemberships {
		if m.PrincipalID == principalID {
			memberships = append(memberships, m)
		}
	}
	return memberships, nil
}

func (r *Repository) CreateWorkforceMembership(_ context.Context, membership domain.WorkforceMembership) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := membership.Validate(); err != nil {
		return fmt.Errorf("validate workforce membership: %w", err)
	}
	if membership.ID == "" {
		return errors.New("workforce membership id is required")
	}
	if _, exists := r.Principals[membership.PrincipalID]; !exists {
		return fmt.Errorf("principal %s does not exist", membership.PrincipalID)
	}
	if _, exists := r.WorkforceMemberships[membership.ID]; exists {
		return fmt.Errorf("workforce membership %s already exists", membership.ID)
	}
	// Mirror migration 000032's UNIQUE(principal_id, tenant_id): the
	// in-memory repository is keyed by membership ID alone, so without this
	// scan two different IDs could otherwise both claim the same
	// (principal, tenant) pair -- something the real Postgres constraint
	// would reject -- and GetWorkforceMembership's first-match-wins
	// iteration over a Go map would then return whichever one it happened
	// to encounter first, nondeterministically.
	for _, existing := range r.WorkforceMemberships {
		if existing.PrincipalID == membership.PrincipalID && existing.TenantID == membership.TenantID {
			return fmt.Errorf("workforce membership for principal %s in tenant %s already exists", membership.PrincipalID, membership.TenantID)
		}
	}
	r.WorkforceMemberships[membership.ID] = membership
	return nil
}

func (r *Repository) SetWorkforceMembershipStatus(_ context.Context, membershipID, status string) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if !domain.ValidWorkforceMembershipStatus(status) {
		return errors.New("status must be ACTIVE, SUSPENDED or DISABLED")
	}
	membership, ok := r.WorkforceMemberships[membershipID]
	if !ok {
		return ErrWorkforceMembershipNotFound
	}
	membership.Status = status
	r.WorkforceMemberships[membershipID] = membership
	return nil
}

func (r *Repository) SetWorkforceMembershipRoles(_ context.Context, membershipID string, roles []string) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if len(roles) == 0 {
		return errors.New("at least one role is required")
	}
	for _, role := range roles {
		if role == "" {
			return errors.New("role must not be empty")
		}
	}
	membership, ok := r.WorkforceMemberships[membershipID]
	if !ok {
		return ErrWorkforceMembershipNotFound
	}
	membership.Roles = roles
	r.WorkforceMemberships[membershipID] = membership
	return nil
}

func (r *Repository) LinkExternalIdentityAudited(ctx context.Context, external domain.ExternalIdentity, actor AuditActor, reason string) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := r.LinkExternalIdentity(ctx, external); err != nil {
		return err
	}
	r.LinkAudit = append(r.LinkAudit, LinkAuditRecord{External: external, Actor: actor, Reason: reason})
	return nil
}

func (r *Repository) UnlinkExternalIdentityAudited(_ context.Context, principalID, issuer, subject string, administrative bool, actor AuditActor, reason string) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	key := externalIdentityKey(issuer, subject)
	external, ok := r.ExternalIdentities[key]
	if !ok || external.PrincipalID != principalID || external.Status != "ACTIVE" {
		return ErrExternalIdentityNotLinked
	}
	if !administrative {
		activeCount := 0
		for _, e := range r.ExternalIdentities {
			if e.PrincipalID == principalID && e.Status == "ACTIVE" {
				activeCount++
			}
		}
		if activeCount <= 1 {
			return ErrLastCredentialDenied
		}
	}
	external.Status = "UNLINKED"
	r.ExternalIdentities[key] = external
	r.UnlinkAudit = append(r.UnlinkAudit, UnlinkAuditRecord{
		PrincipalID: principalID, Issuer: issuer, Subject: subject,
		Administrative: administrative, Actor: actor, Reason: reason,
	})
	return nil
}

func (r *Repository) MergePrincipalsAudited(_ context.Context, sourcePrincipalID, targetPrincipalID string, actor AuditActor, reason string) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if sourcePrincipalID == "" || targetPrincipalID == "" {
		return errors.New("source and target principal ids are required")
	}
	if sourcePrincipalID == targetPrincipalID {
		return ErrMergeSourceEqualsTarget
	}
	source, ok := r.Principals[sourcePrincipalID]
	if !ok {
		return ErrIdentityNotFound
	}
	target, ok := r.Principals[targetPrincipalID]
	if !ok {
		return ErrIdentityNotFound
	}
	if source.Status == "ARCHIVED" || target.Status == "ARCHIVED" {
		return ErrMergeNotEligible
	}

	var transferredExternal []domain.ExternalIdentity
	for key, external := range r.ExternalIdentities {
		if external.PrincipalID != sourcePrincipalID {
			continue
		}
		external.PrincipalID = targetPrincipalID
		r.ExternalIdentities[key] = external
		transferredExternal = append(transferredExternal, external)
	}
	var transferredReferences []domain.IdentityReference
	for key, reference := range r.IdentityReferences {
		if reference.PrincipalID != sourcePrincipalID {
			continue
		}
		reference.PrincipalID = targetPrincipalID
		r.IdentityReferences[key] = reference
		transferredReferences = append(transferredReferences, reference)
	}

	// Workforce memberships (ADR-0009 §27): a source membership transfers
	// to the target unless the target already has its own membership for
	// that tenant, in which case transferring would create a second row
	// for the same (principal, tenant) pair -- exactly what migration
	// 000032's UNIQUE(principal_id, tenant_id) exists to prevent. Since
	// requireAdminRole (api/router.go) resolves a token's issuer+subject to
	// the target principal after the merge, an unreconciled source
	// membership would otherwise be stranded on the now-archived source
	// and silently lose the merged administrator's tenant access -- so a
	// conflicting source row is disabled in place (audit-visible, not
	// deleted) rather than left dangling, and the target's own existing
	// membership for that tenant remains authoritative.
	targetTenants := map[string]bool{}
	for _, m := range r.WorkforceMemberships {
		if m.PrincipalID == targetPrincipalID {
			targetTenants[m.TenantID] = true
		}
	}
	var transferredMemberships, disabledMemberships []domain.WorkforceMembership
	for id, m := range r.WorkforceMemberships {
		if m.PrincipalID != sourcePrincipalID {
			continue
		}
		if targetTenants[m.TenantID] {
			m.Status = "DISABLED"
			r.WorkforceMemberships[id] = m
			disabledMemberships = append(disabledMemberships, m)
			continue
		}
		m.PrincipalID = targetPrincipalID
		r.WorkforceMemberships[id] = m
		transferredMemberships = append(transferredMemberships, m)
	}

	source.Status = "ARCHIVED"
	r.Principals[sourcePrincipalID] = source

	r.MergeAudit = append(r.MergeAudit, MergeAuditRecord{
		SourcePrincipalID:               sourcePrincipalID,
		TargetPrincipalID:               targetPrincipalID,
		TransferredExternalIdentities:   transferredExternal,
		TransferredIdentityReferences:   transferredReferences,
		TransferredWorkforceMemberships: transferredMemberships,
		DisabledWorkforceMemberships:    disabledMemberships,
		Actor:                           actor,
		Reason:                          reason,
	})
	return nil
}

func identityReferenceKey(engine, externalType, externalID string) string {
	return engine + "\x00" + externalType + "\x00" + externalID
}

func (r *Repository) CreateIdentityReference(_ context.Context, reference domain.IdentityReference) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := reference.Validate(); err != nil {
		return fmt.Errorf("validate identity reference: %w", err)
	}
	if reference.ID == "" {
		return errors.New("identity reference id is required")
	}
	if _, exists := r.Principals[reference.PrincipalID]; !exists {
		return fmt.Errorf("principal %s does not exist", reference.PrincipalID)
	}
	key := identityReferenceKey(reference.Engine, reference.ExternalType, reference.ExternalID)
	if _, exists := r.IdentityReferences[key]; exists {
		return ErrIdentityReferenceAlreadyMapped
	}
	r.IdentityReferences[key] = reference
	return nil
}

func (r *Repository) ListIdentityReferences(_ context.Context, principalID string) ([]domain.IdentityReference, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []domain.IdentityReference
	for _, reference := range r.IdentityReferences {
		if reference.PrincipalID == principalID {
			out = append(out, reference)
		}
	}
	return out, nil
}

func (r *Repository) ResolveIdentityReference(_ context.Context, engine, externalType, externalID string) (domain.IdentityReference, error) {
	if r == nil {
		return domain.IdentityReference{}, errors.New("repository is nil")
	}
	reference, ok := r.IdentityReferences[identityReferenceKey(engine, externalType, externalID)]
	if !ok {
		return domain.IdentityReference{}, ErrIdentityReferenceNotFound
	}
	return reference, nil
}

func (r *Repository) ListBindings(_ context.Context, capabilityKey string) ([]resolver.CapabilityBinding, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	bindings, ok := r.Bindings[capabilityKey]
	if !ok {
		return nil, fmt.Errorf("no bindings for %s", capabilityKey)
	}
	return bindings, nil
}

func (r *Repository) CreateBinding(_ context.Context, binding resolver.CapabilityBinding) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if binding.CapabilityKey == "" {
		return errors.New("capability key is required")
	}
	if binding.EngineID == "" || binding.EngineInstanceID == "" {
		return errors.New("engine and engine instance are required")
	}
	for _, existing := range r.Bindings[binding.CapabilityKey] {
		if existing.EngineInstanceID == binding.EngineInstanceID {
			return fmt.Errorf("binding for engine instance %s already exists", binding.EngineInstanceID)
		}
	}
	binding.Version = 1
	r.Bindings[binding.CapabilityKey] = append(r.Bindings[binding.CapabilityKey], binding)
	return nil
}

func (r *Repository) SaveBinding(_ context.Context, binding resolver.CapabilityBinding, expectedVersion int64) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	for index, current := range r.Bindings[binding.CapabilityKey] {
		if current.EngineInstanceID == binding.EngineInstanceID {
			if current.Version != expectedVersion {
				return fmt.Errorf("binding for engine instance %s version conflict: expected %d, got %d", binding.EngineInstanceID, expectedVersion, current.Version)
			}
			binding.Version = current.Version + 1
			r.Bindings[binding.CapabilityKey][index] = binding
			return nil
		}
	}
	return fmt.Errorf("binding for engine instance %s not found", binding.EngineInstanceID)
}

func (r *Repository) ListActiveInstances(_ context.Context, engineID string) ([]resolver.EngineInstance, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	instances, ok := r.EngineInstances[engineID]
	if !ok {
		return nil, fmt.Errorf("no engine instances for %s", engineID)
	}
	active := make([]resolver.EngineInstance, 0, len(instances))
	for _, instance := range instances {
		if instance.Status == "ACTIVE" {
			active = append(active, instance)
		}
	}
	return active, nil
}

func (r *Repository) CreateContext(_ context.Context, resolved domain.Context) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := resolved.Validate(); err != nil {
		return fmt.Errorf("validate context: %w", err)
	}
	if resolved.ID == "" {
		return errors.New("context id is required")
	}
	if _, exists := r.Contexts[resolved.ID]; exists {
		return fmt.Errorf("context %s already exists", resolved.ID)
	}
	r.Contexts[resolved.ID] = resolved
	return nil
}

func (r *Repository) GetContext(_ context.Context, contextID string) (domain.Context, error) {
	if r == nil {
		return domain.Context{}, errors.New("repository is nil")
	}
	resolved, ok := r.Contexts[contextID]
	if !ok || resolved.IsExpired(time.Now().UTC()) {
		return domain.Context{}, ErrContextNotFound
	}
	return resolved, nil
}

func (r *Repository) DeleteContextsByTenant(_ context.Context, tenantID string) (int64, error) {
	if r == nil {
		return 0, errors.New("repository is nil")
	}
	var removed int64
	for id, resolved := range r.Contexts {
		if resolved.TenantID == tenantID {
			delete(r.Contexts, id)
			removed++
		}
	}
	return removed, nil
}

func (r *Repository) CreateDigitalEstate(_ context.Context, estate domain.DigitalEstate) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := estate.Validate(); err != nil {
		return fmt.Errorf("validate digital estate: %w", err)
	}
	if estate.ID == "" {
		return errors.New("digital estate id is required")
	}
	if _, exists := r.DigitalEstates[estate.ID]; exists {
		return fmt.Errorf("digital estate %s already exists", estate.ID)
	}
	for _, existing := range r.DigitalEstates {
		if existing.Domain == estate.Domain {
			return fmt.Errorf("digital estate domain %q is already in use", estate.Domain)
		}
	}
	r.DigitalEstates[estate.ID] = estate
	return nil
}

func (r *Repository) GetDigitalEstate(_ context.Context, id string) (domain.DigitalEstate, error) {
	if r == nil {
		return domain.DigitalEstate{}, errors.New("repository is nil")
	}
	estate, ok := r.DigitalEstates[id]
	if !ok {
		return domain.DigitalEstate{}, fmt.Errorf("digital estate %s not found", id)
	}
	return estate, nil
}

func (r *Repository) ListDigitalEstatesForTenant(_ context.Context, tenantID string) ([]domain.DigitalEstate, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []domain.DigitalEstate
	for _, estate := range r.DigitalEstates {
		if estate.TenantID == tenantID {
			out = append(out, estate)
		}
	}
	return out, nil
}

func (r *Repository) CreateMarket(_ context.Context, market domain.Market) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := market.Validate(); err != nil {
		return fmt.Errorf("validate market: %w", err)
	}
	if market.ID == "" {
		return errors.New("market id is required")
	}
	if _, exists := r.Markets[market.ID]; exists {
		return fmt.Errorf("market %s already exists", market.ID)
	}
	for _, existing := range r.Markets {
		if existing.Code == market.Code {
			return fmt.Errorf("market code %q is already in use", market.Code)
		}
	}
	r.Markets[market.ID] = market
	return nil
}

func (r *Repository) GetMarket(_ context.Context, id string) (domain.Market, error) {
	if r == nil {
		return domain.Market{}, errors.New("repository is nil")
	}
	market, ok := r.Markets[id]
	if !ok {
		return domain.Market{}, fmt.Errorf("market %s not found", id)
	}
	return market, nil
}

func (r *Repository) GetMarketByCode(_ context.Context, code string) (domain.Market, error) {
	if r == nil {
		return domain.Market{}, errors.New("repository is nil")
	}
	for _, market := range r.Markets {
		if market.Code == code {
			return market, nil
		}
	}
	return domain.Market{}, fmt.Errorf("market with code %q not found", code)
}

// marketAssignmentPeriodsOverlap mirrors market_assignment_active_excl's
// [effective_from, effective_to) semantics: a nil EffectiveTo behaves as
// unbounded (+infinity).
func marketAssignmentPeriodsOverlap(a, b domain.MarketAssignment) bool {
	aEndsBeforeBStarts := a.EffectiveTo != nil && !a.EffectiveTo.After(b.EffectiveFrom)
	bEndsBeforeAStarts := b.EffectiveTo != nil && !b.EffectiveTo.After(a.EffectiveFrom)
	return !aEndsBeforeBStarts && !bEndsBeforeAStarts
}

func (r *Repository) AssignMarketToTenant(_ context.Context, assignment domain.MarketAssignment) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := assignment.Validate(); err != nil {
		return fmt.Errorf("validate market assignment: %w", err)
	}
	if assignment.ID == "" {
		return errors.New("market assignment id is required")
	}
	if _, exists := r.MarketAssignments[assignment.ID]; exists {
		return fmt.Errorf("market assignment %s already exists", assignment.ID)
	}
	for _, existing := range r.MarketAssignments {
		if existing.TenantID == assignment.TenantID && existing.MarketID == assignment.MarketID && marketAssignmentPeriodsOverlap(existing, assignment) {
			return fmt.Errorf("%w: tenant %s, market %s", ErrMarketAssignmentOverlap, assignment.TenantID, assignment.MarketID)
		}
	}
	r.MarketAssignments[assignment.ID] = assignment
	return nil
}

func (r *Repository) ListActiveMarketsForTenant(_ context.Context, tenantID string, at time.Time) ([]domain.Market, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []domain.Market
	for _, assignment := range r.MarketAssignments {
		if assignment.TenantID != tenantID {
			continue
		}
		if assignment.EffectiveFrom.After(at) {
			continue
		}
		if assignment.EffectiveTo != nil && !assignment.EffectiveTo.After(at) {
			continue
		}
		market, ok := r.Markets[assignment.MarketID]
		if !ok {
			continue
		}
		out = append(out, market)
	}
	return out, nil
}

func (r *Repository) ListMarketAssignmentsForTenant(_ context.Context, tenantID string) ([]domain.MarketAssignment, error) {
	if r == nil {
		return nil, errors.New("repository is nil")
	}
	var out []domain.MarketAssignment
	for _, assignment := range r.MarketAssignments {
		if assignment.TenantID == tenantID {
			out = append(out, assignment)
		}
	}
	return out, nil
}

func (r *Repository) CreateIsolationProfile(_ context.Context, profile domain.IsolationProfile) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate isolation profile: %w", err)
	}
	if profile.ID == "" {
		return errors.New("isolation profile id is required")
	}
	if _, exists := r.IsolationProfiles[profile.ID]; exists {
		return fmt.Errorf("isolation profile %s already exists", profile.ID)
	}
	for _, existing := range r.IsolationProfiles {
		if existing.Name == profile.Name {
			return fmt.Errorf("isolation profile name %q is already in use", profile.Name)
		}
	}
	r.IsolationProfiles[profile.ID] = profile
	return nil
}

func (r *Repository) GetIsolationProfile(_ context.Context, id string) (domain.IsolationProfile, error) {
	if r == nil {
		return domain.IsolationProfile{}, errors.New("repository is nil")
	}
	profile, ok := r.IsolationProfiles[id]
	if !ok {
		return domain.IsolationProfile{}, fmt.Errorf("isolation profile %s not found", id)
	}
	return profile, nil
}

func tenantIsolationProfileKey(a domain.TenantIsolationProfileAssignment) string {
	return a.TenantID + "\x00" + a.IsolationProfileID + "\x00" + a.EffectiveFrom.Format(time.RFC3339Nano)
}

// tenantIsolationProfilePeriodsOverlap mirrors
// tenant_isolation_profile_active_excl's [effective_from, effective_to)
// semantics: a nil EffectiveTo behaves as unbounded (+infinity).
func tenantIsolationProfilePeriodsOverlap(a, b domain.TenantIsolationProfileAssignment) bool {
	aEndsBeforeBStarts := a.EffectiveTo != nil && !a.EffectiveTo.After(b.EffectiveFrom)
	bEndsBeforeAStarts := b.EffectiveTo != nil && !b.EffectiveTo.After(a.EffectiveFrom)
	return !aEndsBeforeBStarts && !bEndsBeforeAStarts
}

func (r *Repository) AssignIsolationProfileToTenant(_ context.Context, assignment domain.TenantIsolationProfileAssignment) error {
	if r == nil {
		return errors.New("repository is nil")
	}
	if err := assignment.Validate(); err != nil {
		return fmt.Errorf("validate tenant isolation profile assignment: %w", err)
	}
	key := tenantIsolationProfileKey(assignment)
	if _, exists := r.TenantIsolationProfiles[key]; exists {
		return fmt.Errorf("tenant isolation profile assignment %s already exists", key)
	}
	for _, existing := range r.TenantIsolationProfiles {
		if existing.TenantID == assignment.TenantID && tenantIsolationProfilePeriodsOverlap(existing, assignment) {
			return fmt.Errorf("%w: tenant %s", ErrTenantIsolationProfileOverlap, assignment.TenantID)
		}
	}
	r.TenantIsolationProfiles[key] = assignment
	return nil
}

func (r *Repository) GetCurrentIsolationProfileForTenant(_ context.Context, tenantID string, at time.Time) (domain.IsolationProfile, error) {
	if r == nil {
		return domain.IsolationProfile{}, errors.New("repository is nil")
	}
	for _, assignment := range r.TenantIsolationProfiles {
		if assignment.TenantID != tenantID {
			continue
		}
		if assignment.EffectiveFrom.After(at) {
			continue
		}
		if assignment.EffectiveTo != nil && !assignment.EffectiveTo.After(at) {
			continue
		}
		profile, ok := r.IsolationProfiles[assignment.IsolationProfileID]
		if !ok {
			continue
		}
		return profile, nil
	}
	return domain.IsolationProfile{}, fmt.Errorf("no active isolation profile assignment for tenant %s", tenantID)
}
