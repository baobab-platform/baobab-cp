package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/resolver"
)

type fakeResolver struct {
	calls  int
	result ResolutionResult
	err    error
}

func (f *fakeResolver) Resolve(ctx context.Context, req ResolutionRequest) (ResolutionResult, error) {
	f.calls++
	return f.result, f.err
}

func baseCachingRequest() ResolutionRequest {
	return ResolutionRequest{
		TenantID:          "tenant-123",
		CanonicalEntityID: "entity-abc",
		CapabilityKey:     "commerce.order.create",
		Context: resolver.Context{
			PrincipalID:   "principal-abc",
			TenantID:      "tenant-123",
			MarketID:      "market-789",
			CorrelationID: "correlation-1",
		},
	}
}

func TestCachingResolutionServiceCachesSuccessfulResolution(t *testing.T) {
	inner := &fakeResolver{result: ResolutionResult{Context: resolver.Context{TenantID: "tenant-123"}}}
	cache := &InMemoryResolutionCache{}
	svc := CachingResolutionService{Inner: inner, Cache: cache, TTL: time.Minute}

	first := baseCachingRequest()
	if _, err := svc.Resolve(context.Background(), first); err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected 1 call to inner resolver, got %d", inner.calls)
	}

	second := baseCachingRequest()
	second.Context.CorrelationID = "correlation-2"
	result, err := svc.Resolve(context.Background(), second)
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected cache hit to avoid a second inner call, got %d calls", inner.calls)
	}
	// The decision is served from cache, but the returned Context and trace
	// correlation must reflect the current request, not the one that
	// originally populated the cache entry.
	if result.Trace.CorrelationID != "correlation-2" {
		t.Fatalf("expected cached result to carry the current request's correlation id, got %q", result.Trace.CorrelationID)
	}
	if result.Context.CorrelationID != "correlation-2" {
		t.Fatalf("expected cached result's Context to reflect the current request, got %q", result.Context.CorrelationID)
	}
}

func TestCachingResolutionServiceMissesOnDifferingDimension(t *testing.T) {
	inner := &fakeResolver{result: ResolutionResult{}}
	cache := &InMemoryResolutionCache{}
	svc := CachingResolutionService{Inner: inner, Cache: cache, TTL: time.Minute}

	first := baseCachingRequest()
	if _, err := svc.Resolve(context.Background(), first); err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}

	second := baseCachingRequest()
	second.Context.MarketID = "market-999"
	if _, err := svc.Resolve(context.Background(), second); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("expected a differing security-relevant dimension to miss the cache, got %d calls", inner.calls)
	}
}

func TestCachingResolutionServiceDoesNotShareAcrossCapabilities(t *testing.T) {
	inner := &fakeResolver{result: ResolutionResult{}}
	cache := &InMemoryResolutionCache{}
	svc := CachingResolutionService{Inner: inner, Cache: cache, TTL: time.Minute}

	first := baseCachingRequest()
	if _, err := svc.Resolve(context.Background(), first); err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}

	second := baseCachingRequest()
	second.CapabilityKey = "commercial.quotation.manage"
	if _, err := svc.Resolve(context.Background(), second); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("expected a different capability to miss the cache, got %d calls", inner.calls)
	}
}

func TestCachingResolutionServiceDoesNotCacheErrors(t *testing.T) {
	inner := &fakeResolver{err: errors.New("resolution failed")}
	cache := &InMemoryResolutionCache{}
	svc := CachingResolutionService{Inner: inner, Cache: cache, TTL: time.Minute}

	req := baseCachingRequest()
	if _, err := svc.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected error from inner resolver to propagate")
	}
	if _, err := svc.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected error from inner resolver to propagate on second call")
	}
	if inner.calls != 2 {
		t.Fatalf("expected an error result to never be cached, got %d calls", inner.calls)
	}
}

func TestCachingResolutionServiceDisabledByZeroTTL(t *testing.T) {
	inner := &fakeResolver{result: ResolutionResult{}}
	cache := &InMemoryResolutionCache{}
	svc := CachingResolutionService{Inner: inner, Cache: cache}

	req := baseCachingRequest()
	if _, err := svc.Resolve(context.Background(), req); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if _, err := svc.Resolve(context.Background(), req); err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("expected zero-value TTL to disable caching entirely, got %d calls", inner.calls)
	}
}

func TestInMemoryResolutionCacheExpiresEntries(t *testing.T) {
	now := time.Now()
	cache := &InMemoryResolutionCache{Now: func() time.Time { return now }}
	dims := CacheDimensions{Tenant: "tenant-123"}
	cache.Set(dims, ResolutionResult{}, time.Minute)

	if _, ok := cache.Get(dims); !ok {
		t.Fatal("expected a fresh entry to be present")
	}

	now = now.Add(2 * time.Minute)
	if _, ok := cache.Get(dims); ok {
		t.Fatal("expected an expired entry to be evicted")
	}
}

func TestInMemoryResolutionCacheInvalidateTenant(t *testing.T) {
	cache := &InMemoryResolutionCache{}
	tenantA := CacheDimensions{Tenant: "tenant-a"}
	tenantB := CacheDimensions{Tenant: "tenant-b"}
	cache.Set(tenantA, ResolutionResult{}, time.Minute)
	cache.Set(tenantB, ResolutionResult{}, time.Minute)

	cache.InvalidateTenant("tenant-a")

	if _, ok := cache.Get(tenantA); ok {
		t.Fatal("expected tenant-a's entry to be invalidated")
	}
	if _, ok := cache.Get(tenantB); !ok {
		t.Fatal("expected tenant-b's entry to remain cached")
	}
}

func TestInMemoryResolutionCachePurge(t *testing.T) {
	cache := &InMemoryResolutionCache{}
	dims := CacheDimensions{Tenant: "tenant-123"}
	cache.Set(dims, ResolutionResult{}, time.Minute)
	cache.Purge()
	if _, ok := cache.Get(dims); ok {
		t.Fatal("expected purge to remove all entries")
	}
}
