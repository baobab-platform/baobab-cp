// Target path: internal/provisioning/capability_binding_provisioner_test.go
package provisioning

import (
	"context"
	"testing"
	"time"

	capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/resolver"
)

type bindingStoreFake struct {
	capability capabilitydomain.Capability
	scope      capabilitydomain.CapabilityScope
	bindings   []resolver.CapabilityBinding
	instances  []resolver.EngineInstance
}

func (f *bindingStoreFake) GetCapability(context.Context, string) (capabilitydomain.Capability, error) {
	return f.capability, nil
}
func (f *bindingStoreFake) GetCapabilityScope(context.Context, string) (capabilitydomain.CapabilityScope, error) {
	return f.scope, nil
}
func (f *bindingStoreFake) ListBindings(context.Context, string) ([]resolver.CapabilityBinding, error) {
	return f.bindings, nil
}
func (f *bindingStoreFake) CreateBinding(_ context.Context, b resolver.CapabilityBinding) error {
	f.bindings = append(f.bindings, b)
	return nil
}
func (f *bindingStoreFake) ListActiveInstances(context.Context, string) ([]resolver.EngineInstance, error) {
	return f.instances, nil
}

func TestBindingApplyValidatesExactEngineInstance(t *testing.T) {
	now := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	f := &bindingStoreFake{
		capability: capabilitydomain.Capability{ID: "cap", Key: "commerce.order", Lifecycle: capabilitydomain.CapabilityLifecycleActive},
		scope:      capabilitydomain.CapabilityScope{ScopeID: "scope", TenantID: "tn_zuri"},
		instances: []resolver.EngineInstance{{
			ID: "instance", EngineID: "engine", Status: "ACTIVE",
			HealthStatus: "HEALTHY", Environment: "production", Region: "africa-south1",
		}},
	}
	p := NewCapabilityBindingProvisioner(f)
	p.now = func() time.Time { return now }
	p.newID = func() string { return "01990000-0000-7000-8000-000000000002" }

	_, created, err := p.Apply(context.Background(), DesiredCapabilityBinding{
		CapabilityKey: "commerce.order", EngineID: "engine", EngineInstanceID: "instance",
		ScopeID: "scope", BindingMode: capabilitydomain.BindingModePrimary,
		ContractVersion: "1", EffectiveFrom: now,
	}, resolver.Context{
		TenantID: "tn_zuri", Environment: "production", DeploymentRegion: "africa-south1",
	})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
}

var _ = domain.EngineInstance{}
