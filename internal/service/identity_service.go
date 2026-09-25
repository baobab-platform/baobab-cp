package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// ErrProvisioningNotAllowed is returned by IdentityService.Resolve when no
// Principal exists for the given (issuer, subject) and the configured
// ProvisioningPolicy declines to create one.
var ErrProvisioningNotAllowed = errors.New("identity provisioning is not allowed for this actor type")

// ProvisioningPolicy decides whether a first authentication for the given
// actor type may automatically create a new Principal (ADR-0004 §12,
// "First Authentication"). ADR-0004 §13 ("Provisioning Policy by Actor")
// deliberately defers the actual rules to later domain-specific ADRs that
// don't exist yet for humans (Zuribeans, Thamani, Supplier), and ADR-0007
// §46 describes workload provisioning as an explicit, out-of-band
// registration flow ("new service approved -> register workload -> create
// IAM client -> ... -> create canonical workload mapping"), not self-service
// on first authentication. IdentityService therefore takes no position on
// which actor types get auto-provisioned; callers supply the policy.
type ProvisioningPolicy func(actorType string) bool

// IdentityService implements ADR-0004 §11/§12's resolution-then-provisioning
// flow: resolve (issuer, subject) to a Principal, and on first
// authentication -- no ExternalIdentity found -- create one if policy
// allows. Concurrent first authentications for the same (issuer, subject)
// are handled per §56 ("Identity Provisioning Race"): the request that
// loses the race on external_identity's UNIQUE(issuer, subject) constraint
// re-reads and returns the winner's Principal rather than failing.
type IdentityService struct {
	Repository repository.IdentityRepository
	// Provision decides whether an unknown (issuer, subject) may be
	// auto-provisioned. A nil Provision denies provisioning for every actor
	// type (fail closed), matching ADR-0004 §13's "automatic provisioning
	// SHOULD not be universal."
	Provision ProvisioningPolicy
}

// WorkloadOnlyProvisioningPolicy allows automatic Principal provisioning
// only for workload actors, denying every other actor type. It is meant for
// call sites reached only after OIDC verification plus required-scope and
// tenant/client checks already passed -- api.ResolverHandler's /v1/resolve
// is the one call site as of Gate IAM-3 phase 4. ADR-0007 §46 describes
// workload onboarding (IAM client registration, scope assignment,
// credential provisioning) as happening before a workload's first token is
// ever issued, so a request reaching this policy is already
// controlled-provisioned at the IAM layer; this just materializes
// baobab-cp's own identity.principal/external_identity record to match.
// Human actors are always denied here: ADR-0004 §13 defers human
// provisioning policy to domain-specific ADRs that don't exist yet.
func WorkloadOnlyProvisioningPolicy(actorType string) bool { return actorType == "workload" }

// ApplicantProvisioningPolicy allows automatic Principal provisioning only
// for human actors. ADR-BCP-017 section 5 is the domain ADR ADR-0004 §13
// defers to for applicants: registering to apply establishes an
// authenticated applicant principal and nothing else -- no tenant,
// membership, role or capability. It is used only on the applicant routes
// (application:read/application:write), after OIDC verification and the
// scope check.
func ApplicantProvisioningPolicy(actorType string) bool { return actorType == "human" }

// Resolve returns the Principal for (issuer, subject), provisioning one on
// first authentication if actorType's policy allows it.
func (s IdentityService) Resolve(ctx context.Context, issuer, subject, actorType string) (domain.Principal, error) {
	if s.Repository == nil {
		return domain.Principal{}, errors.New("identity repository is required")
	}
	if issuer == "" || subject == "" {
		return domain.Principal{}, errors.New("issuer and subject are required")
	}

	principal, err := s.Repository.ResolveIdentity(ctx, issuer, subject)
	if err == nil {
		return principal, nil
	}
	if !errors.Is(err, repository.ErrIdentityNotFound) {
		return domain.Principal{}, fmt.Errorf("resolve identity: %w", err)
	}

	if s.Provision == nil || !s.Provision(actorType) {
		return domain.Principal{}, ErrProvisioningNotAllowed
	}

	principal = domain.Principal{ID: domain.NewPrincipalID(), ActorType: actorType, Status: "ACTIVE"}
	if err := s.Repository.CreateIdentity(ctx, principal); err != nil {
		return domain.Principal{}, fmt.Errorf("create identity: %w", err)
	}

	external := domain.ExternalIdentity{
		ID:          domain.NewExternalIdentityID(),
		PrincipalID: principal.ID,
		Issuer:      issuer,
		Subject:     subject,
		Status:      "ACTIVE",
	}
	if err := s.Repository.LinkExternalIdentity(ctx, external); err != nil {
		if errors.Is(err, repository.ErrExternalIdentityAlreadyLinked) {
			// ADR-0004 §56: someone else provisioned this (issuer, subject)
			// concurrently -- re-read and return the winner's Principal
			// instead of failing. The Principal created above is left
			// unlinked; that is a cosmetic loss, not a correctness one --
			// no two Principals ever resolve for the same (issuer,
			// subject) pair.
			return s.Repository.ResolveIdentity(ctx, issuer, subject)
		}
		return domain.Principal{}, fmt.Errorf("link external identity: %w", err)
	}
	return principal, nil
}
