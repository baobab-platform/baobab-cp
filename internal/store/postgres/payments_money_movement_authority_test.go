package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// P0-04B Payments: CP retains every money-movement decision immutably, a
// payout can only ever be recorded as not activated, and only a live
// baobab-payments instance bound in the tenant's own scope counts as bound.
func TestPaymentsMoneyMovementDecisionHistoryAndBinding(t *testing.T) {
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
	tenant := "tn_pay" + suffix
	fingerprint := strings.Repeat("ab", 32)
	authorized := PaymentsMoneyMovementDecision{
		ClientID: "baobab-payments-legal-actor-assessor-staging", ContextID: domain.NewUUIDv7(),
		EngineInstanceID: "ei_pay" + suffix, OperationClass: "payments.capture.execute",
		OperationReference: "payments/capture/pay_1/key", Attempt: 2, InstructionFingerprint: fingerprint,
		Outcome: "AUTHORIZED", TenantID: tenant, OperatingOrganisationID: domain.NewUUIDv7(), Market: "UG",
		LegalActorRole: "PAYMENT_BENEFICIARY", Activity: "PAYMENT_COLLECTION", CapabilityKey: "payment.payment.capture",
		MandateID: domain.NewUUIDv7(), ResponsibleLegalEntityID: "SYNTHETIC-LE-01",
		EvidenceReferences: []string{"synthetic:evidence"}, MandateEffectiveFrom: &from,
		EvaluatedAt: now, ExpiresAt: &until, PolicyReference: "ADR-SHARED-035/payments-money-movement-authority:v1",
	}
	id, err := db.RecordPaymentsMoneyMovementDecision(ctx, authorized)
	if err != nil || !domain.IsUUID(id) {
		t.Fatalf("record authorized decision: %q %v", id, err)
	}
	for name, mutate := range map[string]func(*PaymentsMoneyMovementDecision){
		"lease-over-30s": func(d *PaymentsMoneyMovementDecision) {
			long := now.Add(31 * time.Second)
			d.ExpiresAt = &long
		},
		"authorized-without-mandate":    func(d *PaymentsMoneyMovementDecision) { d.MandateID = "" },
		"authorized-without-capability": func(d *PaymentsMoneyMovementDecision) { d.CapabilityKey = "" },
		"authorized-without-evidence":   func(d *PaymentsMoneyMovementDecision) { d.EvidenceReferences = nil },
		"denied-discloses-mandate":      func(d *PaymentsMoneyMovementDecision) { d.Outcome = "AMBIGUOUS" },
		"erp-class":                     func(d *PaymentsMoneyMovementDecision) { d.OperationClass = "erp.payment.record" },
		"bad-fingerprint":               func(d *PaymentsMoneyMovementDecision) { d.InstructionFingerprint = strings.Repeat("A", 64) },
		"attempt-zero":                  func(d *PaymentsMoneyMovementDecision) { d.Attempt = 0 },
		"authorized-payout": func(d *PaymentsMoneyMovementDecision) {
			d.OperationClass = "payments.payout.execute"
			d.Activity = "BENEFICIARY_DISBURSEMENT"
		},
		"not-activated-collection": func(d *PaymentsMoneyMovementDecision) {
			*d = PaymentsMoneyMovementDecision{ClientID: d.ClientID, ContextID: d.ContextID,
				EngineInstanceID: d.EngineInstanceID, OperationClass: "payments.capture.execute",
				OperationReference: d.OperationReference, Attempt: 1, InstructionFingerprint: fingerprint,
				Outcome: "OPERATION_NOT_ACTIVATED", Activity: "PAYMENT_COLLECTION", EvaluatedAt: now,
				PolicyReference: d.PolicyReference}
		},
		"unknown-outcome": func(d *PaymentsMoneyMovementDecision) { d.Outcome = "PROVIDER_RESTRICTED" },
	} {
		bad := authorized
		mutate(&bad)
		if _, err := db.RecordPaymentsMoneyMovementDecision(ctx, bad); err == nil {
			t.Errorf("%s: invalid decision was recorded", name)
		}
	}
	payout := PaymentsMoneyMovementDecision{
		ClientID: authorized.ClientID, ContextID: authorized.ContextID, EngineInstanceID: authorized.EngineInstanceID,
		OperationClass: "payments.payout.execute", OperationReference: "payments/payout/1/key", Attempt: 1,
		InstructionFingerprint: fingerprint, Outcome: "OPERATION_NOT_ACTIVATED", Activity: "BENEFICIARY_DISBURSEMENT",
		EvaluatedAt: now, PolicyReference: authorized.PolicyReference,
	}
	if _, err := db.RecordPaymentsMoneyMovementDecision(ctx, payout); err != nil {
		t.Fatalf("record payout refusal: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE registry.payments_money_movement_decision
		SET responsible_legal_entity_id='OTHER-LE' WHERE decision_id=$1::uuid`, id); err == nil {
		t.Fatal("historical decision was rewritten")
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM registry.payments_money_movement_decision
		WHERE decision_id=$1::uuid`, id); err == nil {
		t.Fatal("historical decision was deleted")
	}

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	engine := func(code string) string {
		t.Helper()
		var id string
		if err := db.pool.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code=$1`, code).Scan(&id); err != nil {
			if err := db.pool.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1)
				RETURNING engine_id::text`, code).Scan(&id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	instance := func(engineID string) (string, string) {
		t.Helper()
		var id, key string
		if err := db.pool.QueryRow(ctx, `INSERT INTO topology.engine_instance (engine_id, region, environment, status)
			VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_id::text, engine_instance_key`,
			engineID).Scan(&id, &key); err != nil {
			t.Fatal(err)
		}
		return id, key
	}
	paymentsEngine, erpEngine := engine("baobab-payments"), engine("baobab-erp")
	payID, payKey := instance(paymentsEngine)
	erpID, erpKey := instance(erpEngine)
	var capabilityID, scopeID, provider string
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability (code, name)
		VALUES ($1, 'synthetic payments test') RETURNING capability_id::text`,
		"payment.money-movement-test-"+suffix).Scan(&capabilityID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability_scope (tenant_id)
		VALUES ($1) RETURNING scope_id::text`, tenant).Scan(&scopeID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability_provider
		(provider_key, name, provider_type, engine_id, status)
		VALUES ($1, 'synthetic payments provider', 'BAOBAB_ENGINE', $2::uuid, 'ACTIVE') RETURNING provider_id::text`,
		"payments-money-movement-test-"+suffix+".main", paymentsEngine).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	bound := func(key string) bool {
		t.Helper()
		ok, err := db.PaymentsEngineInstanceBound(ctx, tenant, key, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	var erpCapability string
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability (code, name)
		VALUES ($1, 'synthetic ERP payment recording test') RETURNING capability_id::text`,
		"finance.payment-record-test-"+suffix).Scan(&erpCapability); err != nil {
		t.Fatal(err)
	}
	for capability, i := range map[string]string{capabilityID: payID, erpCapability: erpID} {
		mustExec(`INSERT INTO capability.capability_binding (capability_id, engine_instance_id, scope_id,
			binding_mode, status, contract_version, effective_from, provider_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '30 minutes', $4::uuid)`,
			capability, i, scopeID, provider)
	}
	if bound(erpKey) {
		t.Fatal("an ERP instance counted as a Payments engine instance")
	}
	if !bound(payKey) {
		t.Fatal("live Payments binding not recognised")
	}
	mustExec(`UPDATE topology.engine_instance SET status='RETIRED' WHERE engine_instance_id=$1::uuid`, payID)
	if bound(payKey) {
		t.Fatal("a retired engine instance counted as bound")
	}
}
