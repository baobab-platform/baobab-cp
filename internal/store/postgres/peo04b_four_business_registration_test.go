package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

// A disposable DB proves the EXISTING human-reviewed v2 registration path can
// preserve four separate business Organisations and Tenants. This test never
// enters a real Nabhold, Thamani, ZuriBeans or Equator & Estate onboarding
// decision, and never grants CIPC VERIFIED, INTERNAL or live Trade/ERP rights.
func TestPEO04BFourFoundingBusinessShapesRequireIndependentGovernedTenantAdmission(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL 17 TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	type scenario struct {
		name    string
		legal   bool
		product string
		marker  string
	}
	cases := []scenario{
		{"Synthetic Nabhold holding company", true, "baobab-erp", "NABHOLD"},
		{"Synthetic Thamani logistics company", true, "baobab-trade", "THAMANI-GLOBAL"},
		{"Synthetic ZuriBeans trading business", false, "baobab-trade", "ZURIBEANS"},
		{"Synthetic Equator operating business", false, "baobab-trade", "EQUATOR-ESTATE"},
	}
	seenTenant := map[string]bool{}
	seenOrg := map[string]bool{}
	for _, c := range cases {
		t.Run(c.marker, func(t *testing.T) {
			var people [6]string
			for i := range people {
				if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type) VALUES('human') RETURNING principal_id::text`).Scan(&people[i]); err != nil {
					t.Fatal(err)
				}
			}
			meta := func(i int) basestore.RequestMetadata {
				return basestore.RequestMetadata{ActorID: people[i], ActorType: "human", CorrelationID: domain.NewUUIDv7()}
			}
			org := domain.NewUUIDv7()
			key := "PEO04B-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", ""))[:14]
			if _, err = db.pool.Exec(ctx, `INSERT INTO registry.canonical_entity(canonical_entity_id,entity_type,status)
    VALUES($1::uuid,'ORGANISATION','active')`, org); err != nil {
				t.Fatal(err)
			}
			if _, err = db.pool.Exec(ctx, `INSERT INTO registry.organisation_profile
    (canonical_entity_id,display_name,verification_state,source_authority,status,effective_from,evidence_references)
    VALUES($1::uuid,$2,'UNVERIFIED','test-synthetic-reviewed-admission','ACTIVE',$3,'[]'::jsonb)`,
				org, c.name, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			identityClass, claim := "OPERATING_BUSINESS", "NOT_INCORPORATED"
			if c.legal {
				identityClass, claim = "LEGAL_PERSON", "REGISTERED_CLAIMED"
			}
			if _, err = db.pool.Exec(ctx, `INSERT INTO registry.first_party_organisation_identity
    (first_party_id,organisation_id,identity_class,incorporation_claim,registry_evidence_reference)
    VALUES($1,$2::uuid,$3,$4,$5)`, key, org, identityClass, claim, "synthetic-test:"+c.marker); err != nil {
				t.Fatal(err)
			}
			form, incorporation := "UNINCORPORATED_ORGANISATION", "NOT_INCORPORATED"
			businessIdentity := map[string]any{
				"operating_name": c.name, "organisation_form": form,
				"operating_country": "ZA", "incorporation_claim": incorporation,
				"authorised_representative": map[string]any{"full_name": "Synthetic Human Applicant", "role": "owner"},
			}
			if c.legal {
				businessIdentity["organisation_form"] = "COMPANY"
				businessIdentity["incorporation_claim"] = "INCORPORATED_CLAIMED"
				businessIdentity["legal_name"] = c.name
				businessIdentity["jurisdiction_of_incorporation"] = "ZA"
				businessIdentity["registration_identifiers"] = []map[string]any{{
					"type": "COMPANY_REGISTRATION", "issuing_jurisdiction": "ZA",
					"value": "synthetic-not-an-official-registration",
				}}
			}
			raw, err := json.Marshal(map[string]any{
				"business_identity": businessIdentity,
				"requested_markets": []map[string]string{{"country_code": "ZA"}},
				"requirements":      map[string]bool{"operates_b2b": true},
			})
			if err != nil {
				t.Fatal(err)
			}
			app, err := db.CreateProgressiveApplication(ctx, meta(0), people[0], "peo04b-app-"+domain.NewUUIDv7(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.ChangeProgressiveApplication(ctx, meta(0), people[0], app.ID, nil, true); err != nil {
				t.Fatal(err)
			}
			review, err := db.ReviewProgressiveAdmission(ctx, "peo04b-review-"+domain.NewUUIDv7(), meta(1),
				people[1], app.ID, ProgressiveAdmissionReviewInput{
					OrganisationID:                    org,
					EvidenceReference:                 "synthetic:evidence/identity-review",
					IdentityResolutionPolicyReference: "policy:synthetic/organisation-first",
					SubscriptionType:                  "COMMERCIAL", MarketScope: []string{"ZA"},
					ProductRequirements: []string{c.product}, IsolationStrategy: "row_level_security",
				})
			if err != nil {
				t.Fatal(err)
			}
			approved, err := db.DecideProgressiveAdmission(ctx, "peo04b-decide-"+domain.NewUUIDv7(), meta(2),
				people[2], review.ResourceID, ProgressiveAdmissionDecisionInput{
					Decision:          "APPROVED",
					Reason:            "Synthetic controlled admission test only; not official legal status",
					EvidenceReference: "synthetic:evidence/independent-review",
				})
			if err != nil {
				t.Fatal(err)
			}
			request, err := db.RequestProgressiveOnboarding(ctx, "peo04b-request-"+domain.NewUUIDv7(), meta(3),
				people[3], approved.ResourceID, ProgressiveOnboardingInput{
					DisplayName: c.name, ResidencyRegion: "af-south-1",
					Reason:              "Synthetic founding structure for LA-T01/PEO-T19",
					MarketParticipation: []domain.OnboardingMarketParticipation{{Market: "ZA", Activities: []string{"SELLING"}}},
				})
			if err != nil {
				t.Fatal(err)
			}
			cmd := domain.RegisterTenantV2{
				Basis:                     domain.RegistrationOnboarding,
				TenantOnboardingRequestID: request.ResourceID,
				OrganisationID:            org, TenantID: domain.NewTenantID(), DisplayName: c.name,
				IsolationStrategy: "row_level_security", ResidencyRegion: "af-south-1",
				RequestedProducts: []string{c.product},
			}
			if _, err = db.RegisterProgressiveTenantV2(ctx, "peo04b-premature-"+domain.NewUUIDv7(), meta(5), cmd); err == nil {
				t.Fatal("unapproved founding tenant registered")
			}
			if _, err = db.AuthoriseProgressiveOnboarding(ctx, "peo04b-authorise-"+domain.NewUUIDv7(),
				meta(4), people[4], request.ResourceID, ProgressiveAuthorisationInput{
					PolicyReference:   "synthetic:policy/human-onboarding-authority",
					EvidenceReference: "synthetic:evidence/authorisation",
				}); err != nil {
				t.Fatal(err)
			}
			result, err := db.RegisterProgressiveTenantV2(ctx, "peo04b-register-"+domain.NewUUIDv7(), meta(5), cmd)
			if err != nil {
				t.Fatal(err)
			}
			if seenTenant[result.TenantID] || seenOrg[org] {
				t.Fatal("cross-business identity shared")
			}
			seenTenant[result.TenantID] = true
			seenOrg[org] = true
			var primary string
			if err = db.pool.QueryRow(ctx, `SELECT organisation_id::text FROM registry.tenant_organisation_mapping
    WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION' AND status='ACTIVE'`, result.TenantID).Scan(&primary); err != nil || primary != org {
				t.Fatalf("wrong business primary organisation: %s %v", primary, err)
			}
			var profiles, mandates, intents int
			if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM registry.legal_entity_profile WHERE organisation_id=$1::uuid`, org).Scan(&profiles); err != nil {
				t.Fatal(err)
			}
			if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM registry.operating_legal_actor_mandate WHERE tenant_id=$1`, result.TenantID).Scan(&mandates); err != nil {
				t.Fatal(err)
			}
			if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.progressive_onboarding_request
    WHERE tenant_id=$1 AND status='FULFILLED'`, result.TenantID).Scan(&intents); err != nil {
				t.Fatal(err)
			}
			if profiles != 0 || mandates != 0 || intents != 1 {
				t.Fatalf("registration manufactured company or mandate: profiles=%d mandates=%d fulfilled=%d", profiles, mandates, intents)
			}
			if c.legal {
				t.Logf("%s: declared incorporated, independent CIPC still PENDING; tenant %s has NO legal-person VERIFIED", c.marker, result.TenantID)
			}
			var prior int
			if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM registry.tenant_organisation_mapping
    WHERE tenant_id=$1 AND mapping_role='PRIMARY_ORGANISATION'`, result.TenantID).Scan(&prior); err != nil || prior != 1 {
				t.Fatalf("duplicate founding primary mapping: %d %v", prior, err)
			}
		})
	}
	if len(seenTenant) != 4 || len(seenOrg) != 4 {
		t.Fatalf("expected four isolated tenants and organisations")
	}
}
