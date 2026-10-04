package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ReadFederationIdentity reads both sides of the issuer+subject mapping in one
// PostgreSQL statement/snapshot. No rows are inserted and no provider_type,
// email or organisation field participates in canonical identity resolution.
func (r *PostgresRepository) ReadFederationIdentity(ctx context.Context, issuer, subject string) (FederationIdentity, error) {
	if r == nil || r.pool == nil {
		return FederationIdentity{}, errors.New("identity repository unavailable")
	}
	var result FederationIdentity
	p, e := &result.Principal, &result.ExternalIdentity
	err := r.pool.QueryRow(ctx, `
 SELECT p.principal_id::text, p.actor_type, p.status, p.created_at, p.updated_at,
        e.external_identity_id::text, e.principal_id::text, e.issuer, e.subject,
        COALESCE(e.provider_type,''), e.status, e.created_at, e.last_seen_at
 FROM identity.external_identity e
 JOIN identity.principal p ON p.principal_id=e.principal_id
 WHERE e.issuer=$1 AND e.subject=$2`, issuer, subject).Scan(
		&p.ID, &p.ActorType, &p.Status, &p.CreatedAt, &p.UpdatedAt,
		&e.ID, &e.PrincipalID, &e.Issuer, &e.Subject, &e.ProviderType, &e.Status, &e.CreatedAt, &e.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FederationIdentity{}, ErrIdentityNotFound
	}
	if err != nil {
		return FederationIdentity{}, err
	}
	return result, nil
}

var _ FederationIdentityReader = (*PostgresRepository)(nil)
var _ FederationIdentityReader = (*Repository)(nil)
