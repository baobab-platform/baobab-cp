// Target path: internal/provisioning/readiness.go
//
// ZB-02 readiness evidence. READY/ACTIVE are earned states: desired and
// observed versions must converge and every required control must pass.
package provisioning

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

type ReadinessStatus string

const (
	ReadinessPass ReadinessStatus = "PASS"
	ReadinessFail ReadinessStatus = "FAIL"
)

type ReadinessEvidence struct {
	Check      string
	Status     ReadinessStatus
	Reason     string
	Reference  string
	ObservedAt time.Time
}

type ReadinessReport struct {
	TenantID             string
	ProvisioningID       string
	DesiredStateVersion  int64
	ObservedStateVersion int64
	Ready                bool
	BlockingReasons      []string
	Evidence             []ReadinessEvidence
	EvaluatedAt          time.Time
}

type ReadinessCheck interface {
	Key() string
	Evaluate(ctx context.Context, op provisioningdomain.TenantProvisioning) (ReadinessEvidence, error)
}

type ReadinessEvaluatorImpl struct {
	Checks []ReadinessCheck
	now    func() time.Time
}

func NewReadinessEvaluator(checks ...ReadinessCheck) *ReadinessEvaluatorImpl {
	return &ReadinessEvaluatorImpl{
		Checks: checks,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Evaluate satisfies the ReadinessEvaluator consumed by ReadinessWorker.
func (r *ReadinessEvaluatorImpl) Evaluate(
	ctx context.Context,
	op provisioningdomain.TenantProvisioning,
) (int64, []string, map[string]string, error) {
	report, err := r.Report(ctx, op)
	if err != nil {
		return op.ObservedStateVersion, nil, nil, err
	}
	evidence := make(map[string]string, len(report.Evidence))
	for _, item := range report.Evidence {
		evidence[item.Check] = item.Reference
	}
	return report.ObservedStateVersion, report.BlockingReasons, evidence, nil
}

func (r *ReadinessEvaluatorImpl) Report(
	ctx context.Context,
	op provisioningdomain.TenantProvisioning,
) (ReadinessReport, error) {
	if r == nil || len(r.Checks) == 0 {
		return ReadinessReport{}, errors.New("at least one readiness check is required")
	}
	report := ReadinessReport{
		TenantID:             op.TenantID,
		ProvisioningID:       op.ID,
		DesiredStateVersion:  op.DesiredStateVersion,
		ObservedStateVersion: op.ObservedStateVersion,
		EvaluatedAt:          r.now(),
	}

	seen := map[string]struct{}{}
	for _, check := range r.Checks {
		if check == nil || check.Key() == "" {
			return ReadinessReport{}, errors.New("readiness check key is required")
		}
		if _, exists := seen[check.Key()]; exists {
			return ReadinessReport{}, fmt.Errorf("duplicate readiness check %q", check.Key())
		}
		seen[check.Key()] = struct{}{}

		item, err := check.Evaluate(ctx, op)
		if err != nil {
			return ReadinessReport{}, fmt.Errorf("%s readiness check: %w", check.Key(), err)
		}
		item.Check = check.Key()
		if item.ObservedAt.IsZero() {
			item.ObservedAt = report.EvaluatedAt
		}
		report.Evidence = append(report.Evidence, item)
		if item.Status != ReadinessPass {
			reason := item.Reason
			if reason == "" {
				reason = check.Key() + " did not pass"
			}
			report.BlockingReasons = append(report.BlockingReasons, reason)
		}
	}

	if op.ObservedStateVersion != op.DesiredStateVersion {
		report.BlockingReasons = append(report.BlockingReasons,
			fmt.Sprintf("desired/observed state drift: desired=%d observed=%d",
				op.DesiredStateVersion, op.ObservedStateVersion))
	}
	sort.Strings(report.BlockingReasons)
	sort.Slice(report.Evidence, func(i, j int) bool {
		return report.Evidence[i].Check < report.Evidence[j].Check
	})
	report.Ready = len(report.BlockingReasons) == 0
	return report, nil
}
