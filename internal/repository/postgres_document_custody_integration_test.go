package repository

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
	"sync"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/documentcustody"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

// This synthetic backend tests the durable DB protocol, not cloud acceptance.
// SDK request/header/version/retention tests live in documentcustody.
type custodyFixtureBackend struct {
	mu                        sync.Mutex
	objects                   map[string]authority.CustodyObject
	bodies                    map[string][]byte
	creates, deletes, rewraps int
	onCreate                  func()
	deleteFails               bool
	key                       []byte
}

func (b *custodyFixtureBackend) GenerateKey(context.Context, string) ([]byte, []byte, string, error) {
	return append([]byte(nil), b.key...), []byte("synthetic-wrapped-key"), "synthetic-key-v1", nil
}
func (b *custodyFixtureBackend) UnwrapKey(context.Context, authority.CustodyObject) ([]byte, error) {
	return append([]byte(nil), b.key...), nil
}
func (b *custodyFixtureBackend) RewrapKey(_ context.Context, _ authority.CustodyObject, target string) ([]byte, error) {
	b.rewraps++
	return []byte("synthetic-wrapped-key-" + target), nil
}
func (b *custodyFixtureBackend) Create(_ context.Context, o authority.CustodyObject, raw []byte) (string, error) {
	if b.onCreate != nil {
		b.onCreate()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.creates++
	o.VersionID = "synthetic-version-1"
	b.objects[o.ObjectKey] = o
	b.bodies[o.ObjectKey] = append([]byte(nil), raw...)
	return o.VersionID, nil
}
func (b *custodyFixtureBackend) Stat(_ context.Context, o authority.CustodyObject) (authority.CustodyObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	got, ok := b.objects[o.ObjectKey]
	if !ok || o.VersionID != "" && o.VersionID != got.VersionID {
		return o, documentcustody.ErrAbsent
	}
	return got, nil
}
func (b *custodyFixtureBackend) Read(_ context.Context, o authority.CustodyObject) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	got, ok := b.objects[o.ObjectKey]
	if !ok || o.VersionID != got.VersionID {
		return nil, documentcustody.ErrAbsent
	}
	return append([]byte(nil), b.bodies[o.ObjectKey]...), nil
}
func (b *custodyFixtureBackend) Hold(_ context.Context, o authority.CustodyObject, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	o = b.objects[o.ObjectKey]
	o.LegalHold = on
	b.objects[o.ObjectKey] = o
	return nil
}
func (b *custodyFixtureBackend) Delete(_ context.Context, o authority.CustodyObject) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deleteFails {
		return documentcustody.ErrBackend
	}
	b.deletes++
	delete(b.objects, o.ObjectKey)
	delete(b.bodies, o.ObjectKey)
	return nil
}

type custodyFixturePolicy struct{ policy RetentionPolicy }

func (p custodyFixturePolicy) CustodyPolicy(context.Context, string, string) (RetentionPolicy, error) {
	return p.policy, nil
}

func TestP002CDurableCustodyLifecyclePostgres(t *testing.T) {
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
	repo, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := now
	backend := &custodyFixtureBackend{objects: map[string]authority.CustodyObject{}, bodies: map[string][]byte{}, key: bytes.Repeat([]byte{0x31}, 32)}
	policy := RetentionPolicy{ID: "synthetic-custody", Version: "v1", Purpose: "ORGANISATION_ADMISSION", Jurisdiction: "ZA", Classification: "CONFIDENTIAL", MinimumHours: 1, MaximumHours: 4, OrphanGraceHours: 1, QuarantineHours: 1}
	vault := DurableDocumentVault{Repository: repo, Backend: backend, Policies: custodyFixturePolicy{policy}, Prefix: "synthetic/", Now: func() time.Time { return clock }}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ledger := DocumentLedger{PostgresRepository: repo, ScannerClientID: authority.ScannerClientID, ScannerKeys: map[string]ed25519.PublicKey{"synthetic-scanner/v1": pub}, Now: func() time.Time { return clock }}
	scanner := AuditActor{ActorID: "prn_synthetic_scanner", ActorType: "workload", ClientID: authority.ScannerClientID, CorrelationID: domain.NewUUIDv7()}
	maintainer := AuditActor{ActorID: "synthetic-approved-role", ActorType: "workload", ClientID: "baobab-document-custody-maintenance", CorrelationID: domain.NewUUIDv7()}
	fixture := func() (string, string, authority.ScanProof, []byte) {
		id := hex.EncodeToString([]byte(domain.NewUUIDv7()))
		c, cl, e, a := "vcase_"+id, "ecl_"+id, "evr_"+id, "eart_"+id
		sub := verification.Subject{SubjectType: "ORGANISATION", SubjectID: domain.NewUUIDv7()}
		records := []struct {
			query string
			args  []any
		}{
			{`INSERT INTO evidence.verification_case(case_id,subject_type,subject_id,status,opened_by,opened_at,document)VALUES($1,'ORGANISATION',$2,'VERIFYING','prn_synthetic_reviewer',$3,$4::jsonb)`, []any{c, sub.SubjectID, now, verification.Case{CaseID: c, Subject: sub, Purpose: policy.Purpose, Status: verification.CaseVerifying, ClaimIDs: []string{cl}, OpenedAt: now, Version: 1}}},
			{`INSERT INTO evidence.claim(claim_id,case_id,asserted_by,status,document,created_at)VALUES($1,$2,'prn_synthetic_applicant','UNDER_VERIFICATION',$3::jsonb,$4)`, []any{cl, c, verification.Claim{ClaimID: cl, Subject: sub, ClaimType: "LEGAL_NAME", Jurisdiction: "ZA", Purpose: policy.Purpose, Status: verification.ClaimUnderCheck, AssertedBy: "prn_synthetic_applicant", EvidenceIDs: []string{e}, Version: 1}, now}},
			{`INSERT INTO evidence.record(evidence_id,source_id,status,obtained_by,document,created_at)VALUES($1,'esrc_cipc','AVAILABLE','prn_synthetic_operator',$2::jsonb,$3)`, []any{e, verification.Evidence{EvidenceID: e, ArtifactID: a, Subject: sub, EvidenceType: "DOCUMENT", Purpose: policy.Purpose, SourceID: "esrc_cipc", Status: verification.EvidenceAvail, Classification: policy.Classification, RetentionPolicy: policy.ID, CreatedAt: now, Version: 1}, now}},
		}
		for _, r := range records {
			for i, arg := range r.args {
				switch arg.(type) {
				case verification.Case, verification.Claim, verification.Evidence:
					r.args[i], _ = json.Marshal(arg)
				}
			}
			if _, err := repo.pool.Exec(ctx, r.query, r.args...); err != nil {
				t.Fatal(err)
			}
		}
		document := []byte("%PDF-1.7\nsynthetic confidential durable custody")
		h := sha256.Sum256(document)
		p := authority.ScanProof{EvidenceID: e, ArtifactID: a, MediaType: "application/pdf", Digest: "sha256:" + hex.EncodeToString(h[:]), ScannerID: "synthetic-scanner", ScannerVersion: "v1", Outcome: "CLEAN", ScannedAt: now}
		p.Signature = ed25519.Sign(priv, p.SigningPayload())
		return c, cl, p, document
	}
	job := func(o authority.CustodyObject, op string) CustodyApproval {
		return CustodyApproval{Operation: op, EvidenceID: o.EvidenceID, ArtifactID: o.ArtifactID, ProofHash: o.ProofHash, ExpectedState: o.State, ExpectedRevision: o.Revision, ExpectedVersion: o.VersionID, ProposedBy: "synthetic-maker", ApprovedBy: "synthetic-checker", ApprovalReference: "synthetic-maintenance-approval", EffectiveFrom: clock.Add(-time.Minute), ExpiresAt: clock.Add(time.Hour)}
	}
	get := func(p authority.ScanProof) authority.CustodyObject {
		o, e := scanCustody(repo.pool.QueryRow(ctx, custodySelect, p.EvidenceID, p.ArtifactID))
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	ingest := func(c, cl string, p authority.ScanProof, doc []byte, commit bool) error {
		return ledger.WithScanAdmission(ctx, c, cl, p.EvidenceID, p.ArtifactID, scanner, func(admitted context.Context) error {
			if e := vault.Seal(admitted, p, doc); e != nil {
				return e
			}
			if !commit {
				return errors.New("synthetic post-upload revocation")
			}
			return ledger.RegisterScannedArtifact(admitted, p, scanner)
		})
	}
	c, cl, p, doc := fixture()
	if vault.Seal(ctx, p, doc) == nil {
		t.Fatal("write bypassed admission")
	}
	if err = ingest(c, cl, p, doc, true); err != nil {
		t.Fatal(err)
	}
	o := get(p)
	if o.State != "ATTACHED" || o.VersionID == "" {
		t.Fatal("custody not atomically attached")
	}
	if _, err = vault.Open(ctx, p); err == nil {
		t.Fatal("plaintext bypassed read lease")
	}
	var saved context.Context
	read := func() error {
		return ledger.WithLockedDocument(ctx, c, cl, p.EvidenceID, "prn_synthetic_extractor", func(locked context.Context) error {
			saved = locked
			plain, e := vault.Open(locked, p)
			if e != nil || !bytes.Equal(plain, doc) {
				return errors.New("synthetic read failed")
			}
			return nil
		})
	}
	if err = read(); err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Open(saved, p); err == nil {
		t.Fatal("retained read context worked")
	}
	if err = ingest(c, cl, p, doc, true); err != nil || backend.creates != 1 {
		t.Fatal("replay uploaded new bytes")
	}
	wrong := p
	wrong.ScannerVersion = "v2"
	wrong.Signature = ed25519.Sign(priv, wrong.SigningPayload())
	if err = ingest(c, cl, wrong, doc, true); err == nil {
		t.Fatal("proof drift accepted")
	}
	// Encrypted derived candidates share the document's purpose cutoff.
	ledger.CandidateKey = bytes.Repeat([]byte{0x45}, 32)
	ledger.ExtractorClientID = "synthetic-extractor"
	extractor := AuditActor{ActorID: "prn_synthetic_extractor", ActorType: "workload", ClientID: ledger.ExtractorClientID, CorrelationID: domain.NewUUIDv7()}
	ocrKey := "synthetic-ocr-" + p.ArtifactID
	candidate := authority.DocumentCandidateAttempt{CaseID: c, ClaimID: cl, EvidenceID: p.EvidenceID, ArtifactID: p.ArtifactID, ActorID: extractor.ActorID, Receipt: authority.DocumentExtractionReceipt{EvidenceID: p.EvidenceID, DocumentDigest: p.Digest, ProcessorID: "synthetic-ocr", ProcessorVersion: "v1", Standing: "DOCUMENT_CANDIDATE_ONLY", ObservedAt: now, Candidates: []authority.DocumentCandidate{{Field: "legal_name", Value: "Synthetic Ltd", Confidence: 0.7, Page: 1}}}}
	if _, err = ledger.RecordDocumentCandidates(ctx, candidate, ocrKey, extractor); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.GetDocumentCandidateByKey(ctx, extractor.ActorID, ocrKey); err != nil {
		t.Fatal(err)
	}
	changedPolicy := policy
	changedPolicy.QuarantineHours++
	vault.Policies = custodyFixturePolicy{changedPolicy}
	if _, err = vault.Maintain(ctx, job(o, "HOLD"), maintainer); err == nil {
		t.Fatal("policy content changed under retained version")
	}
	vault.Policies = custodyFixturePolicy{policy}
	rotate := job(o, "ROTATE")
	rotate.TargetKey = "synthetic-key-v2"
	if _, err = vault.Maintain(ctx, rotate, maintainer); err != nil {
		t.Fatal(err)
	}
	rotated := get(p)
	if _, err = vault.Maintain(ctx, rotate, maintainer); err == nil {
		t.Fatal("stale revision approval replayed after rotation")
	}
	if rotated.KeyID == o.KeyID || rotated.CipherDigest != o.CipherDigest || rotated.VersionID != o.VersionID || !bytes.Equal(rotated.Nonce, o.Nonce) || backend.creates != 1 {
		t.Fatal("rotation rewrote ciphertext or version")
	}
	if err = read(); err != nil {
		t.Fatal("historical document unreadable after rewrap")
	}
	if _, err = vault.Maintain(ctx, job(rotated, "HOLD"), maintainer); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(5 * time.Hour)
	held := get(p)
	if _, err = vault.Maintain(ctx, job(held, "DELETE"), maintainer); err == nil || backend.deletes != 0 {
		t.Fatal("held document deleted")
	}
	if _, err = vault.Maintain(ctx, job(held, "RELEASE_HOLD"), maintainer); err != nil {
		t.Fatal(err)
	}
	o = get(p)
	stage := job(o, "DELETE")
	if _, err = vault.Maintain(ctx, stage, maintainer); err != nil {
		t.Fatal(err)
	}
	if backend.deletes != 0 || get(p).State != "DELETING" {
		t.Fatal("deletion skipped durable cutoff")
	}
	if _, err = vault.Maintain(ctx, stage, maintainer); err == nil {
		t.Fatal("stale approval accepted")
	}
	clock = now
	if err = read(); err == nil {
		t.Fatal("DELETING returned plaintext")
	}
	if _, err = ledger.GetDocumentCandidateByKey(ctx, extractor.ActorID, ocrKey); err == nil {
		t.Fatal("DELETING returned cached OCR plaintext")
	}
	clock = now.Add(5 * time.Hour)
	backend.deleteFails = true
	if _, err = vault.Maintain(ctx, job(get(p), "DELETE"), maintainer); err == nil || get(p).State != "DELETING" {
		t.Fatal("failed deletion recorded as complete")
	}
	backend.deleteFails = false
	// Force a DB audit failure AFTER successful remote erasure. The persisted
	// DELETING cutoff survives rollback and a reviewed exact-version retry
	// completes absence confirmation without deleting a second object.
	invalidAudit := maintainer
	invalidAudit.CorrelationID = "synthetic-invalid-uuid"
	if _, err = vault.Maintain(ctx, job(get(p), "DELETE"), invalidAudit); err == nil || get(p).State != "DELETING" || backend.deletes != 1 {
		t.Fatal("ambiguous DB receipt lost deletion cutoff")
	}
	if _, err = vault.Maintain(ctx, job(get(p), "DELETE"), maintainer); err != nil {
		t.Fatal(err)
	}
	if get(p).State != "DELETED" || backend.deletes != 1 {
		t.Fatal("no confirmed deletion tombstone")
	}
	var candidatesLeft int
	if repo.pool.QueryRow(ctx, `SELECT count(*) FROM evidence.document_candidate_attempt WHERE evidence_id=$1 AND artifact_id=$2`, p.EvidenceID, p.ArtifactID).Scan(&candidatesLeft) != nil || candidatesLeft != 0 {
		t.Fatal("encrypted derived candidates survived destruction")
	}
	if get(p).KeyID != "" || len(get(p).WrappedKey) != 1 {
		t.Fatal("deleted envelope still has a key dependency")
	}
	var status string
	if repo.pool.QueryRow(ctx, `SELECT status FROM evidence.record WHERE evidence_id=$1`, p.EvidenceID).Scan(&status) != nil || status != "DESTROYED" {
		t.Fatal("destruction status missing")
	}
	if err = ingest(c, cl, p, doc, true); err == nil {
		t.Fatal("destroyed artifact recreated")
	}
	clock = now
	c2, cl2, p2, doc2 := fixture()
	if err = ingest(c2, cl2, p2, doc2, false); err == nil {
		t.Fatal("failed proof committed")
	}
	pending := get(p2)
	if pending.State != "PENDING" || pending.VersionID != "" {
		t.Fatal("failed commit lost durable intent")
	}
	if _, err = vault.Maintain(ctx, job(pending, "RECONCILE"), maintainer); err == nil {
		t.Fatal("young orphan quarantined")
	}
	clock = now.Add(2 * time.Hour)
	if _, err = vault.Maintain(ctx, job(pending, "RECONCILE"), maintainer); err != nil {
		t.Fatal(err)
	}
	q := get(p2)
	if q.State != "QUARANTINED" || q.VersionID == "" {
		t.Fatal("orphan version not quarantined")
	}
	if _, err = vault.Maintain(ctx, job(q, "DELETE"), maintainer); err == nil {
		t.Fatal("quarantine grace ignored")
	}
	clock = now.Add(3 * time.Hour)
	if _, err = vault.Maintain(ctx, job(q, "DELETE"), maintainer); err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Maintain(ctx, job(get(p2), "DELETE"), maintainer); err != nil {
		t.Fatal(err)
	}
	if backend.deletes != 2 {
		t.Fatal("orphan not destroyed")
	}
	// The artifact fence must prevent an old pending-intent observation from
	// deleting an intake that is still uploading and will commit its proof.
	clock = now
	c3, cl3, p3, doc3 := fixture()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	backend.onCreate = func() { close(entered); <-release }
	go func() { done <- ingest(c3, cl3, p3, doc3, true) }()
	<-entered
	waiting := get(p3)
	a := job(waiting, "RECONCILE")
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	if _, err = vault.Maintain(short, a, maintainer); err == nil {
		t.Fatal("cleanup raced active upload")
	}
	cancel()
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	backend.onCreate = nil
	if get(p3).State != "ATTACHED" {
		t.Fatal("active upload was quarantined")
	}
	reports, err := vault.Reports(ctx, "", "", 100)
	if err != nil || len(reports) < 3 {
		t.Fatal("custody report unavailable")
	}
}
