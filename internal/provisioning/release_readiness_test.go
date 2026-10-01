package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

type fakeReleaseReadiness struct {
	got release.TenantReleaseReadiness
	err error
}

func (f fakeReleaseReadiness) TenantReleaseReadiness(context.Context, string, time.Time) (release.TenantReleaseReadiness, error) {
	return f.got, f.err
}

// TestReleaseReadinessProbe: only drift that BLOCKS a tenant fails the
// release readiness check; degraded drift passes (it is visible in the
// readiness view), and a read failure is an error, never a pass.
func TestReleaseReadinessProbe(t *testing.T) {
	blocked := release.Aggregate([]release.CapabilityReadiness{{CapabilityKey: "commerce.order.manage", Effect: release.EffectBlocked,
		Findings: []release.ReadinessFinding{{Reason: release.DriftRevokedReleaseRunning}}}})
	degraded := release.Aggregate([]release.CapabilityReadiness{{CapabilityKey: "commerce.order.manage", Effect: release.EffectDegraded,
		Findings: []release.ReadinessFinding{{Reason: release.DriftReleaseUnobserved}}}})
	for _, c := range []struct {
		name string
		src  fakeReleaseReadiness
		ok   bool
	}{
		{"no drift", fakeReleaseReadiness{}, true},
		{"degraded drift does not block", fakeReleaseReadiness{got: degraded}, true},
		{"blocking drift fails", fakeReleaseReadiness{got: blocked}, false},
	} {
		ok, _, reason, err := releaseReadinessProbe{source: c.src}.Probe(context.Background(), "tn_a", time.Now())
		if err != nil || ok != c.ok {
			t.Fatalf("%s: ok=%v err=%v", c.name, ok, err)
		}
		if !ok && !strings.Contains(reason, "commerce.order.manage") && !strings.Contains(reason, release.DriftRevokedReleaseRunning) {
			t.Fatalf("%s: reason %q does not name the capability and drift", c.name, reason)
		}
	}
	if _, _, _, err := (releaseReadinessProbe{source: fakeReleaseReadiness{err: errors.New("db down")}}).Probe(context.Background(), "tn_a", time.Now()); err == nil {
		t.Fatal("an unreadable release readiness must be an error, not a pass")
	}
}
