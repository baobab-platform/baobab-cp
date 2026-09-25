package resolver

import (
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestScopeMatcherPrefersMoreSpecificMatch(t *testing.T) {
	matcher := DefaultScopeMatcher{}
	ctx := Context{
		TenantID:      "tenant-123",
		LegalEntityID: "legal-456",
		MarketID:      "market-789",
		CountryCode:   "ZA",
		CurrencyCode:  "ZAR",
		Locale:        "en-ZA",
	}

	scope := domain.MappingScope{
		TenantID:      "tenant-123",
		LegalEntityID: "legal-456",
		MarketID:      "market-789",
		Country:       "ZA",
		Currency:      "ZAR",
		Locale:        "en-ZA",
	}

	match := matcher.Match(ctx, scope)
	if !match.Compatible {
		t.Fatal("expected compatible scope match")
	}
	if match.Specificity == 0 {
		t.Fatal("expected positive specificity")
	}
	if len(match.Matched) == 0 {
		t.Fatal("expected at least one matched dimension")
	}
}

// TestScopeMatcherChecksOrganisationAndBusinessUnit is a regression test
// for the organisation_id/business_unit_id dimensions ADR-BCP-004 §5 lists
// on PlatformContext's canonical model but domain.Context did not carry
// until now: a scope restricted to a different organisation/business unit
// must be rejected, not silently treated as compatible.
func TestScopeMatcherChecksOrganisationAndBusinessUnit(t *testing.T) {
	matcher := DefaultScopeMatcher{}
	ctx := Context{TenantID: "tenant-123", OrganisationID: "org-a", BusinessUnitID: "bu-a"}

	compatible := matcher.Match(ctx, domain.MappingScope{TenantID: "tenant-123", OrganisationID: "org-a", BusinessUnitID: "bu-a"})
	if !compatible.Compatible {
		t.Fatal("expected matching organisation/business_unit to be compatible")
	}

	incompatible := matcher.Match(ctx, domain.MappingScope{TenantID: "tenant-123", OrganisationID: "org-b"})
	if incompatible.Compatible {
		t.Fatal("expected a scope restricted to a different organisation to be incompatible")
	}
}

func TestContextResolverMergesEvidenceAndTrust(t *testing.T) {
	resolver := ContextResolverImpl{}
	evidence := ResolutionEvidence{
		PrincipalID:   "baobab-trade",
		TenantID:      "tenant-123",
		LegalEntityID: "legal-456",
		MarketID:      "market-789",
		CountryCode:   "ZA",
		CurrencyCode:  "ZAR",
		Locale:        "en-ZA",
		CorrelationID: "correlation-123",
		Provenance: map[string]ContextSource{
			"tenant":    {Source: "authn", TrustLevel: TrustAuthorised, Evidence: "jwt-subject"},
			"principal": {Source: "authn", TrustLevel: TrustAuthorised, Evidence: "jwt-subject"},
		},
	}

	ctx, err := resolver.Resolve(t.Context(), evidence)
	if err != nil {
		t.Fatalf("context resolution failed: %v", err)
	}
	if ctx.TenantID != "tenant-123" {
		t.Fatalf("tenant mismatch: got %q", ctx.TenantID)
	}
	if ctx.Provenance["tenant"].TrustLevel != TrustAuthorised {
		t.Fatal("expected authorised provenance")
	}
	// ADR-BCP-004 §70: every successfully resolved context SHALL receive a
	// context_id.
	if ctx.ID == "" {
		t.Fatal("resolved Context has no context_id")
	}
	if ctx.ExpiresAt != nil {
		t.Fatal("expected no ExpiresAt when evidence.TTL is unset")
	}
}

func TestContextResolverAppliesRequestedTTL(t *testing.T) {
	resolver := ContextResolverImpl{}
	evidence := ResolutionEvidence{
		PrincipalID:   "baobab-trade",
		TenantID:      "tenant-123",
		CorrelationID: "correlation-123",
		TTL:           5 * time.Minute,
	}
	ctx, err := resolver.Resolve(t.Context(), evidence)
	if err != nil {
		t.Fatalf("context resolution failed: %v", err)
	}
	if ctx.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be set when evidence.TTL is positive")
	}
	if got := ctx.ExpiresAt.Sub(ctx.ResolvedAt); got != 5*time.Minute {
		t.Fatalf("expected a 5-minute lifetime, got %s", got)
	}
	if ctx.IsExpired(ctx.ResolvedAt) {
		t.Fatal("a freshly resolved context reported itself already expired")
	}
	if !ctx.IsExpired(ctx.ExpiresAt.Add(time.Second)) {
		t.Fatal("a context past its ExpiresAt did not report itself expired")
	}
}
