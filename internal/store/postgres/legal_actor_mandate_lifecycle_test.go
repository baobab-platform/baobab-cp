package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func TestLegalActorLifecycleGuardedActivationAndRevocation(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL 17 TEST_DATABASE_URL unavailable")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := domain.NewTenantID()
	entity := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16])
	_, err = db.RegisterTenant(ctx, "la04d-bootstrap-"+domain.NewUUIDv7(),
		basestore.RequestMetadata{ActorID: "test-bootstrap", ActorType: "workload", CorrelationID: domain.NewUUIDv7()},
		domain.RegisterTenant{TenantID: tenant, LegalEntityID: entity,
			Basis: domain.RegistrationBootstrap, BootstrapReason: "synthetic LA04D lifecycle test",
			BootstrapEvidenceReference: "synthetic-evidence", DisplayName: "LA04D test",
			IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var org string
	if err = db.pool.QueryRow(ctx, `SELECT organisation_id::text FROM registry.tenant_organisation_mapping
 WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION'
 AND status='ACTIVE'`, tenant).Scan(&org); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 4)
	for i := range ids {
		if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type)
  VALUES('human') RETURNING principal_id::text`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	maker, checker, operator, revoker := ids[0], ids[1], ids[2], ids[3]
	meta := func(actor string) basestore.RequestMetadata {
		return basestore.RequestMetadata{ActorID: actor, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	_, err = db.pool.Exec(ctx, `UPDATE registry.legal_entity_profile
 SET verification_state='VERIFIED',legal_status='ACTIVE',
 source_authority='synthetic-independent-legal-registry',
 evidence_references='["test/official-legal-registry"]'::jsonb,
 verified_by=$2, verified_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE legal_entity_id=$1`, entity, revoker)
	if err != nil {
		t.Fatal(err)
	}
	intent := legalactor.ProposeCommand{
		TenantID: tenant, OperatingOrganisationID: org, ResponsibleLegalEntityID: entity,
		Roles: []string{"SELLER_OF_RECORD"}, ActivityScope: []string{"B2B_COFFEE_SALE"},
		MarketScope: []string{"ZA"}, EvidenceReferences: []string{"test/governed-mandate"},
		AuthorityBasisReference: "test/mandate-legal-basis",
		EffectiveFrom:           time.Now().UTC().Add(-time.Minute),
	}
	createApproved := func() string {
		t.Helper()
		p, err := db.ProposeOperatingLegalActorMandate(ctx, "proposal-"+domain.NewUUIDv7(), meta(maker), maker, intent)
		if err != nil {
			t.Fatal(err)
		}
		decision := legalactor.DecideCommand{Decision: "APPROVE",
			DecisionBasisReference:          "test/independent-checker",
			EvidenceReferences:              []string{"test/official-legal-registry"},
			LegalActorVerificationReference: "test/official-legal-registry"}
		if _, err = db.DecideOperatingLegalActorMandate(ctx, "decision-"+domain.NewUUIDv7(),
			meta(checker), checker, p.MandateID, decision); err != nil {
			t.Fatal(err)
		}
		return p.MandateID
	}
	activation := legalactor.LifecycleCommand{Action: "ACTIVATE",
		AuthorityBasisReference: "test/third-person-activation", EvidenceReferences: []string{"test/activation-acceptance"}}
	first := createApproved()
	if _, err := db.pool.Exec(ctx, `UPDATE registry.operating_legal_actor_mandate
		SET status='ACTIVE', approved_by=$2::uuid, approved_at=clock_timestamp(),
		legal_actor_verification_reference='test/official-legal-registry'
		WHERE mandate_id=$1::uuid`, first, checker); err == nil {
		t.Fatal("direct SQL activation bypassed transition history and third human operator")
	}
	if _, err = db.TransitionOperatingLegalActorMandate(ctx, "selfactivate-"+domain.NewUUIDv7(),
		meta(checker), checker, first, activation); err == nil {
		t.Fatal("independent checker activated own decision")
	}
	key := "activate-" + domain.NewUUIDv7()
	active, err := db.TransitionOperatingLegalActorMandate(ctx, key, meta(operator), operator, first, activation)
	if err != nil {
		t.Fatalf("third-person activation: %v", err)
	}
	if active.Status != "ACTIVE" || active.MandateID != first {
		t.Fatalf("bad ACTIVE receipt %+v", active)
	}
	replay, err := db.TransitionOperatingLegalActorMandate(ctx, key, meta(operator), operator, first, activation)
	if err != nil || replay.RecordedAt != active.RecordedAt {
		t.Fatalf("replay not converged %+v %v", replay, err)
	}
	resolution, err := db.ResolveOperatingLegalActor(ctx, legalactor.Request{
		TenantID: tenant, OperatingOrganisationID: org, Role: "SELLER_OF_RECORD",
		Activity: "B2B_COFFEE_SALE", Market: "ZA", EffectiveAt: time.Now().UTC()})
	if err != nil || resolution.Outcome != legalactor.Authorized || resolution.MandateID != first {
		t.Fatalf("trusted active mandate resolution failure %+v %v", resolution, err)
	}
	intent.SupersedesMandateID = first
	second := createApproved()
	if _, err = db.TransitionOperatingLegalActorMandate(ctx, "conflict-"+domain.NewUUIDv7(),
		meta(operator), operator, second, activation); err == nil {
		t.Fatal("overlapping scope activated without first mandate's termination")
	}
	if _, err = db.pool.Exec(ctx, `UPDATE registry.operating_legal_actor_mandate
 SET status='REVOKED',revoked_at=clock_timestamp() WHERE mandate_id=$1::uuid`, first); err == nil {
		t.Fatal("direct SQL revocation bypassed governed transition")
	}
	revoke := legalactor.LifecycleCommand{Action: "REVOKE",
		AuthorityBasisReference: "test/emergency-revocation",
		EvidenceReferences:      []string{"test/revocation-case"}}
	revoked, err := db.TransitionOperatingLegalActorMandate(ctx,
		"revoke-"+domain.NewUUIDv7(), meta(revoker), revoker, first, revoke)
	if err != nil || revoked.Status != "REVOKED" {
		t.Fatalf("governed revocation %+v %v", revoked, err)
	}
	after, err := db.ResolveOperatingLegalActor(ctx, legalactor.Request{
		TenantID: tenant, OperatingOrganisationID: org, Role: "SELLER_OF_RECORD",
		Activity: "B2B_COFFEE_SALE", Market: "ZA", EffectiveAt: time.Now().UTC()})
	if err != nil || after.Outcome == legalactor.Authorized {
		t.Fatalf("revoked mandate retained authority %+v %v", after, err)
	}
	if _, err = db.TransitionOperatingLegalActorMandate(ctx, "resurrect-"+domain.NewUUIDv7(),
		meta(operator), operator, first, activation); err == nil {
		t.Fatal("revoked mandate resurrected")
	}
	newActive, err := db.TransitionOperatingLegalActorMandate(ctx,
		"activate-successor-"+domain.NewUUIDv7(), meta(operator), operator, second, activation)
	if err != nil || newActive.Status != "ACTIVE" {
		t.Fatalf("successor after revoke %+v %v", newActive, err)
	}
	// An ACTIVE row is not authorising if its checker-approved verification
	// reference disappears from the independently verified legal profile.
	_, err = db.pool.Exec(ctx, `UPDATE registry.legal_entity_profile
		SET evidence_references='["test/retracted"]'::jsonb
		WHERE legal_entity_id=$1`, entity)
	if err != nil {
		t.Fatal(err)
	}
	afterWithdrawal, err := db.ResolveOperatingLegalActor(ctx, legalactor.Request{
		TenantID: tenant, OperatingOrganisationID: org, Role: "SELLER_OF_RECORD",
		Activity: "B2B_COFFEE_SALE", Market: "ZA", EffectiveAt: time.Now().UTC(),
	})
	if err != nil || afterWithdrawal.Outcome == legalactor.Authorized {
		t.Fatalf("removed verification evidence retained actor authority: %+v %v", afterWithdrawal, err)
	}
	_, err = db.pool.Exec(ctx, `UPDATE registry.legal_entity_profile
		SET evidence_references='["test/official-legal-registry"]'::jsonb
		WHERE legal_entity_id=$1`, entity)
	if err != nil {
		t.Fatal(err)
	}
	suspend := legalactor.LifecycleCommand{Action: "SUSPEND",
		AuthorityBasisReference: "test/immediate-stop", EvidenceReferences: []string{"test/stop-case"}}
	suspended, err := db.TransitionOperatingLegalActorMandate(ctx, "suspend-"+domain.NewUUIDv7(),
		meta(revoker), revoker, second, suspend)
	if err != nil || suspended.Status != "SUSPENDED" {
		t.Fatalf("suspension %+v %v", suspended, err)
	}
	if _, err = db.TransitionOperatingLegalActorMandate(ctx, "resume-"+domain.NewUUIDv7(),
		meta(operator), operator, second, activation); err == nil {
		t.Fatal("suspended mandate resurrected; requires new approved proposal")
	}
	var eventCount int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM messaging.outbox
 WHERE aggregate_type='legal-actor-mandate' AND aggregate_id=$1`, first).
		Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 4 {
		t.Fatalf("expected proposal+decision+activate+revoke events; got %d", eventCount)
	}
}
