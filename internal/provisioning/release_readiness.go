package provisioning

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// ReleaseReadinessCheckKey is the readiness check that carries release drift
// into provisioning readiness (ADR-BCP-025 section 2.8). A failure of it is
// explained by release_drift codes, which the readiness view derives from
// live drift rather than from this check's stored reason.
const ReleaseReadinessCheckKey = "engine-instance-release"

// ReleaseReadinessSource judges a tenant's release-drift readiness.
type ReleaseReadinessSource interface {
	TenantReleaseReadiness(ctx context.Context, tenantID string, now time.Time) (release.TenantReleaseReadiness, error)
}

// releaseReadinessProbe fails only when drift BLOCKS the tenant: a mandatory
// capability all of whose usable bindings run a revoked release, an
// unrecorded artifact, or in the wrong place. Degraded drift passes, and is
// visible in the readiness view. This changes readiness, never routing.
type releaseReadinessProbe struct{ source ReleaseReadinessSource }

func (p releaseReadinessProbe) Probe(ctx context.Context, tenantID string, at time.Time) (bool, string, string, error) {
	got, err := p.source.TenantReleaseReadiness(ctx, tenantID, at)
	if err != nil {
		return false, "", "", err
	}
	if got.Effect != release.EffectBlocked {
		return true, "no mandatory dependency is blocked by release drift", "", nil
	}
	var blocked []string
	for _, c := range got.Capabilities {
		if c.Effect != release.EffectBlocked {
			continue
		}
		for _, f := range c.Findings {
			blocked = append(blocked, fmt.Sprintf("%s (%s)", c.CapabilityKey, f.Reason))
		}
	}
	return false, "", "mandatory capabilities blocked by release drift: " + strings.Join(blocked, ", "), nil
}
