package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrGrantNotFound is a grant id that names no grant.
	ErrGrantNotFound = errors.New("administrative grant not found")
	// ErrGrantVersionMismatch is a command made against a stale version.
	ErrGrantVersionMismatch = errors.New("administrative grant version is stale")
	// ErrGrantCommandKeyReused is an Idempotency-Key replayed with a
	// different request.
	ErrGrantCommandKeyReused = errors.New("idempotency key reused for a different request")
)

// GrantFilter narrows ListAdministrativeGrants.
type GrantFilter struct {
	PrincipalID string
	Permission  string
	Status      administration.Status
}

// AdministrativeGrantAdministrator issues, inspects, changes and delegates
// AdministrativeGrants (ADR-BCP-020 sections 57-64; gate ADA-05). Every
// change is audited in the same transaction.
type AdministrativeGrantAdministrator interface {
	AdministrativeGrantReader
	GetAdministrativeGrant(ctx context.Context, grantID string) (administration.Grant, error)
	ListAdministrativeGrants(ctx context.Context, f GrantFilter, limit int, cursor string) ([]administration.Grant, string, error)
	// IssueAdministrativeGrant records g. A replay of (actor, key) with the
	// same hash returns the grant it created.
	IssueAdministrativeGrant(ctx context.Context, actor AuditActor, key, hash string, g administration.Grant) (administration.Grant, error)
	// DelegateAdministrativeGrant locks the source grant, asks plan for the
	// delegation it supports, and records it.
	DelegateAdministrativeGrant(ctx context.Context, actor AuditActor, key, hash, sourceGrantID string,
		plan func(source administration.Grant, sources map[string]administration.Grant) (administration.Grant, error)) (administration.Grant, error)
	// TransitionAdministrativeGrant performs a person-issued lifecycle
	// command at expectedVersion. Revoking also revokes every grant
	// delegated from it, directly or further down (section 47).
	TransitionAdministrativeGrant(ctx context.Context, actor AuditActor, key, hash, grantID string, command administration.Command,
		reason string, expectedVersion int64, now time.Time) (administration.Grant, error)
	// SweepAdministrativeGrants is the Control Plane's own lifecycle work:
	// PENDING grants whose start has come become ACTIVE, and grants past
	// their end become EXPIRED (section 61).
	SweepAdministrativeGrants(ctx context.Context, now time.Time) (activated, expired int, err error)
}

var _ AdministrativeGrantAdministrator = (*PostgresRepository)(nil)

// GetAdministrativeGrant implements AdministrativeGrantAdministrator.
func (r *PostgresRepository) GetAdministrativeGrant(ctx context.Context, grantID string) (administration.Grant, error) {
	if r == nil || r.pool == nil {
		return administration.Grant{}, errors.New("repository is not initialized")
	}
	rowID, err := domain.ParseResourceID(grantIDPrefix, grantID)
	if err != nil {
		return administration.Grant{}, ErrGrantNotFound
	}
	return queryGrant(ctx, r.pool, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid`, rowID)
}

type grantQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryGrant(ctx context.Context, q grantQuerier, sql string, args ...any) (administration.Grant, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return administration.Grant{}, err
	}
	grants, err := pgx.CollectRows(rows, scanGrant)
	if err != nil {
		return administration.Grant{}, err
	}
	if len(grants) == 0 {
		return administration.Grant{}, ErrGrantNotFound
	}
	return grants[0], nil
}

// ListAdministrativeGrants implements AdministrativeGrantAdministrator:
// newest first, keyset-paged on (created_at, grant_id).
func (r *PostgresRepository) ListAdministrativeGrants(ctx context.Context, f GrantFilter, limit int, cursor string) ([]administration.Grant, string, error) {
	if r == nil || r.pool == nil {
		return nil, "", errors.New("repository is not initialized")
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var afterAt any
	var afterID any
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		parts := strings.SplitN(string(raw), "|", 2)
		if err != nil || len(parts) != 2 {
			return nil, "", ErrInvalidCursor
		}
		at, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", ErrInvalidCursor
		}
		afterAt, afterID = at, parts[1]
	}
	rows, err := r.pool.Query(ctx, `SELECT `+grantColumns+` FROM policy.administrative_grant g
		WHERE ($1 = '' OR g.principal_id = $1) AND ($2 = '' OR g.permission = $2) AND ($3 = '' OR g.status = $3)
			AND ($4::timestamptz IS NULL OR (g.created_at, g.grant_id) < ($4::timestamptz, $5::uuid))
		ORDER BY g.created_at DESC, g.grant_id DESC LIMIT $6`,
		f.PrincipalID, f.Permission, string(f.Status), afterAt, afterID, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list administrative grants: %w", err)
	}
	grants, err := pgx.CollectRows(rows, scanGrant)
	if err != nil {
		return nil, "", fmt.Errorf("list administrative grants: %w", err)
	}
	next := ""
	if len(grants) > limit {
		grants = grants[:limit]
		last := grants[len(grants)-1]
		rowID, err := domain.ParseResourceID(grantIDPrefix, last.GrantID)
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + rowID))
	}
	return grants, next, nil
}

// ErrInvalidCursor is a list cursor this repository did not issue.
var ErrInvalidCursor = errors.New("invalid cursor")

// replayed returns the grant an earlier command with this key created, if
// any; the same key with another request is ErrGrantCommandKeyReused.
func replayed(ctx context.Context, tx pgx.Tx, actorID, key, hash string) (administration.Grant, bool, error) {
	var storedHash, rowID string
	err := tx.QueryRow(ctx, `SELECT request_hash, grant_id::text FROM policy.administrative_grant_command
		WHERE actor_id = $1 AND idempotency_key = $2`, actorID, key).Scan(&storedHash, &rowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return administration.Grant{}, false, nil
	}
	if err != nil {
		return administration.Grant{}, false, err
	}
	if storedHash != hash {
		return administration.Grant{}, false, ErrGrantCommandKeyReused
	}
	g, err := queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid`, rowID)
	return g, err == nil, err
}

func recordCommand(ctx context.Context, tx pgx.Tx, actorID, key, hash, grantID string) error {
	rowID, err := domain.ParseResourceID(grantIDPrefix, grantID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO policy.administrative_grant_command (actor_id, idempotency_key, request_hash, grant_id)
		VALUES ($1, $2, $3, $4::uuid)`, actorID, key, hash, rowID)
	return err
}

// withCommand runs fn in a transaction, retrying once if a concurrent
// command with the same key won the race (the replay then answers it).
func (r *PostgresRepository) withCommand(ctx context.Context, actor AuditActor, fn func(tx pgx.Tx) (administration.Grant, error)) (administration.Grant, error) {
	if r == nil || r.pool == nil {
		return administration.Grant{}, errors.New("repository is not initialized")
	}
	if err := validateActor(actor); err != nil {
		return administration.Grant{}, err
	}
	for attempt := 0; ; attempt++ {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return administration.Grant{}, err
		}
		g, err := fn(tx)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && attempt == 0 {
			continue
		}
		return g, err
	}
}

// IssueAdministrativeGrant implements AdministrativeGrantAdministrator.
func (r *PostgresRepository) IssueAdministrativeGrant(ctx context.Context, actor AuditActor, key, hash string, g administration.Grant) (administration.Grant, error) {
	catalogue, err := administration.DefaultCatalogue()
	if err != nil {
		return administration.Grant{}, err
	}
	return r.withCommand(ctx, actor, func(tx pgx.Tx) (administration.Grant, error) {
		if prior, ok, err := replayed(ctx, tx, actor.ActorID, key, hash); ok || err != nil {
			return prior, err
		}
		g.GrantID = domain.NewResourceID(grantIDPrefix)
		if err := insertGrant(ctx, tx, catalogue, g, actor); err != nil {
			return administration.Grant{}, err
		}
		if err := recordCommand(ctx, tx, actor.ActorID, key, hash, g.GrantID); err != nil {
			return administration.Grant{}, err
		}
		return g, nil
	})
}

// DelegateAdministrativeGrant implements AdministrativeGrantAdministrator.
func (r *PostgresRepository) DelegateAdministrativeGrant(ctx context.Context, actor AuditActor, key, hash, sourceGrantID string,
	plan func(source administration.Grant, sources map[string]administration.Grant) (administration.Grant, error)) (administration.Grant, error) {
	catalogue, err := administration.DefaultCatalogue()
	if err != nil {
		return administration.Grant{}, err
	}
	return r.withCommand(ctx, actor, func(tx pgx.Tx) (administration.Grant, error) {
		if prior, ok, err := replayed(ctx, tx, actor.ActorID, key, hash); ok || err != nil {
			return prior, err
		}
		sourceRow, err := domain.ParseResourceID(grantIDPrefix, sourceGrantID)
		if err != nil {
			return administration.Grant{}, ErrGrantNotFound
		}
		// Locking the source serialises delegation with its revocation.
		source, err := queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid FOR UPDATE`, sourceRow)
		if err != nil {
			return administration.Grant{}, err
		}
		sources, err := delegationSources(ctx, tx, sourceRow)
		if err != nil {
			return administration.Grant{}, err
		}
		g, err := plan(source, sources)
		if err != nil {
			return administration.Grant{}, err
		}
		g.GrantID = domain.NewResourceID(grantIDPrefix)
		if err := insertGrant(ctx, tx, catalogue, g, actor); err != nil {
			return administration.Grant{}, err
		}
		if err := recordCommand(ctx, tx, actor.ActorID, key, hash, g.GrantID); err != nil {
			return administration.Grant{}, err
		}
		return g, nil
	})
}

// delegationSources loads the chain a grant's own delegation rests on, at
// most three hops, by grant id.
func delegationSources(ctx context.Context, tx pgx.Tx, grantRow string) (map[string]administration.Grant, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE chain(grant_id, hops) AS (
			SELECT delegated_from, 1 FROM policy.administrative_grant WHERE grant_id = $1::uuid AND delegated_from IS NOT NULL
			UNION
			SELECT s.delegated_from, c.hops + 1 FROM chain c JOIN policy.administrative_grant s ON s.grant_id = c.grant_id
			WHERE s.delegated_from IS NOT NULL AND c.hops < 3
		)
		SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id IN (SELECT grant_id FROM chain)`, grantRow)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, scanGrant)
	if err != nil {
		return nil, err
	}
	out := make(map[string]administration.Grant, len(list))
	for _, s := range list {
		out[s.GrantID] = s
	}
	return out, nil
}

// TransitionAdministrativeGrant implements AdministrativeGrantAdministrator.
func (r *PostgresRepository) TransitionAdministrativeGrant(ctx context.Context, actor AuditActor, key, hash, grantID string,
	command administration.Command, reason string, expectedVersion int64, now time.Time) (administration.Grant, error) {
	return r.withCommand(ctx, actor, func(tx pgx.Tx) (administration.Grant, error) {
		if prior, ok, err := replayed(ctx, tx, actor.ActorID, key, hash); ok || err != nil {
			return prior, err
		}
		rowID, err := domain.ParseResourceID(grantIDPrefix, grantID)
		if err != nil {
			return administration.Grant{}, ErrGrantNotFound
		}
		g, err := queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid FOR UPDATE`, rowID)
		if err != nil {
			return administration.Grant{}, err
		}
		if g.Version != expectedVersion {
			return administration.Grant{}, ErrGrantVersionMismatch
		}
		to, err := administration.TransitionTarget(g, command, now)
		if err != nil {
			return administration.Grant{}, err
		}
		if to == administration.StatusRevoked {
			_, err = tx.Exec(ctx, `UPDATE policy.administrative_grant SET status = 'REVOKED', revoked_at = $2, revoked_by = $3,
				revocation_reason = $4, updated_at = $2, version = version + 1 WHERE grant_id = $1::uuid`,
				rowID, now, actor.ActorID, reason)
		} else {
			_, err = tx.Exec(ctx, `UPDATE policy.administrative_grant SET status = $2, updated_at = $3, version = version + 1
				WHERE grant_id = $1::uuid`, rowID, string(to), now)
		}
		if err != nil {
			return administration.Grant{}, fmt.Errorf("%s administrative grant: %w", command, err)
		}
		payload := map[string]any{"grant_id": grantID, "command": command, "from": g.Status, "to": to, "reason": reason,
			"permission": g.Permission, "principal_id": g.PrincipalID, "scope": g.Scope}
		if err := insertProvisioningAudit(ctx, tx, actor, g.Scope.TenantID, "administrative_grant."+string(to), grantID, payload); err != nil {
			return administration.Grant{}, err
		}
		if to == administration.StatusRevoked {
			if err := revokeDelegations(ctx, tx, actor, rowID, now, "source grant "+grantID+" revoked"); err != nil {
				return administration.Grant{}, err
			}
		}
		if err := recordCommand(ctx, tx, actor.ActorID, key, hash, grantID); err != nil {
			return administration.Grant{}, err
		}
		return queryGrant(ctx, tx, `SELECT `+grantColumns+` FROM policy.administrative_grant g WHERE g.grant_id = $1::uuid`, rowID)
	})
}

// revokeDelegations revokes every live grant delegated from rootRow,
// directly or through further delegations (section 47).
func revokeDelegations(ctx context.Context, tx pgx.Tx, actor AuditActor, rootRow string, now time.Time, reason string) error {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE d(grant_id) AS (
			SELECT grant_id FROM policy.administrative_grant WHERE delegated_from = $1::uuid
			UNION
			SELECT g.grant_id FROM policy.administrative_grant g JOIN d ON g.delegated_from = d.grant_id
		)
		UPDATE policy.administrative_grant SET status = 'REVOKED', revoked_at = $2, revoked_by = $3, revocation_reason = $4,
			updated_at = $2, version = version + 1
		WHERE grant_id IN (SELECT grant_id FROM d) AND status IN ('PENDING', 'ACTIVE', 'SUSPENDED')
		RETURNING grant_id::text, permission, principal_id`, rootRow, now, actor.ActorID, reason)
	if err != nil {
		return fmt.Errorf("revoke delegations: %w", err)
	}
	type revoked struct{ id, permission, principal string }
	var ended []revoked
	for rows.Next() {
		var v revoked
		if err := rows.Scan(&v.id, &v.permission, &v.principal); err != nil {
			rows.Close()
			return err
		}
		ended = append(ended, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, v := range ended {
		id, err := domain.FormatResourceID(grantIDPrefix, v.id)
		if err != nil {
			return err
		}
		if err := insertProvisioningAudit(ctx, tx, actor, "", "administrative_grant.REVOKED", id,
			map[string]any{"grant_id": id, "permission": v.permission, "principal_id": v.principal, "reason": reason, "cascade": true}); err != nil {
			return err
		}
	}
	return nil
}

// lifecycleActor is the audit actor of the Control Plane's own lifecycle
// work: activation and expiry belong to the platform, not a person (section 61).
var lifecycleActor = AuditActor{ActorID: "controlplane-lifecycle", ActorType: "workload", ClientID: "baobab-cp"}

// SweepAdministrativeGrants implements AdministrativeGrantAdministrator.
func (r *PostgresRepository) SweepAdministrativeGrants(ctx context.Context, now time.Time) (int, int, error) {
	if r == nil || r.pool == nil {
		return 0, 0, errors.New("repository is not initialized")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	actor := lifecycleActor
	actor.CorrelationID = domain.NewUUIDv7()
	step := func(to, set string, args ...any) (int, error) {
		rows, err := tx.Query(ctx, set, args...)
		if err != nil {
			return 0, err
		}
		type moved struct{ id, permission, principal string }
		var list []moved
		for rows.Next() {
			var m moved
			if err := rows.Scan(&m.id, &m.permission, &m.principal); err != nil {
				rows.Close()
				return 0, err
			}
			list = append(list, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
		for _, m := range list {
			id, err := domain.FormatResourceID(grantIDPrefix, m.id)
			if err != nil {
				return 0, err
			}
			if err := insertProvisioningAudit(ctx, tx, actor, "", "administrative_grant."+to, id,
				map[string]any{"grant_id": id, "permission": m.permission, "principal_id": m.principal, "to": to}); err != nil {
				return 0, err
			}
		}
		return len(list), nil
	}
	expired, err := step("EXPIRED", `UPDATE policy.administrative_grant SET status = 'EXPIRED', updated_at = $1, version = version + 1
		WHERE status IN ('PENDING', 'ACTIVE', 'SUSPENDED') AND valid_until <= $1
		RETURNING grant_id::text, permission, principal_id`, now)
	if err != nil {
		return 0, 0, fmt.Errorf("expire administrative grants: %w", err)
	}
	activated, err := step("ACTIVE", `UPDATE policy.administrative_grant SET status = 'ACTIVE', updated_at = $1, version = version + 1
		WHERE status = 'PENDING' AND valid_from <= $1 AND (valid_until IS NULL OR valid_until > $1)
		RETURNING grant_id::text, permission, principal_id`, now)
	if err != nil {
		return 0, 0, fmt.Errorf("activate administrative grants: %w", err)
	}
	return activated, expired, tx.Commit(ctx)
}
