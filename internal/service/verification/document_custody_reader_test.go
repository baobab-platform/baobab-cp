package verificationservice

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type scanProofFixture struct{ proof authority.ScanProof }

func (s scanProofFixture) FindScanProof(_ context.Context, _, _ string) (authority.ScanProof, error) {
	return s.proof, nil
}
func (s scanProofFixture) WithLockedDocument(ctx context.Context, _, _, _, _ string, run func(context.Context) error) error {
	return run(ctx) // synthetic fixture only, PostgreSQL integration verifies real row locks.
}
func readWithSyntheticLease(r VerifiedScannedArtifactReader, e verification.Evidence, actor repository.AuditActor) (ScannedDocument, error) {
	var doc ScannedDocument
	err := r.WithDocumentLease(context.Background(), "vcase_fixture", "ecl_fixture", e.EvidenceID, actor, func(ctx context.Context) error {
		var openErr error
		doc, openErr = r.OpenScanned(ctx, e, actor)
		return openErr
	})
	return doc, err
}

func TestPEO03KReaderRequiresIndependentScannerSignatureAndWorkload(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	data := []byte("%PDF-1.7\nsynthetic readable document")
	hash := sha256.Sum256(data)
	proof := authority.ScanProof{EvidenceID: "evr_123456789", ArtifactID: "eart_123456789",
		MediaType: "application/pdf", Digest: "sha256:" + hex.EncodeToString(hash[:]),
		ScannerID: "independent-fixture", ScannerVersion: "v1", Outcome: "CLEAN", ScannedAt: now.Add(-time.Minute)}
	proof.Signature = ed25519.Sign(private, proof.SigningPayload())
	vault := authority.ScannedDocumentVault{Directory: t.TempDir(), Key: bytes.Repeat([]byte{0x70}, 32)}
	if err := vault.Seal(context.Background(), proof, data); err != nil {
		t.Fatal(err)
	}
	reader := VerifiedScannedArtifactReader{Proofs: scanProofFixture{proof},
		Vault: vault, ScannerKeys: map[string]ed25519.PublicKey{"independent-fixture/v1": public},
		AllowedClientID: "baobab-document-extractor-staging", Now: func() time.Time { return now }}
	e := verification.Evidence{EvidenceID: proof.EvidenceID, ArtifactID: proof.ArtifactID,
		EvidenceType: "DOCUMENT", Status: verification.EvidenceAvail}
	actor := repository.AuditActor{ActorID: "prn_worker", ActorType: "workload",
		ClientID: "baobab-document-extractor-staging"}
	if _, err := readWithSyntheticLease(reader, e, actor); err == nil {
		t.Fatal("direct read outside canonical lease disclosed scanned document")
	}
	d, err := readWithSyntheticLease(reader, e, actor)
	if err != nil || !bytes.Equal(d.Bytes, data) || d.ScanOutcome != "CLEAN" || d.ScanAttestation == "" {
		t.Fatalf("signed proof rejected: %v", err)
	}
	actor.ClientID = "baobab-cipc-verifier-staging"
	if _, err := readWithSyntheticLease(reader, e, actor); !errors.Is(err, ErrScannedArtifactUnavailable) {
		t.Fatalf("wrong workload read: %v", err)
	}
	actor.ClientID = "baobab-document-extractor-staging"
	rogue := reader
	rogue.ScannerKeys = map[string]ed25519.PublicKey{}
	if _, err := readWithSyntheticLease(rogue, e, actor); err == nil {
		t.Fatal("scanner without trusted public key accepted")
	}
	rogue = reader
	tampered := proof
	tampered.Digest = "sha256:" + hex.EncodeToString(bytes.Repeat([]byte{0x00}, 32))
	rogue.Proofs = scanProofFixture{tampered}
	if _, err := readWithSyntheticLease(rogue, e, actor); err == nil {
		t.Fatal("unsigned digest modification accepted")
	}
	e.Status = "RESTRICTED"
	if _, err := readWithSyntheticLease(reader, e, actor); err == nil {
		t.Fatal("restricted evidence readable")
	}
}
