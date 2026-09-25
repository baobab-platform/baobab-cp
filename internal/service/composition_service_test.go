package service

import (
	"context"
	"errors"
	"testing"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	productdomain "github.com/baobab-platform/baobab-cp/internal/product/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func activeProductVersion() productdomain.ProductVersion {
	return productdomain.ProductVersion{ID: "prodver_1", ProductID: "baobab-xbt", Version: "1.0.0", CompositionKey: "solution.baobab-xbt", Status: productdomain.ProductLifecycleActive}
}

func TestCompositionExpansionServiceExpandsMandatoryAndImportantMembers(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()
	if err := repo.CreateComposition(ctx, capabilitydomain.CapabilityComposition{
		CompositionKey:  "solution.baobab-xbt",
		CompositionType: capabilitydomain.CompositionTypeProduct,
		Version:         "1.0.0",
		Lifecycle:       capabilitydomain.CapabilityLifecycleActive,
		Members: []capabilitydomain.CompositionMember{
			{CapabilityKey: "commerce.order.create", Criticality: capabilitydomain.MembershipCriticalityMandatory},
			{CapabilityKey: "logistics.shipment.create", Criticality: capabilitydomain.MembershipCriticalityImportant},
			{CapabilityKey: "commerce.order.refund", Criticality: capabilitydomain.MembershipCriticalityOptional},
			{CapabilityKey: "commerce.order.cancel", Criticality: capabilitydomain.MembershipCriticalityMandatory, ActivationCondition: "channel == B2B"},
		},
	}); err != nil {
		t.Fatalf("create composition: %v", err)
	}

	svc := CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}
	projections, err := svc.Expand(ctx, "sub_1", "tenant-123", activeProductVersion())
	if err != nil {
		t.Fatalf("expand: %v", err)
	}

	if len(projections) != 2 {
		t.Fatalf("expected 2 projections (MANDATORY + IMPORTANT only), got %d: %#v", len(projections), projections)
	}
	seen := map[string]productdomain.EntitlementProjection{}
	for _, p := range projections {
		seen[p.CapabilityKey] = p
	}
	for _, key := range []string{"commerce.order.create", "logistics.shipment.create"} {
		p, ok := seen[key]
		if !ok {
			t.Fatalf("expected a projection for %q", key)
		}
		if p.Status != productdomain.EntitlementProjectionStatusMaterialized {
			t.Fatalf("expected %q to be MATERIALIZED, got %s (reason: %s)", key, p.Status, p.FailureReason)
		}
		if p.GrantID == "" {
			t.Fatalf("expected %q to cite a grant_id", key)
		}
	}
	if _, ok := seen["commerce.order.refund"]; ok {
		t.Fatal("expected OPTIONAL member to not expand")
	}
	if _, ok := seen["commerce.order.cancel"]; ok {
		t.Fatal("expected member with a non-empty activation_condition to not expand")
	}

	grants, err := repo.ListGrants(ctx, "tenant-123", "commerce.order.create")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected exactly one grant for commerce.order.create, got %d", len(grants))
	}
	if grants[0].Source != capabilitydomain.GrantSourceProductSubscription || grants[0].SourceReference != "sub_1" {
		t.Fatalf("expected grant sourced from the subscription, got %#v", grants[0])
	}

	scopes, err := repo.ListCapabilityScopes(ctx, "tenant-123")
	if err != nil {
		t.Fatalf("list scopes: %v", err)
	}
	if len(scopes) != 1 {
		t.Fatalf("expected exactly one tenant-baseline scope to be created, got %d", len(scopes))
	}
}

func TestCompositionExpansionServiceReusesExistingTenantBaselineScope(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()
	if err := repo.CreateCapabilityScope(ctx, capabilitydomain.CapabilityScope{ScopeID: "scope-existing", TenantID: "tenant-123"}); err != nil {
		t.Fatalf("create scope: %v", err)
	}
	if err := repo.CreateComposition(ctx, capabilitydomain.CapabilityComposition{
		CompositionKey: "solution.baobab-xbt", CompositionType: capabilitydomain.CompositionTypeProduct, Version: "1.0.0",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Members:   []capabilitydomain.CompositionMember{{CapabilityKey: "commerce.order.create", Criticality: capabilitydomain.MembershipCriticalityMandatory}},
	}); err != nil {
		t.Fatalf("create composition: %v", err)
	}

	svc := CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}
	if _, err := svc.Expand(ctx, "sub_1", "tenant-123", activeProductVersion()); err != nil {
		t.Fatalf("expand: %v", err)
	}

	grants, err := repo.ListGrants(ctx, "tenant-123", "commerce.order.create")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 || grants[0].ScopeID != "scope-existing" {
		t.Fatalf("expected the grant to reuse the existing tenant-baseline scope, got %#v", grants)
	}
	scopes, err := repo.ListCapabilityScopes(ctx, "tenant-123")
	if err != nil {
		t.Fatalf("list scopes: %v", err)
	}
	if len(scopes) != 1 {
		t.Fatalf("expected no new scope to be created, got %d scopes", len(scopes))
	}
}

func TestCompositionExpansionServiceRejectsNonActiveProductVersion(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}
	draft := activeProductVersion()
	draft.Status = productdomain.ProductLifecycleDraft
	if _, err := svc.Expand(context.Background(), "sub_1", "tenant-123", draft); err == nil {
		t.Fatal("expected rejection of a non-ACTIVE product version")
	}
}

func TestCompositionExpansionServiceFailsClosedWhenCompositionMissing(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	svc := CompositionExpansionService{Compositions: repo, Grants: repo, Projections: repo}
	if _, err := svc.Expand(context.Background(), "sub_1", "tenant-123", activeProductVersion()); err == nil {
		t.Fatal("expected resolution to fail closed when the composition_key has no registered composition")
	}
}

func TestCompositionExpansionServiceRecordsFailedProjectionOnGrantError(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	ctx := context.Background()
	if err := repo.CreateComposition(ctx, capabilitydomain.CapabilityComposition{
		CompositionKey: "solution.baobab-xbt", CompositionType: capabilitydomain.CompositionTypeProduct, Version: "1.0.0",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Members: []capabilitydomain.CompositionMember{
			{CapabilityKey: "commerce.order.create", Criticality: capabilitydomain.MembershipCriticalityMandatory},
			{CapabilityKey: "commerce.order.fulfil", Criticality: capabilitydomain.MembershipCriticalityMandatory},
		},
	}); err != nil {
		t.Fatalf("create composition: %v", err)
	}

	svc := CompositionExpansionService{Compositions: repo, Grants: failingGrantWriter{repo, "commerce.order.create"}, Projections: repo}
	projections, err := svc.Expand(ctx, "sub_1", "tenant-123", activeProductVersion())
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(projections) != 2 {
		t.Fatalf("expected both members to still produce a projection, got %d", len(projections))
	}
	var failed, materialized int
	for _, p := range projections {
		switch p.Status {
		case productdomain.EntitlementProjectionStatusFailed:
			failed++
			if p.FailureReason == "" {
				t.Fatal("expected a failure_reason on a FAILED projection")
			}
		case productdomain.EntitlementProjectionStatusMaterialized:
			materialized++
		}
	}
	if failed != 1 || materialized != 1 {
		t.Fatalf("expected exactly one FAILED and one MATERIALIZED projection, got failed=%d materialized=%d", failed, materialized)
	}
}

// failingGrantWriter wraps *repository.Repository but rejects CreateGrant
// for one specific capability key, proving Expand records a FAILED
// projection for that member and still processes the rest.
type failingGrantWriter struct {
	*repository.Repository
	failCapabilityKey string
}

func (f failingGrantWriter) CreateGrant(ctx context.Context, grant capabilitydomain.CapabilityGrant) error {
	if grant.CapabilityKey == f.failCapabilityKey {
		return errors.New("capability not provisioned")
	}
	return f.Repository.CreateGrant(ctx, grant)
}
