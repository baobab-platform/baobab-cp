package resolver

import (
	"errors"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestResolveMappingInContext(t *testing.T) {
	const tenant = "tn_0199a1b2c3d47e8f"
	ctx := Context{TenantID: tenant, MarketID: "mkt_kenya"}
	kenya := domain.MappingScope{TenantID: tenant, MarketID: "mkt_kenya"}
	uganda := domain.MappingScope{TenantID: tenant, MarketID: "mkt_uganda"}
	scopes := map[string]domain.MappingScope{"scope_kenya": kenya, "scope_uganda": uganda}
	mapping := func(id, target, scope string, priority int, confidence string) domain.Mapping {
		return domain.Mapping{ID: id, TenantID: tenant, ExternalReferenceID: target, ScopeID: scope,
			ResolutionPriority: priority, Confidence: confidence, Status: "ACTIVE"}
	}

	cases := []struct {
		name       string
		candidates []domain.Mapping
		wantID     string
		wantReason string
		wantErr    error
	}{
		{name: "unscoped default", candidates: []domain.Mapping{mapping("map_a", "ref_a", "", 0, "CONFIRMED")},
			wantID: "map_a", wantReason: "default_mapping"},
		{name: "matching scope outranks the default",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "", 500, "CONFIRMED"), mapping("map_b", "ref_b", "scope_kenya", 0, "")},
			wantID:     "map_b", wantReason: "scope_matched"},
		{name: "a scope the context contradicts never applies",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "scope_uganda", 0, "CONFIRMED")}, wantErr: ErrMappingNotFound},
		{name: "a scope that cannot be loaded never applies",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "scope_missing", 0, "CONFIRMED")}, wantErr: ErrMappingNotFound},
		{name: "priority decides between equally specific candidates",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "", 1, "CONFIRMED"), mapping("map_b", "ref_b", "", 2, "CONFIRMED")},
			wantID:     "map_b", wantReason: "priority_applied"},
		{name: "confidence decides after priority",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "", 0, "PROBABLE"), mapping("map_b", "ref_b", "", 0, "CONFIRMED")},
			wantID:     "map_b", wantReason: "priority_applied"},
		{name: "equal candidates with different targets are ambiguous",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "", 0, "CONFIRMED"), mapping("map_b", "ref_b", "", 0, "")},
			wantErr:    ErrMappingAmbiguous},
		{name: "equal candidates with one target are not ambiguous",
			candidates: []domain.Mapping{mapping("map_b", "ref_a", "", 0, "CONFIRMED"), mapping("map_a", "ref_a", "", 0, "CONFIRMED")},
			wantID:     "map_a", wantReason: "default_mapping"},
		{name: "candidate and rejected mappings never resolve",
			candidates: []domain.Mapping{mapping("map_a", "ref_a", "", 0, "CANDIDATE"), mapping("map_b", "ref_b", "", 0, "REJECTED")},
			wantErr:    ErrMappingNotFound},
		{name: "only ACTIVE mappings resolve",
			candidates: []domain.Mapping{{ID: "map_a", TenantID: tenant, ExternalReferenceID: "ref_a", Status: "VALIDATED"}},
			wantErr:    ErrMappingNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveMappingInContext(ctx, tc.candidates, scopes)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v, got %+v, %v", tc.wantErr, got, err)
				}
				return
			}
			if err != nil || got.Mapping.ID != tc.wantID || got.Reason != tc.wantReason {
				t.Fatalf("expected %s (%s), got %+v, %v", tc.wantID, tc.wantReason, got, err)
			}
		})
	}
}
