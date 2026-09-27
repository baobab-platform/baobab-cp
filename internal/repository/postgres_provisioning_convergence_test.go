package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestProvisioningConvergenceStore covers migration 000062 (ADR-SHARED-015):
// the canonical state is derived from the legacy status until apply runs as
// an operation; a plan is immutable and only one is current; an approval
// other than APPROVED gives its reason; provenance is recorded whole.
func TestProvisioningConvergenceStore(t *testing.T) {
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

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[12:]
	tenant, row := "tn_"+suffix[:20], domain.NewUUIDv7()
	digest := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	t.Cleanup(func() {
		admin.Exec(ctx, `DELETE FROM provisioning.plan_approval WHERE tenant_provisioning_id = $1::uuid`, row)
		admin.Exec(ctx, `DELETE FROM provisioning.plan WHERE tenant_provisioning_id = $1::uuid`, row)
		admin.Exec(ctx, `DELETE FROM provisioning.desired_state WHERE tenant_provisioning_id = $1::uuid`, row)
		admin.Exec(ctx, `DELETE FROM provisioning.tenant_provisioning WHERE tenant_provisioning_id = $1::uuid`, row)
	})
	refused := func(label, constraint string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (constraint != "" && pgErr.ConstraintName != constraint && pgErr.Code != constraint) {
			t.Fatalf("%s should be refused (%s), got %v", label, constraint, err)
		}
	}

	// Provenance is recorded whole or not at all.
	_, err = admin.Exec(ctx, `INSERT INTO provisioning.tenant_provisioning (tenant_provisioning_id, tenant_id, idempotency_key, request_hash,
		tenant_onboarding_request_id) VALUES ($1::uuid, $2, 'key-provenance-000001', 'h', 'tor_0199a1b2')`, domain.NewUUIDv7(), tenant)
	refused("provenance without its admission decision", "tenant_provisioning_provenance_ck", err)
	if _, err := admin.Exec(ctx, `INSERT INTO provisioning.tenant_provisioning (tenant_provisioning_id, tenant_id, idempotency_key, request_hash,
		tenant_onboarding_request_id, admission_decision_id, created_by) VALUES ($1::uuid, $2, 'key-provenance-000002', 'h', 'tor_0199a1b2', 'adm_0199a1b2', 'prn_requester')`,
		row, tenant); err != nil {
		t.Fatal(err)
	}

	// The canonical state is derived from the legacy status.
	var key, state, readiness string
	if err := admin.QueryRow(ctx, `SELECT tenant_provisioning_key, state, readiness_status FROM provisioning.tenant_provisioning
		WHERE tenant_provisioning_id = $1::uuid`, row).Scan(&key, &state, &readiness); err != nil {
		t.Fatal(err)
	}
	if key != "tp_"+strings.ReplaceAll(row, "-", "") || state != "PLANNED" || readiness != "UNKNOWN" {
		t.Fatalf("new provisioning: key %s state %s readiness %s", key, state, readiness)
	}
	for legacy, canonical := range map[string]string{"APPLY": "REGISTERING", "RECONCILE": "VERIFYING_READINESS", "READY": "READY", "CANCELLED": "CANCELLED"} {
		if err := admin.QueryRow(ctx, `UPDATE provisioning.tenant_provisioning SET status = $2 WHERE tenant_provisioning_id = $1::uuid RETURNING state`,
			row, legacy).Scan(&state); err != nil || state != canonical {
			t.Fatalf("legacy %s projects to %s, got %s %v", legacy, canonical, state, err)
		}
	}

	// A plan is immutable, and one plan is current.
	if _, err := admin.Exec(ctx, `INSERT INTO provisioning.desired_state (tenant_provisioning_id, desired_state_version, document, desired_state_digest)
		VALUES ($1::uuid, 1, '{}', $2)`, row, digest("a")); err != nil {
		t.Fatal(err)
	}
	plan := "plan_" + suffix
	if _, err := admin.Exec(ctx, `INSERT INTO provisioning.plan (plan_id, tenant_provisioning_id, plan_version, plan_digest, base_revision,
		desired_state_version, document) VALUES ($1, $2::uuid, 1, $3, 1, 1, '{}')`, plan, row, digest("b")); err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `UPDATE provisioning.plan SET plan_digest = $2 WHERE plan_id = $1`, plan, digest("c"))
	refused("rewriting a plan's digest", "25006", err)
	_, err = admin.Exec(ctx, `INSERT INTO provisioning.plan (plan_id, tenant_provisioning_id, plan_version, plan_digest, base_revision,
		desired_state_version, document) VALUES ($1, $2::uuid, 2, $3, 1, 1, '{}')`, plan+"x", row, digest("d"))
	refused("a second current plan", "plan_current_uq", err)
	if _, err := admin.Exec(ctx, `UPDATE provisioning.plan SET superseded_at = now() WHERE plan_id = $1`, plan); err != nil {
		t.Fatalf("superseding a plan: %v", err)
	}
	_, err = admin.Exec(ctx, `UPDATE provisioning.plan SET superseded_at = NULL WHERE plan_id = $1`, plan)
	refused("reinstating a superseded plan", "25006", err)

	// A decision other than APPROVED gives its reason.
	_, err = admin.Exec(ctx, `INSERT INTO provisioning.plan_approval (approval_id, tenant_provisioning_id, plan_id, plan_version, plan_digest,
		decision, decided_by) VALUES ($1, $2::uuid, $3, 1, $4, 'REJECTED', 'prn_approver')`, "apd_"+suffix, row, plan, digest("b"))
	refused("a rejection without a reason", "", err)
	if _, err := admin.Exec(ctx, `INSERT INTO provisioning.plan_approval (approval_id, tenant_provisioning_id, plan_id, plan_version, plan_digest,
		decision, decided_by) VALUES ($1, $2::uuid, $3, 1, $4, 'APPROVED', 'prn_approver')`, "apd_"+suffix, row, plan, digest("b")); err != nil {
		t.Fatal(err)
	}
}
