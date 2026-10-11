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
		AssertedBy: "synthetic-applicant", AssertedVia: "APPLICANT",
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
		claimID,caseID,claim.AssertedBy,encode(claim),now); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(ctx, `INSERT INTO evidence.record
	  (evidence_id,source_id,status,obtained_by,document,created_at)
	  VALUES($1,'esrc_cipc','AVAILABLE',$2,$3::jsonb,$4)`,
		evidenceID,evidence.ObtainedBy,encode(evidence),now); err != nil {
		t.Fatal(err)
	}
	obs := verification.SourceObservation{
		CaseID: caseID, ClaimID: claimID, Subject: subject, ClaimType: "LEGAL_NAME",
		Jurisdiction: "ZA", Purpose: vercase.Purpose,
		SourceID: "esrc_cipc", Method: "SOURCE_API_LOOKUP", Outcome: "MATCHED",
		SourceReference: obsRef, ProvenanceRef: "synthetic/immutable-proof/"+suffix,
		ResponseDigest: "sha256:" + strings.Repeat("c", 64),
		ObservedAt: now.Add(-time.Second), FreshnessUntil: now.Add(time.Hour),
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
}
