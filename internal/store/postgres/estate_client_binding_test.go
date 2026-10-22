package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// ADR-SHARED-042 CAA-04 (ADR-BCP-029): a binding's scope is immutable, every
// change increments its revision by one, REVOKED is terminal, nothing is
// deleted, and every resolution answer is immutable evidence naming a binding
// only when ATTESTED.
func TestEstateClientBindingGovernanceAndEvidence(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]
	issuer, client := "https://iam.example.test", "synthetic-bff-"+suffix
	insert := func(estate, environment, status string) (string, error) {
		var id string
		err := db.pool.QueryRow(ctx, `
			INSERT INTO registry.estate_client_binding (issuer, bff_client_id, digital_estate_id, tenant_id,
			    engine_instance_id, environment, deployment_reference, status, revision, effective_from, evidence_reference)
			VALUES ($1, $2, $3, 'tn_synthetic01', 'ei_synthetic01', $4, 'deployment.synthetic', $5, 1, now() - interval '1 hour',
			        'changeset:synthetic')
			RETURNING binding_id::text`, issuer, client, estate, environment, status).Scan(&id)
		return id, err
	}
	rejects := func(name, sql string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	id, err := insert("estate_synthetic", "staging", "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insert("Estate-1", "staging", "ACTIVE"); err == nil {
		t.Fatal("a non-canonical estate id was accepted")
	}
	if _, err := insert("estate_other", "qa", "ACTIVE"); err == nil {
		t.Fatal("an unknown environment was accepted")
	}
	if _, err := insert("estate_synthetic", "staging", "ACTIVE"); err == nil {
		t.Fatal("a duplicate binding scope was accepted")
	}

	rejects("a scope change", `UPDATE registry.estate_client_binding SET tenant_id = 'tn_other01', revision = 2 WHERE binding_id = $1::uuid`, id)
	rejects("an unversioned change", `UPDATE registry.estate_client_binding SET status = 'SUSPENDED' WHERE binding_id = $1::uuid`, id)
	rejects("a revision jump", `UPDATE registry.estate_client_binding SET status = 'SUSPENDED', revision = 3 WHERE binding_id = $1::uuid`, id)
	rejects("a delete", `DELETE FROM registry.estate_client_binding WHERE binding_id = $1::uuid`, id)
	if _, err := db.pool.Exec(ctx, `UPDATE registry.estate_client_binding SET status = 'SUSPENDED', revision = 2
		WHERE binding_id = $1::uuid`, id); err != nil {
		t.Fatalf("a governed suspension was refused: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE registry.estate_client_binding SET status = 'REVOKED', revision = 3
		WHERE binding_id = $1::uuid`, id); err != nil {
		t.Fatalf("a governed revocation was refused: %v", err)
	}
	rejects("a resurrection", `UPDATE registry.estate_client_binding SET status = 'ACTIVE', revision = 4 WHERE binding_id = $1::uuid`, id)

	second, err := insert("estate_second", "development", "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := db.EstateClientBindings(ctx, issuer, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings = %+v", bindings)
	}
	for _, b := range bindings {
		if b.BindingID == id && (b.Status != "REVOKED" || b.Revision != 3) {
			t.Fatalf("revoked binding read back as %+v", b)
		}
		if b.BindingID == second && (b.Environment != "development" || b.DigitalEstateID != "estate_second") {
			t.Fatalf("second binding read back as %+v", b)
		}
	}
	if other, err := db.EstateClientBindings(ctx, "https://other.example.test", client); err != nil || len(other) != 0 {
		t.Fatalf("another issuer's lookup = %+v, %v", other, err)
	}

	now := time.Now().UTC()
	if err := db.RecordEstateClientBindingResolution(ctx, EstateClientBindingResolution{CallerClientID: "baobab-trade",
		Issuer: issuer, BFFClientID: client, EngineInstanceID: "ei_synthetic01", Environment: "development",
		Outcome: "ATTESTED", BindingID: second, Revision: 1, CandidateCount: 1, ResolvedAt: now,
		CorrelationID: domain.NewUUIDv7()}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEstateClientBindingResolution(ctx, EstateClientBindingResolution{CallerClientID: "baobab-trade",
		EngineInstanceID: "ei_synthetic01", Environment: "staging", Outcome: "CLIENT_EVIDENCE_INVALID", ResolvedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEstateClientBindingResolution(ctx, EstateClientBindingResolution{CallerClientID: "baobab-trade",
		EngineInstanceID: "ei_synthetic01", Environment: "staging", Outcome: "ATTESTED", ResolvedAt: now}); err == nil {
		t.Fatal("an attestation naming no binding was recorded")
	}
	if err := db.RecordEstateClientBindingResolution(ctx, EstateClientBindingResolution{CallerClientID: "baobab-trade",
		EngineInstanceID: "ei_synthetic01", Environment: "staging", Outcome: "BINDING_AMBIGUOUS", BindingID: second,
		Revision: 1, ResolvedAt: now}); err == nil {
		t.Fatal("a denial naming a binding was recorded")
	}
	rejects("an evidence update", `UPDATE registry.estate_client_binding_resolution SET outcome = 'ATTESTED' WHERE bff_client_id = $1`, client)
	rejects("an evidence delete", `DELETE FROM registry.estate_client_binding_resolution WHERE bff_client_id = $1`, client)
}
