package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// P0-04B ERP: CP retains every posting-authority decision immutably, and an
// engine instance counts as bound only through a live ACTIVE binding in a
// scope the tenant owns.
func TestErpPostingAuthorityDecisionHistoryAndBinding(t *testing.T) {
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
	now := time.Now().UTC()
	until := now.Add(30 * time.Second)
	from := now.Add(-time.Hour)
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]
	tenant := "tn_erp" + suffix
	authorized := ErpPostingAuthorityDecision{
		ClientID: "baobab-erp-legal-actor-assessor-staging", ContextID: domain.NewUUIDv7(),
		EngineInstanceID: "ei_erp" + suffix, OperationClass: "erp.gl-journal.post",
		PlacementID: domain.NewUUIDv7(), PlacementVersion: 1, OperationReference: "erp/gl/1/complete/trx",
		Outcome: "AUTHORIZED", TenantID: tenant, OperatingOrganisationID: domain.NewUUIDv7(), Market: "UG",
		LegalActorRole: "ACCOUNTING_ENTITY", Activity: "GENERAL_LEDGER_POSTING",
		MandateID: domain.NewUUIDv7(), ResponsibleLegalEntityID: "SYNTHETIC-LE-01",
		EvidenceReferences: []string{"synthetic:evidence"}, MandateEffectiveFrom: &from,
		EvaluatedAt: now, ExpiresAt: &until, PolicyReference: "ADR-SHARED-034/erp-posting-authority:v1",
	}
	id, err := db.RecordErpPostingAuthorityDecision(ctx, authorized)
	if err != nil || !domain.IsUUID(id) {
		t.Fatalf("record authorized decision: %q %v", id, err)
	}
	for name, mutate := range map[string]func(*ErpPostingAuthorityDecision){
		"lease-over-30s": func(d *ErpPostingAuthorityDecision) {
			long := now.Add(31 * time.Second)
			d.ExpiresAt = &long
		},
		"authorized-without-mandate":  func(d *ErpPostingAuthorityDecision) { d.MandateID = "" },
		"authorized-without-evidence": func(d *ErpPostingAuthorityDecision) { d.EvidenceReferences = nil },
		"denied-discloses-mandate":    func(d *ErpPostingAuthorityDecision) { d.Outcome = "AMBIGUOUS" },
		"vendor-instance":             func(d *ErpPostingAuthorityDecision) { d.EngineInstanceID = "AD_Client_11" },
		"unknown-outcome":             func(d *ErpPostingAuthorityDecision) { d.Outcome = "PROVIDER_RESTRICTED" },
	} {
		bad := authorized
		mutate(&bad)
		if _, err := db.RecordErpPostingAuthorityDecision(ctx, bad); err == nil {
			t.Errorf("%s: invalid decision was recorded", name)
		}
	}
	denied := ErpPostingAuthorityDecision{
		ClientID: authorized.ClientID, ContextID: authorized.ContextID, EngineInstanceID: authorized.EngineInstanceID,
		OperationClass: authorized.OperationClass, PlacementID: authorized.PlacementID, PlacementVersion: 1,
		OperationReference: authorized.OperationReference, Outcome: "ENGINE_INSTANCE_NOT_BOUND",
		TenantID: tenant, EvaluatedAt: now, PolicyReference: authorized.PolicyReference,
	}
	if _, err := db.RecordErpPostingAuthorityDecision(ctx, denied); err != nil {
		t.Fatalf("record denial: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE registry.erp_posting_authority_decision
		SET responsible_legal_entity_id='OTHER-LE' WHERE decision_id=$1::uuid`, id); err == nil {
		t.Fatal("historical decision was rewritten")
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM registry.erp_posting_authority_decision
		WHERE decision_id=$1::uuid`, id); err == nil {
		t.Fatal("historical decision was deleted")
	}

	// Engine-instance binding: only a live ERP instance actively bound in the
	// tenant's own scope counts.
	var engineID, instanceID, instanceKey, capabilityID, scopeID, otherScope string
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.pool.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code='baobab-erp'`).Scan(&engineID); err != nil {
		if err := db.pool.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ('baobab-erp', 'Baobab ERP')
			RETURNING engine_id::text`).Scan(&engineID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO topology.engine_instance (engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_id::text, engine_instance_key`,
		engineID).Scan(&instanceID, &instanceKey); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability (code, name)
		VALUES ($1, 'synthetic ERP posting test') RETURNING capability_id::text`,
		"finance.erp-posting-test-"+suffix).Scan(&capabilityID); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		tenant string
		into   *string
	}{{tenant, &scopeID}, {"tn_other" + suffix, &otherScope}} {
		if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability_scope (tenant_id)
			VALUES ($1) RETURNING scope_id::text`, s.tenant).Scan(s.into); err != nil {
			t.Fatal(err)
		}
	}
	var provider string
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability_provider
		(provider_key, name, provider_type, engine_id, status)
		VALUES ($1, 'synthetic ERP provider', 'BAOBAB_ENGINE', $2::uuid, 'ACTIVE') RETURNING provider_id::text`,
		"erp-posting-test-"+suffix+".main", engineID).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	bound := func(tenantID string) bool {
		t.Helper()
		ok, err := db.ErpEngineInstanceBound(ctx, tenantID, instanceKey, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if bound(tenant) {
		t.Fatal("an unbound instance counted as bound")
	}
	mustExec(`INSERT INTO capability.capability_binding (capability_id, engine_instance_id, scope_id,
		binding_mode, status, contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 hour', $4::uuid)`,
		capabilityID, instanceID, otherScope, provider)
	if bound(tenant) {
		t.Fatal("another tenant's binding counted as bound")
	}
	mustExec(`INSERT INTO capability.capability_binding (capability_id, engine_instance_id, scope_id,
		binding_mode, status, contract_version, effective_from, effective_to, provider_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '2 hours', now() - interval '1 hour', $4::uuid)`,
		capabilityID, instanceID, scopeID, provider)
	if bound(tenant) {
		t.Fatal("an ended binding counted as bound")
	}
	mustExec(`INSERT INTO capability.capability_binding (capability_id, engine_instance_id, scope_id,
		binding_mode, status, contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '30 minutes', $4::uuid)`,
		capabilityID, instanceID, scopeID, provider)
	if !bound(tenant) {
		t.Fatal("live tenant binding not recognised")
	}
	mustExec(`UPDATE topology.engine_instance SET status='RETIRED' WHERE engine_instance_id=$1::uuid`, instanceID)
	if bound(tenant) {
		t.Fatal("a retired engine instance counted as bound")
	}
}
