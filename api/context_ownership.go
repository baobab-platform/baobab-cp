package api

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// callerOwnsContext is the single caller-binding rule for a stored Context
// (Shared control-plane/v1 1.33.1, design context-authority-for-workloads):
// a context_id is a handle, not a bearer credential, so it may only be
// consumed by the principal that resolved it. The authenticated caller's
// canonical principal must equal Context.PrincipalID.
//
// The canonical principal is looked up, never created: a caller that has no
// canonical principal cannot own anything, and consuming a context must not
// provision identities as a side effect. A lookup that fails for any reason
// other than "no such identity" is returned so the caller can answer 503
// rather than guess.
//
// Every consumer of a stored Context (capability resolution, batch
// resolution, mapping resolution, context validation) calls this function;
// none re-implements the comparison.
func callerOwnsContext(ctx context.Context, identities repository.IdentityRepository, principal auth.Principal, stored domain.Context) (bool, error) {
	if identities == nil {
		return false, errors.New("identity lookup is not configured")
	}
	return issuerSubjectOwnsContext(ctx, identities, principal.Issuer, principal.Subject, stored)
}

// issuerSubjectOwnsContext is callerOwnsContext for a verified (issuer,
// subject) pair that is not the request's own principal -- the subject of a
// forwarded token, verified separately.
func issuerSubjectOwnsContext(ctx context.Context, identities repository.IdentityRepository, issuer, subject string, stored domain.Context) (bool, error) {
	_, owned, err := canonicalOwner(ctx, identities, issuer, subject, stored)
	return owned, err
}

// canonicalOwner is issuerSubjectOwnsContext that also returns the canonical
// principal id it resolved ("" when there is none), for audit.
func canonicalOwner(ctx context.Context, identities repository.IdentityRepository, issuer, subject string, stored domain.Context) (principalID string, owned bool, err error) {
	if issuer == "" || subject == "" || stored.PrincipalID == "" {
		return "", false, nil
	}
	resolved, err := identities.ResolveIdentity(ctx, issuer, subject)
	if errors.Is(err, repository.ErrIdentityNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return resolved.ID, resolved.ID == stored.PrincipalID, nil
}
