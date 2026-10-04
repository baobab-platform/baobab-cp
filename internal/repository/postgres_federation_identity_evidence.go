package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) ReadFederationIdentityEvidence(ctx context.Context, issuer, subject, environment, instance string) (FederationIdentityEvidence, error) {
	if r == nil || r.pool == nil {
		return FederationIdentityEvidence{}, errors.New("identity evidence repository unavailable")
	}
	var result FederationIdentityEvidence
	p, e, ref := &result.Identity.Principal, &result.Identity.ExternalIdentity, &result.Reference
	err := r.pool.QueryRow(ctx, `
 SELECT p.principal_id::text,p.actor_type,p.status,p.created_at,p.updated_at,
        e.external_identity_id::text,e.principal_id::text,e.issuer,e.subject,e.status,e.created_at,e.last_seen_at,
        x.external_reference_id,x.system_namespace,x.engine_id,x.engine_instance_id,x.environment,
        x.native_entity_type,x.native_id,x.source_authority,COALESCE(x.fingerprint,''),x.status,x.last_verified_at
 FROM identity.external_identity e
 JOIN identity.principal p ON p.principal_id=e.principal_id
 JOIN mapping.external_reference x ON x.native_id=e.external_identity_id::text
      AND x.system_namespace='baobab_cp' AND x.engine_id='baobab-cp'
      AND x.native_entity_type='canonical_identity_mapping'
      AND x.environment=$3 AND x.engine_instance_id=$4
 JOIN topology.engine_instance i ON i.engine_instance_key=x.engine_instance_id
 JOIN topology.engine g ON g.engine_id=i.engine_id
 WHERE e.issuer=$1 AND e.subject=$2 AND i.status='ACTIVE' AND i.environment=$3 AND g.code='baobab-cp'`, issuer, subject, environment, instance).Scan(
		&p.ID, &p.ActorType, &p.Status, &p.CreatedAt, &p.UpdatedAt,
		&e.ID, &e.PrincipalID, &e.Issuer, &e.Subject, &e.Status, &e.CreatedAt, &e.LastSeenAt,
		&ref.ID, &ref.SystemNamespace, &ref.EngineID, &ref.EngineInstanceID, &ref.Environment,
		&ref.NativeEntityType, &ref.NativeID, &ref.SourceAuthority, &ref.Fingerprint, &ref.Status, &ref.LastVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FederationIdentityEvidence{}, ErrIdentityNotFound
	}
	if err != nil {
		return FederationIdentityEvidence{}, err
	}
	return result, nil
}

var _ FederationIdentityEvidenceReader = (*PostgresRepository)(nil)
