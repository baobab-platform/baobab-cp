package verificationservice

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

var ErrScannedArtifactUnavailable = errors.New("authorised, independently scanned document unavailable")

type ScanProofLookup interface {
	FindScanProof(context.Context, string, string) (authority.ScanProof, error)
}
type DocumentVaultReader interface {
	Open(context.Context, authority.ScanProof) ([]byte, error)
}

// VerifiedScannedArtifactReader is the concrete OEV-02 custody bridge for
// DocumentExtractionWorker, with a separately configured independent scanner
// public key per exact scanner ID and scanner version.
type VerifiedScannedArtifactReader struct {
	Proofs          ScanProofLookup
	Vault           DocumentVaultReader
	ScannerKeys     map[string]ed25519.PublicKey
	AllowedClientID string
	Now             func() time.Time
}

func (r VerifiedScannedArtifactReader) OpenScanned(ctx context.Context, e verification.Evidence, actor repository.AuditActor) (ScannedDocument, error) {
	empty := ScannedDocument{}
	if ctx == nil || ctx.Err() != nil || r.Proofs == nil || r.Vault == nil || r.Now == nil ||
		r.AllowedClientID == "" || actor.ClientID != r.AllowedClientID ||
		actor.ActorType != "workload" || actor.ActorID == "" ||
		e.EvidenceID == "" || e.ArtifactID == "" || e.Status != verification.EvidenceAvail ||
		e.EvidenceType != "DOCUMENT" || e.ExpiresAt != nil && !e.ExpiresAt.After(r.Now().UTC()) {
		return empty, ErrScannedArtifactUnavailable
	}
	p, err := r.Proofs.FindScanProof(ctx, e.EvidenceID, e.ArtifactID)
	if err != nil || p.EvidenceID != e.EvidenceID || p.ArtifactID != e.ArtifactID ||
		authority.VerifyScanProof(p, r.ScannerKeys[p.ScannerID+"/"+p.ScannerVersion], r.Now()) != nil {
		return empty, ErrScannedArtifactUnavailable
	}
	contents, err := r.Vault.Open(ctx, p)
	if err != nil || len(contents) < 8 || len(contents) > authority.MaxScannedDocumentBytes {
		return empty, ErrScannedArtifactUnavailable
	}
	sum := sha256.Sum256(contents)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if digest != p.Digest {
		return empty, ErrScannedArtifactUnavailable
	}
	return ScannedDocument{
		EvidenceID: e.EvidenceID, ArtifactID: e.ArtifactID,
		MediaType: p.MediaType, Bytes: contents, SHA256: digest,
		ScanOutcome: p.Outcome, ScanAttestation: "ed25519:" + hex.EncodeToString(p.Signature),
		ScannedAt: p.ScannedAt.UTC(),
	}, nil
}
