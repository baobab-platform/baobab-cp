package repository

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// FederationIdentity is a read projection of existing identity records, not a
// second canonical identity or a new public federation approval contract.
// Generic mapping.canonical_mapping belongs to business CanonicalEntity;
// it must not be substituted for identity.external_identity's actor mapping.
type FederationIdentity struct {
	Principal        domain.Principal
	ExternalIdentity domain.ExternalIdentity
}

// FederationIdentityReader is deliberately read-only. It cannot provision,
// link, merge or assign any human, tenancy, role or business permission.
type FederationIdentityReader interface {
	ReadFederationIdentity(context.Context, string, string) (FederationIdentity, error)
}

func (r *Repository) ReadFederationIdentity(ctx context.Context, issuer, subject string) (FederationIdentity, error) {
	if r == nil {
		return FederationIdentity{}, errors.New("identity repository unavailable")
	}
	if err := ctx.Err(); err != nil {
		return FederationIdentity{}, err
	}
	external, ok := r.ExternalIdentities[externalIdentityKey(issuer, subject)]
	if !ok {
		return FederationIdentity{}, ErrIdentityNotFound
	}
	principal, ok := r.Principals[external.PrincipalID]
	if !ok {
		return FederationIdentity{}, errors.New("identity mapping inconsistent")
	}
	return FederationIdentity{principal, external}, nil
}
