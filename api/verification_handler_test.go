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
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestVerificationRoutes works a Ugandan admission case through every
// Verification route (ADR-BCP-023 OEV-03): a registration number is
// verified against a URSB registry record by a second reviewer; the legal
// name conflicts, is held as a discrepancy and resolved; the case concludes
// NOT_VERIFIED because the name was never verified. Along the way it proves
// that nobody checks or decides their own claim, that VERIFIED needs an
// authoritative source, that concluding needs verification:decide, and that
// every body conforms to the Shared contract.
func TestVerificationRoutes(t *testing.T) {
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

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	subject := "ce_ver" + suffix
	cleanup := func() {
		ids := `(SELECT case_id FROM evidence.verification_case WHERE subject_id = $1)`
		for _, stmt := range []string{
			`UPDATE evidence.claim SET current_result_id = NULL WHERE case_id IN ` + ids,
			`DELETE FROM evidence.result WHERE case_id IN ` + ids,
			`DELETE FROM evidence.check WHERE case_id IN ` + ids,
			`DELETE FROM evidence.discrepancy WHERE case_id IN ` + ids,
			`DELETE FROM evidence.claim_evidence WHERE claim_id IN (SELECT claim_id FROM evidence.claim WHERE case_id IN ` + ids + `)`,
			`DELETE FROM evidence.claim WHERE case_id IN ` + ids,
			`DELETE FROM evidence.verification_case WHERE subject_id = $1`,
			`UPDATE evidence.record SET supersedes = NULL WHERE document->'subject'->>'subject_id' = $1`,
			`DELETE FROM evidence.record WHERE document->'subject'->>'subject_id' = $1`,
		} {
			admin.Exec(ctx, stmt, subject)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	identities := repository.NewInMemoryRepository()
	for _, sub := range []string{"maker", "checker"} {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: sub, Status: "ACTIVE"}))
	}
	human := func(sub string, scopes ...string) auth.Principal {
		granted := map[string]struct{}{}
		for _, s := range scopes {
			granted[s] = struct{}{}
		}
		return auth.Principal{Subject: sub, Issuer: testRealm, ActorType: "human", TokenID: "t-" + sub + strings.Join(scopes, ""),
			Scopes: granted, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	all := []string{"verification:read", "verification:write", "verification:decide"}
	handler := New(Dependencies{Store: &fakeStore{}, Identities: identities, Verification: repo, AdminVerifier: tokenVerifier{
		"maker":            human("maker", all...),
		"checker":          human("checker", all...),
		"checker-nodecide": human("checker", "verification:read", "verification:write"),
	}})
	call := func(method, path, who string, headers map[string]string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+who)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	dir := contracttest.SharedDir(t)
	conforms := func(label, file, definition string, w *httptest.ResponseRecorder, status int, into any) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("%s: %d %s", label, w.Code, w.Body.String())
		}
		var body any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "evidence/v1/"+file+"#/$defs/"+definition), body)
		if into != nil {
			mustNoError(t, json.Unmarshal(w.Body.Bytes(), into))
		}
	}
	refused := func(label string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || !strings.Contains(w.Body.String(), code) {
			t.Fatalf("%s: want %d %s, got %d %s", label, status, code, w.Code, w.Body.String())
		}
	}
	type caseView struct {
		CaseID  string `json:"case_id"`
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	ifMatch := func(v int64) map[string]string { return map[string]string{"If-Match": entityTag(v)} }

	// Sources and evidence.
	conforms("sources", "evidence.schema.json", "EvidenceSourceList", call(http.MethodGet, "/v1/admin/evidence-sources", "maker", nil, nil), http.StatusOK, nil)
	le := map[string]string{"subject_type": "LEGAL_ENTITY", "subject_id": subject}
	registration := map[string]any{"evidence_type": "REGISTRY_EXTRACT", "subject": le, "purpose": "ORGANISATION_ADMISSION",
		"source_id": "esrc_ursb", "source_record_reference": "URSB-80020012345", "observed_at": "2026-10-02T10:00:00Z",
		"classification": "INTERNAL", "retention_policy": "organisation-admission/v1"}
	refused("evidence naming an upload", call(http.MethodPost, "/v1/admin/evidence", "checker", map[string]string{"Idempotency-Key": "evidence-art-" + suffix},
		map[string]any{"evidence_type": "REGISTRY_EXTRACT", "subject": le, "purpose": "ORGANISATION_ADMISSION", "source_id": "esrc_ursb",
			"artifact_id": "eart_x", "classification": "INTERNAL", "retention_policy": "organisation-admission/v1"}), http.StatusBadRequest, "VALIDATION_FAILED")
	unknown := map[string]any{}
	for k, v := range registration {
		unknown[k] = v
	}
	unknown["source_id"] = "esrc_nosuch"
	refused("evidence from an unregistered source", call(http.MethodPost, "/v1/admin/evidence", "checker",
		map[string]string{"Idempotency-Key": "evidence-unk-" + suffix}, unknown), http.StatusUnprocessableEntity, "EVIDENCE_SOURCE_UNKNOWN")
	var extract struct {
		EvidenceID string `json:"evidence_id"`
		ObtainedBy string `json:"obtained_by"`
		Status     string `json:"status"`
	}
	evidenceKey := map[string]string{"Idempotency-Key": "evidence-ursb-" + suffix}
	conforms("register evidence", "evidence.schema.json", "EvidenceRecord", call(http.MethodPost, "/v1/admin/evidence", "checker", evidenceKey, registration),
		http.StatusCreated, &extract)
	if extract.Status != "AVAILABLE" || !strings.HasPrefix(extract.ObtainedBy, "principal:") {
		t.Fatalf("registered evidence: %+v", extract)
	}
	var replay struct {
		EvidenceID string `json:"evidence_id"`
	}
	conforms("replayed evidence", "evidence.schema.json", "EvidenceRecord", call(http.MethodPost, "/v1/admin/evidence", "checker", evidenceKey, registration),
		http.StatusCreated, &replay)
	if replay.EvidenceID != extract.EvidenceID {
		t.Fatal("a replayed registration registered again")
	}
	w := call(http.MethodGet, "/v1/admin/evidence/"+extract.EvidenceID, "maker", nil, nil)
	conforms("evidence metadata", "evidence.schema.json", "EvidenceReference", w, http.StatusOK, nil)
	if strings.Contains(w.Body.String(), "URSB-80020012345") {
		t.Fatal("evidence metadata leaks the source record")
	}

	// A case, its claims and its progress to VERIFYING.
	caseKey := map[string]string{"Idempotency-Key": "case-open-" + suffix}
	var c caseView
	conforms("open case", "verification.schema.json", "VerificationCase", call(http.MethodPost, "/v1/admin/verification-cases", "maker", caseKey,
		map[string]any{"subject": le, "purpose": "ORGANISATION_ADMISSION", "organisation_id": subject}), http.StatusCreated, &c)
	base := "/v1/admin/verification-cases/" + c.CaseID
	addClaim := func(claimType, value string) string {
		var claim struct {
			ClaimID string `json:"claim_id"`
			Status  string `json:"status"`
		}
		conforms("claim "+claimType, "evidence.schema.json", "EvidenceClaim", call(http.MethodPost, base+"/claims", "maker", nil,
			map[string]any{"subject": le, "claim_type": claimType, "jurisdiction": "UG", "claimed_value": map[string]string{"value": value},
				"purpose": "ORGANISATION_ADMISSION", "evidence_ids": []string{extract.EvidenceID}}), http.StatusCreated, &claim)
		if claim.Status != "UNDER_VERIFICATION" {
			t.Fatalf("a reviewer's claim starts UNDER_VERIFICATION: %+v", claim)
		}
		return claim.ClaimID
	}
	regno, name := addClaim("REGISTRATION_IDENTIFIER", "80020012345"), addClaim("LEGAL_NAME", "ACME Foods Limited")
	refused("a claim for another purpose", call(http.MethodPost, base+"/claims", "maker", nil, map[string]any{"subject": le, "claim_type": "ENTITY_STATUS",
		"claimed_value": map[string]string{"value": "active"}, "purpose": "PERIODIC_REVIEW"}), http.StatusUnprocessableEntity, "VERIFICATION_UNSUPPORTED")
	other := map[string]string{"subject_type": "LEGAL_ENTITY", "subject_id": "ce_other" + suffix}
	refused("a claim about another subject", call(http.MethodPost, base+"/claims", "maker", nil, map[string]any{"subject": other, "claim_type": "ENTITY_STATUS",
		"claimed_value": map[string]string{"value": "active"}, "purpose": "ORGANISATION_ADMISSION"}), http.StatusUnprocessableEntity, "VERIFICATION_UNSUPPORTED")
	conforms("claims", "evidence.schema.json", "EvidenceClaimList", call(http.MethodGet, base+"/claims", "maker", nil, nil), http.StatusOK, nil)
	conforms("case", "verification.schema.json", "VerificationCase", call(http.MethodGet, base, "maker", nil, nil), http.StatusOK, &c)
	refused("a check before verifying", call(http.MethodPost, base+"/checks", "checker", nil, map[string]any{"claim_id": regno,
		"method": "MANUAL_REGISTRY_LOOKUP", "source_id": "esrc_ursb", "outcome": "NOT_VERIFIED", "dimensions": []any{}, "reason_codes": []string{},
		"evidence_ids": []string{}}), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")
	refused("a transition without If-Match", call(http.MethodPost, base+"/transitions", "maker", nil, map[string]any{"command": "start_collection"}),
		http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	refused("a stale transition", call(http.MethodPost, base+"/transitions", "maker", ifMatch(c.Version+5), map[string]any{"command": "start_collection"}),
		http.StatusPreconditionFailed, "VERSION_MISMATCH")
	refused("a conclusion through the working route", call(http.MethodPost, base+"/transitions", "maker", ifMatch(c.Version),
		map[string]any{"command": "complete_verified"}), http.StatusBadRequest, "VALIDATION_FAILED")
	for _, command := range []string{"start_collection", "submit", "begin_verification"} {
		conforms(command, "verification.schema.json", "VerificationCase", call(http.MethodPost, base+"/transitions", "maker", ifMatch(c.Version),
			map[string]any{"command": command}), http.StatusOK, &c)
	}
	if c.Status != "VERIFYING" {
		t.Fatalf("case: %+v", c)
	}

	// Checks: never by the claim's asserter; a positive one needs evidence.
	regCheck := map[string]any{"claim_id": regno, "method": "MANUAL_REGISTRY_LOOKUP", "source_id": "esrc_ursb",
		"source_record_reference": "URSB-80020012345", "outcome": "VERIFIED", "reason_codes": []string{}, "evidence_ids": []string{extract.EvidenceID},
		"dimensions": []map[string]string{{"dimension": "SOURCE_AUTHENTICITY", "outcome": "PASSED"}, {"dimension": "ISSUER_AUTHORITY", "outcome": "PASSED"},
			{"dimension": "CLAIM_MATCH", "outcome": "PASSED"}}}
	refused("the asserter checking their claim", call(http.MethodPost, base+"/checks", "maker", nil, regCheck), http.StatusForbidden, "VERIFICATION_SELF_CHECK")
	var check struct {
		CheckID     string `json:"check_id"`
		PerformedBy string `json:"performed_by"`
	}
	conforms("registry check", "verification.schema.json", "VerificationCheck", call(http.MethodPost, base+"/checks", "checker", nil, regCheck), http.StatusCreated, &check)
	docCheck := map[string]any{"claim_id": regno, "method": "OFFICIAL_DOCUMENT_REVIEW", "source_id": "esrc_applicant", "outcome": "MATCHED",
		"reason_codes": []string{}, "evidence_ids": []string{extract.EvidenceID},
		"dimensions": []map[string]string{{"dimension": "DOCUMENT_INTEGRITY", "outcome": "PASSED"}, {"dimension": "CLAIM_MATCH", "outcome": "PASSED"}}}
	refused("citing another source's evidence", call(http.MethodPost, base+"/checks", "checker", nil, docCheck), http.StatusUnprocessableEntity, "VERIFICATION_UNSUPPORTED")
	docCheck["evidence_ids"], docCheck["source_record_reference"] = []string{}, "applicant-certificate-"+suffix
	var applicantCheck struct {
		CheckID string `json:"check_id"`
	}
	conforms("applicant-source check", "verification.schema.json", "VerificationCheck", call(http.MethodPost, base+"/checks", "checker", nil, docCheck),
		http.StatusCreated, &applicantCheck)

	// Results: VERIFIED needs an authoritative source, and decide.
	verified := map[string]any{"claim_id": regno, "outcome": "VERIFIED", "freshness": "CURRENT", "reason_codes": []string{},
		"dimensions": []map[string]string{{"dimension": "ISSUER_AUTHORITY", "outcome": "PASSED"}, {"dimension": "CLAIM_MATCH", "outcome": "PASSED"}}}
	onApplicant := map[string]any{}
	for k, v := range verified {
		onApplicant[k] = v
	}
	onApplicant["check_ids"] = []string{applicantCheck.CheckID}
	refused("VERIFIED on an applicant source alone", call(http.MethodPost, base+"/results", "checker", nil, onApplicant), http.StatusUnprocessableEntity, "VERIFICATION_UNSUPPORTED")
	verified["check_ids"] = []string{check.CheckID, applicantCheck.CheckID}
	refused("a result without decide", call(http.MethodPost, base+"/results", "checker-nodecide", nil, verified), http.StatusForbidden, "AUTHORIZATION_DENIED")
	refused("the asserter deciding their claim", call(http.MethodPost, base+"/results", "maker", nil, verified), http.StatusForbidden, "VERIFICATION_SELF_CHECK")
	conforms("VERIFIED result", "verification.schema.json", "VerificationResult", call(http.MethodPost, base+"/results", "checker", nil, verified), http.StatusCreated, nil)

	// The legal name conflicts with the registry.
	nameCheck := map[string]any{"claim_id": name, "method": "MANUAL_REGISTRY_LOOKUP", "source_id": "esrc_ursb", "outcome": "CONFLICTED",
		"reason_codes": []string{"LEGAL_NAME_MISMATCH"}, "evidence_ids": []string{extract.EvidenceID},
		"dimensions": []map[string]string{{"dimension": "ISSUER_AUTHORITY", "outcome": "PASSED"}, {"dimension": "CLAIM_MATCH", "outcome": "FAILED"}}}
	var nc struct {
		CheckID string `json:"check_id"`
	}
	conforms("name check", "verification.schema.json", "VerificationCheck", call(http.MethodPost, base+"/checks", "checker", nil, nameCheck), http.StatusCreated, &nc)
	var disc struct {
		DiscrepancyID string `json:"discrepancy_id"`
		Status        string `json:"status"`
		Version       int64  `json:"version"`
		ResolvedBy    string `json:"resolved_by"`
	}
	conforms("discrepancy", "verification.schema.json", "EvidenceDiscrepancy", call(http.MethodPost, base+"/discrepancies", "checker", nil,
		map[string]any{"subject": le, "claim_type": "LEGAL_NAME", "severity": "MEDIUM", "conflicting_values": []map[string]string{
			{"source_id": "esrc_applicant", "value": "ACME Foods Limited"},
			{"source_id": "esrc_ursb", "value": "ACME Foods (U) Limited", "evidence_id": extract.EvidenceID, "check_id": nc.CheckID}}}), http.StatusCreated, &disc)
	conforms("CONFLICTED result", "verification.schema.json", "VerificationResult", call(http.MethodPost, base+"/results", "checker", nil,
		map[string]any{"claim_id": name, "outcome": "CONFLICTED", "check_ids": []string{nc.CheckID}, "freshness": "CURRENT",
			"reason_codes": []string{"LEGAL_NAME_MISMATCH"}, "discrepancy_ids": []string{disc.DiscrepancyID},
			"dimensions": []map[string]string{{"dimension": "CLAIM_MATCH", "outcome": "FAILED"}}}), http.StatusCreated, nil)
	conforms("case after conflict", "verification.schema.json", "VerificationCase", call(http.MethodGet, base, "maker", nil, nil), http.StatusOK, &c)
	if c.Status != "CONFLICTED" {
		t.Fatalf("a CONFLICTED result stops the case: %+v", c)
	}

	// The discrepancy is reviewed, then closed only under decide.
	dpath := "/v1/admin/evidence-discrepancies/" + disc.DiscrepancyID
	refused("resuming with the discrepancy open", call(http.MethodPost, base+"/transitions", "checker", ifMatch(c.Version),
		map[string]any{"command": "resume"}), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")
	conforms("begin review", "verification.schema.json", "EvidenceDiscrepancy", call(http.MethodPost, dpath+"/transitions", "checker", ifMatch(disc.Version),
		map[string]any{"command": "begin_review"}), http.StatusOK, &disc)
	refused("resuming with the discrepancy under review", call(http.MethodPost, base+"/transitions", "checker", ifMatch(c.Version),
		map[string]any{"command": "resume"}), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")
	resolution := map[string]any{"command": "resolve", "resolution": "SOURCE_VALUE_ADOPTED", "reason": "The registry name is authoritative."}
	refused("closing without decide", call(http.MethodPost, dpath+"/resolution", "checker-nodecide", ifMatch(disc.Version), resolution), http.StatusForbidden, "AUTHORIZATION_DENIED")
	conforms("resolve", "verification.schema.json", "EvidenceDiscrepancy", call(http.MethodPost, dpath+"/resolution", "checker", ifMatch(disc.Version), resolution),
		http.StatusOK, &disc)
	if disc.Status != "RESOLVED" || disc.ResolvedBy == "" {
		t.Fatalf("resolved discrepancy: %+v", disc)
	}
	conforms("discrepancies", "verification.schema.json", "EvidenceDiscrepancyList", call(http.MethodGet, base+"/discrepancies", "maker", nil, nil), http.StatusOK, nil)

	// The name was never verified: the case concludes NOT_VERIFIED.
	conforms("resume", "verification.schema.json", "VerificationCase", call(http.MethodPost, base+"/transitions", "checker", ifMatch(c.Version),
		map[string]any{"command": "resume"}), http.StatusOK, &c)
	conclusion := map[string]any{"command": "complete_verified", "reason": "Every claim checked."}
	refused("verifying with a claim unverified", call(http.MethodPost, base+"/conclusion", "checker", ifMatch(c.Version), conclusion),
		http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")
	conclusion["command"], conclusion["reason"] = "complete_not_verified", "The applicant's legal name does not match the registry."
	refused("concluding without decide", call(http.MethodPost, base+"/conclusion", "checker-nodecide", ifMatch(c.Version), conclusion), http.StatusForbidden, "AUTHORIZATION_DENIED")
	conforms("conclude", "verification.schema.json", "VerificationCase", call(http.MethodPost, base+"/conclusion", "checker", ifMatch(c.Version), conclusion),
		http.StatusOK, &c)
	if c.Status != "NOT_VERIFIED" {
		t.Fatalf("concluded case: %+v", c)
	}
	refused("a claim on a concluded case", call(http.MethodPost, base+"/claims", "maker", nil, map[string]any{"subject": le, "claim_type": "ENTITY_STATUS",
		"claimed_value": map[string]string{"value": "active"}, "purpose": "ORGANISATION_ADMISSION"}), http.StatusConflict, "VERIFICATION_CASE_STATE_CONFLICT")

	// Lists conform.
	conforms("checks", "verification.schema.json", "VerificationCheckList", call(http.MethodGet, base+"/checks", "maker", nil, nil), http.StatusOK, nil)
	conforms("results", "verification.schema.json", "VerificationResultList", call(http.MethodGet, base+"/results", "maker", nil, nil), http.StatusOK, nil)
	conforms("cases", "verification.schema.json", "VerificationCasePage", call(http.MethodGet, "/v1/admin/verification-cases?subject_id="+subject, "maker", nil, nil),
		http.StatusOK, nil)
	refused("an unknown case", call(http.MethodGet, "/v1/admin/verification-cases/vcase_nosuch", "maker", nil, nil), http.StatusNotFound, "VERIFICATION_NOT_FOUND")
	refused("a bad status filter", call(http.MethodGet, "/v1/admin/verification-cases?status=DONE", "maker", nil, nil), http.StatusBadRequest, "VALIDATION_FAILED")

	// Every command is audited.
	var audited int
	mustNoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE target = $1`, "verification-case/"+c.CaseID).Scan(&audited))
	if audited < 14 {
		t.Fatalf("expected every case command audited, got %d", audited)
	}
}
