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

func (s ScannedArtifactIngestor) Ingest(ctx context.Context, evidenceID, artifactID, mediaType string, document []byte, actor repository.AuditActor) (authority.ScanProof, error) {
	var empty authority.ScanProof
	if ctx == nil || ctx.Err() != nil || s.Attester == nil || s.Vault == nil || s.Ledger == nil ||
		s.AllowedClientID == "" || actor.ActorID == "" || actor.ActorType != "workload" ||
		actor.ClientID != s.AllowedClientID || evidenceID == "" || artifactID == "" {
		return empty, ErrScanIngestionDenied
	}
	proof, err := s.Attester.ScanAndAttest(ctx, evidenceID, artifactID, mediaType, document)
	if err != nil || ctx.Err() != nil || proof.EvidenceID != evidenceID || proof.ArtifactID != artifactID ||
		proof.MediaType != mediaType || proof.Outcome != "CLEAN" {
		return empty, ErrScanIngestionDenied
	}
	if err = s.Vault.Seal(ctx, proof, document); err != nil {
		return empty, ErrScanIngestionDenied
	}
	if err = s.Ledger.RegisterScannedArtifact(ctx, proof, actor); err != nil {
		return empty, ErrScanIngestionDenied
	}
	return proof, nil
}
