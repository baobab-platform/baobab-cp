// Target path: internal/provisioning/capability_grant_provisioner.go
//
// ZB-02 CapabilityGrant materialisation. CapabilityGrant answers entitlement
// only; provider selection remains CapabilityBinding's responsibility.
package provisioning

import (
    "context"
    "errors"
    "fmt"
    "sort"
    "strings"
    "time"

    capabilitydomain "github.com/nabhold/baobab-cp/internal/capability/domain"
    "github.com/nabhold/baobab-cp/internal/domain"
)

// CapabilityGrantStore is the minimum persistence surface required for
// idempotent desired-state materialisation.
type CapabilityGrantStore interface {
    GetCapability(ctx context.Context, capabilityKey string) (capabilitydomain.Capability, error)
    GetCapabilityScope(ctx context.Context, scopeID string) (capabilitydomain.CapabilityScope, error)
    ListGrants(ctx context.Context, tenantID, capabilityKey string) ([]capabilitydomain.CapabilityGrant, error)
    CreateGrant(ctx context.Context, grant capabilitydomain.CapabilityGrant) error
    RevokeGrant(ctx context.Context, grantID, revokedBy, reason string, expectedVersion int64) error
}

// DesiredCapabilityGrant is provisioning input, not a new canonical domain
// aggregate. It describes the grant CP should ensure exists.
type DesiredCapabilityGrant struct {
    TenantID        string
    CapabilityKey   string
    ScopeID         string
    Source          capabilitydomain.GrantSource
    SourceReference string
    EffectiveFrom   time.Time
    EffectiveTo     *time.Time
    Constraints     map[string]any
    GrantedBy       string
}

func (d DesiredCapabilityGrant) Validate() error {
    probe := capabilitydomain.CapabilityGrant{
        TenantID: d.TenantID, CapabilityKey: d.CapabilityKey, ScopeID: d.ScopeID,
        Source: d.Source, SourceReference: d.SourceReference,
        Status: capabilitydomain.GrantStatusActive,
        EffectiveFrom: d.EffectiveFrom, EffectiveTo: d.EffectiveTo,
        Constraints: d.Constraints, GrantedBy: d.GrantedBy,
    }
    return probe.Validate()
}

// CapabilityGrantProvisioner materialises desired entitlements without
// conflating them with bindings or engine topology.
type CapabilityGrantProvisioner struct {
    store CapabilityGrantStore
    now   func() time.Time
    newID func() string
}

func NewCapabilityGrantProvisioner(store CapabilityGrantStore) *CapabilityGrantProvisioner {
    return &CapabilityGrantProvisioner{
        store: store,
        now: func() time.Time { return time.Now().UTC() },
        newID: domain.NewUUIDv7,
    }
}

// Apply is idempotent for the semantic identity
// (tenant, capability, scope, source, source_reference).
func (p *CapabilityGrantProvisioner) Apply(ctx context.Context, desired DesiredCapabilityGrant) (capabilitydomain.CapabilityGrant, bool, error) {
    if p == nil || p.store == nil {
        return capabilitydomain.CapabilityGrant{}, false, errors.New("capability grant provisioner is not initialized")
    }
    if err := desired.Validate(); err != nil {
        return capabilitydomain.CapabilityGrant{}, false, fmt.Errorf("validate desired capability grant: %w", err)
    }

    capability, err := p.store.GetCapability(ctx, desired.CapabilityKey)
    if err != nil {
        return capabilitydomain.CapabilityGrant{}, false, fmt.Errorf("resolve capability %q: %w", desired.CapabilityKey, err)
    }
    if !capability.IsResolvable() {
        return capabilitydomain.CapabilityGrant{}, false, fmt.Errorf("capability %q is not ACTIVE/resolvable", desired.CapabilityKey)
    }

    scope, err := p.store.GetCapabilityScope(ctx, desired.ScopeID)
    if err != nil {
        return capabilitydomain.CapabilityGrant{}, false, fmt.Errorf("resolve capability scope %q: %w", desired.ScopeID, err)
    }
    if scope.TenantID != desired.TenantID {
        return capabilitydomain.CapabilityGrant{}, false, errors.New("capability scope tenant does not match desired grant tenant")
    }

    grants, err := p.store.ListGrants(ctx, desired.TenantID, desired.CapabilityKey)
    if err != nil {
        return capabilitydomain.CapabilityGrant{}, false, err
    }
    for _, existing := range grants {
        if sameGrantIdentity(existing, desired) && existing.Status != capabilitydomain.GrantStatusRevoked {
            // Existing non-revoked source grant is the materialised record.
            // Configuration drift is surfaced rather than silently mutating
            // historical entitlement provenance.
            if !sameGrantWindow(existing, desired) {
                return capabilitydomain.CapabilityGrant{}, false, fmt.Errorf("capability grant drift for %s: effective window differs", existing.ID)
            }
            return existing, false, nil
        }
    }

    grant := capabilitydomain.CapabilityGrant{
        ID: p.newID(), TenantID: desired.TenantID,
        CapabilityID: capability.ID, CapabilityKey: desired.CapabilityKey,
        ScopeID: desired.ScopeID, Source: desired.Source,
        SourceReference: desired.SourceReference,
        Status: capabilitydomain.GrantStatusActive,
        EffectiveFrom: desired.EffectiveFrom, EffectiveTo: desired.EffectiveTo,
        Constraints: desired.Constraints, GrantedBy: desired.GrantedBy,
    }
    if grant.EffectiveFrom.IsZero() {
        grant.EffectiveFrom = p.now()
    }
    if err := grant.Validate(); err != nil {
        return capabilitydomain.CapabilityGrant{}, false, err
    }
    if err := p.store.CreateGrant(ctx, grant); err != nil {
        return capabilitydomain.CapabilityGrant{}, false, err
    }
    return grant, true, nil
}

// Reconcile applies every desired grant deterministically and returns the
// resulting materialised set. Revocation of grants omitted from desired state
// is deliberately separate: absence is not proof that another independent
// source's entitlement should be revoked.
func (p *CapabilityGrantProvisioner) Reconcile(ctx context.Context, desired []DesiredCapabilityGrant) ([]capabilitydomain.CapabilityGrant, error) {
    sort.SliceStable(desired, func(i, j int) bool {
        return grantDesiredKey(desired[i]) < grantDesiredKey(desired[j])
    })
    out := make([]capabilitydomain.CapabilityGrant, 0, len(desired))
    seen := map[string]struct{}{}
    for _, d := range desired {
        key := grantDesiredKey(d)
        if _, ok := seen[key]; ok {
            return nil, fmt.Errorf("duplicate desired capability grant %s", key)
        }
        seen[key] = struct{}{}
        grant, _, err := p.Apply(ctx, d)
        if err != nil {
            return nil, err
        }
        out = append(out, grant)
    }
    return out, nil
}

// RevokeSourceGrant revokes exactly one provenance record. It never revokes
// sibling grants for the same capability/scope from other sources.
func (p *CapabilityGrantProvisioner) RevokeSourceGrant(ctx context.Context, grant capabilitydomain.CapabilityGrant, actor, reason string) error {
    if strings.TrimSpace(reason) == "" {
        return errors.New("revocation reason is required")
    }
    if grant.Status == capabilitydomain.GrantStatusRevoked {
        return nil
    }
    return p.store.RevokeGrant(ctx, grant.ID, actor, reason, grant.Version)
}

func sameGrantIdentity(g capabilitydomain.CapabilityGrant, d DesiredCapabilityGrant) bool {
    return g.TenantID == d.TenantID &&
        g.CapabilityKey == d.CapabilityKey &&
        g.ScopeID == d.ScopeID &&
        g.Source == d.Source &&
        g.SourceReference == d.SourceReference
}

func sameGrantWindow(g capabilitydomain.CapabilityGrant, d DesiredCapabilityGrant) bool {
    if !d.EffectiveFrom.IsZero() && !g.EffectiveFrom.Equal(d.EffectiveFrom) {
        return false
    }
    if (g.EffectiveTo == nil) != (d.EffectiveTo == nil) {
        return false
    }
    return g.EffectiveTo == nil || g.EffectiveTo.Equal(*d.EffectiveTo)
}

func grantDesiredKey(d DesiredCapabilityGrant) string {
    return strings.Join([]string{d.TenantID, d.CapabilityKey, d.ScopeID, string(d.Source), d.SourceReference}, "|")
}
