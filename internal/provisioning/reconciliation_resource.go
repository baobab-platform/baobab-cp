// Target path: internal/provisioning/reconciliation_resource.go
package provisioning

import (
	"context"

	provisioningdomain "github.com/nabhold/baobab-cp/internal/provisioning/domain"
)

// DesiredResourceReader returns canonical desired-state hashes for one
// resource family. ObservedResourceReader must independently read durable CP
// state/provider state; it must not echo the desired declaration.
type DesiredResourceReader interface {
	Desired(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]ResourceObservation,error)
}
type ObservedResourceReader interface {
	Observed(ctx context.Context, op provisioningdomain.TenantProvisioning) ([]ResourceObservation,error)
}

type HashResourceReconciler struct {
	ResourceType string
	DesiredReader DesiredResourceReader
	ObservedReader ObservedResourceReader
	UnexpectedIsBlocker bool
}

func (r HashResourceReconciler) Key() string { return r.ResourceType }

func (r HashResourceReconciler) ReconcileResource(
	ctx context.Context,
	op provisioningdomain.TenantProvisioning,
) ([]Drift,error) {
	desired,err:=r.DesiredReader.Desired(ctx,op); if err!=nil{return nil,err}
	observed,err:=r.ObservedReader.Observed(ctx,op); if err!=nil{return nil,err}

	dm:=map[string]ResourceObservation{}
	om:=map[string]ResourceObservation{}
	for _,v:=range desired { dm[v.ResourceKey]=v }
	for _,v:=range observed { om[v.ResourceKey]=v }

	var drift []Drift
	for key,d:=range dm {
		o,ok:=om[key]
		if !ok || !o.Present {
			drift=append(drift,Drift{ResourceType:r.ResourceType,ResourceKey:key,Kind:DriftMissing,Reason:"desired resource is not observed",Repairable:true})
			continue
		}
		if d.DesiredHash!=o.ObservedHash {
			drift=append(drift,Drift{ResourceType:r.ResourceType,ResourceKey:key,Kind:DriftMismatch,Reason:"observed state differs from desired state",Repairable:true})
		}
	}
	if r.UnexpectedIsBlocker {
		for key,o:=range om {
			if !o.Present {continue}
			if _,ok:=dm[key];!ok {
				drift=append(drift,Drift{ResourceType:r.ResourceType,ResourceKey:key,Kind:DriftUnexpected,Reason:"unexpected observed resource requires explicit policy decision",Repairable:false})
			}
		}
	}
	return drift,nil
}
