// Target path: internal/provisioning/orchestrator_test.go
package provisioning

import (
	"context"
	"testing"
	"time"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

type opStoreFake struct{ op provisioningdomain.TenantProvisioning }
func (f *opStoreFake) GetTenantProvisioning(context.Context,string)(provisioningdomain.TenantProvisioning,error){ return f.op,nil }
func (f *opStoreFake) UpdateTenantProvisioning(_ context.Context,n provisioningdomain.TenantProvisioning, expected int64) error {
	if f.op.Version!=expected { panic("optimistic version mismatch") }
	f.op=n; return nil
}
type workerFake struct{ name string; result PhaseResult; err error }
func (w workerFake) Name()string{return w.name}
func (w workerFake) Run(context.Context,provisioningdomain.TenantProvisioning)(PhaseResult,error){return w.result,w.err}

func TestOrchestratorActivatesOnlyAfterConvergence(t *testing.T){
	s:=&opStoreFake{op:provisioningdomain.TenantProvisioning{
		ID:"op",TenantID:"tn_zuri",IdempotencyKey:"k",RequestHash:"h",
		Status:provisioningdomain.ProvisioningStatusPlan,
		DesiredStateVersion:3,ObservedStateVersion:0,StartedAt:time.Now().UTC(),Version:1,
	}}
	o:=NewOrchestrator(s,
		workerFake{name:"APPLY",result:PhaseResult{}},
		workerFake{name:"RECONCILE",result:PhaseResult{ObservedStateVersion:3}},
		workerFake{name:"READY",result:PhaseResult{ObservedStateVersion:3,Evidence:map[string]string{"probe":"ok"}}},
	)
	got,err:=o.Run(context.Background(),"op")
	if err!=nil{t.Fatal(err)}
	if got.Status!=provisioningdomain.ProvisioningStatusActive{t.Fatalf("got %s",got.Status)}
	if got.CompletedAt==nil{t.Fatal("ACTIVE must have completed_at")}
}

func TestOrchestratorStopsAtReconcileWhenDriftExists(t *testing.T){
	s:=&opStoreFake{op:provisioningdomain.TenantProvisioning{
		ID:"op",TenantID:"tn_zuri",IdempotencyKey:"k",RequestHash:"h",
		Status:provisioningdomain.ProvisioningStatusReconcile,
		DesiredStateVersion:3,ObservedStateVersion:2,StartedAt:time.Now().UTC(),Version:1,
	}}
	o:=NewOrchestrator(s,workerFake{name:"APPLY"},
		workerFake{name:"RECONCILE",result:PhaseResult{ObservedStateVersion:2,BlockingReasons:[]string{"binding drift"}}},
		workerFake{name:"READY"})
	got,err:=o.Run(context.Background(),"op")
	if err!=nil{t.Fatal(err)}
	if got.Status!=provisioningdomain.ProvisioningStatusReconcile{t.Fatalf("got %s",got.Status)}
}
