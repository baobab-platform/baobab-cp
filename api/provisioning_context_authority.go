package api

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// admissibleProvisioningStates are the canonical TenantProvisioning states in which provider provisioning executes or is
// verified (Shared tenant-provisioning-lifecycle.yaml; docs/architecture/context-authority-for-workloads.md section 13.2 P5).
// A TENANT_PROVISIONING context is authority only while its provisioning is in one of them: the authority ends when the
// provisioning leaves them, even though the context has not expired.
var admissibleProvisioningStates = map[string]struct{}{
	"PROVISIONING_PROVIDERS": {}, "VERIFYING_READINESS": {}, "REMEDIATING": {},
}

// admissibleTenantStates are the tenant lifecycle states a provisioning context may act for (judged by the validator). A pending tenant is
// admissible by design (provisioning is evidence for activation, ADR-BCP-017 sections 22 and 45); a suspended,
// decommissioning or decommissioned one is not (ADR-BCP-009 section 64: tenant suspension denies tenant-scoped access),
// and an unknown state fails closed.
var admissibleTenantStates = map[string]struct{}{
	"pending": {}, string(domain.LifecycleProvisioning): {}, string(domain.LifecycleActive): {},
}

// provisioningAuthorityState is the Control Plane's answer about a provisioning context.
type provisioningAuthorityState int

const (
	provisioningAuthorityCurrent provisioningAuthorityState = iota
	provisioningAuthorityNotCurrent
)

// provisioningAuthority judges a stored TENANT_PROVISIONING context against authoritative current state, every time it
// is presented (ADR-BCP-009 section 61: revocation must not depend on events or on the row; ADR-BCP-004 section 71: the
// context is never rewritten). The context is current only when ALL hold:
//
//   - the TenantProvisioning it names exists, belongs to the context's tenant, and is in an admissible canonical state;
//   - that provisioning has an approved, current plan (approvedPlanProblem, the single definition shared with the ERP
//     assignment projection) and its plan id, version and digest equal the context's tuple member by member.
//
// The tenant's own state is judged by the caller (a suspended tenant is TENANT_NOT_ACTIVE, not "authority not current").
// An error is the Control Plane failing to read a source, never an answer about the context.
type provisioningAuthority struct {
	Provisionings erpAssignmentSources
}

func (a provisioningAuthority) judge(ctx context.Context, stored domain.Context) (provisioningAuthorityState, string, error) {
	authority := stored.ProvisioningAuthority
	if stored.Purpose() != domain.ContextPurposeTenantProvisioning || authority == nil {
		return provisioningAuthorityNotCurrent, "not_a_provisioning_context", nil
	}
	id, err := repository.ProvisioningUUID(authority.TenantProvisioningID)
	if err != nil {
		return provisioningAuthorityNotCurrent, "provisioning_malformed", nil
	}
	c, err := a.Provisionings.GetConvergedProvisioning(ctx, id)
	switch {
	case errors.Is(err, repository.ErrProvisioningNotFound):
		return provisioningAuthorityNotCurrent, "provisioning_unknown", nil
	case err != nil:
		return 0, "", err
	}
	if c.TenantID != stored.TenantID || c.Key != authority.TenantProvisioningID {
		return provisioningAuthorityNotCurrent, "provisioning_of_another_tenant", nil
	}
	if _, ok := admissibleProvisioningStates[c.State]; !ok {
		return provisioningAuthorityNotCurrent, "provisioning_state_not_admissible", nil
	}
	desired, err := a.Provisionings.GetDesiredState(ctx, c.ID, c.DesiredStateVersion)
	switch {
	case errors.Is(err, repository.ErrProvisioningNotFound):
		return provisioningAuthorityNotCurrent, "desired_state_missing", nil
	case err != nil:
		return 0, "", err
	}
	if approvedPlanProblem(c, desired) != "" {
		return provisioningAuthorityNotCurrent, "no_approved_current_plan", nil
	}
	if c.Plan.PlanID != authority.PlanID || c.Plan.PlanVersion != authority.PlanVersion || c.Plan.PlanDigest != authority.PlanDigest {
		return provisioningAuthorityNotCurrent, "plan_superseded", nil
	}
	return provisioningAuthorityCurrent, "provisioning_authority_current", nil
}

// runtimeContexts is the view of the context store every RUNTIME consumer uses: capability resolution, batch resolution,
// mapping resolution and capability explanation. A TENANT_PROVISIONING context is never ordinary runtime authority
// (docs/architecture/context-authority-for-workloads.md section 13.2 P6), so to these consumers it does not exist: the
// answer is the same ErrContextNotFound an unknown or expired context gets, and nothing distinguishes the two. Only the
// validation endpoint reads the unfiltered store.
type runtimeContexts struct{ repository.ContextRepository }

// GetContext implements repository.ContextRepository.
func (r runtimeContexts) GetContext(ctx context.Context, contextID string) (domain.Context, error) {
	stored, err := r.ContextRepository.GetContext(ctx, contextID)
	if err != nil {
		return domain.Context{}, err
	}
	if !stored.IsRuntime() {
		return domain.Context{}, repository.ErrContextNotFound
	}
	return stored, nil
}

// runtimeOnly narrows a context store to RUNTIME contexts, keeping a nil store nil so callers' "not configured" checks
// still work.
func runtimeOnly(store repository.ContextRepository) repository.ContextRepository {
	if store == nil {
		return nil
	}
	return runtimeContexts{store}
}
