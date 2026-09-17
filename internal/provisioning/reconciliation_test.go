// Target path: internal/provisioning/reconciliation_test.go
package provisioning

import (
	"context"
	"testing"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

type resourceFake struct{ key string; drift []Drift }
func (f resourceFake) Key()string{return f.key}
func (f resourceFake) ReconcileResource(context.Context,provisioningdomain.TenantProvisioning)([]Drift,error){return f.drift,nil}

func TestReconciliationAdvancesObservedVersionOnlyOnFullConvergence(t *testing.T){
	r:=DesiredObservedReconciler{Resources:[]ResourceReconciler{
		resourceFake{key:"market-participation"},
		resourceFake{key:"capability-grants"},
	}}
	report,err:=r.Report(context.Background(),provisioningdomain.TenantProvisioning{
		DesiredStateVersion:7,ObservedStateVersion:6,
	})
	if err!=nil{t.Fatal(err)}
	if !report.Converged || report.ObservedStateVersion!=7 { t.Fatalf("%+v",report) }
}

func TestReconciliationPreservesObservedVersionOnDrift(t *testing.T){
	r:=DesiredObservedReconciler{Resources:[]ResourceReconciler{
		resourceFake{key:"bindings",drift:[]Drift{{ResourceType:"binding",ResourceKey:"commerce.order",Kind:DriftMismatch,Reason:"wrong instance"}}},
	}}
	report,err:=r.Report(context.Background(),provisioningdomain.TenantProvisioning{
		DesiredStateVersion:7,ObservedStateVersion:6,
	})
	if err!=nil{t.Fatal(err)}
	if report.Converged || report.ObservedStateVersion!=6 { t.Fatalf("%+v",report) }
}
