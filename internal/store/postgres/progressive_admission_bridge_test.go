package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func TestPEO03BIndependentDecisionToAuthorisedRequest(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PostgreSQL 17 integration database not configured")
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
	var people [5]string
	for i := range people {
		if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type)
			VALUES('human') RETURNING principal_id::text`).Scan(&people[i]); err != nil {
			t.Fatal(err)
		}
	}
	applicant, reviewer, decider, requester, authoriser := people[0], people[1], people[2], people[3], people[4]
	meta := func(actor string) basestore.RequestMetadata {
		return basestore.RequestMetadata{ActorID: actor, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	raw := []byte(`{"business_identity":{
		"operating_name":"Synthetic Inchoate Business","organisation_form":"UNINCORPORATED_ORGANISATION",
		"operating_country":"ZA","incorporation_claim":"NOT_INCORPORATED",
		"authorised_representative":{"full_name":"Synthetic Applicant","role":"owner"}},
		"requested_markets":[{"country_code":"ZA"}],"requirements":{"operates_b2b":true}}`)
	app, err := db.CreateProgressiveApplication(ctx, meta(applicant), applicant, "peo03b-app-"+domain.NewUUIDv7(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ChangeProgressiveApplication(ctx, meta(applicant), applicant, app.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	org := domain.NewUUIDv7()
	_, err = db.pool.Exec(ctx, `INSERT INTO registry.canonical_entity(canonical_entity_id,entity_type,status)
		VALUES($1::uuid,'ORGANISATION','active')`, org)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO registry.organisation_profile
		(canonical_entity_id,display_name,verification_state,source_authority,status,effective_from,evidence_references)
		VALUES($1::uuid,'Synthetic Inchoate Business','UNVERIFIED',
		'control-plane-approved-admission','ACTIVE',$2,'[]'::jsonb)`, org, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	reviewInput := ProgressiveAdmissionReviewInput{OrganisationID: org,
		EvidenceReference: "review/synthetic/001", IdentityResolutionPolicyReference: "policy/synthetic-identity/v1",
		SubscriptionType: "COMMERCIAL", MarketScope: []string{"ZA"},
		ProductRequirements: []string{"baobab-trade"}, IsolationStrategy: "row_level_security"}
	reviewKey := "review-" + domain.NewUUIDv7()
	if _, err = db.ReviewProgressiveAdmission(ctx, reviewKey, meta(applicant), applicant, app.ID, reviewInput); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("applicant was allowed to review own declaration: %v", err)
	}
	unknown := reviewInput
	unknown.MarketScope = []string{"UG"}
	if _, err = db.ReviewProgressiveAdmission(ctx, "bad-market-"+domain.NewUUIDv7(), meta(reviewer), reviewer, app.ID, unknown); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("approved a market not requested: %v", err)
	}
	internal := reviewInput
	internal.SubscriptionType = "INTERNAL"
	if _, err = db.ReviewProgressiveAdmission(ctx, "bad-internal-"+domain.NewUUIDv7(), meta(reviewer), reviewer, app.ID, internal); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("INTERNAL without governed sponsorship: %v", err)
	}
	review, err := db.ReviewProgressiveAdmission(ctx, reviewKey, meta(reviewer), reviewer, app.ID, reviewInput)
	if err != nil || review.Status != "REVIEWED" {
		t.Fatalf("review %+v %v", review, err)
	}
	repeat, err := db.ReviewProgressiveAdmission(ctx, reviewKey, meta(reviewer), reviewer, app.ID, reviewInput)
	if err != nil || repeat != review {
		t.Fatalf("idempotent review %+v %v", repeat, err)
	}
	if _, err = db.ReviewProgressiveAdmission(ctx, reviewKey, meta(reviewer), reviewer, app.ID, internal); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatal("review replay accepted changed classification")
	}
	decisionInput := ProgressiveAdmissionDecisionInput{
		Decision: "APPROVED", Reason: "Independent review accepted the synthetic market assessment",
		EvidenceReference: "review/synthetic/decision-001"}
	if _, err = db.DecideProgressiveAdmission(ctx, "self-decide-"+domain.NewUUIDv7(), meta(reviewer), reviewer, review.ResourceID, decisionInput); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatal("reviewer self-decided")
	}
	dec, err := db.DecideProgressiveAdmission(ctx, "decide-"+domain.NewUUIDv7(), meta(decider), decider, review.ResourceID, decisionInput)
	if err != nil || dec.Status != "APPROVED" {
		t.Fatalf("decision %+v %v", dec, err)
	}
	requestInput := ProgressiveOnboardingInput{DisplayName: "Synthetic Inchoate Business",
		ResidencyRegion: "af-south-1", Reason: "Synthetic controlled staging onboarding technical demonstration",
		MarketParticipation: []domain.OnboardingMarketParticipation{{Market: "ZA", Activities: []string{"SELLING"}}}}
	if _, err = db.RequestProgressiveOnboarding(ctx, "self-request-"+domain.NewUUIDv7(), meta(decider), decider, dec.ResourceID, requestInput); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatal("admission decider requested own onboarding")
	}
	req, err := db.RequestProgressiveOnboarding(ctx, "request-"+domain.NewUUIDv7(), meta(requester), requester, dec.ResourceID, requestInput)
	if err != nil || req.Status != "REQUESTED" {
		t.Fatalf("request %+v %v", req, err)
	}
	authorization := ProgressiveAuthorisationInput{PolicyReference: "policy/synthetic-admission/v1",
		EvidenceReference: "review/synthetic/onboard-approve-001"}
	if _, err = db.AuthoriseProgressiveOnboarding(ctx, "self-auth-"+domain.NewUUIDv7(), meta(requester), requester, req.ResourceID, authorization); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatal("requester self-authorised")
	}
	approved, err := db.AuthoriseProgressiveOnboarding(ctx, "authorize-"+domain.NewUUIDv7(), meta(authoriser), authoriser, req.ResourceID, authorization)
	if err != nil || approved.Status != "AUTHORISED" {
		t.Fatalf("authorisation %+v %v", approved, err)
	}
	var status string
	var tenantCount int
	if err = db.pool.QueryRow(ctx, `SELECT status FROM admission.progressive_onboarding_request
		WHERE request_id=$1::uuid`, mustProgressiveTestID(t, req.ResourceID)).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "AUTHORISED" {
		t.Fatalf("durable status %q", status)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.tenant_onboarding_request
		WHERE correlation_id=$1::uuid`, meta(applicant).CorrelationID).Scan(&tenantCount); err != nil {
		t.Fatal(err)
	}
	if tenantCount != 0 {
		t.Fatal("v2 command illicitly created v1 onboarding")
	}
	var auditCount, commandCount int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target LIKE 'admission-v2/%'
	  AND correlation_id<>$1::uuid`, domain.NewUUIDv7()).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 4 {
		t.Fatal("admission bridge missing durable audit evidence")
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.progressive_bridge_command
	  WHERE actor_id=ANY($1::uuid[])`, people[:]).Scan(&commandCount); err != nil {
		t.Fatal(err)
	}
	if commandCount != 4 {
		t.Fatalf("expected four exact human commands, got %d", commandCount)
	}
}

func mustProgressiveTestID(t *testing.T, value string) string {
	t.Helper()
	id, err := domain.ParseResourceID(domain.TenantOnboardingRequestIDPrefix, value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
