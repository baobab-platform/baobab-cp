package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ADR-BCP-017 client application workflow against real PostgreSQL.
// Assertions are scoped to each test's own records: other tests share the
// database.

type env struct {
	ctx   context.Context
	repo  *repository.PostgresRepository
	admin *pgxpool.Pool
	svc   *application.Service
	elig  *fakeEligibility
	now   time.Time
}

type fakeEligibility struct {
	eligible map[string][]domain.PlatformRelationship
}

func (f *fakeEligibility) InternalEligibilityBasis(_ context.Context, org string, _ time.Time) ([]domain.PlatformRelationship, error) {
	return f.eligible[org], nil
}
func (f *fakeEligibility) Platform() string { return "baobab-platform" }

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
	e := &env{ctx: ctx, repo: repo, admin: admin, elig: &fakeEligibility{eligible: map[string][]domain.PlatformRelationship{}},
		now: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)}
	e.svc = &application.Service{Repo: repo, Eligibility: e.elig, Now: func() time.Time { return e.now }}
	return e
}

func (e *env) principal(t *testing.T) application.Actor {
	t.Helper()
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	if err := e.repo.CreateIdentity(e.ctx, p); err != nil {
		t.Fatal(err)
	}
	return application.Actor{PrincipalID: p.ID, Audit: repository.AuditActor{ActorID: p.ID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}}
}

func (e *env) tick() { e.now = e.now.Add(time.Minute) }

func (e *env) create(t *testing.T, applicant application.Actor, body []byte) domain.ClientApplication {
	t.Helper()
	app, _, err := e.svc.Create(e.ctx, applicant, body, "")
	if err != nil {
		t.Fatal(err)
	}
	return app
}

const completeDraft = `{
	"organisation_profile": {
		"legal_name": "Kilima Fresh Produce Ltd", "organisation_form": "COMPANY", "jurisdiction_of_incorporation": "UG",
		"registration_identifiers": [{"type": "COMPANY_REGISTRATION", "value": "80020001234567", "issuing_jurisdiction": "UG"}],
		"authorised_representative": {"full_name": "Applicant Representative", "role": "Managing Director"}
	},
	"requirements": {"operates_b2b": true, "trades_cross_border": true},
	"requested_markets": [{"country_code": "UG"}, {"country_code": "KE"}],
	"evidence": [{"evidence_type": "REGISTRATION_EVIDENCE", "evidence_reference": "evd_kilima_certificate"}]
}`

// ok returns a checker for a (value, error) call: ok[T](t)(call()).
func ok[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

var (
	okApp      = ok[domain.ClientApplication]
	okApps     = ok[[]domain.ClientApplication]
	okDecision = ok[domain.AdmissionDecision]
)

func (e *env) outbox(t *testing.T, app string) []events.Envelope {
	t.Helper()
	row, err := domain.ParseResourceID(domain.ClientApplicationIDPrefix, app)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := e.admin.Query(e.ctx, `SELECT payload FROM messaging.outbox WHERE aggregate_type = 'client_application'
		AND aggregate_id = $1 ORDER BY aggregate_version`, row)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []events.Envelope
	for rows.Next() {
		var raw []byte
		var env events.Envelope
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		out = append(out, env)
	}
	return out
}

func eventTypes(envs []events.Envelope) []string {
	out := make([]string, len(envs))
	for i, env := range envs {
		out[i] = env.Type
	}
	return out
}

func (e *env) audits(t *testing.T, app string) []string {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT action FROM audit_events WHERE target = $1 ORDER BY occurred_at, audit_id`, "client-application/"+app)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		out = append(out, action)
	}
	return out
}

func invalid(err error, wantApplication bool) bool {
	var inv *application.InvalidError
	return errors.As(err, &inv) && inv.Application == wantApplication
}

// TestClientApplicationApprovalJourney is ADR-BCP-017 sections 5-8 and
// 21-22 end to end: an applicant drafts, edits and submits; a reviewer
// validates, asks for information and reviews; a separate decider approves.
// Every step is audited, the section 40 events are published once, and
// approval creates no tenant.
func TestClientApplicationApprovalJourney(t *testing.T) {
	e := newEnv(t)
	applicant, reviewer, decider := e.principal(t), e.principal(t), e.principal(t)

	app := e.create(t, applicant, []byte(`{"organisation_profile":{"legal_name":"Kilima"}}`))
	if app.Status != domain.ApplicationDraft || app.Channel != domain.ChannelSelfService || app.Version != 1 ||
		app.ApplicantPrincipalID != applicant.PrincipalID || !strings.HasPrefix(app.Reference, "APP-") {
		t.Fatalf("created application: %+v", app)
	}

	// An incomplete draft cannot be submitted.
	if _, err := e.svc.Submit(e.ctx, applicant, app.ID); !invalid(err, true) {
		t.Fatalf("an incomplete draft must not be submitted: %v", err)
	}
	// Editing needs the version it was based on.
	update := func(version int64, body string) string {
		var doc map[string]any
		_ = json.Unmarshal([]byte(body), &doc)
		doc["version"] = version
		raw, _ := json.Marshal(doc)
		return string(raw)
	}
	if _, err := e.svc.Update(e.ctx, applicant, app.ID, []byte(update(9, completeDraft))); !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("a stale version must conflict: %v", err)
	}
	e.tick()
	app = okApp(t)(e.svc.Update(e.ctx, applicant, app.ID, []byte(update(1, completeDraft))))
	if app.Version != 2 || len(app.Evidence) != 1 || !app.Evidence[0].RecordedAt.Equal(e.now) {
		t.Fatalf("updated application: %+v", app)
	}
	e.tick()
	app = okApp(t)(e.svc.Submit(e.ctx, applicant, app.ID))
	if app.Status != domain.ApplicationSubmitted || app.SubmittedAt == nil {
		t.Fatalf("submitted: %+v", app)
	}
	// Submitted content is frozen until the platform asks for more.
	if _, err := e.svc.Update(e.ctx, applicant, app.ID, []byte(update(app.Version, `{"requirements":{}}`))); err == nil {
		t.Fatal("a submitted application must not be edited")
	}

	app = okApp(t)(e.svc.BeginValidation(e.ctx, reviewer, app.ID))
	if app.Status != domain.ApplicationValidating || app.AssignedReviewer != reviewer.PrincipalID {
		t.Fatalf("validating: %+v", app)
	}
	e.tick()
	app = okApp(t)(e.svc.RequestInformation(e.ctx, reviewer, app.ID, []byte(`{"message":"Please provide a tax clearance certificate."}`)))
	if app.Status != domain.ApplicationInformationRequired || len(app.InformationRequests) != 1 {
		t.Fatalf("information required: %+v", app)
	}
	// The applicant edits, then answers; the certificate keeps its own time.
	firstEvidenceAt := app.Evidence[0].RecordedAt
	e.tick()
	withTax := strings.Replace(completeDraft, `"evidence": [`, `"evidence": [{"evidence_type": "TAX_EVIDENCE", "evidence_reference": "evd_kilima_tax"}, `, 1)
	app = okApp(t)(e.svc.Update(e.ctx, applicant, app.ID, []byte(update(app.Version, withTax))))
	if len(app.Evidence) != 2 || !app.Evidence[1].RecordedAt.Equal(firstEvidenceAt) || !app.Evidence[0].RecordedAt.Equal(e.now) {
		t.Fatalf("evidence after edit: %+v", app.Evidence)
	}
	app = okApp(t)(e.svc.Respond(e.ctx, applicant, app.ID, []byte(`{"version":`+jsonInt(app.Version)+`,"response":"Uploaded."}`)))
	if app.Status != domain.ApplicationValidating || app.InformationRequests[0].Response != "Uploaded." {
		t.Fatalf("responded: %+v", app)
	}
	app = okApp(t)(e.svc.BeginReview(e.ctx, reviewer, app.ID))

	// The applicant can never decide their own application.
	approval := `{"decision":"APPROVED","reason":"Registration and tax standing verified.","approved_subscription_type":"COMMERCIAL",
		"approved_market_scope":["UG"],"approved_product_requirements":["b2b-trade"],"evidence_references":["evd_kilima_certificate","evd_kilima_tax"]}`
	if _, err := e.svc.Decide(e.ctx, applicant, app.ID, []byte(approval)); !errors.Is(err, application.ErrSelfDecision) {
		t.Fatalf("self-decision must be refused: %v", err)
	}
	if _, err := e.svc.Decide(e.ctx, decider, app.ID, []byte(strings.Replace(approval, `["UG"]`, `["UG","TZ"]`, 1))); !errors.Is(err, application.ErrMarketScope) {
		t.Fatalf("an approval beyond the requested markets must be refused: %v", err)
	}
	e.tick()
	decision := okDecision(t)(e.svc.Decide(e.ctx, decider, app.ID, []byte(approval)))
	if decision.Decision != domain.DecisionApproved || decision.DecidedBy != decider.PrincipalID || decision.InternalEligibility != nil ||
		!strings.HasPrefix(decision.ID, "adm_") {
		t.Fatalf("decision: %+v", decision)
	}
	if _, err := e.svc.Decide(e.ctx, decider, app.ID, []byte(approval)); err == nil {
		t.Fatal("a decided application cannot be decided again")
	}
	final := okApp(t)(e.svc.Get(e.ctx, app.ID))
	if final.Status != domain.ApplicationApproved || final.ClosedAt == nil || final.Decision == nil || final.Decision.AdmissionDecisionID != decision.ID {
		t.Fatalf("approved application: %+v", final)
	}
	stored := okDecision(t)(e.svc.GetDecision(e.ctx, app.ID))
	if stored.ID != decision.ID || !slices.Equal(stored.EvidenceReferences, decision.EvidenceReferences) || !stored.DecidedAt.Equal(decision.DecidedAt) {
		t.Fatalf("stored decision %+v differs from %+v", stored, decision)
	}

	// Approval activates nothing (section 22): the applicant holds no
	// tenant membership, and the decision cannot be edited.
	var memberships int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM identity.workforce_membership WHERE principal_id = $1::uuid`, applicant.PrincipalID).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("approval must not grant tenant membership: %d %v", memberships, err)
	}
	decisionRow, _ := domain.ParseResourceID(domain.AdmissionDecisionIDPrefix, decision.ID)
	_, err := e.admin.Exec(e.ctx, `UPDATE admission.admission_decision SET reason = 'edited' WHERE admission_decision_id = $1::uuid`, decisionRow)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("an admission decision must be immutable: %v", err)
	}

	wantEvents := []string{events.ClientApplicationCreated, events.ClientApplicationSubmitted,
		events.ClientApplicationInformationRequested, events.ClientApplicationApproved}
	published := e.outbox(t, app.ID)
	if got := eventTypes(published); !slices.Equal(got, wantEvents) {
		t.Fatalf("events %v, want %v", got, wantEvents)
	}
	approved := published[len(published)-1].Data
	if approved["approved_subscription_type"] != "COMMERCIAL" || approved["admission_decision_id"] != decision.ID {
		t.Fatalf("approved event data: %v", approved)
	}
	wantAudits := []string{"client_application.created", "client_application.updated", "client_application.submitted",
		"client_application.validation_started", "client_application.information_requested", "client_application.updated",
		"client_application.information_provided", "client_application.review_started", "client_application.approved"}
	if got := e.audits(t, app.ID); !slices.Equal(got, wantAudits) {
		t.Fatalf("audit trail %v, want %v", got, wantAudits)
	}
}

func jsonInt(v int64) string { raw, _ := json.Marshal(v); return string(raw) }

// TestClientApplicationOwnershipAndReplay: an applicant sees and changes
// only their own applications, and a repeated Idempotency-Key converges.
func TestClientApplicationOwnershipAndReplay(t *testing.T) {
	e := newEnv(t)
	alice, mallory := e.principal(t), e.principal(t)
	key := "create-" + domain.NewUUIDv7()
	first, replayed, err := e.svc.Create(e.ctx, alice, []byte(completeDraft), key)
	if err != nil || replayed {
		t.Fatalf("create: %v %v", replayed, err)
	}
	again, replayed, err := e.svc.Create(e.ctx, alice, []byte(completeDraft), key)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("a replayed create must return the original: %v %v %v", again.ID, replayed, err)
	}
	if _, _, err := e.svc.Create(e.ctx, alice, []byte(`{"requirements":{}}`), key); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("a reused key with another body must conflict: %v", err)
	}
	// The same key from another applicant is theirs alone.
	other, replayed, err := e.svc.Create(e.ctx, mallory, []byte(completeDraft), key)
	if err != nil || replayed || other.ID == first.ID {
		t.Fatalf("keys are per applicant: %v %v", replayed, err)
	}

	if _, err := e.svc.GetForApplicant(e.ctx, mallory, first.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("another applicant's application must not be visible: %v", err)
	}
	for name, run := range map[string]func() error{
		"edit": func() error {
			_, err := e.svc.Update(e.ctx, mallory, first.ID, []byte(`{"version":1,"requirements":{}}`))
			return err
		},
		"submit":   func() error { _, err := e.svc.Submit(e.ctx, mallory, first.ID); return err },
		"withdraw": func() error { _, err := e.svc.Withdraw(e.ctx, mallory, first.ID, []byte(`{"reason":"x"}`)); return err },
	} {
		if err := run(); !errors.Is(err, application.ErrNotFound) {
			t.Errorf("%s by another applicant: %v", name, err)
		}
	}
	mine := okApps(t)(e.svc.ListForApplicant(e.ctx, alice, 10, ""))
	if len(mine) != 1 || mine[0].ID != first.ID {
		t.Fatalf("alice's applications: %+v", mine)
	}

	// Applicants write evidence, never state.
	for _, body := range []string{`{"status":"APPROVED"}`, `{"approved_subscription_type":"INTERNAL"}`,
		`{"organisation_profile":{"registration_identifiers":[{"type":"LEI","value":"X","verified":true}]}}`} {
		if _, _, err := e.svc.Create(e.ctx, alice, []byte(body), ""); !invalid(err, false) {
			t.Errorf("%s must be refused as invalid: %v", body, err)
		}
	}

	// A draft withdrawn before submission needs no completeness.
	draft := e.create(t, alice, []byte(`{}`))
	withdrawn := okApp(t)(e.svc.Withdraw(e.ctx, alice, draft.ID, []byte(`{"reason":"Applied by mistake."}`)))
	if withdrawn.Status != domain.ApplicationWithdrawn || withdrawn.ClosedAt == nil || withdrawn.SubmittedAt != nil {
		t.Fatalf("withdrawn draft: %+v", withdrawn)
	}
	if _, err := e.svc.Submit(e.ctx, alice, draft.ID); err == nil {
		t.Fatal("a withdrawn application is final")
	}
}

// TestInternalClassificationIsServerAuthoritative is ADR-BCP-017 sections
// 10-13: INTERNAL is granted only when the Control Plane finds the named
// organisation eligible, and the decision records the relationships it
// rests on; nobody can supply that evidence.
func TestInternalClassificationIsServerAuthoritative(t *testing.T) {
	e := newEnv(t)
	applicant, reviewer, decider := e.principal(t), e.principal(t), e.principal(t)
	underReview := func() string {
		app := e.create(t, applicant, []byte(completeDraft))
		app = okApp(t)(e.svc.Submit(e.ctx, applicant, app.ID))
		app = okApp(t)(e.svc.BeginValidation(e.ctx, reviewer, app.ID))
		app = okApp(t)(e.svc.BeginReview(e.ctx, reviewer, app.ID))
		return app.ID
	}
	internal := func(org string) []byte {
		return []byte(`{"decision":"APPROVED","reason":"Group affiliate.","approved_subscription_type":"INTERNAL",
			"internal_eligibility_organisation_id":"` + org + `","approved_market_scope":["KE"],"evidence_references":["evd_share_register"]}`)
	}

	id := underReview()
	if _, err := e.svc.Decide(e.ctx, decider, id, internal("ce_external")); !errors.Is(err, application.ErrNotInternalEligible) {
		t.Fatalf("an ineligible organisation must not receive INTERNAL: %v", err)
	}
	if _, err := e.svc.Decide(e.ctx, decider, id, []byte(`{"decision":"APPROVED","reason":"x","approved_subscription_type":"COMMERCIAL",
		"approved_market_scope":["KE"],"evidence_references":["evd"],"internal_eligibility":{"organisation_id":"ce_x"}}`)); !invalid(err, false) {
		t.Fatalf("a decider cannot supply eligibility evidence: %v", err)
	}
	if app := okApp(t)(e.svc.Get(e.ctx, id)); app.Status != domain.ApplicationUnderReview {
		t.Fatalf("a refused decision changes nothing: %s", app.Status)
	}

	e.elig.eligible["ce_zuribeans"] = []domain.PlatformRelationship{{ID: "prel_01k8zuriaffil"}, {ID: "prel_01k8zuriaffil"}}
	decision := okDecision(t)(e.svc.Decide(e.ctx, decider, id, internal("ce_zuribeans")))
	ev := decision.InternalEligibility
	if decision.ApprovedSubscriptionType != domain.SubscriptionInternal || ev == nil || ev.OrganisationID != "ce_zuribeans" ||
		ev.EligibilityStatus != "ELIGIBLE" || !slices.Equal(ev.BasisRelationshipIDs, []string{"prel_01k8zuriaffil"}) || !ev.EvaluatedAt.Equal(decision.DecidedAt) {
		t.Fatalf("INTERNAL decision: %+v %+v", decision, ev)
	}
	stored := okDecision(t)(e.svc.GetDecision(e.ctx, id))
	if stored.InternalEligibility == nil || !slices.Equal(stored.InternalEligibility.BasisRelationshipIDs, ev.BasisRelationshipIDs) {
		t.Fatalf("stored eligibility: %+v", stored.InternalEligibility)
	}

	// Rejection and platform cancellation.
	rejected := underReview()
	decision = okDecision(t)(e.svc.Decide(e.ctx, decider, rejected, []byte(`{"decision":"REJECTED","reason":"Could not verify registration."}`)))
	if app := okApp(t)(e.svc.Get(e.ctx, rejected)); app.Status != domain.ApplicationRejected || app.Decision.Reason != "Could not verify registration." {
		t.Fatalf("rejected: %+v", app)
	}
	cancelled := underReview()
	app := okApp(t)(e.svc.Cancel(e.ctx, reviewer, cancelled, []byte(`{"reason":"Duplicate of an approved application."}`)))
	if app.Status != domain.ApplicationCancelled || app.ClosedAt == nil {
		t.Fatalf("cancelled: %+v", app)
	}
	if got := eventTypes(e.outbox(t, cancelled)); !slices.Equal(got, []string{events.ClientApplicationCreated, events.ClientApplicationSubmitted}) {
		t.Fatalf("cancellation is audited, not published: %v", got)
	}
}

// TestClientApplicationsConformToSharedContract validates what the Control
// Plane stores, returns and publishes against the pinned Shared contract.
func TestClientApplicationsConformToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	e := newEnv(t)
	applicant, reviewer, decider := e.principal(t), e.principal(t), e.principal(t)
	e.elig.eligible["ce_zuribeans"] = []domain.PlatformRelationship{{ID: "prel_01k8zuriaffil"}}

	app := e.create(t, applicant, []byte(completeDraft))
	app = okApp(t)(e.svc.Submit(e.ctx, applicant, app.ID))
	app = okApp(t)(e.svc.BeginValidation(e.ctx, reviewer, app.ID))
	app = okApp(t)(e.svc.RequestInformation(e.ctx, reviewer, app.ID, []byte(`{"message":"More please."}`)))
	app = okApp(t)(e.svc.Respond(e.ctx, applicant, app.ID, []byte(`{"version":`+jsonInt(app.Version)+`,"response":"Done."}`)))
	app = okApp(t)(e.svc.BeginReview(e.ctx, reviewer, app.ID))
	decision := okDecision(t)(e.svc.Decide(e.ctx, decider, app.ID, []byte(`{"decision":"APPROVED","reason":"Affiliate.","approved_subscription_type":"INTERNAL",
		"internal_eligibility_organisation_id":"ce_zuribeans","approved_market_scope":["KE","UG"],"approved_isolation_requirements":"schema_per_tenant",
		"conditions":["Annual review."],"evidence_references":["evd_share_register"]}`)))
	platformView := okApp(t)(e.svc.Get(e.ctx, app.ID))
	applicantView := okApp(t)(e.svc.GetForApplicant(e.ctx, applicant, app.ID))
	if applicantView.AssignedReviewer != "" || platformView.AssignedReviewer != reviewer.PrincipalID {
		t.Fatalf("the reviewer is internal: applicant sees %q", applicantView.AssignedReviewer)
	}

	appSchema := contracttest.CompileSchema(t, dir, "admission/v1/application.schema.json#/$defs/ClientApplication")
	for _, v := range []domain.ClientApplication{platformView, applicantView} {
		contracttest.ValidateJSON(t, appSchema, v)
	}
	contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "admission/v1/decision.schema.json#/$defs/AdmissionDecision"), decision)

	envelope := contracttest.CompileSchema(t, dir, "events/v1/envelope.schema.json")
	published := e.outbox(t, app.ID)
	if len(published) != 4 {
		t.Fatalf("expected 4 events, got %v", eventTypes(published))
	}
	for _, env := range published {
		contracttest.ValidateJSON(t, envelope, env)
		payload := contracttest.CompileSchema(t, dir, "admission/v1/events.schema.json#/$defs/"+events.AdmissionPayloadDef(env.Type))
		contracttest.ValidateJSON(t, payload, env.Data)
		if env.Subject != "client-application/"+app.ID || env.TenantID != "" {
			t.Errorf("%s: subject %q tenant %q", env.Type, env.Subject, env.TenantID)
		}
	}
}
