package verificationservice

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"github.com/jackc/pgx/v5/pgconn"
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
	vault := authority.ScannedDocumentVault{Directory: t.TempDir(), Key: bytes.Repeat([]byte{0x55}, 32)}
	ledger := repository.DocumentLedger{PostgresRepository: base,
		CandidateKey: bytes.Repeat([]byte{0x32}, 32), ExtractorClientID: "baobab-document-extractor-staging",
		ScannerClientID: "baobab-independent-scanner-staging",
		ScannerKeys:     map[string]ed25519.PublicKey{"synthetic-independent-scanner/v1": scannerPub},
		Now:             func() time.Time { return now }}
	scannerActor := repository.AuditActor{ActorID: "prn_scanner", ActorType: "workload",
		ClientID: "baobab-independent-scanner-staging", TokenID: "synthetic-scan-token", CorrelationID: domain.NewUUIDv7()}
	scanCounter := &countedCleanScanner{}
	pipeline := ScannedArtifactIngestor{
		Attester: authority.IndependentDocumentAttester{
			Scanner:   scanCounter,
			Signer:    syntheticEd25519Signer{key: scannerPriv},
			ScannerID: "synthetic-independent-scanner", ScannerVersion: "v1",
			Now: func() time.Time { return now },
		},
		Vault: vault, Ledger: ledger, AllowedClientID: scannerActor.ClientID,
	}
	// Admission must deny before scanner/custody side effects, including a
	// correctly authenticated scanner pointing at another claim or subject.
	for _, tc := range []struct {
		name   string
		mutate func(*verification.Case, *verification.Claim, *verification.Evidence)
	}{
		{"case no longer verifying", func(c *verification.Case, _ *verification.Claim, _ *verification.Evidence) { c.Status = "COMPLETED" }},
		{"claim not under verification", func(_ *verification.Case, cl *verification.Claim, _ *verification.Evidence) { cl.Status = "ASSERTED" }},
		{"evidence not linked", func(_ *verification.Case, cl *verification.Claim, _ *verification.Evidence) { cl.EvidenceIDs = nil }},
		{"claim belongs to foreign organisation", func(_ *verification.Case, cl *verification.Claim, _ *verification.Evidence) {
			cl.Subject.SubjectID = domain.NewUUIDv7()
		}},
		{"evidence belongs to foreign organisation", func(_ *verification.Case, _ *verification.Claim, e *verification.Evidence) {
			e.Subject.SubjectID = domain.NewUUIDv7()
		}},
		{"evidence wrong purpose", func(_ *verification.Case, _ *verification.Claim, e *verification.Evidence) {
			e.Purpose = "LEGAL_ACTOR_AUTHORITY"
		}},
		{"claim wrong purpose", func(_ *verification.Case, cl *verification.Claim, _ *verification.Evidence) {
			cl.Purpose = "LEGAL_ACTOR_AUTHORITY"
		}},
		{"self asserted claim", func(_ *verification.Case, cl *verification.Claim, _ *verification.Evidence) {
			cl.AssertedBy = scannerActor.ActorID
		}},
		{"wrong artifact", func(_ *verification.Case, _ *verification.Claim, e *verification.Evidence) {
			e.ArtifactID = "eart_foreign"
		}},
		{"restricted evidence", func(_ *verification.Case, _ *verification.Claim, e *verification.Evidence) { e.Status = "RESTRICTED" }},
		{"destroyed evidence", func(_ *verification.Case, _ *verification.Claim, e *verification.Evidence) { e.Status = "DESTROYED" }},
		{"expired evidence", func(_ *verification.Case, _ *verification.Claim, e *verification.Evidence) { e.ExpiresAt = &now }},
	} {
		t.Run("scan admission/"+tc.name, func(t *testing.T) {
			c, cl, e := vcase, claim, proofRecord
			tc.mutate(&c, &cl, &e)
			for _, update := range []struct {
				query  string
				id     string
				record any
			}{
				{`UPDATE evidence.verification_case SET document=$2::jsonb WHERE case_id=$1`, caseID, c},
				{`UPDATE evidence.claim SET document=$2::jsonb WHERE claim_id=$1`, claimID, cl},
				{`UPDATE evidence.record SET document=$2::jsonb WHERE evidence_id=$1`, evidenceID, e},
			} {
				if _, err = db.Exec(ctx, update.query, update.id, encode(update.record)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = pipeline.Ingest(ctx, caseID, claimID, evidenceID, artifactID, "application/pdf", content, scannerActor); !errors.Is(err, ErrScanIngestionDenied) {
				t.Fatalf("invalid canonical admission accepted: %v", err)
			}
			if scanCounter.calls != 0 {
				t.Fatal("denied admission reached antivirus")
			}
			entries, readErr := os.ReadDir(vault.Directory)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("denied admission created custody object: %v", readErr)
			}
		})
	}
	for _, update := range []struct {
		query  string
		id     string
		record any
	}{
		{`UPDATE evidence.verification_case SET document=$2::jsonb WHERE case_id=$1`, caseID, vcase},
		{`UPDATE evidence.claim SET document=$2::jsonb WHERE claim_id=$1`, claimID, claim},
		{`UPDATE evidence.record SET document=$2::jsonb WHERE evidence_id=$1`, evidenceID, proofRecord},
	} {
		if _, err = db.Exec(ctx, update.query, update.id, encode(update.record)); err != nil {
			t.Fatal(err)
		}
	}
	signed, err := pipeline.Ingest(ctx, caseID, claimID, evidenceID, artifactID, "application/pdf", content, scannerActor)
	if err != nil {
		t.Fatal(err)
	}
	wrongScanner := scannerActor
	wrongScanner.ClientID = "baobab-cipc-verifier-staging"
	if err = ledger.RegisterScannedArtifact(ctx, signed, wrongScanner); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("untrusted scanner promoted CLEAN evidence: %v", err)
	}
	if err = ledger.RegisterScannedArtifact(ctx, signed, scannerActor); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("direct proof replay bypassed canonical admission lease: %v", err)
	}
	var retained context.Context
	err = ledger.WithScanAdmission(ctx, caseID, claimID, evidenceID, artifactID, scannerActor, func(admitted context.Context) error {
		retained = admitted
		return ledger.RegisterScannedArtifact(admitted, signed, scannerActor)
	})
	if err != nil {
		t.Fatalf("exact proof replay under current admission denied: %v", err)
	}
	if err = ledger.RegisterScannedArtifact(retained, signed, scannerActor); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("retained lease outlived its transaction: %v", err)
	}
	// A lifecycle write on another connection cannot pass the evidence lock
	// while custody operates. No sleeps or probabilistic goroutine ordering.
	err = ledger.WithScanAdmission(ctx, caseID, claimID, evidenceID, artifactID, scannerActor, func(admitted context.Context) error {
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE evidence.record SET status='RESTRICTED' WHERE evidence_id=$1`, evidenceID); err == nil {
			t.Fatal("concurrent evidence restriction bypassed custody lease lock")
		}
		var lockFailure *pgconn.PgError
		if !errors.As(err, &lockFailure) || lockFailure.Code != "55P03" {
			t.Fatalf("restriction failed for reason other than admission lock: %v", err)
		}
		return ledger.RegisterScannedArtifact(admitted, signed, scannerActor)
	})
	if err != nil {
		t.Fatalf("scan lease failed concurrent restriction test: %v", err)
	}
	// Expiry is time-based and is not stopped by a row lock. Crossing the
	// boundary after registration must roll back the transaction at commit.
	expiry := now.Add(time.Second)
	expiring := proofRecord
	expiring.ExpiresAt = &expiry
	if _, err = db.Exec(ctx, `UPDATE evidence.record SET document=$2::jsonb WHERE evidence_id=$1`, evidenceID, encode(expiring)); err != nil {
		t.Fatal(err)
	}
	clock := now
	clockLedger := ledger
	clockLedger.Now = func() time.Time { return clock }
	err = clockLedger.WithScanAdmission(ctx, caseID, claimID, evidenceID, artifactID, scannerActor, func(admitted context.Context) error {
		if err := clockLedger.RegisterScannedArtifact(admitted, signed, scannerActor); err != nil {
			return err
		}
		clock = expiry
		return nil
	})
	if !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("expired admission committed: %v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE evidence.record SET document=$2::jsonb WHERE evidence_id=$1`, evidenceID, encode(proofRecord)); err != nil {
		t.Fatal(err)
	}
	// P0-02B: CP receives an independent signed proof, with no scanner or
	// private signing key in its intake composition. IAM/registry checks are
	// refreshed before custody and before the admission transaction commits.
	trustReader := &intakeTrustFixture{key: scannerPub}
	intakeVault := &intakeVaultFixture{vault: vault}
	intake := ScannedArtifactIntake{Repository: base, Vault: intakeVault, Trust: trustReader, Now: ledger.Now}
	submission := authority.ScanSubmission{CaseID: caseID, ClaimID: claimID, Proof: signed, Document: content}
	checks := 0
	current := func(context.Context) error { checks++; return nil }
	receipt, err := intake.Receive(ctx, submission, scannerActor, current)
	if err != nil || receipt.AuthorityGranted || receipt.Standing != "SCAN_PROOF_RECORDED" || checks != 3 || intakeVault.calls != 1 {
		t.Fatalf("authenticated split intake failed: %+v %v, checks=%d seals=%d", receipt, err, checks, intakeVault.calls)
	}
	bad := submission
	bad.Proof.Signature = append([]byte(nil), signed.Signature...)
	bad.Proof.Signature[0] ^= 1
	before := intakeVault.calls
	if _, err := intake.Receive(ctx, bad, scannerActor, current); err == nil || intakeVault.calls != before {
		t.Fatal("tampered independent signature reached custody")
	}
	checks = 0
	if _, err := intake.Receive(ctx, submission, scannerActor, func(context.Context) error {
		checks++
		if checks >= 3 {
			return errors.New("synthetic in-flight token revocation")
		}
		return nil
	}); err == nil {
		t.Fatal("revoked in-flight token returned committed scan receipt")
	}
	trustReader.calls = 0
	trustReader.revokeAt = 4
	if _, err := intake.Receive(ctx, submission, scannerActor, current); err == nil {
		t.Fatal("removed in-flight scanner trust returned committed receipt")
	}
	trustReader.revokeAt = 0
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
	// Revoking the independent scanner key must immediately block cached
	// candidate plaintext, even while the metadata row still says CLEAN.
	trusted := ledger.ScannerKeys
	ledger.ScannerKeys = nil
	if _, err = ledger.GetDocumentCandidateByKey(ctx, extractor.ActorID, key); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("revoked scanner key was ignored on candidate replay: %v", err)
	}
	ledger.ScannerKeys = trusted
	if _, err = db.Exec(ctx, `UPDATE evidence.record SET status='RESTRICTED',
  document=jsonb_set(document,'{status}','"RESTRICTED"') WHERE evidence_id=$1`, evidenceID); err != nil {
		t.Fatal(err)
	}
	before = intakeVault.calls
	if _, err := intake.Receive(ctx, submission, scannerActor, current); err == nil || intakeVault.calls != before {
		t.Fatal("restricted evidence admitted signed intake custody")
	}
	if _, err = worker.Extract(ctx, caseID, claimID, evidenceID, key, extractor); !errors.Is(err, ErrDocumentExtractionDenied) {
		t.Fatalf("restricted private candidates leaked from PostgreSQL: %v", err)
	}
	if _, err = ledger.GetDocumentCandidateByKey(ctx, extractor.ActorID, key); !errors.Is(err, repository.ErrDocumentLedgerDenied) {
		t.Fatalf("direct ledger bypassed revoked evidence: %v", err)
	}
}

type syntheticAlwaysCleanScanner struct{}

func (syntheticAlwaysCleanScanner) ScanDocument(context.Context, []byte) error { return nil }

type countedCleanScanner struct{ calls int }

func (s *countedCleanScanner) ScanDocument(context.Context, []byte) error { s.calls++; return nil }

type syntheticEd25519Signer struct{ key ed25519.PrivateKey }

func (s syntheticEd25519Signer) Sign(payload []byte) ([]byte, error) {
	return ed25519.Sign(s.key, payload), nil
}

// Synthetic trust/transport ports are deliberately not government/scanner certification.
type intakeTrustFixture struct {
	key             ed25519.PublicKey
	calls, revokeAt int
}

func (s *intakeTrustFixture) ScannerKey(context.Context, string, string) (ed25519.PublicKey, error) {
	s.calls++
	if s.revokeAt > 0 && s.calls >= s.revokeAt {
		return nil, errors.New("synthetic trust revoked")
	}
	return s.key, nil
}

type intakeVaultFixture struct {
	vault authority.ScannedDocumentVault
	calls int
}

func (s *intakeVaultFixture) Seal(ctx context.Context, p authority.ScanProof, b []byte) error {
	s.calls++
	return s.vault.Seal(ctx, p, b)
}
