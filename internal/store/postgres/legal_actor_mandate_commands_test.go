package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/legalactor"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

// Exercises the actual PostgreSQL constraints, replay ledger and transactional
// audit/outbox boundary. No real company or legal authority is generated.
func TestLegalActorMandateGovernanceRejectAndReplay(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is needed for LA-04C PostgreSQL proof")
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
	_, err = db.RegisterTenant(ctx, "la04c-bootstrap-"+domain.NewUUIDv7(),
		basestore.RequestMetadata{ActorID: "test-admin", ActorType: "workload", CorrelationID: domain.NewUUIDv7()},
		domain.RegisterTenant{TenantID: tenant, LegalEntityID: entity,
			Basis: domain.RegistrationBootstrap, BootstrapReason: "synthetic mandate governance test",
			BootstrapEvidenceReference: "synthetic-la04c", DisplayName: "LA04C test business",
			IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var org string
	if err := db.pool.QueryRow(ctx, `SELECT organisation_id::text
		FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION' AND status='ACTIVE'`, tenant).Scan(&org); err != nil {
		t.Fatal(err)
	}
	var maker, checker string
	for _, id := range []*string{&maker, &checker} {
		if err := db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type)
			VALUES('human') RETURNING principal_id::text`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	makerMeta := basestore.RequestMetadata{ActorID: maker, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	checkerMeta := basestore.RequestMetadata{ActorID: checker, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	intent := legalactor.ProposeCommand{
		TenantID: tenant, OperatingOrganisationID: org, ResponsibleLegalEntityID: entity,
		Roles: []string{"SELLER_OF_RECORD"}, ActivityScope: []string{"B2B_COFFEE_SALE"},
		MarketScope: []string{"ZA"}, AuthorityBasisReference: "test/basis",
		EvidenceReferences: []string{"test/evidence"}, EffectiveFrom: time.Now().UTC().Add(-time.Minute),
	}
	key := "la04c-propose-" + domain.NewUUIDv7()
	receipt, err := db.ProposeOperatingLegalActorMandate(ctx, key, makerMeta, maker, intent)
	if err != nil {
		t.Fatalf("inert maker intent: %v", err)
	}
	if receipt.MandateID == "" || receipt.Status != "PENDING" || receipt.Command != "PROPOSE" {
		t.Fatalf("unexpected proposal receipt %+v", receipt)
	}
	again, err := db.ProposeOperatingLegalActorMandate(ctx, key, makerMeta, maker, intent)
	if err != nil || again.MandateID != receipt.MandateID {
		t.Fatalf("idempotent retry changed mandate: %+v %v", again, err)
	}
	changed := intent
	changed.MarketScope = []string{"UG"}
	if _, err = db.ProposeOperatingLegalActorMandate(ctx, key, makerMeta, maker, changed); !errors.Is(err, ErrLegalActorMandateConflict) {
		t.Fatalf("same key different scope was allowed: %v", err)
	}
	reject := legalactor.DecideCommand{Decision: "REJECT", DecisionBasisReference: "test/no-incorporation",
		EvidenceReferences: []string{"test/reviewer"}}
	if _, err = db.DecideOperatingLegalActorMandate(ctx, "la04c-self-"+domain.NewUUIDv7(),
		makerMeta, maker, receipt.MandateID, reject); err == nil {
		t.Fatal("maker decided own mandate")
	}
	approve := legalactor.DecideCommand{Decision: "APPROVE", DecisionBasisReference: "test/review",
		EvidenceReferences: []string{"test/reviewer"}, LegalActorVerificationReference: "test/unverified"}
	if _, err = db.DecideOperatingLegalActorMandate(ctx, "la04c-invalid-"+domain.NewUUIDv7(),
		checkerMeta, checker, receipt.MandateID, approve); err == nil {
		t.Fatal("unverified synthetic legal actor was approved")
	}
	dkey := "la04c-reject-" + domain.NewUUIDv7()
	decision, err := db.DecideOperatingLegalActorMandate(ctx, dkey, checkerMeta, checker, receipt.MandateID, reject)
	if err != nil {
		t.Fatalf("independent rejection: %v", err)
	}
	if decision.DecisionID == "" || decision.Status != "PENDING" || decision.Command != "REJECT" {
		t.Fatalf("checker granted legal authority: %+v", decision)
	}
	replay, err := db.DecideOperatingLegalActorMandate(ctx, dkey, checkerMeta, checker, receipt.MandateID, reject)
	if err != nil || replay.DecisionID != decision.DecisionID {
		t.Fatalf("decision retry duplicated record: %+v %v", replay, err)
	}
	var decisions, audit, outbox int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM registry.operating_legal_actor_mandate_decision
		WHERE mandate_id=$1::uuid`, receipt.MandateID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
		WHERE target=$1`, "legal-actor-mandate/"+receipt.MandateID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM messaging.outbox
		WHERE aggregate_type='legal-actor-mandate' AND aggregate_id=$1`, receipt.MandateID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || audit != 2 || outbox != 2 {
		t.Fatalf("expected one independent decision and one audit/outbox per command, got decisions=%d audit=%d outbox=%d", decisions, audit, outbox)
	}
	var status string
	if err = db.pool.QueryRow(ctx, `SELECT status FROM registry.operating_legal_actor_mandate
		WHERE mandate_id=$1::uuid`, receipt.MandateID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" {
		t.Fatalf("decision improperly activated mandate: %s", status)
	}

	// Independent, synthetic verification exercises the APPROVE branch.
	// The approved CHECKER decision STILL cannot make this legal mandate ACTIVE.
	_, err = db.pool.Exec(ctx, `UPDATE registry.legal_entity_profile
		SET verification_state='VERIFIED', legal_status='ACTIVE',
			source_authority='synthetic-external-registry-test',
			evidence_references='["test/official-registry"]'::jsonb,
			verified_by=$2, verified_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE legal_entity_id=$1`, entity, checker)
	if err != nil {
		t.Fatalf("set synthetic verified legal profile: %v", err)
	}
	approvedIntent, err := db.ProposeOperatingLegalActorMandate(ctx,
		"la04c-approved-proposal-"+domain.NewUUIDv7(), makerMeta, maker, intent)
	if err != nil {
		t.Fatalf("create proposal with independently verified legal profile: %v", err)
	}
	approvalCommand := legalactor.DecideCommand{
		Decision: "APPROVE", DecisionBasisReference: "test/independent-review",
		EvidenceReferences: []string{"test/official-registry"},
		LegalActorVerificationReference: "test/official-registry",
	}
	approval, err := db.DecideOperatingLegalActorMandate(ctx,
		"la04c-approved-decision-"+domain.NewUUIDv7(),
		checkerMeta, checker, approvedIntent.MandateID, approvalCommand)
	if err != nil {
		t.Fatalf("approve evidence-backed mandate without activating: %v", err)
	}
	if approval.Command != "APPROVE" || approval.DecisionID == "" || approval.Status != "PENDING" {
		t.Fatalf("approved intent incorrectly became active: %+v", approval)
	}
	if err = db.pool.QueryRow(ctx,
		`SELECT status FROM registry.operating_legal_actor_mandate WHERE mandate_id=$1::uuid`,
		approvedIntent.MandateID).Scan(&status); err != nil || status != "PENDING" {
		t.Fatalf("SQL activation gate violated by checker: %s %v", status, err)
	}
}
