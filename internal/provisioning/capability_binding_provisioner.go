// Target path: internal/provisioning/capability_binding_provisioner.go
//
// ZB-02 CapabilityBinding + EngineInstance resolution provisioning.
// Entitlement MUST already be materialised/validated before runtime provider
// selection; this code never treats a binding as a grant.
package provisioning

import (
    "context"
    "errors"
    "fmt"
    "time"

    capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
    "github.com/nabhold/baobab-cp/internal/domain"
    "github.com/nabhold/baobab-cp/internal/resolver"
)

type CapabilityBindingStore interface {
    GetCapability(ctx context.Context, capabilityKey string) (capabilitydomain.Capability, error)
    GetCapabilityScope(ctx context.Context, scopeID string) (capabilitydomain.CapabilityScope, error)
    ListBindings(ctx context.Context, capabilityKey string) ([]resolver.CapabilityBinding, error)
    CreateBinding(ctx context.Context, binding resolver.CapabilityBinding) error
    ListActiveInstances(ctx context.Context, engineID string) ([]resolver.EngineInstance, error)
}

type DesiredCapabilityBinding struct {
    CapabilityKey   string
    EngineID        string
    EngineInstanceID string
    ScopeID         string
    BindingMode     capabilitydomain.BindingMode
    Priority        int
    ContractVersion string
    EffectiveFrom   time.Time
    EffectiveTo     *time.Time
}

func (d DesiredCapabilityBinding) Validate() error {
    b := capabilitydomain.CapabilityBinding{
        CapabilityKey: d.CapabilityKey, EngineID: d.EngineID,
        EngineInstanceID: d.EngineInstanceID, ScopeID: d.ScopeID,
        BindingMode: d.BindingMode, Priority: d.Priority,
        Status: "ACTIVE", ContractVersion: d.ContractVersion,
        EffectiveFrom: d.EffectiveFrom, EffectiveTo: d.EffectiveTo,
    }
    if err := b.Validate(); err != nil { return err }
    if d.EngineID == "" { return errors.New("engine_id is required") }
    if d.ContractVersion == "" { return errors.New("contract_version is required") }
    return nil
}

type CapabilityBindingProvisioner struct {
    store CapabilityBindingStore
    now func() time.Time
    newID func() string
}

func NewCapabilityBindingProvisioner(store CapabilityBindingStore) *CapabilityBindingProvisioner {
    return &CapabilityBindingProvisioner{
        store: store,
        now: func() time.Time { return time.Now().UTC() },
        newID: domain.NewUUIDv7,
    }
}

// Apply validates capability, scope and the exact selected engine instance
// before persisting a binding. It fails closed on topology mismatch.
func (p *CapabilityBindingProvisioner) Apply(ctx context.Context, desired DesiredCapabilityBinding, trusted resolver.Context) (resolver.CapabilityBinding, bool, error) {
    if p == nil || p.store == nil {
        return resolver.CapabilityBinding{}, false, errors.New("capability binding provisioner is not initialized")
    }
    if err := desired.Validate(); err != nil {
        return resolver.CapabilityBinding{}, false, fmt.Errorf("validate desired binding: %w", err)
    }

    capability, err := p.store.GetCapability(ctx, desired.CapabilityKey)
    if err != nil { return resolver.CapabilityBinding{}, false, err }
    if !capability.IsResolvable() {
        return resolver.CapabilityBinding{}, false, fmt.Errorf("capability %q is not resolvable", desired.CapabilityKey)
    }

    scope, err := p.store.GetCapabilityScope(ctx, desired.ScopeID)
    if err != nil { return resolver.CapabilityBinding{}, false, err }
    if trusted.TenantID != "" && scope.TenantID != trusted.TenantID {
        return resolver.CapabilityBinding{}, false, errors.New("binding scope tenant mismatch")
    }

    instances, err := p.store.ListActiveInstances(ctx, desired.EngineID)
    if err != nil { return resolver.CapabilityBinding{}, false, err }
    selected, err := (resolver.TopologyResolverImpl{}).Resolve(ctx, resolver.TopologyResolutionQuery{
        Context: trusted,
        SelectedEngineInstanceID: desired.EngineInstanceID,
        EngineInstances: instances,
        At: effectiveAt(desired.EffectiveFrom, p.now()),
    })
    if err != nil {
        return resolver.CapabilityBinding{}, false, fmt.Errorf("engine instance is not eligible: %w", err)
    }
    if selected.EngineID != desired.EngineID {
        return resolver.CapabilityBinding{}, false, errors.New("selected engine instance belongs to another engine")
    }

    existing, err := p.store.ListBindings(ctx, desired.CapabilityKey)
    if err != nil { return resolver.CapabilityBinding{}, false, err }
    for _, b := range existing {
        if bindingIdentityMatches(b, desired) && b.Status == "ACTIVE" {
            return b, false, nil
        }
    }

    b := resolver.CapabilityBinding{
        ID: p.newID(), CapabilityKey: desired.CapabilityKey,
        EngineID: desired.EngineID, EngineInstanceID: desired.EngineInstanceID,
        ScopeID: desired.ScopeID, BindingMode: desired.BindingMode,
        Priority: desired.Priority, Status: "ACTIVE",
        ContractVersion: desired.ContractVersion,
        EffectiveFrom: desired.EffectiveFrom, EffectiveTo: desired.EffectiveTo,
    }
    if b.EffectiveFrom.IsZero() { b.EffectiveFrom = p.now() }
    if err := b.Validate(); err != nil { return resolver.CapabilityBinding{}, false, err }
    if err := p.store.CreateBinding(ctx, b); err != nil { return resolver.CapabilityBinding{}, false, err }
    return b, true, nil
}

// Resolve verifies the canonical binding resolver result and then validates
// the selected concrete topology instance. This preserves the accepted
// two-stage Binding -> EngineInstance resolution model.
func (p *CapabilityBindingProvisioner) Resolve(
    ctx context.Context,
    capabilityKey string,
    trusted resolver.Context,
    scopes map[string]capabilitydomain.CapabilityScope,
) (resolver.ResolvedCapability, resolver.EngineInstance, error) {
    capability, err := p.store.GetCapability(ctx, capabilityKey)
    if err != nil { return resolver.ResolvedCapability{}, resolver.EngineInstance{}, err }
    bindings, err := p.store.ListBindings(ctx, capabilityKey)
    if err != nil { return resolver.ResolvedCapability{}, resolver.EngineInstance{}, err }

    resolved, err := (resolver.CapabilityResolverImpl{}).Resolve(ctx, resolver.CapabilityResolutionQuery{
        CapabilityKey: capabilityKey, Context: trusted, Bindings: bindings,
        Scopes: scopes, At: p.now(), Capability: &capability,
    })
    if err != nil { return resolver.ResolvedCapability{}, resolver.EngineInstance{}, err }

    instances, err := p.store.ListActiveInstances(ctx, resolved.EngineID)
    if err != nil { return resolver.ResolvedCapability{}, resolver.EngineInstance{}, err }
    instance, err := (resolver.TopologyResolverImpl{}).Resolve(ctx, resolver.TopologyResolutionQuery{
        Context: trusted, SelectedEngineInstanceID: resolved.EngineInstanceID,
        EngineInstances: instances, At: p.now(),
    })
    if err != nil { return resolver.ResolvedCapability{}, resolver.EngineInstance{}, err }
    return resolved, instance, nil
}

func bindingIdentityMatches(b resolver.CapabilityBinding, d DesiredCapabilityBinding) bool {
    return b.CapabilityKey == d.CapabilityKey &&
        b.EngineID == d.EngineID &&
        b.EngineInstanceID == d.EngineInstanceID &&
        b.ScopeID == d.ScopeID &&
        b.BindingMode == d.BindingMode &&
        b.ContractVersion == d.ContractVersion
}

func effectiveAt(from, fallback time.Time) time.Time {
    if !from.IsZero() { return from }
    return fallback
}
