// Target path: internal/provisioning/capability_grant_provisioner_test.go
package provisioning

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
)

type grantStoreFake struct {
	capability capabilitydomain.Capability
	scope      capabilitydomain.CapabilityScope
	grants     []capabilitydomain.CapabilityGrant
}

func (f *grantStoreFake) GetCapability(context.Context, string) (capabilitydomain.Capability, error) {
	return f.capability, nil
}
func (f *grantStoreFake) GetCapabilityScope(context.Context, string) (capabilitydomain.CapabilityScope, error) {
	return f.scope, nil
}
func (f *grantStoreFake) ListGrants(context.Context, string, string) ([]capabilitydomain.CapabilityGrant, error) {
	return f.grants, nil
}
func (f *grantStoreFake) CreateGrant(_ context.Context, g capabilitydomain.CapabilityGrant) error {
	f.grants = append(f.grants, g)
	return nil
}
func (f *grantStoreFake) RevokeGrant(context.Context, string, string, string, int64) error {
	return nil
}

func TestCapabilityGrantApplyIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	f := &grantStoreFake{
		capability: capabilitydomain.Capability{ID: "cap-id", Key: "commerce.order", Lifecycle: capabilitydomain.CapabilityLifecycleActive},
		scope:      capabilitydomain.CapabilityScope{ScopeID: "scope-id", TenantID: "tn_zuri"},
	}
	p := NewCapabilityGrantProvisioner(f)
	p.now = func() time.Time { return now }
	p.newID = func() string { return "01990000-0000-7000-8000-000000000001" }
	d := DesiredCapabilityGrant{
		TenantID: "tn_zuri", CapabilityKey: "commerce.order", ScopeID: "scope-id",
		Source: capabilitydomain.GrantSourceInternalPolicy, SourceReference: "zb-02:zuri",
		EffectiveFrom: now,
	}
	_, created, err := p.Apply(context.Background(), d)
	if err != nil || !created {
		t.Fatalf("first apply: created=%v err=%v", created, err)
	}
	_, created, err = p.Apply(context.Background(), d)
	if err != nil || created {
		t.Fatalf("second apply must reuse existing: created=%v err=%v", created, err)
	}
	if len(f.grants) != 1 {
		t.Fatalf("expected one grant, got %d", len(f.grants))
	}
}

func TestCapabilityGrantRejectsCrossTenantScope(t *testing.T) {
	f := &grantStoreFake{
		capability: capabilitydomain.Capability{ID: "cap-id", Key: "commerce.order", Lifecycle: capabilitydomain.CapabilityLifecycleActive},
		scope:      capabilitydomain.CapabilityScope{ScopeID: "scope-id", TenantID: "tn_other"},
	}
	p := NewCapabilityGrantProvisioner(f)
	_, _, err := p.Apply(context.Background(), DesiredCapabilityGrant{
		TenantID: "tn_zuri", CapabilityKey: "commerce.order", ScopeID: "scope-id",
		Source: capabilitydomain.GrantSourceInternalPolicy, SourceReference: "zb-02:zuri",
		EffectiveFrom: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected cross-tenant scope to fail")
	}
}
