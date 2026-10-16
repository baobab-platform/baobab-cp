package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

// P0-04B Trade-0 (ADR-SHARED-039): CP owns the Seller Operating Binding
// lifecycle. Maker, checker and operator are three distinct humans, every
// status change needs its own transition row, activation revalidates the
// seller, a successor supersedes its predecessor atomically, terminal states
// never resurrect and history is immutable.
func TestSellerOperatingBindingLifecycleAndDecisionHistory(t *testing.T) {
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
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]
	tenant := domain.NewTenantID()
	entity := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16])
	if _, err := db.RegisterTenant(ctx, "trade0-bootstrap-"+domain.NewUUIDv7(),
		basestore.RequestMetadata{ActorID: "test-bootstrap", ActorType: "workload", CorrelationID: domain.NewUUIDv7()},
		domain.RegisterTenant{TenantID: tenant, LegalEntityID: entity, Basis: domain.RegistrationBootstrap,
			BootstrapReason: "synthetic Trade-0 binding test", BootstrapEvidenceReference: "synthetic-evidence",
			DisplayName: "Trade-0 test", IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1"}, nil); err != nil {
		t.Fatal(err)
	}
	var org string
	if err := db.pool.QueryRow(ctx, `SELECT organisation_id::text FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION' AND status='ACTIVE'`, tenant).Scan(&org); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 4)
	for i := range ids {
		if err := db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type) VALUES('human')
			RETURNING principal_id::text`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	maker, checker, operator, verifier := ids[0], ids[1], ids[2], ids[3]
	meta := func(actor string) basestore.RequestMetadata {
		return basestore.RequestMetadata{ActorID: actor, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}

	// A live baobab-trade instance bound in the tenant's own scope.
	var engineID, instanceID, instanceKey, capabilityID, scopeID, provider string
	if err := db.pool.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code='baobab-trade'`).Scan(&engineID); err != nil {
		if err := db.pool.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ('baobab-trade', 'Baobab Trade')
			RETURNING engine_id::text`).Scan(&engineID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO topology.engine_instance (engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_id::text, engine_instance_key`,
		engineID).Scan(&instanceID, &instanceKey); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability (code, name) VALUES ($1, 'synthetic Trade test')
		RETURNING capability_id::text`, "commerce.trade0-test-"+suffix).Scan(&capabilityID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability_scope (tenant_id) VALUES ($1)
		RETURNING scope_id::text`, tenant).Scan(&scopeID); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `INSERT INTO capability.capability_provider (provider_key, name, provider_type, engine_id, status)
		VALUES ($1, 'synthetic Trade provider', 'BAOBAB_ENGINE', $2::uuid, 'ACTIVE') RETURNING provider_id::text`,
		"trade0-test-"+suffix+".main", engineID).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.TradeEngineInstanceBound(ctx, tenant, instanceKey, time.Now().UTC()); err != nil || ok {
		t.Fatalf("an unbound Trade instance counted as bound: %v %v", ok, err)
	}
	mustExec(`INSERT INTO capability.capability_binding (capability_id, engine_instance_id, scope_id,
		binding_mode, status, contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 hour', $4::uuid)`,
		capabilityID, instanceID, scopeID, provider)
	if ok, err := db.TradeEngineInstanceBound(ctx, tenant, instanceKey, time.Now().UTC()); err != nil || !ok {
		t.Fatalf("a live Trade instance binding was not recognised: %v %v", ok, err)
	}

	attest := func(digest string) TradeNativeConfigurationAttestation {
		t.Helper()
		a, err := db.RecordTradeNativeConfigurationAttestation(ctx, TradeNativeConfigurationAttestation{
			ClientID: "baobab-trade-legal-actor-assessor-staging", ContextID: domain.NewUUIDv7(), TenantID: tenant,
			OperatingOrganisationID: org, Market: "UG", EngineInstanceID: instanceKey,
			NativeConfigurationReference: "trade:seller-config/" + suffix, NativeConfigurationDigest: digest,
			CurrencyScope: []string{"UGX"}, AttestedAt: time.Now().UTC().Add(-time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	first := attest(strings.Repeat("a", 64))
	proposal := ProposeSellerOperatingBinding{AttestationID: first.AttestationID, ResponsibleLegalEntityID: entity,
		EffectiveFrom: time.Now().UTC().Add(-time.Minute), EvidenceReferences: []string{"test/binding-evidence"}}
	key := "propose-" + domain.NewUUIDv7()
	v1, err := db.ProposeOperatingBinding(ctx, key, meta(maker), maker, proposal)
	if err != nil || v1.Status != "PENDING_APPROVAL" || v1.BindingVersion != 1 || v1.TradeEngineInstanceID != instanceKey ||
		v1.NativeConfigurationDigest != first.NativeConfigurationDigest {
		t.Fatalf("proposal: %+v %v", v1, err)
	}
	if replay, err := db.ProposeOperatingBinding(ctx, key, meta(maker), maker, proposal); err != nil || replay.BindingID != v1.BindingID {
		t.Fatalf("replay did not converge: %+v %v", replay, err)
	}
	if _, err := db.ProposeOperatingBinding(ctx, key, meta(checker), checker, proposal); !errors.Is(err, basestore.ErrIdempotencyConflict) {
		t.Fatalf("a different principal reused the key: %v", err)
	}
	// Direct SQL can neither skip the lifecycle nor rewrite scope.
	for _, sql := range []string{
		`UPDATE registry.seller_operating_binding SET status='ACTIVE', revision=revision+1 WHERE binding_id=$1::uuid`,
		`UPDATE registry.seller_operating_binding SET market='KE', revision=revision+1 WHERE binding_id=$1::uuid`,
		`DELETE FROM registry.seller_operating_binding WHERE binding_id=$1::uuid`,
	} {
		if _, err := db.pool.Exec(ctx, sql, v1.BindingID); err == nil {
			t.Fatalf("direct SQL bypassed the binding guard: %s", sql)
		}
	}
	step := func(actor, transition, expected string) (SellerOperatingBinding, error) {
		return db.TransitionOperatingBinding(ctx, transition+"-"+domain.NewUUIDv7(), meta(actor), actor,
			TransitionSellerOperatingBinding{BindingID: v1.BindingID, BindingVersion: 1, Transition: transition,
				ExpectedStatus: expected, Reason: "synthetic " + transition})
	}
	if _, err := step(maker, "APPROVE", "PENDING_APPROVAL"); !errors.Is(err, ErrSellerBindingConflict) {
		t.Fatalf("the maker approved its own proposal: %v", err)
	}
	approved, err := step(checker, "APPROVE", "PENDING_APPROVAL")
	if err != nil || approved.Status != "APPROVED" || approved.ApprovedBy != checker {
		t.Fatalf("checker approval: %+v %v", approved, err)
	}
	for _, actor := range []string{maker, checker} {
		if _, err := step(actor, "ACTIVATE", "APPROVED"); !errors.Is(err, ErrSellerBindingConflict) {
			t.Fatalf("maker or checker activated: %v", err)
		}
	}
	if _, err := step(operator, "ACTIVATE", "PENDING_APPROVAL"); !errors.Is(err, ErrSellerBindingConflict) {
		t.Fatalf("a stale expected status was accepted: %v", err)
	}
	// Revalidation: an unverified legal entity cannot be activated.
	if _, err := step(operator, "ACTIVATE", "APPROVED"); !errors.Is(err, ErrSellerBindingConflict) {
		t.Fatalf("activation succeeded without a verified legal entity: %v", err)
	}
	mustExec(`UPDATE registry.legal_entity_profile SET verification_state='VERIFIED', legal_status='ACTIVE',
		source_authority='synthetic-independent-legal-registry', evidence_references='["test/legal-registry"]'::jsonb,
		verified_by=$2, verified_at=clock_timestamp(), updated_at=clock_timestamp() WHERE legal_entity_id=$1`, entity, verifier)
	active, err := step(operator, "ACTIVATE", "APPROVED")
	if err != nil || active.Status != "ACTIVE" || active.ActivatedBy != operator || active.ActivationID == "" {
		t.Fatalf("third-person activation: %+v %v", active, err)
	}

	// A successor version supersedes its predecessor in one transaction.
	second := attest(strings.Repeat("b", 64))
	v2, err := db.ProposeOperatingBinding(ctx, "propose-"+domain.NewUUIDv7(), meta(maker), maker, ProposeSellerOperatingBinding{
		AttestationID: second.AttestationID, ResponsibleLegalEntityID: entity, EffectiveFrom: time.Now().UTC().Add(-time.Minute),
		SupersedesBindingID: v1.BindingID, SupersedesBindingVersion: 1, EvidenceReferences: []string{"test/binding-evidence-v2"}})
	if err != nil || v2.BindingVersion != 2 || v2.BindingID != v1.BindingID {
		t.Fatalf("successor proposal: %+v %v", v2, err)
	}
	stepV2 := func(actor, transition, expected string) (SellerOperatingBinding, error) {
		return db.TransitionOperatingBinding(ctx, transition+"-"+domain.NewUUIDv7(), meta(actor), actor,
			TransitionSellerOperatingBinding{BindingID: v1.BindingID, BindingVersion: 2, Transition: transition,
				ExpectedStatus: expected, Reason: "synthetic " + transition})
	}
	if _, err := stepV2(checker, "APPROVE", "PENDING_APPROVAL"); err != nil {
		t.Fatal(err)
	}
	if b, err := db.GetSellerOperatingBinding(ctx, v1.BindingID, 1); err != nil || b.Status != "ACTIVE" {
		t.Fatalf("approving a successor must not end its predecessor: %+v %v", b, err)
	}
	activeV2, err := stepV2(operator, "ACTIVATE", "APPROVED")
	if err != nil || activeV2.Status != "ACTIVE" {
		t.Fatalf("successor activation: %+v %v", activeV2, err)
	}
	if b, err := db.GetSellerOperatingBinding(ctx, v1.BindingID, 1); err != nil || b.Status != "SUPERSEDED" {
		t.Fatalf("predecessor not superseded: %+v %v", b, err)
	}
	// Suspend, reinstate with a fresh activation, then revoke: terminal.
	if b, err := stepV2(operator, "SUSPEND", "ACTIVE"); err != nil || b.Status != "SUSPENDED" {
		t.Fatalf("suspend: %+v %v", b, err)
	}
	reinstated, err := stepV2(operator, "ACTIVATE", "SUSPENDED")
	if err != nil || reinstated.Status != "ACTIVE" || reinstated.ActivationID == activeV2.ActivationID {
		t.Fatalf("reinstatement must be a fresh activation: %+v %v", reinstated, err)
	}
	if b, err := stepV2(operator, "REVOKE", "ACTIVE"); err != nil || b.Status != "REVOKED" {
		t.Fatalf("revoke: %+v %v", b, err)
	}
	if _, err := stepV2(operator, "ACTIVATE", "REVOKED"); !errors.Is(err, ErrSellerBindingConflict) {
		t.Fatalf("a revoked binding was resurrected: %v", err)
	}
	for _, sql := range []string{
		`UPDATE registry.trade_native_configuration_attestation SET market='KE' WHERE attestation_id=$1::uuid`,
		`DELETE FROM registry.seller_operating_binding_transition WHERE binding_id=(SELECT binding_id FROM registry.trade_native_configuration_attestation a JOIN registry.seller_operating_binding b USING (attestation_id) WHERE a.attestation_id=$1::uuid LIMIT 1)`,
	} {
		if _, err := db.pool.Exec(ctx, sql, first.AttestationID); err == nil {
			t.Fatalf("history was rewritten: %s", sql)
		}
	}
	var events int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM messaging.outbox
		WHERE aggregate_type='seller-operating-binding' AND aggregate_id LIKE $1`, v1.BindingID+"/%").Scan(&events); err != nil || events < 8 {
		t.Fatalf("lifecycle events not written to the outbox: %d %v", events, err)
	}

	// Decision history: immutable, AUTHORIZED complete, reserved classes never authorised.
	now := time.Now().UTC()
	until := now.Add(30 * time.Second)
	from := now.Add(-time.Hour)
	authorized := TradeOrderAuthorityDecision{
		ClientID: "baobab-trade-legal-actor-assessor-staging", ContextID: domain.NewUUIDv7(), EngineInstanceID: instanceKey,
		OperationClass: "trade.order.commit", OperationReference: "trade/trade.order.commit/cart-1/r1/a1",
		BindingID: v1.BindingID, BindingVersion: 2, NativeConfigurationDigest: second.NativeConfigurationDigest, Currency: "UGX",
		Outcome: "AUTHORIZED", TenantID: tenant, OperatingOrganisationID: org, Market: "UG",
		LegalActorRole: "SELLER_OF_RECORD", Activity: "CHECKOUT_ORDER_COMMITMENT", MandateID: domain.NewUUIDv7(),
		ResponsibleLegalEntityID: entity, EvidenceReferences: []string{"synthetic:evidence"}, MandateEffectiveFrom: &from,
		EvaluatedAt: now, ExpiresAt: &until, PolicyReference: "ADR-SHARED-039/trade-order-authority:v1",
	}
	id, err := db.RecordTradeOrderAuthorityDecision(ctx, authorized)
	if err != nil || !domain.IsUUID(id) {
		t.Fatalf("record authorized decision: %q %v", id, err)
	}
	for name, mutate := range map[string]func(*TradeOrderAuthorityDecision){
		"lease-over-30s":             func(d *TradeOrderAuthorityDecision) { long := now.Add(31 * time.Second); d.ExpiresAt = &long },
		"reserved-class-authorized":  func(d *TradeOrderAuthorityDecision) { d.OperationClass = "trade.payment-refund.request" },
		"active-class-not-activated": func(d *TradeOrderAuthorityDecision) { d.Outcome = "OPERATION_NOT_ACTIVATED"; d.MandateID = "" },
		"wrong-role":                 func(d *TradeOrderAuthorityDecision) { d.LegalActorRole = "PAYMENT_BENEFICIARY" },
		"denied-discloses-mandate":   func(d *TradeOrderAuthorityDecision) { d.Outcome = "BINDING_NOT_ACTIVE" },
		"lowercase-currency":         func(d *TradeOrderAuthorityDecision) { d.Currency = "ugx" },
	} {
		bad := authorized
		mutate(&bad)
		if _, err := db.RecordTradeOrderAuthorityDecision(ctx, bad); err == nil {
			t.Errorf("%s: invalid decision was recorded", name)
		}
	}
	if _, err := db.RecordTradeOrderAuthorityDecision(ctx, TradeOrderAuthorityDecision{
		ClientID: authorized.ClientID, ContextID: authorized.ContextID, EngineInstanceID: instanceKey,
		OperationClass: "trade.payment-refund.request", OperationReference: "trade/refund/1", BindingID: v1.BindingID,
		BindingVersion: 2, NativeConfigurationDigest: second.NativeConfigurationDigest, Currency: "UGX",
		Outcome: "OPERATION_NOT_ACTIVATED", Activity: "PAYMENT_REFUND_INSTRUCTION", EvaluatedAt: now,
		PolicyReference: authorized.PolicyReference}); err != nil {
		t.Fatalf("record reserved-class refusal: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE registry.trade_order_authority_decision SET currency='USD'
		WHERE decision_id=$1::uuid`, id); err == nil {
		t.Fatal("a historical decision was rewritten")
	}
}
