package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
	"github.com/baobab-platform/baobab-cp/internal/store"
)

type erpFake struct {
	submitted []string
	submitErr error
	ready     bool
	reason    string
	readyErr  error
}

func (f *erpFake) Submit(_ context.Context, id string) error {
	f.submitted = append(f.submitted, id)
	return f.submitErr
}
func (f *erpFake) Ready(context.Context, string) (bool, string, error) {
	return f.ready, f.reason, f.readyErr
}

func TestERPApplyStepRequestsProvisioningByContractID(t *testing.T) {
	f := &erpFake{}
	op := provisioningdomain.TenantProvisioning{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b", TenantID: "tn_a"}
	if err := (erpProvisioningApplyStep{erp: f}).Apply(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if len(f.submitted) != 1 || f.submitted[0] != "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b" {
		t.Fatalf("submitted %v", f.submitted)
	}
}

func TestERPApplyStepFailsClosed(t *testing.T) {
	op := provisioningdomain.TenantProvisioning{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b"}
	if err := (erpProvisioningApplyStep{}).Apply(context.Background(), op); err == nil {
		t.Fatal("an unconfigured step must not pass")
	}
	if err := (erpProvisioningApplyStep{erp: &erpFake{}}).Apply(context.Background(), provisioningdomain.TenantProvisioning{ID: "legacy"}); err == nil {
		t.Fatal("a provisioning with no contract id must not be submitted")
	}
	boom := errors.New("ERP refused")
	if err := (erpProvisioningApplyStep{erp: &erpFake{submitErr: boom}}).Apply(context.Background(), op); !errors.Is(err, boom) {
		t.Fatalf("APPLY must fail when ERP provisioning cannot be requested: %v", err)
	}
}

func TestERPReadinessHoldsTheTenantUntilERPIsComplete(t *testing.T) {
	ok, _, reason, err := erpProvisioningProbe{erp: &erpFake{reason: "ERP reports the provisioning accepted"}}.Probe(context.Background(), "tn_a", time.Now())
	if err != nil || ok || !strings.Contains(reason, "not complete") {
		t.Fatalf("ok=%v reason=%q err=%v", ok, reason, err)
	}
	ok, ref, _, err := erpProvisioningProbe{erp: &erpFake{ready: true, reason: "active"}}.Probe(context.Background(), "tn_a", time.Now())
	if err != nil || !ok || ref == "" {
		t.Fatalf("ok=%v ref=%q err=%v", ok, ref, err)
	}
	if _, _, _, err := (erpProvisioningProbe{erp: &erpFake{readyErr: errors.New("db")}}).Probe(context.Background(), "tn_a", time.Now()); err == nil {
		t.Fatal("a ledger failure must surface")
	}
}

type pipelineRepo struct{ ZB02Repository }
type pipelineTenants struct{ store.TenantStore }
type pipelineStore struct{ TenantProvisioningStore }

func buildPipeline(t *testing.T, erp ERPProvisioning) *Orchestrator {
	t.Helper()
	o, err := BuildZB02Pipeline(ZB02Dependencies{Tenants: pipelineTenants{}, Repo: pipelineRepo{}, Provisioning: pipelineStore{}, ERP: erp},
		ResolvedManifest{}, "scope")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func stepKeys(o *Orchestrator) string {
	var keys []string
	for _, s := range o.apply.(ApplyWorker).Steps {
		keys = append(keys, s.Key())
	}
	return strings.Join(keys, ",")
}

func TestPipelineIncludesERPProvisioningOnlyWhenConfigured(t *testing.T) {
	if got := stepKeys(buildPipeline(t, nil)); strings.Contains(got, "erp-provisioning") {
		t.Fatalf("an unconfigured pipeline must not call ERP: %s", got)
	}
	if got := stepKeys(buildPipeline(t, &erpFake{})); !strings.HasSuffix(got, "trade-lane,erp-provisioning") {
		t.Fatalf("ERP provisioning follows the bindings it depends on: %s", got)
	}
}

func TestPipelineReadinessWaitsForERPWhenConfigured(t *testing.T) {
	op := provisioningdomain.TenantProvisioning{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b", TenantID: "tn_a"}
	has := func(erp ERPProvisioning) bool {
		o := buildPipeline(t, erp)
		report, _ := o.readiness.(ReadinessWorker).Evaluator.(*ReadinessEvaluatorImpl).Report(context.Background(), op)
		for _, e := range report.Evidence {
			if e.Check == ERPProvisioningCheckKey {
				return true
			}
		}
		return false
	}
	if has(nil) {
		t.Fatal("no ERP check without ERP provisioning")
	}
	if !has(&erpFake{reason: "accepted"}) {
		t.Fatal("ERP provisioning must be a readiness check")
	}
}
