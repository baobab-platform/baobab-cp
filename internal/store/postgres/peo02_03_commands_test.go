package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/service/progressive"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func TestPEO03DurableApplicantOwnershipAndSubmit(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL 17 acceptance DB not configured")
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
	var applicant, other string
	for _, x := range []*string{&applicant, &other} {
		if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type) VALUES('human')
   RETURNING principal_id::text`).Scan(x); err != nil {
			t.Fatal(err)
		}
	}
	meta := basestore.RequestMetadata{ActorID: applicant, ActorType: "human",
		CorrelationID: domain.NewUUIDv7()}
	key := "peo03-create-" + domain.NewUUIDv7()
	draft := []byte(`{"business_identity":{
 "operating_name":"Synthetic Unincorporated Venture",
 "organisation_form":"UNINCORPORATED_ORGANISATION",
 "operating_country":"ZA",
 "incorporation_claim":"NOT_INCORPORATED",
 "authorised_representative":{"full_name":"Synthetic Applicant","role":"owner"}
 },"requested_markets":[{"country_code":"ZA"}],
 "requirements":{"operates_b2b":true}}`)
	app, err := db.CreateProgressiveApplication(ctx, meta, applicant, key, draft)
	if err != nil {
		t.Fatal(err)
	}
	if app.Status != "DRAFT" || app.Version != 1 || len(app.BusinessIdentity) == 0 {
		t.Fatalf("unexpected applicant draft %+v", app)
	}
	again, err := db.CreateProgressiveApplication(ctx, meta, applicant, key, draft)
	if err != nil || again.ID != app.ID {
		t.Fatalf("replay mismatch %+v %v", again, err)
	}
	if _, err = db.CreateProgressiveApplication(ctx, meta, applicant, key, []byte(`{}`)); err == nil {
		t.Fatal("mutated idempotency payload accepted")
	}
	if _, err = db.GetProgressiveApplication(ctx, other, app.ID); !errors.Is(err, ErrProgressiveApplicationNotFound) {
		t.Fatalf("foreign applicant could read: %v", err)
	}
	updated, err := db.ChangeProgressiveApplication(ctx, meta, applicant, app.ID, []byte(`{"version":1,"requirements":{"operates_b2b":true,"trades_cross_border":true}}`), false)
	if err != nil || updated.Version != 2 {
		t.Fatalf("optimistic draft update failed %+v %v", updated, err)
	}
	if _, err = db.ChangeProgressiveApplication(ctx, meta, applicant, app.ID, []byte(`{"version":1}`), false); err == nil {
		t.Fatal("stale update accepted")
	}
	submitted, err := db.ChangeProgressiveApplication(ctx, meta, applicant, app.ID, nil, true)
	if err != nil || submitted.Status != "SUBMITTED" || submitted.SubmittedAt == nil {
		t.Fatalf("submission failed %+v %v", submitted, err)
	}
	if _, err = db.ChangeProgressiveApplication(ctx, meta, applicant, app.ID, []byte(`{"version":3}`), false); err == nil {
		t.Fatal("post-submission mutation accepted")
	}
	var auditCount int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
  WHERE target=$1 AND action LIKE 'progressive_client_application.%'`, "client-application/"+app.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 {
		t.Fatalf("expected 3 durable audited draft lifecycle events, got %d", auditCount)
	}
}
func TestPEO02MakerCheckerNeverCertifiesSyntheticSponsor(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL 17 acceptance DB not configured")
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
	var maker, checker string
	for _, id := range []*string{&maker, &checker} {
		if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type) VALUES('human')
   RETURNING principal_id::text`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	orgs := []string{}
	for i := 0; i < 2; i++ {
		tenant := domain.NewTenantID()
		legal := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:16])
		_, err = db.RegisterTenant(ctx, "peo02-bootstrap-"+domain.NewUUIDv7(),
			basestore.RequestMetadata{ActorID: "test-admin", ActorType: "workload", CorrelationID: domain.NewUUIDv7()},
			domain.RegisterTenant{TenantID: tenant, LegalEntityID: legal,
				Basis: domain.RegistrationBootstrap, BootstrapReason: "synthetic founding policy guardrail",
				BootstrapEvidenceReference: "synthetic-only", DisplayName: "Synthetic founding test",
				IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var org string
		if err = db.pool.QueryRow(ctx, `SELECT organisation_id::text FROM registry.tenant_organisation_mapping
   WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION' AND status='ACTIVE'`, tenant).Scan(&org); err != nil {
			t.Fatal(err)
		}
		orgs = append(orgs, org)
	}
	meta := basestore.RequestMetadata{ActorID: maker, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC()
	proposal := FoundingSponsorshipInput{OperatingOrganisationID: orgs[0], SponsorOrganisationID: orgs[1],
		PlatformID: "baobab", AuthorityBasisReference: "test/review-only",
		EvidenceReferences: []string{"synthetic/unverified"}, EffectiveFrom: now.Add(-time.Hour),
		EffectiveTo: now.Add(24 * time.Hour)}
	// Store command validation never grants on an unverified label.
	raw := []byte(`{}`)
	if _, err = db.ProposeFoundingGovernance(ctx, "peo02-no-document-"+domain.NewUUIDv7(), meta, maker, "SPONSORSHIP", raw); err == nil {
		t.Fatal("empty sponsorship approved")
	}
	raw, err = json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	key := "peo02-proposal-" + domain.NewUUIDv7()
	receipt, err := db.ProposeFoundingGovernance(ctx, key, meta, maker, "SPONSORSHIP", raw)
	if err != nil || receipt.Status != "PENDING" {
		t.Fatalf("proposal rejected %+v %v", receipt, err)
	}
	replay, err := db.ProposeFoundingGovernance(ctx, key, meta, maker, "SPONSORSHIP", raw)
	if err != nil || replay.IntentID != receipt.IntentID {
		t.Fatalf("replay mismatch %+v %v", replay, err)
	}
	if _, err = db.ProposeFoundingGovernance(ctx, key, meta, maker, "SPONSORSHIP", []byte(`{}`)); err == nil {
		t.Fatal("changed intent replay accepted")
	}
	same := FoundingDecisionInput{ReviewReference: "test/fake", Decision: "APPROVE"}
	if _, err = db.DecideFoundingGovernance(ctx, "peo02-self-"+domain.NewUUIDv7(), meta, maker, receipt.IntentID, same); err == nil {
		t.Fatal("self-approval accepted")
	}
	checkerMeta := basestore.RequestMetadata{ActorID: checker, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	if _, err = db.DecideFoundingGovernance(ctx, "peo02-unverified-"+domain.NewUUIDv7(), checkerMeta, checker, receipt.IntentID, same); err == nil {
		t.Fatal("unverified synthetic sponsor was granted admission authority")
	}
	var grantCount int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.founding_group_sponsorship
  WHERE operating_organisation_id=$1::uuid`, orgs[0]).Scan(&grantCount); err != nil {
		t.Fatal(err)
	}
	if grantCount != 0 {
		t.Fatalf("synthetic grant materialised: %d", grantCount)
	}
	reject := FoundingDecisionInput{Decision: "REJECT", ReviewReference: "test/independent-checker-denial"}
	rejectKey := "peo02-reject-" + domain.NewUUIDv7()
	decision, err := db.DecideFoundingGovernance(ctx, rejectKey, checkerMeta, checker, receipt.IntentID, reject)
	if err != nil || decision.Status != "REJECTED" || decision.GrantID != "" {
		t.Fatalf("checker rejection failed: %+v %v", decision, err)
	}
	replayed, err := db.DecideFoundingGovernance(ctx, rejectKey, checkerMeta, checker, receipt.IntentID, reject)
	if err != nil || replayed.IntentID != receipt.IntentID || replayed.Status != "REJECTED" {
		t.Fatalf("checker replay diverged: %+v %v", replayed, err)
	}
	if _, err = db.DecideFoundingGovernance(ctx, rejectKey, checkerMeta, checker, receipt.IntentID,
		FoundingDecisionInput{Decision: "APPROVE", ReviewReference: reject.ReviewReference}); err == nil {
		t.Fatal("changed checker decision re-used a completed idempotency key")
	}
	var decisionEvents int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
 WHERE target=$1 AND action IN ('founding_governance.proposed','founding_governance.decided')`,
		"founding-governance/"+receipt.IntentID).Scan(&decisionEvents); err != nil {
		t.Fatal(err)
	}
	if decisionEvents != 2 {
		t.Fatalf("expected one proposal and one checker decision audit, got %d", decisionEvents)
	}
}

// ADR-BCP-026/A1 fixes the grace window as 24 calendar months from an explicit UTC instant.
// PostgreSQL does month arithmetic in the session time zone, so an unqualified
// `timestamptz + interval '24 months'` moves the expiry by the DST offset difference
// (e.g. 2026-11-03T12:00Z in America/New_York becomes 2028-11-03T11:00Z). The expression
// the migration and command use must agree with progressive.CalendarAnniversaryUTC in
// every session time zone.
func TestFoundingGraceExpiryIsUTCCalendarMonthsInAnySessionTimeZone(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL acceptance DB not configured")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	starts := []time.Time{
		time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC),
		time.Date(2028, 2, 29, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
	}
	for _, zone := range []string{"UTC", "America/New_York", "Africa/Johannesburg", "Pacific/Auckland"} {
		conn, acquireErr := db.pool.Acquire(ctx)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if _, err = conn.Exec(ctx, `SELECT set_config('TimeZone',$1,false)`, zone); err != nil {
			conn.Release()
			t.Fatal(err)
		}
		for _, start := range starts {
			var got time.Time
			if err = conn.QueryRow(ctx, `SELECT admission.founding_grace_expiry($1::timestamptz)`, start).Scan(&got); err != nil {
				conn.Release()
				t.Fatal(err)
			}
			want := progressive.CalendarAnniversaryUTC(start, progressive.FoundingDocumentaryGraceMonths)
			if !got.UTC().Equal(want) {
				t.Errorf("zone %s start %s: database expiry %s, policy expiry %s", zone, start, got.UTC(), want)
			}
		}
		conn.Release()
	}
}
