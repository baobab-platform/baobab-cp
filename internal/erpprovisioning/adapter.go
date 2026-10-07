package erpprovisioning

import (
	"context"
	"errors"
	"fmt"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ReadyState is the ERP state in which a provisioning is complete.
const ReadyState = "active"

// ErrNoSubmission means nothing has been requested from ERP for the tenant.
var ErrNoSubmission = errors.New("no ERP provisioning has been requested for the tenant")

// LatestLedger is the ledger read readiness needs.
type LatestLedger interface {
	LatestForTenant(ctx context.Context, tenantID string) (Submission, bool, error)
}

// ProviderPhase moves a provisioning into the canonical state in which provider
// provisioning executes (PROVISIONING_PROVIDERS), so the provisioning context the
// worker issues is admissible: the Control Plane honours it only while the
// provisioning is in that state, or verifying or remediating it.
type ProviderPhase interface {
	EnterProviderProvisioning(ctx context.Context, provisioningID string) error
}

// Provisioner adapts the worker to the provisioning pipeline: the APPLY step
// requests ERP provisioning for the approved plan, and readiness asks whether
// ERP has reported it complete. ERP's 202 is "accepted", so APPLY finishing
// never makes a tenant ready.
type Provisioner struct {
	Worker Worker
	Phase  ProviderPhase
	Latest LatestLedger
}

// Submit requests ERP provisioning for the provisioning named by its contract
// id. A repeat returns ERP's prior operation, so APPLY is safe to resume.
func (p Provisioner) Submit(ctx context.Context, tenantProvisioningID string) error {
	if p.Phase == nil {
		return errors.New("provider provisioning phase is not configured")
	}
	id, err := repository.ProvisioningUUID(tenantProvisioningID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotAuthorised, err)
	}
	// Admissibility first: a context issued in any other state would be refused by
	// the Control Plane when ERP validates it.
	if err := p.Phase.EnterProviderProvisioning(ctx, id); err != nil {
		return fmt.Errorf("enter provider provisioning: %w", err)
	}
	_, err = p.Worker.Submit(ctx, tenantProvisioningID)
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
