package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CacheDimensions are the security- and routing-relevant dimensions a
// resolution cache key SHALL include (ADR-BCP-003 §46, ADR-BCP-007 §43-44:
// omitting a security-relevant dimension is a defect, not a naming choice --
// e.g. a ZA resolution cached without Market could incorrectly be reused for
// UG). CorrelationID, Context.ID and ResolvedAt/ExpiresAt are deliberately
// excluded: they are per-request bookkeeping that differs on every call even
// for an identical logical decision, so keying on them would mean the cache
// never has a hit.
//
// Two dimensions the ADRs also list -- contract version and
// policy/configuration version -- have no corresponding field in this
// codebase's domain model yet, so they cannot be included here. That is a
// known, tracked gap (issue #74 sub-work item 5), not an oversight.
type CacheDimensions struct {
	Principal        string
	Tenant           string
	LegalEntity      string
	DigitalEstate    string
	DigitalProperty  string
	Channel          string
	Market           string
	Jurisdiction     string
	Currency         string
	Environment      string
	DeploymentRegion string
	IsolationProfile string
	Capability       string
	CanonicalEntity  string
}

func cacheDimensionsFor(req ResolutionRequest) CacheDimensions {
	return CacheDimensions{
		Principal:        req.Context.PrincipalID,
		Tenant:           req.Context.TenantID,
		LegalEntity:      req.Context.LegalEntityID,
		DigitalEstate:    req.Context.DigitalEstateID,
		DigitalProperty:  req.Context.DigitalPropertyID,
		Channel:          req.Context.ChannelID,
		Market:           req.Context.MarketID,
		Jurisdiction:     req.Context.Jurisdiction,
		Currency:         req.Context.CurrencyCode,
		Environment:      req.Context.Environment,
		DeploymentRegion: req.Context.DeploymentRegion,
		IsolationProfile: req.Context.IsolationProfileID,
		Capability:       req.CapabilityKey,
		CanonicalEntity:  req.CanonicalEntityID,
	}
}

func (d CacheDimensions) key() string {
	return fmt.Sprintf(
		"tenant=%s|principal=%s|legal_entity=%s|digital_estate=%s|digital_property=%s|channel=%s|market=%s|jurisdiction=%s|currency=%s|environment=%s|deployment_region=%s|isolation_profile=%s|capability=%s|canonical_entity=%s",
		d.Tenant, d.Principal, d.LegalEntity, d.DigitalEstate, d.DigitalProperty, d.Channel, d.Market, d.Jurisdiction, d.Currency, d.Environment, d.DeploymentRegion, d.IsolationProfile, d.Capability, d.CanonicalEntity,
	)
}

// ResolutionCache stores successful resolution decisions keyed by
// CacheDimensions. Implementations SHALL treat a cache-backend error or
// uncertainty as a miss rather than risk serving a stale privileged decision
// (ADR-BCP-003 §50 "Cache Fail-Safe Rule": re-resolve is preferred over
// continuing to trust stale access).
type ResolutionCache interface {
	Get(dims CacheDimensions) (ResolutionResult, bool)
	Set(dims CacheDimensions, result ResolutionResult, ttl time.Duration)
	// InvalidateTenant removes every cached decision for tenantID
	// (ADR-BCP-003 §49, ADR-BCP-007 §49: tenant suspension/decommission
	// SHALL invalidate relevant cache entries). This is the only
	// invalidation predicate implemented so far; grant/binding/provider
	// scoped invalidation needs those change-event sources to exist first
	// (issue #74 sub-work item 5, still open).
	InvalidateTenant(tenantID string)
	// Purge removes every cached entry.
	Purge()
}

type cacheEntry struct {
	result  ResolutionResult
	tenant  string
	expires time.Time
}

// InMemoryResolutionCache is a process-local, mutex-protected ResolutionCache.
// It has no cross-instance invalidation of its own; event-driven invalidation
// across a fleet (ADR-BCP-007 §50) is out of scope for this first increment
// and needs a message bus that does not exist yet in this codebase.
type InMemoryResolutionCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	// Now, when set, replaces time.Now for expiry checks so tests can
	// exercise TTL expiry deterministically without a real sleep. Nil (the
	// zero value) uses the real clock.
	Now func() time.Time
}

func (c *InMemoryResolutionCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *InMemoryResolutionCache) Get(dims CacheDimensions) (ResolutionResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[dims.key()]
	if !ok {
		return ResolutionResult{}, false
	}
	if !c.now().Before(entry.expires) {
		delete(c.entries, dims.key())
		return ResolutionResult{}, false
	}
	return entry.result, true
}

func (c *InMemoryResolutionCache) Set(dims CacheDimensions, result ResolutionResult, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[dims.key()] = cacheEntry{result: result, tenant: dims.Tenant, expires: c.now().Add(ttl)}
}

func (c *InMemoryResolutionCache) InvalidateTenant(tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if entry.tenant == tenantID {
			delete(c.entries, key)
		}
	}
}

func (c *InMemoryResolutionCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// ResolutionResolver is the minimal interface CachingResolutionService wraps.
// ResolutionService satisfies it without any change to that type.
type ResolutionResolver interface {
	Resolve(ctx context.Context, req ResolutionRequest) (ResolutionResult, error)
}

// CachingResolutionService wraps a ResolutionResolver with a bounded, TTL'd
// cache of successful resolution decisions (ADR-BCP-003 §46-50, ADR-BCP-007
// §41-53: "a cache may accelerate an authorization decision; it may never
// broaden one"). It is purely additive and opt-in -- nothing in
// cmd/controlplane/main.go constructs one, so no existing caller's behavior
// changes because this type exists.
//
// Only successful resolutions are cached. Negative/denial caching
// (ADR-BCP-007 §46) is explicitly out of scope for this increment: caching a
// denial safely requires governing its TTL independently of the positive TTL
// (§48 -- some capabilities may require "no reusable resolution cache" at
// all), which is a separate, deliberate follow-up rather than a corner cut
// here. A cache miss, a nil Cache, a non-positive TTL, or an error from Inner
// always falls through to a fresh authoritative resolution, matching §50's
// fail-closed default.
type CachingResolutionService struct {
	Inner ResolutionResolver
	Cache ResolutionCache
	// TTL bounds how long a successful decision may be served from cache.
	// ADR-BCP-007 §47: "No universal TTL SHALL be hard-coded into
	// architecture" -- callers set this per their own operational/security
	// policy. Zero or negative disables caching entirely, which is this
	// type's zero-value behavior.
	TTL time.Duration
}

func (s CachingResolutionService) Resolve(ctx context.Context, req ResolutionRequest) (ResolutionResult, error) {
	if s.Cache == nil || s.TTL <= 0 {
		return s.Inner.Resolve(ctx, req)
	}
	dims := cacheDimensionsFor(req)
	if cached, ok := s.Cache.Get(dims); ok {
		// The decision (mapping/capability/policy/topology) is what gets
		// accelerated by the cache; the per-request Context and trace
		// correlation identity always belong to the current caller, never
		// replayed from a previous request (ADR-BCP-004 §70-72 identifier
		// and lifetime semantics; ADR-BCP-003 §75-77 explainability depends
		// on the trace matching the request that was actually made).
		cached.Context = req.Context
		cached.Trace.CorrelationID = req.Context.CorrelationID
		return cached, nil
	}
	result, err := s.Inner.Resolve(ctx, req)
	if err != nil {
		return result, err
	}
	s.Cache.Set(dims, result, s.TTL)
	return result, nil
}
