package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

func TestPEO03CSyntheticSourceOnlyInExplicitNonproduction(t *testing.T) {
	for _, env := range []string{"", "staging", "production", "unknown", "prod", "local"} {
		if sourceObservationTestEnvironment(env) {
			t.Fatalf("synthetic evidence admitted in production-class environment %q", env)
		}
	}
	for _, env := range []string{"development", "test", "integration", "sandbox"} {
		if !sourceObservationTestEnvironment(env) {
			t.Fatalf("synthetic test fixture denied in explicit test environment %q", env)
		}
	}
}

// PEO-03C intentionally operates on synthetic cases in an ephemeral PG17
// integration environment. Append-only provenance is NOT deleted afterwards.
// Set TEST_DATABASE_URL only for a disposable, isolated test database.
func TestPEO03CDurableAuthoritativeSourceObservation(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required for PostgreSQL 17 evidence integration")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	repo.Environment = "integration"
	defer repo.Close()
	seed, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	caseID, claimID, evidenceID := "vcase_"+suffix, "ecl_"+suffix, "evr_"+suffix
	obsRef := "synthetic/source-observation/" + suffix
	company := "Synthetic-Company-" + suffix
	subject := verification.Subject{SubjectType: "ORGANISATION", SubjectID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	vercase := verification.Case{CaseID: caseID, Subject: subject,
		Purpose: "ORGANISATION_ADMISSION", Status: verification.CaseVerifying,
		ClaimIDs: []string{claimID}, OpenedAt: now, Version: 1}
	claim := verification.Claim{ClaimID: claimID, Subject: subject,
		ClaimType: "LEGAL_NAME", Jurisdiction: "ZA",
		ClaimedValue: verification.ClaimedValue{Value: company},
		AssertedBy:   "synthetic-applicant", AssertedVia: "APPLICANT",
		AssertedAt: now, Purpose: vercase.Purpose, Status: verification.ClaimUnderCheck,
		EvidenceIDs: []string{evidenceID}, Version: 1}
	evidence := verification.Evidence{EvidenceID: evidenceID, SourceID: "esrc_cipc",
		EvidenceType: "REGISTRY_EXTRACT", Subject: subject, Purpose: vercase.Purpose,
		SourceRecordReference: obsRef, ObtainedBy: "synthetic-authorised-connector",
		Classification: "CONFIDENTIAL", RetentionPolicy: "synthetic-only",
		Status: verification.EvidenceAvail, CreatedAt: now, Version: 1}
	encode := func(v any) []byte {
		t.Helper()
		j, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return j
	}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.verification_case
	  (case_id,subject_type,subject_id,status,opened_by,opened_at,document)
	  VALUES($1,$2,$3,'VERIFYING','synthetic-platform',$4,$5::jsonb)`,
		caseID, subject.SubjectType, subject.SubjectID, now, encode(vercase)); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.claim
	  (claim_id,case_id,asserted_by,status,document,created_at)
	  VALUES($1,$2,$3,'UNDER_VERIFICATION',$4::jsonb,$5)`,
		claimID, caseID, claim.AssertedBy, encode(claim), now); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.record
	  (evidence_id,source_id,status,obtained_by,document,created_at)
	  VALUES($1,'esrc_cipc','AVAILABLE',$2,$3::jsonb,$4)`,
		evidenceID, evidence.ObtainedBy, encode(evidence), now); err != nil {
		t.Fatal(err)
	}
	obs := verification.SourceObservation{
		CaseID: caseID, ClaimID: claimID, Subject: subject, ClaimType: "LEGAL_NAME",
		Jurisdiction: "ZA", Purpose: vercase.Purpose,
		SourceID: "esrc_cipc", Method: "SOURCE_API_LOOKUP", Outcome: "MATCHED",
		SourceReference: obsRef, ProvenanceRef: "synthetic/immutable-proof/" + suffix,
		ResponseDigest: "sha256:" + strings.Repeat("c", 64),
		ObservedAt:     now.Add(-time.Second), FreshnessUntil: now.Add(time.Hour),
		AdapterID: "baobab.cipc.synthetic", Synthetic: true,
	}
	actor := AuditActor{ActorID: "prn_test" + suffix[:12], ActorType: "human",
		CorrelationID: domain.NewUUIDv7()}
	key := "source-observation-" + suffix

	id, err := repo.RecordAuthoritativeSourceObservation(ctx, caseID, obs, evidenceID, key, now, actor, true)
	if err != nil || !strings.HasPrefix(id, "aobs_") {
		t.Fatalf("durable source observation rejected: id=%s err=%v", id, err)
	}
	replay, err := repo.RecordAuthoritativeSourceObservation(ctx, caseID, obs, evidenceID, key, now, actor, true)
	if err != nil || replay != id {
		t.Fatalf("idempotent source replay failed: %s %v", replay, err)
	}
	altered := obs
	altered.SourceReference = "synthetic/other-company"
	if _, err := repo.RecordAuthoritativeSourceObservation(ctx, caseID, altered, evidenceID, key,
		now, actor, true); !errors.Is(err, ErrAuthoritativeSourceConflict) {
		t.Fatalf("reused key changed source provenance: %v", err)
	}
	if _, err := repo.RecordAuthoritativeSourceObservation(ctx, caseID, obs, evidenceID,
		key+"-another", now, actor, false); !errors.Is(err, ErrAuthoritativeSourceConflict) {
		t.Fatalf("synthetic official lookup promoted to real result: %v", err)
	}
	var storedEvidence, storedDigest string
	if err := seed.QueryRow(ctx, `SELECT evidence_id,response_digest FROM
	 evidence.authoritative_source_observation WHERE observation_id=$1`, id).
		Scan(&storedEvidence, &storedDigest); err != nil {
		t.Fatal(err)
	}
	if storedEvidence != evidenceID || storedDigest != obs.ResponseDigest {
		t.Fatal("persisted source proof lost its evidence binding")
	}
	// Database immutability is an independent defence against direct mutation.
	if _, err := seed.Exec(ctx, `UPDATE evidence.authoritative_source_observation
	 SET outcome='INCONCLUSIVE' WHERE observation_id=$1`, id); err == nil {
		t.Fatal("append-only source observation was modified")
	}
	var resultCount int
	if err := seed.QueryRow(ctx, `SELECT COUNT(*) FROM evidence.result WHERE claim_id=$1`,
		claimID).Scan(&resultCount); err != nil {
		t.Fatal(err)
	}
	if resultCount != 0 {
		t.Fatal("observation must not auto-create an OEV-03 VERIFIED decision")
	}
	// PEO-03D: no reviewer can forge a positive official CIPC lookup.
	// Check creation must consume an immutable source observation and bind
	// precisely its case, claim, official reference, expiry and EvidenceRecord.
	sourceCandidate := verification.CheckRecord{
		ClaimID: claimID, SourceID: "esrc_cipc",
		Method: "SOURCE_API_LOOKUP", Outcome: "MATCHED",
		SourceRecordReference: "unobserved/foreign-company",
		Dimensions: []verification.Dimension{
			{Dimension: "CLAIM_MATCH", Outcome: "PASSED"},
			{Dimension: "ISSUER_AUTHORITY", Outcome: "PASSED"},
		},
		ReasonCodes: []string{}, EvidenceIDs: []string{},
	}
	if _, err := repo.RecordVerificationCheck(ctx, caseID, sourceCandidate,
		domain.NewResourceID("vchk"), actor.ActorID, now, actor); !errors.Is(err, ErrAuthoritativeSourceConflict) {
		t.Fatalf("unobserved CIPC record created an authoritative check: %v", err)
	}
	sourceCandidate.SourceRecordReference = obsRef
	sourceCheck, err := repo.RecordVerificationCheck(ctx, caseID, sourceCandidate,
		domain.NewResourceID("vchk"), actor.ActorID, now, actor)
	if err != nil {
		t.Fatalf("verified source check not linked to immutable evidence: %v", err)
	}
	if len(sourceCheck.EvidenceIDs) != 1 || sourceCheck.EvidenceIDs[0] != evidenceID ||
		sourceCheck.SourceObservedAt == nil ||
		!sourceCheck.SourceObservedAt.Equal(obs.ObservedAt) ||
		sourceCheck.FreshnessUntil == nil ||
		!sourceCheck.FreshnessUntil.Equal(obs.FreshnessUntil) {
		t.Fatalf("CIPC check did not inherit exact source evidence: %+v", sourceCheck)
	}
	if resultCount != 0 {
		t.Fatal("source-backed check must not create an OEV-03 decision")
	}
	// Prove that a subsequent legal-name lookup can only use a separate,
	// source-backed VERIFIED registration identifier for this exact case and
	// Organisation. An applicant claim or arbitrary registry number is not
	// a substitute for the CP ledger's current result.
	regClaimID, checkID, resultID := "ecl_reg"+suffix, "vchk_reg"+suffix, "vres_reg"+suffix
	reg := claim
	reg.ClaimID = regClaimID
	reg.ClaimType = "REGISTRATION_IDENTIFIER"
	reg.ClaimedValue.Value = "2020/123456/07"
	reg.EvidenceIDs = []string{}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.claim
	  (claim_id,case_id,asserted_by,status,document,created_at)
	  VALUES($1,$2,$3,'UNDER_VERIFICATION',$4::jsonb,$5)`,
		regClaimID, caseID, reg.AssertedBy, encode(reg), now); err != nil {
		t.Fatal(err)
	}
	// Register an independent, matching source observation for this
	// registration claim BEFORE the check is recorded. A historical check
	// without such provenance must never bind another company identity.
	regEvidenceID := "evr_reg" + suffix
	regProof := evidence
	regProof.EvidenceID = regEvidenceID
	regProof.SourceRecordReference = obsRef + "/registration"
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.record
	  (evidence_id,source_id,status,obtained_by,document,created_at)
	  VALUES($1,'esrc_cipc','AVAILABLE',$2,$3::jsonb,$4)`,
		regEvidenceID, regProof.ObtainedBy, encode(regProof), now); err != nil {
		t.Fatal(err)
	}
	regObservation := obs
	regObservation.ClaimID = regClaimID
	regObservation.ClaimType = "REGISTRATION_IDENTIFIER"
	regObservation.SourceReference = regProof.SourceRecordReference
	if _, err := repo.RecordAuthoritativeSourceObservation(ctx, caseID,
		regObservation, regEvidenceID, key+"-registration", now, actor, true); err != nil {
		t.Fatalf("could not persist independently witnessed CIPC registration: %v", err)
	}
	check := verification.Check{
		CheckID: checkID, CaseID: caseID, ClaimID: regClaimID,
		SourceID: "esrc_cipc", Method: "SOURCE_API_LOOKUP",
		Outcome: "VERIFIED", RequestedAt: now, PerformedAt: &now,
		PerformedBy: "synthetic-reviewer", SourceRecordReference: regObservation.SourceReference,
		Dimensions:  []verification.Dimension{{Dimension: "CLAIM_MATCH", Outcome: "PASSED"}},
		ReasonCodes: []string{}, EvidenceIDs: []string{},
	}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.check
	  (check_id,case_id,claim_id,source_id,outcome,performed_by,performed_at,document)
	  VALUES($1,$2,$3,'esrc_cipc','VERIFIED',$4,$5,$6::jsonb)`,
		checkID, caseID, regClaimID, check.PerformedBy, now, encode(check)); err != nil {
		t.Fatal(err)
	}
	until := now.Add(time.Hour)
	res := verification.Result{
		ResultID: resultID, CaseID: caseID, ClaimID: regClaimID,
		Outcome: "VERIFIED", CheckIDs: []string{checkID},
		Dimensions: []verification.Dimension{{Dimension: "CLAIM_MATCH", Outcome: "PASSED"},
			{Dimension: "ISSUER_AUTHORITY", Outcome: "PASSED"}},
		Freshness: "CURRENT", FreshnessUntil: &until,
		DecidedBy: "synthetic-independent-decider", DecidedAt: now,
		ReasonCodes: []string{},
	}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.result
	  (result_id,case_id,claim_id,outcome,decided_by,decided_at,document)
	  VALUES($1,$2,$3,'VERIFIED',$4,$5,$6::jsonb)`,
		resultID, caseID, regClaimID, res.DecidedBy, now, encode(res)); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(ctx, `UPDATE evidence.claim
	  SET status='VERIFIED', current_result_id=$2 WHERE claim_id=$1`,
		regClaimID, resultID); err != nil {
		t.Fatal(err)
	}
	binding, err := repo.ResolveVerifiedRegistration(ctx, caseID, subject)
	if err != nil || binding.RegistrationNumber != "2020/123456/07" ||
		binding.ResultID != resultID || binding.Subject != subject {
		t.Fatalf("trusted CIPC identifier did not resolve exactly: %+v %v", binding, err)
	}
	// A former positive result cannot remain usable if its official-source
	// EvidenceRecord has since become restricted or withdrawn.
	if _, err := seed.Exec(ctx, `UPDATE evidence.record SET status='RESTRICTED'
	  WHERE evidence_id=$1`, regEvidenceID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResolveVerifiedRegistration(ctx, caseID, subject); !errors.Is(err, ErrVerifiedRegistrationUnavailable) {
		t.Fatalf("restricted registry evidence remained trusted: %v", err)
	}
	if _, err := seed.Exec(ctx, `UPDATE evidence.record SET status='AVAILABLE'
	  WHERE evidence_id=$1`, regEvidenceID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResolveVerifiedRegistration(ctx, caseID,
		verification.Subject{SubjectType: "ORGANISATION", SubjectID: domain.NewUUIDv7()}); !errors.Is(err, ErrVerifiedRegistrationUnavailable) {
		t.Fatalf("foreign organisation borrowed registry identifier: %v", err)
	}
	if _, err := seed.Exec(ctx, `UPDATE evidence.claim SET status='REVOKED'
	  WHERE claim_id=$1`, regClaimID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResolveVerifiedRegistration(ctx, caseID, subject); !errors.Is(err, ErrVerifiedRegistrationUnavailable) {
		t.Fatalf("revoked registry claim remained authoritative: %v", err)
	}
}
