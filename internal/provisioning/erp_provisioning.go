package provisioning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
)

// ERPProvisioningCheckKey is the readiness check that holds a tenant back until
// ERP reports its provisioning complete.
const ERPProvisioningCheckKey = "erp-provisioning"

// ERPProvisioning is the pipeline's view of requesting provider provisioning
// from ERP (Shared erp/v1) as the dedicated provisioner workload.
//
// It is optional. Without it the pipeline is unchanged, which keeps the
// provisioner (still PROVISIONED in Shared's registry) from being exercised
// before its end-to-end evidence exists.
type ERPProvisioning interface {
	// Submit requests ERP provisioning for the provisioning named by its contract
	// id (tp_...). ERP answers "accepted"; it is idempotent, so resuming APPLY
	// is safe.
	Submit(ctx context.Context, tenantProvisioningID string) error
	// Ready reports whether ERP has reported the tenant's provisioning complete.
	Ready(ctx context.Context, tenantID string) (ok bool, reason string, err error)
}

type erpProvisioningApplyStep struct{ erp ERPProvisioning }

func (erpProvisioningApplyStep) Key() string { return "erp-provisioning" }
func (s erpProvisioningApplyStep) Apply(ctx context.Context, op provisioningdomain.TenantProvisioning) error {
	if s.erp == nil {
		return errors.New("ERP provisioning is not configured")
	}
	key, err := domain.FormatResourceID("tp", op.ID)
	if err != nil {
		return err
	}
	if err := s.erp.Submit(ctx, key); err != nil {
		return fmt.Errorf("request ERP provisioning: %w", err)
	}
	return nil
}

type erpProvisioningProbe struct{ erp ERPProvisioning }

func (p erpProvisioningProbe) Probe(ctx context.Context, tenantID string, _ time.Time) (bool, string, string, error) {
	ok, reason, err := p.erp.Ready(ctx, tenantID)
	if err != nil {
		return false, "", "", err
	}
	if ok {
		return true, reason, "", nil
	}
	return false, "", "ERP provisioning is not complete: " + reason, nil
}
