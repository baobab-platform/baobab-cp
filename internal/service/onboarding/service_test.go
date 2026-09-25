package onboarding_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ADR-BCP-017 sections 22-24, 39, 41, 44, 46 against real PostgreSQL: an
// APPROVED decision reaches provisioning only through an explicit,
// separately authorised TenantOnboardingRequest.

type env struct {
	ctx   context.Context
	repo  *repository.PostgresRepository
	admin *pgxpool.Pool
	svc   *onboarding.Service
	at    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := repository.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	return &env{ctx: ctx, repo: repo, admin: admin, at: at,
		svc: &onboarding.Service{Repo: repo, Admissions: repo, Now: func() time.Time { return at }}}
}

func (e *env) principal(t *testing.T) onboarding.Actor {
	t.Helper()
	var id string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO identity.principal (actor_type) VALUES ('human') RETURNING principal_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return onboarding.Actor{PrincipalID: id, Audit: repository.AuditActor{ActorID: id, ActorType: "human", CorrelationID: domain.NewUUIDv7()}}
}

type admission struct {
	applicant, decider onboarding.Actor
	decisionID         string
}

// decided records an APPROVED (or REJECTED) application and its decision.
// isolation "" leaves the decision without isolation requirements.
func (e *env) decided(t *testing.T, decision, isolation string) admission {
	t.Helper()
	a := admission{applicant: e.principal(t), decider: e.principal(t)}
	var app, dec string
	ref := fmt.Sprintf("APP-2026-%09d", rand.IntN(1_000_000_000))
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO admission.client_application (reference, status, application_channel,
		applicant_principal_id, created_at, updated_at, submitted_at, closed_at)
		VALUES ($1, $2, 'SELF_SERVICE', $3::uuid, $4, $4, $4, $4) RETURNING client_application_id::text`,
		ref, decision, a.applicant.PrincipalID, e.at).Scan(&app); err != nil {
		t.Fatal(err)
	}
	var err error
	if decision == "APPROVED" {
		err = e.admin.QueryRow(e.ctx, `INSERT INTO admission.admission_decision (client_application_id, decision, reason, decided_by,
			decided_at, approved_subscription_type, approved_market_scope, approved_product_requirements,
			approved_isolation_requirements, evidence_references)
			VALUES ($1::uuid, 'APPROVED', 'verified', $2::uuid, $3, 'COMMERCIAL', '{UG,KE}', '{b2b-trade}', NULLIF($4, ''), '{evd_x}')
			RETURNING admission_decision_id::text`, app, a.decider.PrincipalID, e.at, isolation).Scan(&dec)
	} else {
		err = e.admin.QueryRow(e.ctx, `INSERT INTO admission.admission_decision (client_application_id, decision, reason, decided_by, decided_at)
			VALUES ($1::uuid, 'REJECTED', 'no', $2::uuid, $3) RETURNING admission_decision_id::text`, app, a.decider.PrincipalID, e.at).Scan(&dec)
	}
	if err != nil {
		t.Fatal(err)
	}
	a.decisionID, _ = domain.FormatResourceID(domain.AdmissionDecisionIDPrefix, dec)
	return a
}

func (e *env) tenant(t *testing.T, isolation, region string) string {
	t.Helper()
	id := "tn_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:24]
	le := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12])
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1) ON CONFLICT DO NOTHING`, le); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO tenants(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region)
		VALUES ($1, $2, 'T', $3, $4)`, id, le, isolation, region); err != nil {
		t.Fatal(err)
	}
	return id
}

func body(decision, isolation string) []byte {
	b := map[string]any{"admission_decision_id": decision, "display_name": "Kilima Traders", "residency_region": "af-south-1",
		"reason": "Onboard the approved client."}
	if isolation != "" {
		b["isolation_strategy"] = isolation
	}
	raw, _ := json.Marshal(b)
	return raw
}

var reason = []byte(`{"reason":"checked"}`)

func TestOnboardingRequestComesFromAnApprovedDecision(t *testing.T) {
	e := newEnv(t)
	requester := e.principal(t)

	rejected := e.decided(t, "REJECTED", "")
	if _, _, err := e.svc.Request(e.ctx, requester, body(rejected.decisionID, "row_level_security")); !errors.Is(err, onboarding.ErrDecisionNotApproved) {
		t.Fatalf("a REJECTED decision is never onboarded: %v", err)
	}
	if _, _, err := e.svc.Request(e.ctx, requester, body("adm_0000000000000000000000000000000f", "row_level_security")); !errors.Is(err, onboarding.ErrDecisionMissing) {
		t.Fatalf("unknown decision: %v", err)
	}
	a := e.decided(t, "APPROVED", "schema_per_tenant")
	for who, actor := range map[string]onboarding.Actor{"applicant": a.applicant, "decider": a.decider} {
		if _, _, err := e.svc.Request(e.ctx, actor, body(a.decisionID, "")); !errors.Is(err, onboarding.ErrSeparationOfDuties) {
			t.Fatalf("the %s cannot request onboarding: %v", who, err)
		}
	}
	if _, _, err := e.svc.Request(e.ctx, requester, body(a.decisionID, "row_level_security")); !errors.Is(err, onboarding.ErrIsolationDecided) {
		t.Fatalf("the request cannot override the decision's isolation: %v", err)
	}
	undecided := e.decided(t, "APPROVED", "")
	if _, _, err := e.svc.Request(e.ctx, requester, body(undecided.decisionID, "")); !errors.Is(err, onboarding.ErrIsolationRequired) {
		t.Fatalf("an isolation strategy is required: %v", err)
	}
	if _, _, err := e.svc.Request(e.ctx, requester, []byte(`{"admission_decision_id":"`+a.decisionID+`","display_name":"X",
		"residency_region":"af-south-1","reason":"x","subscription_type":"INTERNAL"}`)); err == nil {
		t.Fatal("a caller can never set the classification")
	}

	req, created, err := e.svc.Request(e.ctx, requester, body(a.decisionID, ""))
	ds := req.DesiredState
	if err != nil || !created || req.Status != domain.OnboardingRequested || ds.SubscriptionType != domain.SubscriptionCommercial ||
		strings.Join(ds.MarketScope, ",") != "UG,KE" || strings.Join(ds.ProductRequirements, ",") != "b2b-trade" ||
		ds.IsolationStrategy != "schema_per_tenant" || req.RequestedBy != requester.PrincipalID || req.CorrelationID == "" {
		t.Fatalf("the desired state comes from the decision: %v %v %+v", err, created, req)
	}
	again, created, err := e.svc.Request(e.ctx, e.principal(t), body(a.decisionID, ""))
	if err != nil || created || again.ID != req.ID {
		t.Fatalf("one live request per decision; a replay returns it: %v %v %s", err, created, again.ID)
	}
}

func TestOnboardingLifecycleAndSeparationOfDuties(t *testing.T) {
	e := newEnv(t)
	a := e.decided(t, "APPROVED", "row_level_security")
	requester, authoriser := e.principal(t), e.principal(t)
	req, _, err := e.svc.Request(e.ctx, requester, body(a.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	match := e.tenant(t, "row_level_security", "af-south-1")
	if _, err := e.svc.Fulfil(e.ctx, requester, req.ID, []byte(`{"tenant_id":"`+match+`"}`)); !errors.Is(err, onboarding.ErrTransition) {
		t.Fatalf("approval activates nothing: an unauthorised request is never fulfilled: %v", err)
	}
	for who, actor := range map[string]onboarding.Actor{"requester": requester, "applicant": a.applicant} {
		if _, err := e.svc.Authorise(e.ctx, actor, req.ID, reason); !errors.Is(err, onboarding.ErrSeparationOfDuties) {
			t.Fatalf("the %s cannot authorise: %v", who, err)
		}
	}
	authorised, err := e.svc.Authorise(e.ctx, authoriser, req.ID, reason)
	if err != nil || authorised.Status != domain.OnboardingAuthorised || authorised.AuthorisedBy != authoriser.PrincipalID {
		t.Fatalf("authorise: %v %+v", err, authorised)
	}
	if _, err := e.svc.Authorise(e.ctx, e.principal(t), req.ID, reason); !errors.Is(err, onboarding.ErrTransition) {
		t.Fatalf("authorising twice: %v", err)
	}
	wrong := e.tenant(t, "row_level_security", "eu-west-1")
	if _, err := e.svc.Fulfil(e.ctx, requester, req.ID, []byte(`{"tenant_id":"`+wrong+`"}`)); !errors.Is(err, onboarding.ErrDesiredStateMismatch) {
		t.Fatalf("a tenant outside the desired state does not fulfil the request: %v", err)
	}
	if _, err := e.svc.Fulfil(e.ctx, requester, req.ID, []byte(`{"tenant_id":"tn_doesnotexist000000"}`)); !errors.Is(err, onboarding.ErrTenantNotFound) {
		t.Fatalf("unknown tenant: %v", err)
	}
	fulfilled, err := e.svc.Fulfil(e.ctx, requester, req.ID, []byte(`{"tenant_id":"`+match+`"}`))
	if err != nil || fulfilled.Status != domain.OnboardingFulfilled || fulfilled.TenantID != match {
		t.Fatalf("fulfil: %v %+v", err, fulfilled)
	}
	if _, err := e.svc.Cancel(e.ctx, requester, req.ID, reason); !errors.Is(err, onboarding.ErrTransition) {
		t.Fatalf("FULFILLED is final: %v", err)
	}

	// A tenant is produced by one request only.
	b := e.decided(t, "APPROVED", "row_level_security")
	other, _, err := e.svc.Request(e.ctx, requester, body(b.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authorise(e.ctx, authoriser, other.ID, reason); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Fulfil(e.ctx, requester, other.ID, []byte(`{"tenant_id":"`+match+`"}`)); !errors.Is(err, onboarding.ErrTenantAlreadyOnboarded) {
		t.Fatalf("a tenant already produced by another request: %v", err)
	}

	// Cancelling frees the decision for a new request and rewrites nothing.
	cancelled, err := e.svc.Cancel(e.ctx, requester, other.ID, []byte(`{"reason":"wrong region"}`))
	if err != nil || cancelled.Status != domain.OnboardingCancelled {
		t.Fatalf("cancel: %v %+v", err, cancelled)
	}
	if next, created, err := e.svc.Request(e.ctx, requester, body(b.decisionID, "")); err != nil || !created || next.ID == other.ID {
		t.Fatalf("a cancelled request frees its decision: %v %v", err, created)
	}
	dec, err := e.repo.GetAdmissionDecisionByID(e.ctx, b.decisionID)
	if err != nil || dec.Decision != domain.DecisionApproved {
		t.Fatalf("the decision is never rewritten (section 44): %v %+v", err, dec)
	}

	// The database refuses edits to the request itself, skipped steps and deletes.
	row, _ := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, req.ID)
	for _, stmt := range []string{
		`UPDATE admission.tenant_onboarding_request SET display_name = 'Renamed' WHERE tenant_onboarding_request_id = $1::uuid`,
		`UPDATE admission.tenant_onboarding_request SET status = 'REQUESTED', authorised_by = NULL, authorised_at = NULL,
			tenant_id = NULL, fulfilled_at = NULL WHERE tenant_onboarding_request_id = $1::uuid`,
		`DELETE FROM admission.tenant_onboarding_request WHERE tenant_onboarding_request_id = $1::uuid`,
	} {
		if _, err := e.admin.Exec(e.ctx, stmt, row); err == nil {
			t.Fatalf("the database refused nothing: %s", stmt)
		}
	}
	list, err := e.svc.List(e.ctx, domain.OnboardingFulfilled, 200)
	if err != nil || !containsRequest(list, req.ID) {
		t.Fatalf("list by status: %v", err)
	}
}

func containsRequest(list []domain.TenantOnboardingRequest, id string) bool {
	for _, r := range list {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestOnboardingEventsConformToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	e := newEnv(t)
	a := e.decided(t, "APPROVED", "row_level_security")
	requester, authoriser := e.principal(t), e.principal(t)
	req, _, err := e.svc.Request(e.ctx, requester, body(a.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authorise(e.ctx, authoriser, req.ID, reason); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Fulfil(e.ctx, requester, req.ID, []byte(`{"tenant_id":"`+e.tenant(t, "row_level_security", "af-south-1")+`"}`)); err != nil {
		t.Fatal(err)
	}
	b := e.decided(t, "APPROVED", "row_level_security")
	other, _, err := e.svc.Request(e.ctx, requester, body(b.decisionID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Cancel(e.ctx, requester, other.ID, reason); err != nil {
		t.Fatal(err)
	}
	envelopeSchema := contracttest.CompileSchema(t, dir, "events/v1/envelope.schema.json")
	rows, err := e.admin.Query(e.ctx, `SELECT event_type, payload FROM messaging.outbox WHERE correlation_id::text = ANY($1)`,
		[]string{req.CorrelationID, other.CorrelationID})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var typ string
		var payload []byte
		if err := rows.Scan(&typ, &payload); err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		contracttest.ValidateJSON(t, envelopeSchema, envelope)
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "admission/v1/events.schema.json#/$defs/"+events.AdmissionPayloadDef(typ)), envelope["data"])
		if strings.Contains(string(payload), "Onboard the approved client") {
			t.Fatalf("event payloads carry identifiers and state only: %s", payload)
		}
		seen[typ] = true
	}
	for _, want := range []string{events.TenantOnboardingRequested, events.TenantOnboardingAuthorised, events.TenantOnboardingFulfilled,
		events.TenantOnboardingCancelled} {
		if !seen[want] {
			t.Errorf("did not emit %s under the request's correlation id", want)
		}
	}
}
