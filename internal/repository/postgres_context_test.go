package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresResolvedContextRoundTrip proves CreateContext, GetContext and
// DeleteContextsByTenant round-trip against a real PostgreSQL instance
// (migration 000031_resolved_context_store.sql), not just compile.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestPostgresResolvedContextRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	const contextIDActive = "50000000-0000-0000-0000-0000000000c1"
	const contextIDExpired = "50000000-0000-0000-0000-0000000000c2"
	const tenantID = "tn_contexttest"

	cleanup := func() {
		admin.Exec(ctx, `DELETE FROM context.resolved_context WHERE context_id IN ($1::uuid, $2::uuid)`, contextIDActive, contextIDExpired)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	resolved := domain.Context{
		ID:               contextIDActive,
		PrincipalID:      "principal-abc",
		TenantID:         tenantID,
		LegalEntityID:    "legal-456",
		MarketID:         "market-789",
		CountryCode:      "ZA",
		CurrencyCode:     "ZAR",
		Locale:           "en-ZA",
		DeploymentRegion: "af-south-1",
		Environment:      "production",
		CorrelationID:    "correlation-123",
		ResolvedAt:       now,
		ExpiresAt:        &expiresAt,
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified},
		},
	}
	if err := repo.CreateContext(ctx, resolved); err != nil {
		t.Fatalf("create context: %v", err)
	}
	if err := repo.CreateContext(ctx, resolved); err == nil {
		t.Fatal("expected creating a duplicate context id to fail")
	}

	fetched, err := repo.GetContext(ctx, contextIDActive)
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if fetched.TenantID != tenantID || fetched.MarketID != "market-789" || fetched.CurrencyCode != "ZAR" {
		t.Fatalf("unexpected fetched context: %+v", fetched)
	}
	// PostgreSQL's timestamptz stores microsecond precision; Go's time.Now()
	// carries nanoseconds, so compare truncated to the DB's precision rather
	// than exact equality.
	if fetched.ExpiresAt == nil || !fetched.ExpiresAt.Truncate(time.Microsecond).Equal(expiresAt.Truncate(time.Microsecond)) {
		t.Fatalf("expected expires_at to round-trip, got %+v", fetched.ExpiresAt)
	}
	if fetched.Provenance["tenant_id"].TrustLevel != domain.TrustVerified {
		t.Fatalf("expected provenance to round-trip, got %+v", fetched.Provenance)
	}

	if _, err := repo.GetContext(ctx, "50000000-0000-0000-0000-0000000000c9"); !errors.Is(err, ErrContextNotFound) {
		t.Fatalf("expected ErrContextNotFound for a missing context, got %v", err)
	}

	expiredAt := now.Add(-time.Minute)
	expired := domain.Context{
		ID:            contextIDExpired,
		PrincipalID:   "principal-abc",
		TenantID:      tenantID,
		CorrelationID: "correlation-456",
		ResolvedAt:    now.Add(-time.Hour),
		ExpiresAt:     &expiredAt,
		Provenance: map[string]domain.ContextSource{
			"tenant_id": {Source: "verified_token", TrustLevel: domain.TrustVerified},
		},
	}
	if err := repo.CreateContext(ctx, expired); err != nil {
		t.Fatalf("create expired context: %v", err)
	}
	if _, err := repo.GetContext(ctx, contextIDExpired); !errors.Is(err, ErrContextNotFound) {
		t.Fatalf("expected an expired context to report ErrContextNotFound, got %v", err)
	}

	removed, err := repo.DeleteContextsByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("delete contexts by tenant: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected both the active and expired context rows removed, got %d", removed)
	}
	if _, err := repo.GetContext(ctx, contextIDActive); !errors.Is(err, ErrContextNotFound) {
		t.Fatal("expected the deleted context to be gone")
	}
}

// TestPostgresContextAuthorityPurpose proves migration 000094 against a real PostgreSQL instance: the purpose and the
// approved plan tuple round-trip, the database itself refuses a provisioning context that is unbounded, longer than 15
// minutes, or without its tuple (and a RUNTIME context that carries one), and a stored context's purpose, tuple, tenant
// and owner cannot be rewritten afterwards (ADR-BCP-004 section 71) while deleting for tenant invalidation still works.
func TestPostgresContextAuthorityPurpose(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	defer repo.Close()

	const tenantID = "tn_purposetest"
	t.Cleanup(func() { admin.Exec(ctx, `DELETE FROM context.resolved_context WHERE tenant_id = $1`, tenantID) })
	admin.Exec(ctx, `DELETE FROM context.resolved_context WHERE tenant_id = $1`, tenantID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	authority := &domain.ProvisioningAuthority{TenantProvisioningID: "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5a75", PlanID: "plan_0199a1b2c3d47e8f",
		PlanVersion: 2, PlanDigest: "sha256:" + strings.Repeat("a", 64)}
	provisioning := func(id string, expires *time.Time) domain.Context {
		return domain.Context{ID: id, PrincipalID: "prn_provisioner", TenantID: tenantID, CorrelationID: "tp-1", ResolvedAt: now, ExpiresAt: expires,
			Provenance:       map[string]domain.ContextSource{"tenant_id": {Source: "tenant_provisioning", TrustLevel: domain.TrustSystem}},
			AuthorityPurpose: domain.ContextPurposeTenantProvisioning, ProvisioningAuthority: authority}
	}
	in10 := now.Add(10 * time.Minute)
	good := "50000000-0000-0000-0000-0000000000d1"
	if err := repo.CreateContext(ctx, provisioning(good, &in10)); err != nil {
		t.Fatalf("a valid provisioning context: %v", err)
	}
	got, err := repo.GetContext(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose() != domain.ContextPurposeTenantProvisioning || got.IsRuntime() || got.ProvisioningAuthority == nil || *got.ProvisioningAuthority != *authority {
		t.Fatalf("purpose and plan tuple must round-trip: %+v", got)
	}

	runtimeID := "50000000-0000-0000-0000-0000000000d2"
	if err := repo.CreateContext(ctx, domain.Context{ID: runtimeID, PrincipalID: "prn_trade", TenantID: tenantID, CorrelationID: "c", ResolvedAt: now, ExpiresAt: &in10}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetContext(ctx, runtimeID); err != nil || got.Purpose() != domain.ContextPurposeRuntime || got.ProvisioningAuthority != nil {
		t.Fatalf("a context with no recorded purpose is RUNTIME with no tuple: %+v %v", got, err)
	}

	// The domain refuses these first; the database must refuse them too, so a writer that skips Validate cannot store them.
	raw := func(id, purpose, expires string, authority any) error {
		_, err := admin.Exec(ctx, `INSERT INTO context.resolved_context(context_id, principal_id, tenant_id, correlation_id, resolved_at, expires_at, authority_purpose, provisioning_authority)
			VALUES ($1::uuid, 'prn_x', $2, 'c', $3, $3::timestamptz + $4::interval, $5, $6::jsonb)`, id, tenantID, now, expires, purpose, authority)
		return err
	}
	tuple := `{"tenant_provisioning_id":"tp_x","plan_id":"plan_x","plan_version":1,"plan_digest":"sha256:x"}`
	for name, err := range map[string]error{
		"provisioning for 16 minutes":    raw("50000000-0000-0000-0000-0000000000e1", "TENANT_PROVISIONING", "16 minutes", tuple),
		"provisioning without its tuple": raw("50000000-0000-0000-0000-0000000000e2", "TENANT_PROVISIONING", "5 minutes", nil),
		"provisioning with a partial tuple": raw("50000000-0000-0000-0000-0000000000e3", "TENANT_PROVISIONING", "5 minutes",
			`{"tenant_provisioning_id":"tp_x"}`),
		"runtime carrying a tuple": raw("50000000-0000-0000-0000-0000000000e4", "RUNTIME", "5 minutes", tuple),
		"an unknown purpose":       raw("50000000-0000-0000-0000-0000000000e5", "ANYTHING", "5 minutes", nil),
	} {
		if err == nil {
			t.Errorf("%s: the database accepted it", name)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO context.resolved_context(context_id, principal_id, tenant_id, correlation_id, resolved_at, expires_at, authority_purpose, provisioning_authority)
		VALUES ('50000000-0000-0000-0000-0000000000e6', 'prn_x', $1, 'c', $2, NULL, 'TENANT_PROVISIONING', $3::jsonb)`, tenantID, now, tuple); err == nil {
		t.Error("an unbounded provisioning context: the database accepted it")
	}

	// Immutability (ADR-BCP-004 section 71): purpose, tuple, tenant and owner are fixed once stored.
	for name, statement := range map[string]string{
		"purpose":   `UPDATE context.resolved_context SET authority_purpose = 'RUNTIME', provisioning_authority = NULL WHERE context_id = $1::uuid`,
		"tuple":     `UPDATE context.resolved_context SET provisioning_authority = jsonb_set(provisioning_authority, '{plan_version}', '9') WHERE context_id = $1::uuid`,
		"tenant":    `UPDATE context.resolved_context SET tenant_id = 'tn_elsewhere' WHERE context_id = $1::uuid`,
		"principal": `UPDATE context.resolved_context SET principal_id = 'prn_other' WHERE context_id = $1::uuid`,
	} {
		if _, err := admin.Exec(ctx, statement, good); err == nil {
			t.Errorf("rewriting the %s of a stored context was accepted", name)
		}
	}
	if n, err := repo.DeleteContextsByTenant(ctx, tenantID); err != nil || n < 2 {
		t.Fatalf("tenant invalidation must still delete contexts of every purpose: %d %v", n, err)
	}
}
