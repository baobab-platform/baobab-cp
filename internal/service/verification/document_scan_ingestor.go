package verificationservice

import (
	"context"
	"errors"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

var ErrScanIngestionDenied = errors.New("independent scan ingestion denied")

type DocumentScanAttester interface {
	ScanAndAttest(context.Context, string, string, string, []byte) (authority.ScanProof, error)
}

type SealedScannedArtifactWriter interface {
	Seal(context.Context, authority.ScanProof, []byte) error
}

type ScannedArtifactRegistrar interface {
	WithScanAdmission(context.Context, string, string, string, string, repository.AuditActor, func(context.Context) error) error
	RegisterScannedArtifact(context.Context, authority.ScanProof, repository.AuditActor) error
}

// ScannedArtifactIngestor is a private, scanner-owned staging composition.
// A CP/browser/applicant request MUST NEVER be allowed to supply its scanner
// verdict, Ed25519 signature or private key. Scan, seal, then register an
// independently verifiable proof; an unreferenced encrypted blob after a
// denied registration is not an AVAILABLE evidence record.
type ScannedArtifactIngestor struct {
	Attester        DocumentScanAttester
	Vault           SealedScannedArtifactWriter
	Ledger          ScannedArtifactRegistrar
	AllowedClientID string
}

func (s ScannedArtifactIngestor) Ingest(ctx context.Context, caseID, claimID, evidenceID, artifactID, mediaType string, document []byte, actor repository.AuditActor) (authority.ScanProof, error) {
	var empty authority.ScanProof
	if ctx == nil || ctx.Err() != nil || s.Attester == nil || s.Vault == nil || s.Ledger == nil ||
		s.AllowedClientID == "" || actor.ActorID == "" || actor.ActorType != "workload" ||
		actor.ClientID != s.AllowedClientID || caseID == "" || claimID == "" || evidenceID == "" || artifactID == "" {
		return empty, ErrScanIngestionDenied
	}
	var proof authority.ScanProof
	// Canonical admission precedes ANY scan/custody side effect. The same
	// transaction serialises restriction with proof registration; a scanner
	// identity alone is not permission to store an arbitrary case's document.
	err := s.Ledger.WithScanAdmission(ctx, caseID, claimID, evidenceID, artifactID, actor, func(admitted context.Context) error {
		var err error
		proof, err = s.Attester.ScanAndAttest(admitted, evidenceID, artifactID, mediaType, document)
		if err != nil || admitted.Err() != nil || proof.EvidenceID != evidenceID || proof.ArtifactID != artifactID ||
			proof.MediaType != mediaType || proof.Outcome != "CLEAN" {
			return ErrScanIngestionDenied
		}
		if err = s.Vault.Seal(admitted, proof, document); err != nil {
			return ErrScanIngestionDenied
		}
		return s.Ledger.RegisterScannedArtifact(admitted, proof, actor)
	})
	if err != nil {
		return empty, ErrScanIngestionDenied
	}
	return proof, nil
}
