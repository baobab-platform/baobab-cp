package resolver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func BenchmarkMappingResolution100Candidates(b *testing.B) {
	at := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	candidates := make([]domain.Mapping, 100)
	scopes := make(map[string]domain.MappingScope, len(candidates))
	for i := range candidates {
		scopeID := fmt.Sprintf("scope-%03d", i)
		tenantID := "tn_other"
		if i == len(candidates)-1 {
			tenantID = "tn_zuribeans"
		}
		candidates[i] = validMapping(scopeID, "canonical-warehouse", fmt.Sprintf("external-%03d", i))
		candidates[i].ResolutionPriority = i
		scopes[scopeID] = domain.MappingScope{ScopeID: scopeID, TenantID: tenantID}
	}
	query := MappingResolutionQuery{
		CanonicalEntityID: "canonical-warehouse",
		Context:           Context{TenantID: "tn_zuribeans", ResolvedAt: at},
		Candidates:        candidates,
		Scopes:            scopes,
		At:                at,
	}
	resolver := MappingResolverImpl{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolver.Resolve(context.Background(), query); err != nil {
			b.Fatal(err)
		}
	}
}
