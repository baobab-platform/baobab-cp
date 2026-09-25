// Target path: internal/provisioning/readiness_test.go
package provisioning

import (
	"context"
	"testing"
	"time"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
)

type readinessCheckFake struct {
	key    string
	status ReadinessStatus
	reason string
}

func (f readinessCheckFake) Key() string { return f.key }
func (f readinessCheckFake) Evaluate(context.Context, provisioningdomain.TenantProvisioning) (ReadinessEvidence, error) {
	return ReadinessEvidence{Status: f.status, Reason: f.reason, Reference: "test"}, nil
}

func TestReadinessRequiresAllChecksAndVersionConvergence(t *testing.T) {
	r := NewReadinessEvaluator(
		readinessCheckFake{key: "market-participation", status: ReadinessPass},
		readinessCheckFake{key: "capability-grants", status: ReadinessPass},
	)
	r.now = func() time.Time { return time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC) }

	report, err := r.Report(context.Background(), provisioningdomain.TenantProvisioning{
		ID: "op", TenantID: "tn_zuri", DesiredStateVersion: 4, ObservedStateVersion: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report.BlockingReasons)
	}
}

func TestReadinessFailsClosedOnDrift(t *testing.T) {
	r := NewReadinessEvaluator(readinessCheckFake{key: "market-participation", status: ReadinessPass})
	report, err := r.Report(context.Background(), provisioningdomain.TenantProvisioning{
		ID: "op", TenantID: "tn_zuri", DesiredStateVersion: 4, ObservedStateVersion: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready {
		t.Fatal("drift must block readiness")
	}
}

func TestReadinessFailsClosedOnFailedControl(t *testing.T) {
	r := NewReadinessEvaluator(readinessCheckFake{
		key: "engine-instances", status: ReadinessFail, reason: "no healthy eligible engine instance",
	})
	report, err := r.Report(context.Background(), provisioningdomain.TenantProvisioning{
		ID: "op", TenantID: "tn_zuri", DesiredStateVersion: 4, ObservedStateVersion: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready || len(report.BlockingReasons) == 0 {
		t.Fatal("failed control must block readiness")
	}
}
