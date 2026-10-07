package erpprovisioning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ErrProvisionerIdentityNotRegistered means the provisioner workload has no
// canonical principal. A Context must be owned by that principal (Shared
// control-plane/v1 1.33.1: a context_id is a handle, consumable only by the
// principal that resolved it), and issuing one must not provision identities
// as a side effect, so the worker stops until the identity is registered.
var ErrProvisionerIdentityNotRegistered = errors.New("the provisioner workload has no canonical principal")

// ContextIssuer creates the trusted Context ERP validates, internally and
// under the provisioner workload's own canonical principal.
//
// The Context is a TENANT_PROVISIONING context (Shared control-plane/v1 1.34.0;
// docs/architecture/context-authority-for-workloads.md section 13): the tenant
// is not ACTIVE while it is provisioned (activation follows readiness,
// ADR-BCP-017 sections 22 and 45), so an ordinary RUNTIME context could never be
// validated. It is bound to the approved plan tuple, carries no business
// dimension, lives at most 15 minutes, and is never redeemable as runtime
// authority. Whether it is still usable is judged by the Control Plane at
// validation time, not recorded here.
//
// It deliberately does not call POST /v1/platform-context: that is the
// context:resolve audience of tenant-bound runtime callers, which the
// provisioner is never granted. The Control Plane is already the authority for
// the tenant being provisioned, so it records the Context directly.
//
// Issuer and Subject are the iss and sub of the token the provisioner sends to
// ERP (iss = Baobab IAM, sub = baobab-cp-provisioning-workload). ERP forwards
// that token to the validation endpoint, which resolves the same (issuer,
// subject) to the canonical principal and compares it with Context.PrincipalID.
type ContextIssuer struct {
	Identities repository.IdentityRepository
	Contexts   repository.ContextWriter
	Issuer     string
	Subject    string
	// TTL bounds the Context. It must be positive and at most
	// domain.MaxProvisioningContextLifetime: a handle that ERP will present to
	// Control Plane has no reason to be unbounded, and an unbounded Context is
	// answered 404 by validation.
	TTL   time.Duration
	Now   func() time.Time
	NewID func() string
}

// Issue records a TENANT_PROVISIONING Context for tenantID, bound to the approved
// plan authority and owned by the provisioner principal, and returns it.
func (i ContextIssuer) Issue(ctx context.Context, tenantID string, authority Authority, correlationID string) (domain.Context, error) {
	if i.Identities == nil || i.Contexts == nil || i.Issuer == "" || i.Subject == "" || i.TTL <= 0 || i.TTL > domain.MaxProvisioningContextLifetime {
		return domain.Context{}, errors.New("context issuer is not configured")
	}
	principal, err := i.Identities.ResolveIdentity(ctx, i.Issuer, i.Subject)
	if errors.Is(err, repository.ErrIdentityNotFound) {
		return domain.Context{}, ErrProvisionerIdentityNotRegistered
	}
	if err != nil {
		return domain.Context{}, fmt.Errorf("resolve provisioner principal: %w", err)
	}
	now := time.Now().UTC()
	if i.Now != nil {
		now = i.Now().UTC()
	}
	newID := i.NewID
	if newID == nil {
		newID = domain.NewUUIDv7
	}
	expires := now.Add(i.TTL)
	resolved := domain.Context{
		ID:            newID(),
		PrincipalID:   principal.ID,
		TenantID:      tenantID,
		CorrelationID: correlationID,
		ResolvedAt:    now,
		ExpiresAt:     &expires,
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "tenant_provisioning", TrustLevel: domain.TrustSystem,
				Evidence: "control plane provisioning " + authority.TenantProvisioningID},
		},
		AuthorityPurpose: domain.ContextPurposeTenantProvisioning,
		ProvisioningAuthority: &domain.ProvisioningAuthority{TenantProvisioningID: authority.TenantProvisioningID,
			PlanID: authority.PlanID, PlanVersion: authority.PlanVersion, PlanDigest: authority.PlanDigest},
	}
	if err := i.Contexts.CreateContext(ctx, resolved); err != nil {
		return domain.Context{}, fmt.Errorf("record provisioning context: %w", err)
	}
	return resolved, nil
}
