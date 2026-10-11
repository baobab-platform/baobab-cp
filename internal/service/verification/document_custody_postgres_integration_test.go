package verificationservice

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPEO03KPostgresSignedScannedArtifactEncryptedCandidateLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL 17 TEST_DATABASE_URL")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	base, err := repository.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	uuid := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	caseID, claimID, evidenceID, artifactID := "vcase_"+uuid, "ecl_"+uuid, "evr_"+uuid, "eart_"+uuid
	subject := verification.Subject{SubjectType: "ORGANISATION", SubjectID: domain.NewUUIDv7()}
	purpose := "ORGANISATION_ADMISSION"
	vcase := verification.Case{CaseID: caseID, Subject: subject, Purpose: purpose,
		Status: verification.CaseVerifying, ClaimIDs: []string{claimID}, OpenedAt: now, Version: 1}
	claim := verification.Claim{ClaimID: claimID, Subject: subject, ClaimType: "LEGAL_NAME",
		Jurisdiction: "ZA", Status: verification.ClaimUnderCheck, Purpose: purpose,
		AssertedBy: "prn_applicant", AssertedVia: "APPLICANT", AssertedAt: now,
		EvidenceIDs: []string{evidenceID}, Version: 1}
	proofRecord := verification.Evidence{EvidenceID: evidenceID, Subject: subject,
		EvidenceType: "DOCUMENT", ArtifactID: artifactID, Purpose: purpose, SourceID: "esrc_cipc",
		Status: verification.EvidenceAvail, ObtainedBy: "prn_independent_operator",
		Classification: "CONFIDENTIAL", RetentionPolicy: "synthetic-only", CreatedAt: now, Version: 1}
	encode := func(v any) []byte {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	if _, err = db.Exec(ctx, `INSERT INTO evidence.verification_case
 (case_id,subject_type,subject_id,status,opened_by,opened_at,document)
 VALUES($1,'ORGANISATION',$2,'VERIFYING','prn_reviewer',$3,$4::jsonb)`,
		caseID, subject.SubjectID, now, encode(vcase)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO evidence.claim
 (claim_id,case_id,asserted_by,status,document,created_at)
 VALUES($1,$2,$3,'UNDER_VERIFICATION',$4::jsonb,$5)`,
		claimID, caseID, claim.AssertedBy, encode(claim), now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO evidence.record
 (evidence_id,source_id,status,obtained_by,document,created_at)
 VALUES($1,'esrc_cipc','AVAILABLE',$2,$3::jsonb,$4)`,
		evidenceID, proofRecord.ObtainedBy, encode(proofRecord), now); err != nil {
		t.Fatal(err)
	}
	scannerPub, scannerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("%PDF-1.7\nindependently scanned synthetic company certificate")
	sum := sha256.Sum256(content)
	signed := authority.ScanProof{
		EvidenceID: evidenceID, ArtifactID: artifactID, MediaType: "application/pdf",
		Digest: "sha256:" + hex.EncodeToString(sum[:]), ScannerID: "synthetic-independent-scanner",
		ScannerVersion: "v1", Outcome: "CLEAN", ScannedAt: now.Add(-time.Minute),
	}
	signed.Signature = ed25519.Sign(scannerPriv, signed.SigningPayload())
	vault := authority.ScannedDocumentVault{Directory: t.TempDir(), Key: bytes.Repeat([]byte{0x55}, 32)}
	if err = vault.Seal(ctx, signed, content); err != nil {
		t.Fatal(err)
	}
	ledger := repository.DocumentLedger{PostgresRepository: base,
		CandidateKey: bytes.Repeat([]byte{0x32}, 32), ExtractorClientID: "baobab-document-extractor-staging",
		ScannerClientID: "baobab-independent-scanner-staging",
		ScannerKeys:     map[string]ed25519.PublicKey{"synthetic-independent-scanner/v1": scannerPub},
		Now:             func() time.Time { return now }}
	scannerActor := repository.AuditActor{ActorID: "prn_scanner", ActorType: "workload",
		ClientID: "baobab-independent-scanner-staging", CorrelationID: domain.NewUUIDv7()}
	if err = ledger.RegisterScannedArtifact(ctx, signed, scannerActor); err != nil {
		t.Fatal(err)
	}
	wrongScanner := scannerActor
	wrongScanner.ClientID = "baobab-cipc-verifier-staging"
	if err = ledger.RegisterScannedArtifact(ctx, signed, wrongScanner); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("untrusted scanner promoted CLEAN evidence: %v", err)
	}
	reader := VerifiedScannedArtifactReader{Proofs: ledger, Vault: vault, ScannerKeys: ledger.ScannerKeys,
		AllowedClientID: ledger.ExtractorClientID, Now: ledger.Now}
	processor := &extractionProcessorFixture{now: now}
	worker := DocumentExtractionWorker{Repo: ledger, Custody: reader, Processor: processor,
		AllowedClientID: ledger.ExtractorClientID, Now: ledger.Now}
	extractor := repository.AuditActor{ActorID: "prn_extractor", ActorType: "workload",
		ClientID: ledger.ExtractorClientID, CorrelationID: domain.NewUUIDv7()}
	key := "synthetic-scanned-extraction-0001"
	result, err := worker.Extract(ctx, caseID, claimID, evidenceID, key, extractor)
	if err != nil || result.Receipt.Standing != "DOCUMENT_CANDIDATE_ONLY" || len(result.Receipt.Candidates) != 1 {
		t.Fatalf("end-to-end scanned extraction refused: %+v err=%v", result, err)
	}
	var ciphertext []byte
	if err = db.QueryRow(ctx, `SELECT sealed_candidates FROM evidence.document_candidate_attempt
 WHERE actor_id=$1 AND idempotency_key=$2`, extractor.ActorID, key).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("Example Pty Ltd")) || len(ciphertext) < 29 {
		t.Fatal("document candidates were not persisted as ciphertext")
	}
	replay, err := worker.Extract(ctx, caseID, claimID, evidenceID, key, extractor)
	if err != nil || replay.Receipt.DocumentDigest != result.Receipt.DocumentDigest || processor.count != 1 {
		t.Fatalf("durable idempotent replay reprocessed private content: %v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE evidence.record SET status='RESTRICTED',
  document=jsonb_set(document,'{status}','"RESTRICTED"') WHERE evidence_id=$1`, evidenceID); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.Extract(ctx, caseID, claimID, evidenceID, key, extractor); !errors.Is(err, ErrDocumentExtractionDenied) {
		t.Fatalf("restricted private candidates leaked from PostgreSQL: %v", err)
	}
	if _, err = ledger.GetDocumentCandidateByKey(ctx, extractor.ActorID, key); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("direct ledger bypassed revoked evidence: %v", err)
	}
}
