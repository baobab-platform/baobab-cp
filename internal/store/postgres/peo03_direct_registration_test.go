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

func TestPEO03CDirectRegistrationIsAtomicIdempotentAndPreservesV2Authority(t *testing.T) {
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
	var people [6]string
	for i := range people {
		if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type)
			VALUES('human') RETURNING principal_id::text`).Scan(&people[i]); err != nil {
			t.Fatal(err)
		}
	}
	meta := func(actor string) basestore.RequestMetadata {
		return basestore.RequestMetadata{ActorID: actor, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	raw := []byte(`{"business_identity":{
		"operating_name":"Synthetic Independently Reviewed Business","organisation_form":"UNINCORPORATED_ORGANISATION",
		"operating_country":"ZA","incorporation_claim":"NOT_INCORPORATED",
		"authorised_representative":{"full_name":"Synthetic Applicant","role":"owner"}},
		"requested_markets":[{"country_code":"ZA"}],"requirements":{"operates_b2b":true}}`)
	app, err := db.CreateProgressiveApplication(ctx, meta(people[0]), people[0],
		"peo03c-app-"+domain.NewUUIDv7(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ChangeProgressiveApplication(ctx, meta(people[0]), people[0], app.ID, nil, true); err != nil {
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
		VALUES($1::uuid,'Synthetic Independently Reviewed Business','UNVERIFIED',
		'control-plane-approved-admission','ACTIVE',$2,'[]'::jsonb)`, org, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	review, err := db.ReviewProgressiveAdmission(ctx, "review-"+domain.NewUUIDv7(), meta(people[1]),
		people[1], app.ID, ProgressiveAdmissionReviewInput{
			OrganisationID: org, EvidenceReference: "evidence/staging/review",
			IdentityResolutionPolicyReference: "policy/staging/review",
			SubscriptionType:                  "COMMERCIAL", MarketScope: []string{"ZA"},
			ProductRequirements: []string{"baobab-trade"}, IsolationStrategy: "row_level_security",
		})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := db.DecideProgressiveAdmission(ctx, "decide-"+domain.NewUUIDv7(), meta(people[2]),
		people[2], review.ResourceID, ProgressiveAdmissionDecisionInput{
			Decision: "APPROVED", Reason: "Synthetic independent admission assessment approved",
			EvidenceReference: "evidence/staging/approval",
		})
	if err != nil {
		t.Fatal(err)
	}
	request, err := db.RequestProgressiveOnboarding(ctx, "request-"+domain.NewUUIDv7(), meta(people[3]),
		people[3], decision.ResourceID, ProgressiveOnboardingInput{
			DisplayName:     "Synthetic Independently Reviewed Business",
			ResidencyRegion: "af-south-1", Reason: "Synthetic direct v2 governed staging registration",
			MarketParticipation: []domain.OnboardingMarketParticipation{{Market: "ZA", Activities: []string{"SELLING"}}},
		})
	if err != nil {
		t.Fatal(err)
	}
	unauthorised := domain.RegisterTenantV2{
		Basis: domain.RegistrationOnboarding, TenantOnboardingRequestID: request.ResourceID,
		OrganisationID: org, TenantID: domain.NewTenantID(),
		DisplayName:       "Synthetic Independently Reviewed Business",
		IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1",
		RequestedProducts: []string{"baobab-trade"},
	}
	if _, err = db.RegisterProgressiveTenantV2(ctx, "denied-"+domain.NewUUIDv7(), meta(people[5]), unauthorised); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("tenant registered before independent authorisation: %v", err)
	}
	_, err = db.AuthoriseProgressiveOnboarding(ctx, "authorise-"+domain.NewUUIDv7(), meta(people[4]),
		people[4], request.ResourceID, ProgressiveAuthorisationInput{
			PolicyReference: "policy/staging/onboarding", EvidenceReference: "evidence/staging/onboarding",
		})
	if err != nil {
		t.Fatal(err)
	}
	wrong := unauthorised
	wrong.TenantID = domain.NewTenantID()
	wrong.DisplayName = "Changed after approval"
	if _, err = db.RegisterProgressiveTenantV2(ctx, "changed-"+domain.NewUUIDv7(), meta(people[5]), wrong); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("unapproved desired state consumed: %v", err)
	}
	// The same applicant or maker is not a new, independent registrar.
	if _, err = db.RegisterProgressiveTenantV2(ctx, "maker-"+domain.NewUUIDv7(), meta(people[3]), unauthorised); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("onboarding requester also registered tenant: %v", err)
	}
	key := "register-" + domain.NewUUIDv7()
	op, err := db.RegisterProgressiveTenantV2(ctx, key, meta(people[5]), unauthorised)
	if err != nil {
		t.Fatal(err)
	}
	replay := unauthorised
	replay.TenantID = domain.NewTenantID()
	op2, err := db.RegisterProgressiveTenantV2(ctx, key, meta(people[5]), replay)
	if err != nil || op.OperationID != op2.OperationID || op.TenantID != op2.TenantID {
		t.Fatalf("replay duplicated registration: op=%+v op2=%+v err=%v", op, op2, err)
	}
	var status, fulfilledTenant, primary string
	err = db.pool.QueryRow(ctx, `SELECT status,tenant_id FROM admission.progressive_onboarding_request
		WHERE request_id=$1::uuid`, mustProgressiveTestID(t, request.ResourceID)).Scan(&status, &fulfilledTenant)
	if err != nil || status != "FULFILLED" || fulfilledTenant != op.TenantID {
		t.Fatalf("v2 request not atomically fulfilled: %s %s %v", status, fulfilledTenant, err)
	}
	err = db.pool.QueryRow(ctx, `SELECT organisation_id::text FROM registry.tenant_organisation_mapping
		WHERE tenant_id=$1 AND status='ACTIVE' AND mapping_role='PRIMARY_ORGANISATION'`, op.TenantID).Scan(&primary)
	if err != nil || primary != org {
		t.Fatalf("registered another operating Organisation: %s %v", primary, err)
	}
	var legacy, registered, events int
	if err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admission.tenant_onboarding_request
		WHERE tenant_id=$1`, op.TenantID).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE tenant_id=$1`, op.TenantID).Scan(&registered); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM messaging.outbox
		WHERE tenant_id=$1 AND event_type='com.baobab-platform.control-plane.tenant.provisioning-started.v1'`, op.TenantID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if legacy != 0 || registered != 1 || events != 1 {
		t.Fatalf("v2 registration lost atomicity: legacy=%d tenants=%d outbox=%d", legacy, registered, events)
	}
	second := unauthorised
	second.TenantID = domain.NewTenantID()
	if _, err = db.RegisterProgressiveTenantV2(ctx, "second-"+domain.NewUUIDv7(), meta(people[5]), second); !errors.Is(err, ErrProgressiveBridgeDenied) {
		t.Fatalf("second registration consumed fulfilled authority: %v", err)
	}
}
