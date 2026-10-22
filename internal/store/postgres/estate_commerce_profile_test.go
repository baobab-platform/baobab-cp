package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// ADR-SHARED-042 section 14, Decision 8 (ADR-BCP-030): a profile's scope and
// model are immutable, every change increments its revision by one, REVOKED is
// terminal, nothing is deleted, at most one ACTIVE profile covers an (estate,
// engine instance, Market) at a time, and every resolution answer is immutable
// evidence naming a profile only when ATTESTED. Synthetic identifiers only.
func TestEstateCommerceProfileGovernanceAndEvidence(t *testing.T) {
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
	estate := "estate_" + suffix
	insert := func(model, status string, markets []string, from time.Duration, until *time.Duration) (string, error) {
		var end any
		if until != nil {
			end = time.Now().Add(*until)
		}
		var id string
		err := db.pool.QueryRow(ctx, `
			INSERT INTO registry.estate_commerce_profile (digital_estate_id, tenant_id, trade_engine_instance_id,
			    commerce_model, market_scope, status, revision, effective_from, effective_until, approval_reference,
			    evidence_reference)
			VALUES ($1, 'tn_synthetic01', 'ei_synthetic01', $2, $3, $4, 1, $5, $6, 'approval:synthetic',
			        'changeset:synthetic')
			RETURNING commerce_profile_id::text`, estate, model, markets, status, time.Now().Add(from), end).Scan(&id)
		return id, err
	}
	rejects := func(name, sql string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	id, err := insert("B2B", "ACTIVE", []string{"market_ug"}, -time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, markets := range map[string][]string{"no Market": {}, "a non-canonical Market": {"Market-UG"},
		"a repeated Market": {"market_ke", "market_ke"}} {
		if _, err := insert("B2C", "SUSPENDED", markets, -time.Hour, nil); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err := insert("MIXED", "SUSPENDED", []string{"market_ke"}, -time.Hour, nil); err == nil {
		t.Fatal("a MIXED model was accepted")
	}
	if _, err := insert("B2C", "ACTIVE", []string{"market_ke", "market_ug"}, -time.Minute, nil); err == nil {
		t.Fatal("a second ACTIVE profile over the same Market was accepted")
	}
	elsewhere, err := insert("B2C", "ACTIVE", []string{"market_ke"}, -time.Hour, nil)
	if err != nil {
		t.Fatalf("an ACTIVE profile for another Market was refused: %v", err)
	}
	soon := 2 * time.Hour
	if _, err := insert("B2C", "ACTIVE", []string{"market_ug"}, time.Hour, &soon); err == nil {
		t.Fatal("an overlapping future ACTIVE profile was accepted")
	}
	rejects("a revision other than 1", `INSERT INTO registry.estate_commerce_profile (digital_estate_id, tenant_id,
		trade_engine_instance_id, commerce_model, market_scope, status, revision, effective_from, approval_reference,
		evidence_reference) VALUES ($1, 'tn_synthetic01', 'ei_synthetic01', 'B2C', '{market_tz}', 'SUSPENDED', 2, now(),
		'approval:synthetic', 'changeset:synthetic')`, estate)
	rejects("a missing approval", `INSERT INTO registry.estate_commerce_profile (digital_estate_id, tenant_id,
		trade_engine_instance_id, commerce_model, market_scope, status, revision, effective_from, approval_reference,
		evidence_reference) VALUES ($1, 'tn_synthetic01', 'ei_synthetic01', 'B2C', '{market_tz}', 'SUSPENDED', 1, now(),
		' ', 'changeset:synthetic')`, estate)

	rejects("a model change", `UPDATE registry.estate_commerce_profile SET commerce_model = 'B2C', revision = 2 WHERE commerce_profile_id = $1::uuid`, id)
	rejects("a Market change", `UPDATE registry.estate_commerce_profile SET market_scope = '{market_tz}', revision = 2 WHERE commerce_profile_id = $1::uuid`, id)
	rejects("an unversioned change", `UPDATE registry.estate_commerce_profile SET status = 'SUSPENDED' WHERE commerce_profile_id = $1::uuid`, id)
	rejects("a revision jump", `UPDATE registry.estate_commerce_profile SET status = 'SUSPENDED', revision = 3 WHERE commerce_profile_id = $1::uuid`, id)
	rejects("a delete", `DELETE FROM registry.estate_commerce_profile WHERE commerce_profile_id = $1::uuid`, id)
	if _, err := db.pool.Exec(ctx, `UPDATE registry.estate_commerce_profile SET status = 'SUSPENDED', revision = 2
		WHERE commerce_profile_id = $1::uuid`, id); err != nil {
		t.Fatalf("a governed suspension was refused: %v", err)
	}
	successor, err := insert("B2C", "ACTIVE", []string{"market_ug"}, -time.Minute, nil)
	if err != nil {
		t.Fatalf("a successor over a suspended profile's Market was refused: %v", err)
	}
	rejects("a reactivation that overlaps the successor", `UPDATE registry.estate_commerce_profile SET status = 'ACTIVE', revision = 3
		WHERE commerce_profile_id = $1::uuid`, id)
	if _, err := db.pool.Exec(ctx, `UPDATE registry.estate_commerce_profile SET status = 'REVOKED', revision = 3
		WHERE commerce_profile_id = $1::uuid`, id); err != nil {
		t.Fatalf("a governed revocation was refused: %v", err)
	}
	rejects("a resurrection", `UPDATE registry.estate_commerce_profile SET status = 'SUSPENDED', revision = 4 WHERE commerce_profile_id = $1::uuid`, id)
	rejects("a truncate", `TRUNCATE registry.estate_commerce_profile`)

	profiles, err := db.EstateCommerceProfiles(ctx, estate)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 {
		t.Fatalf("profiles = %+v", profiles)
	}
	for _, p := range profiles {
		if p.CommerceProfileID == id && (p.Status != "REVOKED" || p.Revision != 3 || p.CommerceModel != "B2B") {
			t.Fatalf("revoked profile read back as %+v", p)
		}
		if p.CommerceProfileID == elsewhere && (len(p.MarketScope) != 1 || p.MarketScope[0] != "market_ke") {
			t.Fatalf("Market scope read back as %+v", p)
		}
	}
	if other, err := db.EstateCommerceProfiles(ctx, "estate_unregistered"+suffix); err != nil || len(other) != 0 {
		t.Fatalf("an unregistered estate's lookup = %+v, %v", other, err)
	}

	// Provenance: only a fresh ATTESTED binding resolution by the same caller,
	// for an ACTIVE binding at its current revision, counts.
	client := "synthetic-bff-" + suffix
	var binding string
	if err := db.pool.QueryRow(ctx, `
		INSERT INTO registry.estate_client_binding (issuer, bff_client_id, digital_estate_id, tenant_id,
		    engine_instance_id, environment, deployment_reference, status, revision, effective_from, evidence_reference)
		VALUES ('https://iam.example.test', $1, $2, 'tn_synthetic01', 'ei_synthetic01', 'staging', 'deployment.synthetic',
		        'ACTIVE', 1, now() - interval '1 hour', 'changeset:synthetic')
		RETURNING binding_id::text`, client, estate).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	caller, since := "synthetic-trade-"+suffix, time.Now().Add(-time.Minute)
	provenance := func(who, engine string) bool {
		t.Helper()
		ok, err := db.EstateBindingProvenance(ctx, who, binding, estate, engine, since)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if provenance(caller, "ei_synthetic01") {
		t.Fatal("provenance held without an attestation")
	}
	if err := db.RecordEstateClientBindingResolution(ctx, EstateClientBindingResolution{CallerClientID: caller,
		Issuer: "https://iam.example.test", BFFClientID: client, EngineInstanceID: "ei_synthetic01", Environment: "staging",
		Outcome: "ATTESTED", BindingID: binding, Revision: 1, CandidateCount: 1, ResolvedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if !provenance(caller, "ei_synthetic01") {
		t.Fatal("a fresh attestation was not accepted as provenance")
	}
	if provenance("another-workload", "ei_synthetic01") || provenance(caller, "ei_other01") {
		t.Fatal("provenance held for another caller or engine instance")
	}
	if ok, err := db.EstateBindingProvenance(ctx, caller, binding, estate, "ei_synthetic01", time.Now().Add(time.Minute)); err != nil || ok {
		t.Fatalf("a stale attestation was accepted: %v %v", ok, err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE registry.estate_client_binding SET status = 'SUSPENDED', revision = 2
		WHERE binding_id = $1::uuid`, binding); err != nil {
		t.Fatal(err)
	}
	if provenance(caller, "ei_synthetic01") {
		t.Fatal("provenance survived the binding's suspension")
	}

	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	attested := EstateCommerceProfileResolution{AttestationID: domain.NewUUIDv7(), CallerClientID: caller,
		DigitalEstateID: estate, EngineInstanceID: "ei_synthetic01", MarketID: "market_ug",
		ProvenanceKind: "ESTATE_CLIENT_BINDING", ProvenanceReference: binding, Outcome: "ATTESTED",
		CommerceProfileID: successor, ProfileRevision: 1, CommerceModel: "B2C", CandidateCount: 1, ResolvedAt: now,
		ExpiresAt: &expires, CorrelationID: domain.NewUUIDv7()}
	if err := db.RecordEstateCommerceProfileResolution(ctx, attested); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordEstateCommerceProfileResolution(ctx, attested); err == nil {
		t.Fatal("an attestation id was recorded twice")
	}
	denial := EstateCommerceProfileResolution{CallerClientID: caller, DigitalEstateID: estate,
		EngineInstanceID: "ei_synthetic01", MarketID: "market_ug", ProvenanceKind: "ESTATE_CLIENT_BINDING",
		ProvenanceReference: binding, Outcome: "PROFILE_AMBIGUOUS", CandidateCount: 2, ResolvedAt: now}
	if err := db.RecordEstateCommerceProfileResolution(ctx, denial); err != nil {
		t.Fatal(err)
	}
	unnamed := attested
	unnamed.AttestationID, unnamed.CommerceProfileID = domain.NewUUIDv7(), ""
	if err := db.RecordEstateCommerceProfileResolution(ctx, unnamed); err == nil {
		t.Fatal("an attestation naming no profile was recorded")
	}
	naming := denial
	naming.CommerceProfileID, naming.ProfileRevision, naming.CommerceModel = successor, 1, "B2C"
	if err := db.RecordEstateCommerceProfileResolution(ctx, naming); err == nil {
		t.Fatal("a denial naming a profile was recorded")
	}
	rejects("an evidence update", `UPDATE registry.estate_commerce_profile_resolution SET outcome = 'PROFILE_NOT_ACTIVE' WHERE digital_estate_id = $1`, estate)
	rejects("an evidence delete", `DELETE FROM registry.estate_commerce_profile_resolution WHERE digital_estate_id = $1`, estate)
}
