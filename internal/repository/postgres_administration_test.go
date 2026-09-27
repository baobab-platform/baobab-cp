package repository

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/administration"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAdministrativeGrantStore covers migration 000067 (ADR-BCP-020 gate
// ADA-02): grants round-trip with their scope and provenance, delegation
// sources are loaded with them, a delegation must name a grant its grantor
// holds, every creation is audited, and the database itself refuses a
// self-grant, a standing bootstrap grant and a revocation without its
// record.
func TestAdministrativeGrantStore(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	tail := domain.NewUUIDv7()[24:]
	jane, carol, ops := "prn_jane"+tail, "prn_carol"+tail, "prn_ops"+tail
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1) AND delegated_from IS NOT NULL`, []string{jane, carol})
		admin.Exec(ctx, `DELETE FROM policy.administrative_grant WHERE principal_id = ANY($1)`, []string{jane, carol})
	})
	actor := AuditActor{ActorID: ops, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	until := now.Add(24 * time.Hour)
	source := administration.Grant{GrantID: domain.NewResourceID("agr"), PrincipalID: jane, Permission: "tenant.view",
		Scope:      administration.Scope{Level: administration.LevelTenant, TenantID: "tn_acmeug", Environment: "production"},
		Conditions: &administration.Conditions{MinimumACR: "urn:baobab:acr:mfa"}, GrantType: administration.TypeTimeBound,
		Source: administration.SourceProfile, ProfileKey: "tenant-administrator", DelegableDepth: 1, RiskClass: administration.RiskLow,
		ValidFrom: now, ValidUntil: &until, Status: administration.StatusActive, GrantedBy: ops, Reason: "Tenant administrator", Version: 1}
	if err := repo.CreateAdministrativeGrant(ctx, source, actor); err != nil {
		t.Fatal(err)
	}
	delegated := administration.Grant{GrantID: domain.NewResourceID("agr"), PrincipalID: carol, Permission: "tenant.view",
		Scope: source.Scope, GrantType: administration.TypeTimeBound, Source: administration.SourceDelegation,
		DelegatedFromGrantID: source.GrantID, DelegationDepth: 1, RiskClass: administration.RiskLow, ValidFrom: now,
		ValidUntil: &until, Status: administration.StatusActive, GrantedBy: jane, Reason: "Cover", Version: 1}
	if err := repo.CreateAdministrativeGrant(ctx, delegated, actor); err != nil {
		t.Fatal(err)
	}
	stolen := delegated
	stolen.GrantID, stolen.GrantedBy = domain.NewResourceID("agr"), ops
	if err := repo.CreateAdministrativeGrant(ctx, stolen, actor); err == nil {
		t.Fatal("a delegation of a grant the grantor does not hold was recorded")
	}
	bad := source
	bad.GrantID, bad.GrantedBy = domain.NewResourceID("agr"), jane
	if err := repo.CreateAdministrativeGrant(ctx, bad, actor); err == nil {
		t.Fatal("a self-grant was recorded")
	}

	grants, sources, err := repo.AdministrativeGrantsOf(ctx, carol)
	if err != nil || len(grants) != 1 {
		t.Fatalf("carol's grants: %+v %v", grants, err)
	}
	got := grants[0]
	if got.GrantID != delegated.GrantID || got.DelegatedFromGrantID != source.GrantID || !reflect.DeepEqual(got.Scope, delegated.Scope) ||
		got.ValidUntil == nil || !got.ValidUntil.Equal(until) {
		t.Fatalf("delegated grant round trip: %+v", got)
	}
	src, ok := sources[source.GrantID]
	if !ok || src.Conditions == nil || src.Conditions.MinimumACR != "urn:baobab:acr:mfa" || src.ProfileKey != "tenant-administrator" {
		t.Fatalf("delegation source: %+v %v", src, ok)
	}
	if e := administration.Effective(carol, grants, sources, now.Add(time.Minute)); len(e.Grants) != 1 {
		t.Fatalf("carol's delegated authority must be effective while its source is: %+v", e)
	}
	var audits int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'administrative_grant.created' AND target = ANY($1)`,
		[]string{source.GrantID, delegated.GrantID}).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("each grant is audited: %d %v", audits, err)
	}

	refused := func(label, constraint, sql string, args ...any) {
		t.Helper()
		_, err := admin.Exec(ctx, sql, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != constraint {
			t.Fatalf("%s should be refused by %s, got %v", label, constraint, err)
		}
	}
	insert := `INSERT INTO policy.administrative_grant (grant_id, principal_id, permission, scope_level, scope, grant_type, source,
		risk_class, valid_from, valid_until, status, granted_by, reason) VALUES ($1::uuid, $2, 'administrator.grant', 'PLATFORM',
		'{"level":"PLATFORM"}', $3, $4, 'HIGH', now(), $5, 'ACTIVE', $6, 'x')`
	refused("a self-grant", "administrative_grant_not_self_ck", insert, domain.NewUUIDv7(), jane, "STANDING", "DIRECT", nil, jane)
	refused("a standing bootstrap grant", "administrative_grant_bootstrap_ck", insert, domain.NewUUIDv7(), jane, "STANDING", "BOOTSTRAP", nil, ops)
	refused("a standing grant with an end", "administrative_grant_window_ck", insert, domain.NewUUIDv7(), jane, "STANDING", "DIRECT", until, ops)
	refused("a revocation without its record", "administrative_grant_revocation_ck",
		`UPDATE policy.administrative_grant SET status = 'REVOKED' WHERE principal_id = $1`, carol)
}
