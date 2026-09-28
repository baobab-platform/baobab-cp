package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestApplicantClaimRoutes is ADR-BCP-023 sections 7, 9 and 191-192 over
// HTTP: an applicant asserts claims on their own application, which open
// and join the application's one ORGANISATION_ADMISSION case, SELF_ASSERTED;
// they never name a subject, reach someone else's application or claim, or
// add claims once the application is submitted. A reviewer takes a claim
// under verification at its version.
func TestApplicantClaimRoutes(t *testing.T) {
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
	dir := contracttest.SharedDir(t)
	conforms := func(name, def string, response *httptest.ResponseRecorder, want int, into any) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("%s: %d %s", name, response.Code, response.Body.String())
		}
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "evidence/v1/evidence.schema.json#/$defs/"+def), json.RawMessage(response.Body.Bytes()))
		if into != nil {
			if err := json.Unmarshal(response.Body.Bytes(), into); err != nil {
				t.Fatal(err)
			}
		}
	}
	refused := func(name string, response *httptest.ResponseRecorder, want int, code string) {
		t.Helper()
		if response.Code != want || problemCode(t, response) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, response.Code, response.Body.String(), want, code)
		}
	}

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	applicant := func(sub string) auth.Principal {
		return auth.Principal{Subject: sub + suffix, Issuer: testRealm, ActorType: "human", TokenID: "t-" + sub,
			Scopes: map[string]struct{}{"application:read": {}, "application:write": {}}}
	}
	reviewerID := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	mustNoError(t, repo.CreateIdentity(ctx, reviewerID))
	mustNoError(t, repo.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
		PrincipalID: reviewerID.ID, Issuer: testRealm, Subject: "reviewer" + suffix, Status: "ACTIVE"}))
	reviewer := auth.Principal{Subject: "reviewer" + suffix, Issuer: testRealm, ActorType: "human", TokenID: "t-reviewer",
		Scopes: map[string]struct{}{"verification:read": {}, "verification:write": {}, "verification:decide": {}},
		Roles:  map[string]struct{}{RolePlatformAdmin: {}}}
	handler := New(Dependencies{Store: &fakeStore{}, Identities: repo, Verification: repo,
		Applications:  &application.Service{Repo: repo},
		AdminVerifier: tokenVerifier{"alice": applicant("alice"), "bob": applicant("bob"), "reviewer": reviewer}})
	call := func(method, path, who string, headers map[string]string, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		r.Header.Set("Authorization", "Bearer "+who)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	ifMatch := func(version int64) map[string]string { return map[string]string{"If-Match": entityTag(version)} }

	draft := `{"organisation_profile": {"legal_name": "Kilima Fresh Produce Ltd", "organisation_form": "COMPANY",
		"jurisdiction_of_incorporation": "UG", "registration_identifiers": [{"type": "COMPANY_REGISTRATION", "value": "8002` + suffix + `",
		"issuing_jurisdiction": "UG"}], "authorised_representative": {"full_name": "Applicant Representative", "role": "Managing Director"}},
		"requirements": {"operates_b2b": true, "trades_cross_border": true}, "requested_markets": [{"country_code": "UG"}],
		"evidence": [{"evidence_type": "REGISTRATION_EVIDENCE", "evidence_reference": "evd_kilima_certificate"}]}`
	created := call(http.MethodPost, "/v1/client-applications", "alice", nil, draft)
	if created.Code != http.StatusCreated {
		t.Fatalf("create application: %d %s", created.Code, created.Body.String())
	}
	var app struct {
		ID string `json:"client_application_id"`
	}
	mustNoError(t, json.Unmarshal(created.Body.Bytes(), &app))
	t.Cleanup(func() {
		ids := `(SELECT case_id FROM evidence.verification_case WHERE subject_id = $1)`
		for _, stmt := range []string{
			`UPDATE evidence.claim SET current_result_id = NULL WHERE case_id IN ` + ids,
			`DELETE FROM evidence.result WHERE case_id IN ` + ids,
			`DELETE FROM evidence.check WHERE case_id IN ` + ids,
			`DELETE FROM evidence.claim WHERE case_id IN ` + ids,
			`DELETE FROM evidence.verification_case WHERE subject_id = $1`,
			`DELETE FROM evidence.record WHERE document->'subject'->>'subject_id' = $1`,
		} {
			admin.Exec(ctx, stmt, app.ID)
		}
	})
	claims := "/v1/client-applications/" + app.ID + "/claims"

	type claimView struct {
		ClaimID     string `json:"claim_id"`
		Status      string `json:"status"`
		AssertedVia string `json:"asserted_via"`
		Version     int64  `json:"version"`
		Subject     struct {
			SubjectType string `json:"subject_type"`
			SubjectID   string `json:"subject_id"`
		} `json:"subject"`
	}
	var name, regno claimView
	conforms("legal name claim", "EvidenceClaim", call(http.MethodPost, claims, "alice", nil,
		`{"claim_type": "LEGAL_NAME", "jurisdiction": "UG", "claimed_value": {"value": "Kilima Fresh Produce Ltd"}}`), http.StatusCreated, &name)
	if name.Status != "SELF_ASSERTED" || name.AssertedVia != "APPLICANT" || name.Subject.SubjectType != "APPLICATION" || name.Subject.SubjectID != app.ID {
		t.Fatalf("an applicant's claim is SELF_ASSERTED about their application: %+v", name)
	}
	conforms("registration claim", "EvidenceClaim", call(http.MethodPost, claims, "alice", nil,
		`{"claim_type": "REGISTRATION_IDENTIFIER", "jurisdiction": "UG", "claimed_value": {"value": "8002`+suffix+`"}}`), http.StatusCreated, &regno)
	var cases int
	mustNoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM evidence.verification_case WHERE subject_type = 'APPLICATION' AND subject_id = $1`, app.ID).Scan(&cases))
	if cases != 1 {
		t.Fatalf("an application has one admission case, got %d", cases)
	}
	refused("an applicant naming the subject", call(http.MethodPost, claims, "alice", nil,
		`{"subject": {"subject_type": "LEGAL_ENTITY", "subject_id": "LE-X"}, "claim_type": "LEGAL_NAME", "claimed_value": {"value": "x"}}`),
		http.StatusBadRequest, "VALIDATION_FAILED")
	refused("an applicant asserting verification", call(http.MethodPost, claims, "alice", nil,
		`{"claim_type": "LEGAL_NAME", "claimed_value": {"value": "x"}, "verified": true}`), http.StatusBadRequest, "VALIDATION_FAILED")
	refused("a claim on someone else's application", call(http.MethodPost, claims, "bob", nil,
		`{"claim_type": "LEGAL_NAME", "claimed_value": {"value": "x"}}`), http.StatusNotFound, "CLIENT_APPLICATION_NOT_FOUND")
	refused("reading someone else's claims", call(http.MethodGet, claims, "bob", nil, ""), http.StatusNotFound, "CLIENT_APPLICATION_NOT_FOUND")
	var list struct {
		Items []claimView `json:"items"`
	}
	conforms("claims", "EvidenceClaimList", call(http.MethodGet, claims, "alice", nil, ""), http.StatusOK, &list)
	if len(list.Items) != 2 {
		t.Fatalf("claims: %+v", list)
	}

	// Creating a claim is replayable (Idempotency-Key).
	keyed := map[string]string{"Idempotency-Key": "applicant-claim-" + suffix}
	status := `{"claim_type": "ENTITY_STATUS", "jurisdiction": "UG", "claimed_value": {"value": "active"}}`
	var entity, replayed claimView
	conforms("keyed claim", "EvidenceClaim", call(http.MethodPost, claims, "alice", keyed, status), http.StatusCreated, &entity)
	conforms("replayed claim", "EvidenceClaim", call(http.MethodPost, claims, "alice", keyed, status), http.StatusOK, &replayed)
	if replayed.ClaimID != entity.ClaimID {
		t.Fatalf("a replay minted a second claim: %s and %s", entity.ClaimID, replayed.ClaimID)
	}
	refused("a key reused for another claim", call(http.MethodPost, claims, "alice", keyed,
		`{"claim_type": "ENTITY_STATUS", "claimed_value": {"value": "dissolved"}}`), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")

	// A reviewer takes the registration claim under verification.
	var cases2 struct {
		Items []struct {
			CaseID string `json:"case_id"`
		} `json:"items"`
	}
	listed := call(http.MethodGet, "/v1/admin/verification-cases?subject_id="+app.ID, "reviewer", nil, "")
	mustNoError(t, json.Unmarshal(listed.Body.Bytes(), &cases2))
	if len(cases2.Items) != 1 {
		t.Fatalf("the reviewer finds the application's case: %d %s", listed.Code, listed.Body.String())
	}
	open := "/v1/admin/verification-cases/" + cases2.Items[0].CaseID + "/claims/" + regno.ClaimID + "/open-verification"
	refused("a stale version", call(http.MethodPost, open, "reviewer", ifMatch(regno.Version+1), ""), http.StatusPreconditionFailed, "VERSION_MISMATCH")
	refused("an applicant opening verification", call(http.MethodPost, open, "alice", ifMatch(regno.Version), ""), http.StatusForbidden, "AUTHORIZATION_DENIED")
	conforms("open verification", "EvidenceClaim", call(http.MethodPost, open, "reviewer", ifMatch(regno.Version), ""), http.StatusOK, &regno)
	if regno.Status != "UNDER_VERIFICATION" {
		t.Fatalf("opened claim: %+v", regno)
	}
	refused("opening it twice", call(http.MethodPost, open, "reviewer", ifMatch(regno.Version), ""), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")

	// Only the asserter withdraws, at the claim's version.
	withdraw := claims + "/" + name.ClaimID + "/withdraw"
	refused("withdrawing without If-Match", call(http.MethodPost, withdraw, "alice", nil, ""), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	refused("withdrawing someone else's claim", call(http.MethodPost, withdraw, "bob", ifMatch(name.Version), ""), http.StatusNotFound, "CLIENT_APPLICATION_NOT_FOUND")
	conforms("withdraw", "EvidenceClaim", call(http.MethodPost, withdraw, "alice", ifMatch(name.Version), ""), http.StatusOK, &name)
	if name.Status != "WITHDRAWN" {
		t.Fatalf("withdrawn claim: %+v", name)
	}
	refused("withdrawing twice", call(http.MethodPost, withdraw, "alice", ifMatch(name.Version), ""), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")

	// Withdrawn claims no longer stand: the case concludes VERIFIED on the
	// one claim the registry verified (sections 191-192).
	conforms("withdraw entity status", "EvidenceClaim", call(http.MethodPost, claims+"/"+entity.ClaimID+"/withdraw", "alice",
		ifMatch(entity.Version), ""), http.StatusOK, &entity)
	base := "/v1/admin/verification-cases/" + cases2.Items[0].CaseID
	caseVersion := func() int64 {
		t.Helper()
		var c struct {
			Version int64 `json:"version"`
		}
		mustNoError(t, json.Unmarshal(call(http.MethodGet, base, "reviewer", nil, "").Body.Bytes(), &c))
		return c.Version
	}
	for _, command := range []string{"start_collection", "submit", "begin_verification"} {
		if moved := call(http.MethodPost, base+"/transitions", "reviewer", ifMatch(caseVersion()), `{"command": "`+command+`"}`); moved.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", command, moved.Code, moved.Body.String())
		}
	}
	subject := `{"subject_type": "APPLICATION", "subject_id": "` + app.ID + `"}`
	registered := call(http.MethodPost, "/v1/admin/evidence", "reviewer", map[string]string{"Idempotency-Key": "application-evidence-" + suffix},
		`{"evidence_type": "REGISTRY_EXTRACT", "subject": `+subject+`, "purpose": "ORGANISATION_ADMISSION", "source_id": "esrc_ursb",
		"source_record_reference": "URSB-8002`+suffix+`", "observed_at": "2026-10-02T10:00:00Z", "classification": "INTERNAL",
		"retention_policy": "organisation-admission/v1"}`)
	var extract struct {
		EvidenceID string `json:"evidence_id"`
	}
	mustNoError(t, json.Unmarshal(registered.Body.Bytes(), &extract))
	checked := call(http.MethodPost, base+"/checks", "reviewer", nil, `{"claim_id": "`+regno.ClaimID+`", "method": "MANUAL_REGISTRY_LOOKUP",
		"source_id": "esrc_ursb", "source_record_reference": "URSB-8002`+suffix+`", "outcome": "VERIFIED", "reason_codes": [],
		"evidence_ids": ["`+extract.EvidenceID+`"], "dimensions": [{"dimension": "SOURCE_AUTHENTICITY", "outcome": "PASSED"},
		{"dimension": "ISSUER_AUTHORITY", "outcome": "PASSED"}, {"dimension": "CLAIM_MATCH", "outcome": "PASSED"}]}`)
	var check struct {
		CheckID string `json:"check_id"`
	}
	mustNoError(t, json.Unmarshal(checked.Body.Bytes(), &check))
	if result := call(http.MethodPost, base+"/results", "reviewer", nil, `{"claim_id": "`+regno.ClaimID+`", "outcome": "VERIFIED",
		"check_ids": ["`+check.CheckID+`"], "freshness": "CURRENT", "reason_codes": [],
		"dimensions": [{"dimension": "ISSUER_AUTHORITY", "outcome": "PASSED"}, {"dimension": "CLAIM_MATCH", "outcome": "PASSED"}]}`); result.Code != http.StatusCreated {
		t.Fatalf("check %d %s; result %d %s", checked.Code, checked.Body.String(), result.Code, result.Body.String())
	}
	if concluded := call(http.MethodPost, base+"/conclusion", "reviewer", ifMatch(caseVersion()),
		`{"command": "complete_verified", "reason": "The registry verified the registration number."}`); concluded.Code != http.StatusOK {
		t.Fatalf("withdrawn claims must not block the conclusion: %d %s", concluded.Code, concluded.Body.String())
	}
	refused("a claim on a concluded case", call(http.MethodPost, claims, "alice", nil,
		`{"claim_type": "LEGAL_NAME", "claimed_value": {"value": "Kilima"}}`), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")

	// A submitted application takes no new claims.
	if submitted := call(http.MethodPost, "/v1/client-applications/"+app.ID+"/submit", "alice", nil, ""); submitted.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", submitted.Code, submitted.Body.String())
	}
	refused("a claim on a submitted application", call(http.MethodPost, claims, "alice", nil,
		`{"claim_type": "ENTITY_STATUS", "claimed_value": {"value": "active"}}`), http.StatusConflict, "APPLICATION_NOT_EDITABLE")
}
