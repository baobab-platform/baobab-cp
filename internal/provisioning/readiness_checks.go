// Target path: internal/provisioning/readiness_checks.go
package provisioning

import (
	"context"
	"errors"
	"time"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
)

// ResourceReadinessProbe lets existing repositories/resolvers prove one
// required invariant without coupling the evaluator to persistence details.
type ResourceReadinessProbe interface {
	Probe(ctx context.Context, tenantID string, at time.Time) (ok bool, reference string, reason string, err error)
}

type ProbeCheck struct {
	CheckKey string
	Probe    ResourceReadinessProbe
	now      func() time.Time
}

func NewProbeCheck(key string, probe ResourceReadinessProbe) ProbeCheck {
	return ProbeCheck{CheckKey: key, Probe: probe, now: func() time.Time { return time.Now().UTC() }}
}

func (c ProbeCheck) Key() string { return c.CheckKey }

func (c ProbeCheck) Evaluate(ctx context.Context, op provisioningdomain.TenantProvisioning) (ReadinessEvidence, error) {
	if c.Probe == nil {
		return ReadinessEvidence{}, errors.New("probe is required")
	}
	ok, ref, reason, err := c.Probe.Probe(ctx, op.TenantID, c.now())
	if err != nil {
		return ReadinessEvidence{}, err
	}
	status := ReadinessFail
	if ok {
		status = ReadinessPass
	}
	return ReadinessEvidence{
		Status: status, Reason: reason, Reference: ref, ObservedAt: c.now(),
	}, nil
}

// ZB02RequiredReadinessChecks is the minimum Release-1 control set.
// Concrete probes must query authoritative CP state, not UI declarations.
var ZB02RequiredReadinessChecks = []string{
	"market-participation",
	"capability-grants",
	"capability-bindings",
	"engine-instances",
	"context-resolution",
	"trade-lanes",
	"isolation-and-residency",
}
