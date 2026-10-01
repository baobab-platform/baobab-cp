package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
)

const grantIDPrefix = "agr"

// AdministrativeGrantReader loads the grants administrative authority is
// evaluated from (ADR-BCP-020 gate ADA-02).
type AdministrativeGrantReader interface {
	// AdministrativeGrantsOf returns every grant of the principal, in any
	// state, and every grant their delegations rest on, by grant id.
	AdministrativeGrantsOf(ctx context.Context, principalID string) ([]administration.Grant, map[string]administration.Grant, error)
	// EffectiveRelations returns the canonical relations authority is judged
	// with for the tenants: the organisations with an effective
	// TenantOrganisationMapping to each at the time (ADR-BCP-018 section 50).
	EffectiveRelations(ctx context.Context, tenantIDs []string, at time.Time) (administration.Relations, error)
}

var _ AdministrativeGrantReader = (*PostgresRepository)(nil)

// CreateAdministrativeGrant records one grant with its audit event; see
// CreateAdministrativeGrants.
func (r *PostgresRepository) CreateAdministrativeGrant(ctx context.Context, g administration.Grant, actor AuditActor) error {
	return r.CreateAdministrativeGrants(ctx, []administration.Grant{g}, actor)
}

// CreateAdministrativeGrants records grants and their audit events in one
// transaction: all of them or none, so a profile never lands half-granted.
// Each grant must satisfy the catalogue (administration.Grant.Validate); a
// delegation must name an existing grant held by its grantor; and a
// BOOTSTRAP grant is refused while its principal still holds a live one for
// the same permission, so re-running a bootstrap never duplicates authority.
func (r *PostgresRepository) CreateAdministrativeGrants(ctx context.Context, grants []administration.Grant, actor AuditActor) error {
	if r == nil || r.pool == nil {
		return errors.New("repository is not initialized")
	}
	if err := validateActor(actor); err != nil {
		return err
	}
	catalogue, err := administration.DefaultCatalogue()
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, g := range grants {
		if err := insertGrant(ctx, tx, catalogue, g, actor); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertGrant(ctx context.Context, tx pgx.Tx, catalogue *administration.Catalogue, g administration.Grant, actor AuditActor) error {
	if err := g.Validate(catalogue); err != nil {
		return fmt.Errorf("validate administrative grant %s: %w", g.Permission, err)
	}
	rowID, err := domain.ParseResourceID(grantIDPrefix, g.GrantID)
	if err != nil {
		return err
	}
	var delegatedFrom any
	if g.DelegatedFromGrantID != "" {
		source, err := domain.ParseResourceID(grantIDPrefix, g.DelegatedFromGrantID)
		if err != nil {
			return err
		}
		var holder string
		err = tx.QueryRow(ctx, `SELECT principal_id FROM policy.administrative_grant WHERE grant_id = $1::uuid`, source).Scan(&holder)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && holder != g.GrantedBy) {
			return errors.New("a delegation names a grant its grantor holds")
		}
		if err != nil {
			return err
		}
		delegatedFrom = source
	}
	if g.Source == administration.SourceBootstrap {
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM policy.administrative_grant
			WHERE principal_id = $1 AND permission = $2 AND source = 'BOOTSTRAP' AND status IN ('PENDING', 'ACTIVE', 'SUSPENDED')
				AND valid_until > now())`, g.PrincipalID, g.Permission).Scan(&live); err != nil {
			return err
		}
		if live {
			return fmt.Errorf("%s already holds live bootstrap authority for %s", g.PrincipalID, g.Permission)
		}
	}
	var supersedes any
	if g.SupersedesGrantID != "" {
		row, err := domain.ParseResourceID(grantIDPrefix, g.SupersedesGrantID)
		if err != nil {
			return err
		}
		supersedes = row
	}
	scope, err := json.Marshal(g.Scope)
	if err != nil {
		return err
	}
	var conditions any
	if g.Conditions != nil {
		raw, err := json.Marshal(g.Conditions)
		if err != nil {
			return err
		}
		conditions = raw
	}
	created := g.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO policy.administrative_grant (grant_id, principal_id, permission, scope_level, scope, conditions,
			grant_type, source, profile_key, delegated_from, delegation_depth, delegable_depth, risk_class,
			valid_from, valid_until, status, granted_by, approval_reference, reason, created_at, version, supersedes_grant_id)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9, $10::uuid, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, 1, $21::uuid)`,
		rowID, g.PrincipalID, g.Permission, string(g.Scope.Level), scope, conditions, string(g.GrantType), string(g.Source),
		nullable(g.ProfileKey), delegatedFrom, g.DelegationDepth, g.DelegableDepth, string(g.RiskClass),
		g.ValidFrom, g.ValidUntil, string(g.Status), g.GrantedBy, nullable(g.ApprovalReference), g.Reason, created, supersedes); err != nil {
		return fmt.Errorf("create administrative grant: %w", err)
	}
	payload := map[string]any{"grant_id": g.GrantID, "principal_id": g.PrincipalID, "permission": g.Permission,
		"scope": g.Scope, "grant_type": g.GrantType, "source": g.Source, "risk_class": g.RiskClass, "status": g.Status}
	if g.SupersedesGrantID != "" {
		payload["supersedes_grant_id"] = g.SupersedesGrantID
	}
	return insertProvisioningAudit(ctx, tx, actor, g.Scope.TenantID, "administrative_grant.created", g.GrantID, payload)
}

const grantColumns = `g.grant_id::text, g.principal_id, g.permission, g.scope, g.conditions, g.grant_type, g.source,
	COALESCE(g.profile_key, ''), COALESCE(g.delegated_from::text, ''), g.delegation_depth, g.delegable_depth, g.risk_class,
	g.valid_from, g.valid_until, g.status, g.granted_by, COALESCE(g.approval_reference, ''), g.reason, g.created_at,
	g.updated_at, g.revoked_at, COALESCE(g.revoked_by, ''), COALESCE(g.revocation_reason, ''), g.version,
	COALESCE(g.supersedes_grant_id::text, ''), COALESCE(g.superseded_by_grant_id::text, '')`

// AdministrativeGrantsOf implements AdministrativeGrantReader. The
// delegation chain is followed at most three hops, the deepest delegation
// the contract allows.
func (r *PostgresRepository) AdministrativeGrantsOf(ctx context.Context, principalID string) ([]administration.Grant, map[string]administration.Grant, error) {
	if r == nil || r.pool == nil {
		return nil, nil, errors.New("repository is not initialized")
	}
	rows, err := r.pool.Query(ctx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.principal_id = $1
		ORDER BY g.permission, g.grant_id`, principalID)
	if err != nil {
		return nil, nil, fmt.Errorf("load administrative grants: %w", err)
	}
	grants, err := pgx.CollectRows(rows, scanGrant)
	if err != nil {
		return nil, nil, fmt.Errorf("load administrative grants: %w", err)
	}
	rows, err = r.pool.Query(ctx, `
		WITH RECURSIVE chain(grant_id, hops) AS (
			SELECT delegated_from, 1 FROM policy.administrative_grant WHERE principal_id = $1 AND delegated_from IS NOT NULL
			UNION
			SELECT s.delegated_from, c.hops + 1 FROM chain c JOIN policy.administrative_grant s ON s.grant_id = c.grant_id
			WHERE s.delegated_from IS NOT NULL AND c.hops < 3
		)
		SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id IN (SELECT grant_id FROM chain)`, principalID)
	if err != nil {
		return nil, nil, fmt.Errorf("load delegation sources: %w", err)
	}
	sourceList, err := pgx.CollectRows(rows, scanGrant)
	if err != nil {
		return nil, nil, fmt.Errorf("load delegation sources: %w", err)
	}
	sources := make(map[string]administration.Grant, len(sourceList))
	for _, s := range sourceList {
		sources[s.GrantID] = s
	}
	return grants, sources, nil
}

func scanGrant(row pgx.CollectableRow) (administration.Grant, error) {
	var (
		g                                                   administration.Grant
		rowID, delegatedFrom, grantType, source, risk, stat string
		supersedes, supersededBy                            string
		scope, conditions                                   []byte
	)
	err := row.Scan(&rowID, &g.PrincipalID, &g.Permission, &scope, &conditions, &grantType, &source, &g.ProfileKey,
		&delegatedFrom, &g.DelegationDepth, &g.DelegableDepth, &risk, &g.ValidFrom, &g.ValidUntil, &stat, &g.GrantedBy,
		&g.ApprovalReference, &g.Reason, &g.CreatedAt, &g.UpdatedAt, &g.RevokedAt, &g.RevokedBy, &g.RevocationReason, &g.Version,
		&supersedes, &supersededBy)
	if err != nil {
		return g, err
	}
	for _, link := range []struct {
		row string
		to  *string
	}{{supersedes, &g.SupersedesGrantID}, {supersededBy, &g.SupersededByGrantID}} {
		if link.row == "" {
			continue
		}
		if *link.to, err = domain.FormatResourceID(grantIDPrefix, link.row); err != nil {
			return g, err
		}
	}
	if g.GrantID, err = domain.FormatResourceID(grantIDPrefix, rowID); err != nil {
		return g, err
	}
	if delegatedFrom != "" {
		if g.DelegatedFromGrantID, err = domain.FormatResourceID(grantIDPrefix, delegatedFrom); err != nil {
			return g, err
		}
	}
	if err := json.Unmarshal(scope, &g.Scope); err != nil {
		return g, fmt.Errorf("grant %s scope: %w", g.GrantID, err)
	}
	if len(conditions) > 0 {
		g.Conditions = &administration.Conditions{}
		if err := json.Unmarshal(conditions, g.Conditions); err != nil {
			return g, fmt.Errorf("grant %s conditions: %w", g.GrantID, err)
		}
	}
	g.GrantType, g.Source = administration.GrantType(grantType), administration.Source(source)
	g.RiskClass, g.Status = administration.RiskClass(risk), administration.Status(stat)
	for _, t := range []*time.Time{&g.ValidFrom, &g.CreatedAt} {
		*t = t.UTC()
	}
	return g, nil
}

// EffectiveRelations implements AdministrativeGrantReader. A mapping is
// effective by domain.TenantOrganisationMapping.InEffect, the definition
// attestation uses, applied here rather than restated in SQL.
func (r *PostgresRepository) EffectiveRelations(ctx context.Context, tenantIDs []string, at time.Time) (administration.Relations, error) {
	rel := administration.Relations{TenantOrganisations: map[string][]string{}}
	if len(tenantIDs) == 0 {
		return rel, nil
	}
	if r == nil || r.pool == nil {
		return rel, errors.New("repository is not initialized")
	}
	rows, err := r.pool.Query(ctx, `SELECT tenant_id, organisation_id::text, status, effective_from, effective_to
		FROM registry.tenant_organisation_mapping WHERE tenant_id = ANY($1) AND status <> 'ENDED'`, tenantIDs)
	if err != nil {
		return rel, fmt.Errorf("load tenant organisation mappings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.TenantOrganisationMapping
		if err := rows.Scan(&m.TenantID, &m.OrganisationID, &m.Status, &m.EffectiveFrom, &m.EffectiveTo); err != nil {
			return rel, err
		}
		if m.InEffect(at) {
			rel.TenantOrganisations[m.TenantID] = append(rel.TenantOrganisations[m.TenantID], m.OrganisationID)
		}
	}
	return rel, rows.Err()
}
