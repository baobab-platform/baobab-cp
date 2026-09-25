package subscription_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nabhold/baobab-cp/internal/contracttest"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service/application"
	"github.com/nabhold/baobab-cp/internal/service/organisation"
	"github.com/nabhold/baobab-cp/internal/service/subscription"
	"github.com/nabhold/baobab-cp/internal/store/postgres"
)

// ADR-BCP-018 gate ORG-11 against real PostgreSQL: the ZuriBeans INTERNAL
// acceptance path, the Acme EXTERNAL_CLIENT negative, divestiture drift and
// reclassification, provenance immutability and contract conformance.
// Each test builds its own platform, organisations and tenants.

type env struct {
	ctx   context.Context
	repo  *repository.PostgresRepository
	admin *pgxpool.Pool
	at    time.Time
	now   time.Time
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
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	return &env{ctx: ctx, repo: repo, admin: admin, at: at, now: at.Add(time.Hour)}
}

func token() string { return strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:20] }

func (e *env) clock() time.Time { return e.now }

func orgActor() repository.AuditActor {
	return repository.AuditActor{ActorID: "principal:governance", ActorType: "human", CorrelationID: domain.NewUUIDv7()}
}

func (e *env) evidence() repository.Evidence {
	return repository.Evidence{References: []string{"evd_" + token()}, VerifiedAt: e.at, Reason: "reviewed"}
}

func (e *env) principal(t *testing.T) subscription.Actor {
	t.Helper()
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	if err := e.repo.CreateIdentity(e.ctx, p); err != nil {
		t.Fatal(err)
	}
	return subscription.Actor{PrincipalID: p.ID, Audit: repository.AuditActor{ActorID: p.ID, ActorType: "human", CorrelationID: domain.NewUUIDv7()}}
}

func appActor(a subscription.Actor) application.Actor {
	return application.Actor{PrincipalID: a.PrincipalID, Audit: a.Audit}
}

func (e *env) organisation(t *testing.T) string {
	t.Helper()
	var id string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ('ORGANISATION','active')
		RETURNING canonical_entity_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// group is a platform with its owner (the Nabhold Group Africa role) and
// one verified subsidiary affiliate (the ZuriBeans role).
type group struct {
	platform, owner, sub, ownsSub, affiliate string
	resolver                                 *organisation.EligibilityResolver
}

func (e *env) group(t *testing.T) group {
	t.Helper()
	g := group{platform: "test-platform-" + token(), owner: e.organisation(t), sub: e.organisation(t)}
	g.resolver = &organisation.EligibilityResolver{Orgs: e.repo, PlatformID: g.platform}
	ownerRel, err := e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: g.platform, OrganisationID: g.owner,
		RelationshipType: domain.PlatformRelOwner, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, orgActor())
	if err != nil {
		t.Fatal(err)
	}
	if g.ownsSub, err = e.repo.EnsureCorporateRelationship(e.ctx, domain.CorporateRelationship{SourceOrganisationID: g.owner,
		TargetOrganisationID: g.sub, RelationshipType: domain.CorpRelOwns, DirectOrDerived: domain.CorporateFactDirect,
		VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending, EffectiveFrom: e.at,
		SourceAuthority: "shared-governance"}, orgActor()); err != nil {
		t.Fatal(err)
	}
	if g.affiliate, err = e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: g.platform, OrganisationID: g.sub,
		RelationshipType: domain.PlatformRelGroupAffiliate, BasisRelationshipID: g.ownsSub, VerificationState: domain.VerificationPendingReview,
		Status: domain.RelationshipStatusPending, EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, orgActor()); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.VerifyCorporateRelationship(e.ctx, g.ownsSub, e.evidence(), orgActor()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ownerRel, g.affiliate} {
		if err := e.repo.VerifyPlatformRelationship(e.ctx, id, e.evidence(), orgActor()); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// tenant registers a tenant whose primary organisation is org, subscribed
// to one product, as registration and admission would. relType, when set,
// records the platform relationship admission creates, verified.
func (e *env) tenant(t *testing.T, org, platform string, relType domain.PlatformRelationshipType, decisionID string) (tenantID, subscriptionID string) {
	t.Helper()
	le := "LE-" + strings.ToUpper(token())
	tenantID = "tn_" + token()
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, le); err != nil {
		t.Fatal(err)
	}
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO tenants(tenant_id,legal_entity_id,display_name,isolation_strategy,residency_region)
		VALUES ($1,$2,'T','row_level_security','af-south-1')`, tenantID, le); err != nil {
		t.Fatal(err)
	}
	var row string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO product.product_subscription(tenant_id,product_id,status,source)
		VALUES ($1,'zuritrade','PENDING','ONBOARDING') RETURNING subscription_id::text`, tenantID).Scan(&row); err != nil {
		t.Fatal(err)
	}
	req := organisation.ProvisionRequest{TenantID: tenantID, CanonicalEntityID: org, LegalEntityID: le, DisplayName: "Org " + org[:8],
		SourceAuthority: "control-plane-admission", PlatformID: platform, PlatformRelType: relType, PlatformRelAuthority: "platform-governance",
		AdmissionDecisionID: decisionID, EffectiveFrom: e.at, Actor: orgActor(), SkipPlatformRel: relType == ""}
	res, err := (&organisation.Provisioner{Orgs: e.repo}).ProvisionTenantOrganisation(e.ctx, req)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.PlatformRelationshipID != "" {
		if err := e.repo.VerifyPlatformRelationship(e.ctx, res.PlatformRelationshipID, e.evidence(), orgActor()); err != nil {
			t.Fatal(err)
		}
	}
	subscriptionID, err = domain.FormatResourceID(domain.ProductSubscriptionIDPrefix, row)
	if err != nil {
		t.Fatal(err)
	}
	return tenantID, subscriptionID
}

const completeDraft = `{
	"organisation_profile": {
		"legal_name": "Applicant Holdings Ltd", "organisation_form": "COMPANY", "jurisdiction_of_incorporation": "KE",
		"registration_identifiers": [{"type": "COMPANY_REGISTRATION", "value": "PVT-ABC123", "issuing_jurisdiction": "KE"}],
		"authorised_representative": {"full_name": "Applicant Representative", "role": "Director"}
	},
	"requirements": {"operates_b2b": true},
	"requested_markets": [{"country_code": "KE"}],
	"evidence": [{"evidence_type": "REGISTRATION_EVIDENCE", "evidence_reference": "evd_registration"}]
}`

// decision runs an application from draft to an AdmissionDecision.
func (e *env) decision(t *testing.T, apps *application.Service, applicant, reviewer, decider subscription.Actor, body string) domain.AdmissionDecision {
	t.Helper()
	app, _, err := apps.Create(e.ctx, appActor(applicant), []byte(completeDraft), "")
	if err != nil {
		t.Fatal(err)
	}
	steps := []func() (domain.ClientApplication, error){
		func() (domain.ClientApplication, error) { return apps.Submit(e.ctx, appActor(applicant), app.ID) },
		func() (domain.ClientApplication, error) {
			return apps.BeginValidation(e.ctx, appActor(reviewer), app.ID)
		},
		func() (domain.ClientApplication, error) { return apps.BeginReview(e.ctx, appActor(reviewer), app.ID) },
	}
	for _, step := range steps {
		if _, err := step(); err != nil {
			t.Fatal(err)
		}
	}
	d, err := apps.Decide(e.ctx, appActor(decider), app.ID, []byte(body))
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	return d
}

func internalDecision(org string) string {
	return `{"decision":"APPROVED","reason":"Group affiliate of the platform owner.","approved_subscription_type":"INTERNAL",
		"internal_eligibility_organisation_id":"` + org + `","approved_market_scope":["KE"],"evidence_references":["evd_share_register"]}`
}

const commercialDecision = `{"decision":"APPROVED","reason":"External client.","approved_subscription_type":"COMMERCIAL",
	"approved_market_scope":["KE"],"evidence_references":["evd_registration"]}`

func classifyBody(decisionID string) []byte {
	return []byte(`{"admission_decision_id":"` + decisionID + `","reason":"Classified from the admission decision."}`)
}

func (e *env) classifier(g group) *subscription.Classifier {
	return &subscription.Classifier{Repo: e.repo, Admissions: e.repo, Orgs: e.repo, Memberships: e.repo, Eligibility: g.resolver, Now: e.clock}
}

func (e *env) drift(t *testing.T, subscriptionID string) (domain.RelationshipDriftFinding, bool) {
	t.Helper()
	report, err := e.repo.DetectRelationshipDrift(e.ctx, e.now, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.ResourceID == subscriptionID {
			return f, true
		}
	}
	return domain.RelationshipDriftFinding{}, false
}

func (e *env) outbox(t *testing.T, subscriptionRow string) []events.Envelope {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT payload FROM messaging.outbox WHERE aggregate_type='product_subscription' AND aggregate_id=$1
		ORDER BY aggregate_version`, subscriptionRow)
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

func (e *env) grants(t *testing.T, tenantID string) int {
	t.Helper()
	var n int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM capability.capability_grant WHERE tenant_id=$1`, tenantID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestInternalClassificationAcceptance is the ORG-11 acceptance path: the
// platform owner's verified subsidiary is admitted INTERNAL, its
// ProductSubscription is classified INTERNAL with the eligibility evidence
// and admission provenance, the explanation says why and that no billing
// or payment applies, and a replay changes nothing. CapabilityGrants are
// untouched.
func TestInternalClassificationAcceptance(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	tenantID, sub := e.tenant(t, g.sub, g.platform, "", "")
	decision := e.decision(t, apps, applicant, reviewer, decider, internalDecision(g.sub))
	grantsBefore := e.grants(t, tenantID)

	c := e.classifier(g)
	if _, err := c.Explain(e.ctx, sub); !errors.Is(err, subscription.ErrNotClassified) {
		t.Fatalf("an unclassified subscription has nothing to explain: %v", err)
	}
	if _, _, err := c.ClassifyFromAdmission(e.ctx, applicant, sub, classifyBody(decision.ID)); !errors.Is(err, subscription.ErrSelfClassification) {
		t.Fatalf("the applicant cannot classify from their own decision: %v", err)
	}
	rec, created, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil || !created {
		t.Fatalf("classify: %v created=%v", err, created)
	}
	if rec.SubscriptionType != domain.SubscriptionInternal || rec.Source != domain.ClassificationFromAdmissionDecision ||
		rec.Reference != decision.ID || rec.ClassifiedBy != approver.PrincipalID || rec.TenantID != tenantID || rec.InternalEligibility == nil ||
		rec.InternalEligibility.OrganisationID != g.sub || rec.InternalEligibility.PlatformID != g.platform ||
		!slices.Equal(rec.InternalEligibility.BasisRelationshipIDs, []string{g.affiliate}) || !rec.InternalEligibility.EvaluatedAt.Equal(e.now) {
		t.Fatalf("INTERNAL record: %+v %+v", rec, rec.InternalEligibility)
	}
	replay, created, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil || created || replay.ID != rec.ID {
		t.Fatalf("a replay returns the existing record: %v created=%v %s", err, created, replay.ID)
	}

	x, err := c.Explain(e.ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if x.Current.ID != rec.ID || len(x.History) != 1 || x.CurrentEligibility == nil || x.CurrentEligibility.EligibilityStatus != "ELIGIBLE" ||
		!slices.Equal(x.CurrentEligibility.BasisRelationshipIDs, []string{g.affiliate}) || x.PolicyReference != subscription.PolicyReference {
		t.Fatalf("explanation: %+v", x)
	}
	if p := x.BillingPolicy; p.MonetaryCharge != "ZERO" || p.BillingRequired || p.PaymentExecution != "NEVER" || !p.UsageMetering ||
		!p.EntitlementControl || !p.Audit || !p.ReadinessControl || !p.IsolationControl {
		t.Fatalf("INTERNAL billing policy: %+v", p)
	}
	if y, err := c.ExplainTenantProduct(e.ctx, tenantID, "zuritrade"); err != nil || y.Current.ID != rec.ID {
		t.Fatalf("explain by tenant and product: %v %+v", err, y.Current)
	}

	row, _ := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, sub)
	envs := e.outbox(t, row)
	if len(envs) != 1 || envs[0].Type != events.ProductSubscriptionClassified || envs[0].TenantID != tenantID {
		t.Fatalf("one classified event: %+v", envs)
	}
	data, _ := json.Marshal(envs[0].Data)
	for _, leaked := range []string{"reason", "classified_by", "internal_eligibility", approver.PrincipalID} {
		if strings.Contains(string(data), leaked) {
			t.Fatalf("the event carries identifiers and state only, found %q in %s", leaked, data)
		}
	}
	if got := e.grants(t, tenantID); got != grantsBefore {
		t.Fatalf("classification changes no CapabilityGrant: %d -> %d", grantsBefore, got)
	}
	if _, ok := e.drift(t, sub); ok {
		t.Fatal("an INTERNAL subscription on an in-force basis is not drift")
	}
}

// TestExternalClientIsNeverInternal is the ORG-11 negative: an organisation
// admitted as EXTERNAL_CLIENT (Acme) cannot be admitted INTERNAL, is
// classified COMMERCIAL from its own decision, cannot be reclassified
// INTERNAL, and a decision admitting another organisation classifies
// nothing.
func TestExternalClientIsNeverInternal(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	acme := e.organisation(t)

	app, _, err := apps.Create(e.ctx, appActor(applicant), []byte(completeDraft), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() (domain.ClientApplication, error){
		func() (domain.ClientApplication, error) { return apps.Submit(e.ctx, appActor(applicant), app.ID) },
		func() (domain.ClientApplication, error) {
			return apps.BeginValidation(e.ctx, appActor(reviewer), app.ID)
		},
		func() (domain.ClientApplication, error) { return apps.BeginReview(e.ctx, appActor(reviewer), app.ID) },
	} {
		if _, err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := apps.Decide(e.ctx, appActor(decider), app.ID, []byte(internalDecision(acme))); !errors.Is(err, application.ErrNotInternalEligible) {
		t.Fatalf("Acme cannot be admitted INTERNAL: %v", err)
	}
	decision, err := apps.Decide(e.ctx, appActor(decider), app.ID, []byte(commercialDecision))
	if err != nil {
		t.Fatal(err)
	}
	tenantID, sub := e.tenant(t, acme, g.platform, domain.PlatformRelExternalClient, decision.ID)

	c := e.classifier(g)
	// A decision that admitted another organisation classifies nothing.
	other := e.decision(t, apps, e.principal(t), reviewer, decider, commercialDecision)
	if _, _, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(other.ID)); !errors.Is(err, subscription.ErrDecisionNotForTenant) {
		t.Fatalf("another organisation's decision: %v", err)
	}
	// Nor does the ZuriBeans INTERNAL decision.
	zuriDecision := e.decision(t, apps, e.principal(t), reviewer, decider, internalDecision(g.sub))
	if _, _, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(zuriDecision.ID)); !errors.Is(err, subscription.ErrDecisionNotForTenant) {
		t.Fatalf("an INTERNAL decision for another organisation: %v", err)
	}
	if _, _, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody("adm_0000000000000000000000000000000f")); !errors.Is(err, subscription.ErrDecisionNotFound) {
		t.Fatalf("unknown decision: %v", err)
	}

	rec, created, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil || !created || rec.SubscriptionType != domain.SubscriptionCommercial || rec.InternalEligibility != nil {
		t.Fatalf("Acme is COMMERCIAL: %v %+v", err, rec)
	}
	reclass := []byte(`{"subscription_type":"INTERNAL","classification_reference":"chg_acme_upgrade","reason":"Asked for free access."}`)
	if _, _, err := c.Reclassify(e.ctx, approver, sub, reclass); !errors.Is(err, subscription.ErrNotInternalEligible) {
		t.Fatalf("Acme cannot be reclassified INTERNAL: %v", err)
	}
	if _, _, err := c.Reclassify(e.ctx, approver, sub, []byte(`{"subscription_type":"INTERNAL","classification_reference":"chg_x",
		"reason":"x","internal_eligibility":{"organisation_id":"`+acme+`"}}`)); err == nil {
		t.Fatal("a caller can never supply eligibility evidence")
	}
	if _, _, err := c.Reclassify(e.ctx, approver, sub, []byte(`{"subscription_type":"COMMERCIAL","classification_reference":"chg_same",
		"reason":"x"}`)); !errors.Is(err, subscription.ErrUnchangedType) {
		t.Fatalf("an unchanged type is refused: %v", err)
	}
	x, err := c.Explain(e.ctx, sub)
	if err != nil || x.Current.ID != rec.ID || x.CurrentEligibility != nil || !x.BillingPolicy.BillingRequired ||
		x.BillingPolicy.PaymentExecution != "REQUIRED" || x.BillingPolicy.MonetaryCharge != "PRICED" {
		t.Fatalf("Acme explanation: %v %+v", err, x)
	}

	// Nobody who works for the tenant classifies its subscription.
	member := e.principal(t)
	if err := e.repo.CreateWorkforceMembership(e.ctx, domain.WorkforceMembership{ID: domain.NewUUIDv7(), PrincipalID: member.PrincipalID,
		TenantID: tenantID, Roles: []string{"cp:tenant-admin"}, Status: "ACTIVE"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Reclassify(e.ctx, member, sub, []byte(`{"subscription_type":"PARTNER","classification_reference":"chg_partner",
		"reason":"Partner agreement."}`)); !errors.Is(err, subscription.ErrSelfClassification) {
		t.Fatalf("a tenant member cannot classify its subscription: %v", err)
	}
}

// TestDivestitureDriftAndReclassification is ADR-BCP-018's divestiture
// scenario for ORG-11: when the ownership basis ends, the INTERNAL
// subscription is reported as drift (never silently changed or deleted),
// the explanation shows NOT_ELIGIBLE, INTERNAL can no longer be granted,
// and a governed reclassification to COMMERCIAL appends to the same
// subscription, tenant and organisation, clearing the drift.
func TestDivestitureDriftAndReclassification(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	tenantID, sub := e.tenant(t, g.sub, g.platform, "", "")
	decision := e.decision(t, apps, applicant, reviewer, decider, internalDecision(g.sub))
	c := e.classifier(g)
	internal, _, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil {
		t.Fatal(err)
	}

	reviewerSvc := &organisation.CorporateChangeReviewer{Orgs: e.repo, Eligibility: g.resolver}
	if _, err := reviewerSvc.EndCorporateRelationship(e.ctx, g.ownsSub, e.at.Add(2*time.Hour), "shares sold to a third party", orgActor()); err != nil {
		t.Fatal(err)
	}
	e.now = e.at.Add(3 * time.Hour)

	f, ok := e.drift(t, sub)
	if !ok || f.Rule != domain.DriftInternalClassificationBasisNotInForce || f.Severity != "CRITICAL" || f.ResourceType != "PRODUCT_SUBSCRIPTION" ||
		f.TenantID != tenantID || !slices.Equal(f.OrganisationIDs, []string{g.sub}) || !slices.Equal(f.RelatedResourceIDs, []string{g.affiliate}) ||
		f.Remediation != "REVIEW" || f.AutoRepairable {
		t.Fatalf("divestiture drift: %+v %v", f, ok)
	}
	x, err := c.Explain(e.ctx, sub)
	if err != nil || x.Current.ID != internal.ID || x.CurrentEligibility.EligibilityStatus != "NOT_ELIGIBLE" ||
		len(x.CurrentEligibility.BasisRelationshipIDs) != 0 {
		t.Fatalf("explanation after divestiture: %v %+v", err, x.CurrentEligibility)
	}
	target, err := e.repo.GetSubscriptionClassificationTarget(e.ctx, sub)
	if err != nil || target.SubscriptionType != domain.SubscriptionInternal || target.ClassificationID != internal.ID {
		t.Fatalf("drift changes nothing by itself: %v %+v", err, target)
	}

	if _, _, err := c.Reclassify(e.ctx, approver, sub, []byte(`{"subscription_type":"INTERNAL","classification_reference":"chg_again",
		"reason":"x"}`)); !errors.Is(err, subscription.ErrNotInternalEligible) {
		t.Fatalf("INTERNAL can no longer be granted after the divestiture: %v", err)
	}
	body := []byte(`{"subscription_type":"COMMERCIAL","classification_reference":"chg_01k9zuridivestiture","reason":"Divested: no longer a group affiliate."}`)
	commercial, created, err := c.Reclassify(e.ctx, approver, sub, body)
	if err != nil || !created || commercial.PreviousSubscriptionType != domain.SubscriptionInternal ||
		commercial.Source != domain.ClassificationFromReclassification || commercial.SubscriptionID != sub || commercial.TenantID != tenantID ||
		commercial.InternalEligibility != nil {
		t.Fatalf("reclassification: %v %+v", err, commercial)
	}
	if again, created, err := c.Reclassify(e.ctx, approver, sub, body); err != nil || created || again.ID != commercial.ID {
		t.Fatalf("a replayed reclassification returns the record: %v %v", err, created)
	}
	x, err = c.Explain(e.ctx, sub)
	if err != nil || x.Current.ID != commercial.ID || x.CurrentEligibility != nil || len(x.History) != 2 || x.History[1].ID != internal.ID {
		t.Fatalf("explanation after reclassification: %v %+v", err, x)
	}
	if _, ok := e.drift(t, sub); ok {
		t.Fatal("a reclassified subscription is no longer drift")
	}
	var tenants int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM tenants WHERE tenant_id=$1`, tenantID).Scan(&tenants); err != nil || tenants != 1 {
		t.Fatalf("the tenant survives: %v %d", err, tenants)
	}
	row, _ := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, sub)
	envs := e.outbox(t, row)
	if len(envs) != 2 || envs[1].Data["previous_subscription_type"] != "INTERNAL" {
		t.Fatalf("classified events: %+v", envs)
	}
}

// TestClassificationRecordsAreImmutable: provenance can be appended, never
// edited or removed, and the current pointer always names one of the
// subscription's own records with its type.
func TestClassificationRecordsAreImmutable(t *testing.T) {
	e := newEnv(t)
	g := e.group(t)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	_, sub := e.tenant(t, g.sub, g.platform, "", "")
	decision := e.decision(t, apps, applicant, reviewer, decider, internalDecision(g.sub))
	rec, _, err := e.classifier(g).ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := domain.ParseResourceID(domain.SubscriptionClassificationIDPrefix, rec.ID)
	for _, stmt := range []string{
		`UPDATE product.subscription_classification SET reason='edited' WHERE classification_id=$1::uuid`,
		`DELETE FROM product.subscription_classification WHERE classification_id=$1::uuid`,
	} {
		if _, err := e.admin.Exec(e.ctx, stmt, row); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	subRow, _ := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, sub)
	if _, err := e.admin.Exec(e.ctx, `UPDATE product.product_subscription SET subscription_type='COMMERCIAL' WHERE subscription_id=$1::uuid`,
		subRow); err == nil {
		t.Fatal("the current type must match the current record")
	}
	if _, err := e.admin.Exec(e.ctx, `UPDATE product.product_subscription SET subscription_type=NULL WHERE subscription_id=$1::uuid`,
		subRow); err == nil {
		t.Fatal("a classified subscription keeps its type and record together")
	}
}

// TestClassificationConformsToSharedContract validates the records,
// explanation and event the Control Plane produces against the pinned
// Shared contract checkout.
func TestClassificationConformsToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	e := newEnv(t)
	g := e.group(t)
	apps := &application.Service{Repo: e.repo, Eligibility: g.resolver, Now: e.clock}
	applicant, reviewer, decider, approver := e.principal(t), e.principal(t), e.principal(t), e.principal(t)
	_, sub := e.tenant(t, g.sub, g.platform, "", "")
	decision := e.decision(t, apps, applicant, reviewer, decider, internalDecision(g.sub))
	c := e.classifier(g)
	rec, _, err := c.ClassifyFromAdmission(e.ctx, approver, sub, classifyBody(decision.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Reclassify(e.ctx, approver, sub, []byte(`{"subscription_type":"COMMERCIAL","classification_reference":"chg_contract",
		"reason":"Commercial terms agreed."}`)); err != nil {
		t.Fatal(err)
	}
	x, err := c.Explain(e.ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "product/v1/subscription.schema.json#/$defs/SubscriptionClassificationRecord"), rec)
	contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "product/v1/subscription.schema.json#/$defs/ClassificationExplanation"), x)
	envelope := contracttest.CompileSchema(t, dir, "events/v1/envelope.schema.json")
	payload := contracttest.CompileSchema(t, dir, "product/v1/events.schema.json#/$defs/subscriptionClassifiedEventData")
	row, _ := domain.ParseResourceID(domain.ProductSubscriptionIDPrefix, sub)
	for _, env := range e.outbox(t, row) {
		contracttest.ValidateJSON(t, envelope, env)
		contracttest.ValidateJSON(t, payload, env.Data)
	}
	policy := contracttest.CompileSchema(t, dir, "product/v1/subscription.schema.json#/$defs/billingPolicy")
	all, err := subscription.BillingPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		contracttest.ValidateJSON(t, policy, p)
	}
}
