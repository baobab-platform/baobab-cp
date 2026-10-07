package erpprovisioning

import (
	"context"
	"errors"
)

// ReadyState is the ERP state in which a provisioning is complete.
const ReadyState = "active"

// ErrNoSubmission means nothing has been requested from ERP for the tenant.
var ErrNoSubmission = errors.New("no ERP provisioning has been requested for the tenant")

// LatestLedger is the ledger read readiness needs.
type LatestLedger interface {
	LatestForTenant(ctx context.Context, tenantID string) (Submission, bool, error)
}

// Provisioner adapts the worker to the provisioning pipeline: the APPLY step
// requests ERP provisioning for the approved plan, and readiness asks whether
// ERP has reported it complete. ERP's 202 is "accepted", so APPLY finishing
// never makes a tenant ready.
type Provisioner struct {
	Worker Worker
	Latest LatestLedger
}

// Submit requests ERP provisioning for the provisioning named by its contract
// id. A repeat returns ERP's prior operation, so APPLY is safe to resume.
func (p Provisioner) Submit(ctx context.Context, tenantProvisioningID string) error {
	_, err := p.Worker.Submit(ctx, tenantProvisioningID)
	return err
}

// Ready reports whether ERP has reported the tenant's latest operation active;
// otherwise it says why not.
func (p Provisioner) Ready(ctx context.Context, tenantID string) (bool, string, error) {
	sub, found, err := p.Latest.LatestForTenant(ctx, tenantID)
	if err != nil {
		return false, "", err
	}
	switch {
	case !found:
		return false, ErrNoSubmission.Error(), nil
	case sub.LastState == ReadyState:
		return true, "ERP reports the provisioning active (operation " + sub.OperationID + ")", nil
	default:
		return false, "ERP reports the provisioning " + sub.LastState + " (operation " + sub.OperationID + ")", nil
	}
}
