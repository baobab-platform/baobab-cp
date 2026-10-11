package verificationservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

var ErrDocumentExtractionDenied = errors.New("document extraction requires governed scanned evidence")

// ScannedDocument is returned only by an independently authenticated OEV-02
// custody adapter. Its evidence and artifact IDs, clean scan proof and digest
// MUST be derived from custody metadata, never from a caller's HTTP request.
type ScannedDocument struct {
	EvidenceID      string
	ArtifactID      string
	MediaType       string
	Bytes           []byte
	SHA256          string
	ScanOutcome     string
	ScanAttestation string
	ScannedAt       time.Time
}

type ScannedDocumentReader interface {
	OpenScanned(context.Context, verification.Evidence, repository.AuditActor) (ScannedDocument, error)
}

type DocumentCandidateProcessor interface {
	Extract(context.Context, string, string, string, []byte) (authority.DocumentExtractionReceipt, error)
}

// DocumentCandidateAttempt represents a private, encrypted-at-rest candidate
// ledger entry; it is NOT an OEV-03 check/result or verified legal fact.
type DocumentCandidateAttempt struct {
	CaseID     string
	ClaimID    string
	EvidenceID string
	ArtifactID string
	ActorID    string
	Receipt    authority.DocumentExtractionReceipt
}

type DocumentCandidateRepository interface {
	GetVerificationCase(context.Context, string) (verification.Case, error)
	ListVerificationClaims(context.Context, string) ([]verification.Claim, error)
	GetEvidence(context.Context, string) (verification.Evidence, error)
	GetDocumentCandidateByKey(context.Context, string, string) (DocumentCandidateAttempt, error)
	// Must transactionally recheck current case/claim/evidence + scan status
	// before storing sealed candidates; enforce (actor,key) idempotency and
	// preserve historical attempts instead of overwriting a prior document.
	RecordDocumentCandidates(context.Context, DocumentCandidateAttempt, string, repository.AuditActor) (DocumentCandidateAttempt, error)
}

// DocumentExtractionWorker is the private orchestration seam, never a public
// API. Composition root must inject an IAM-verified dedicated workload,
// an authorised malware-scanned custody reader and a durable sealed ledger.
// Without every dependency it fails closed.
type DocumentExtractionWorker struct {
	Repo            DocumentCandidateRepository
	Custody         ScannedDocumentReader
	Processor       DocumentCandidateProcessor
	AllowedClientID string
	Now             func() time.Time
}

func (w DocumentExtractionWorker) Extract(ctx context.Context, caseID, claimID, evidenceID, idempotencyKey string, actor repository.AuditActor) (DocumentCandidateAttempt, error) {
	deny := DocumentCandidateAttempt{}
	if ctx == nil || ctx.Err() != nil || w.Repo == nil || w.Custody == nil ||
		w.Processor == nil || w.Now == nil || w.AllowedClientID == "" ||
		actor.ActorType != "workload" || actor.ActorID == "" || actor.ClientID != w.AllowedClientID ||
		len(idempotencyKey) < 16 || len(idempotencyKey) > 128 ||
		caseID == "" || claimID == "" || evidenceID == "" {
		return deny, ErrDocumentExtractionDenied
	}
	// Read a committed attempt first; replays must not reach custody or OCR.
	prior, err := w.Repo.GetDocumentCandidateByKey(ctx, actor.ActorID, idempotencyKey)
	if err == nil {
		if prior.CaseID != caseID || prior.ClaimID != claimID ||
			prior.EvidenceID != evidenceID || prior.ActorID != actor.ActorID ||
			prior.Receipt.Standing != "DOCUMENT_CANDIDATE_ONLY" {
			return deny, ErrDocumentExtractionDenied
		}
		return prior, nil
	}
	if !errors.Is(err, repository.ErrVerificationNotFound) {
		return deny, ErrDocumentExtractionDenied
	}
	c, err := w.Repo.GetVerificationCase(ctx, caseID)
	if err != nil || c.Status != verification.CaseVerifying ||
		c.Subject.SubjectType == "" || c.Subject.SubjectID == "" || c.Purpose == "" {
		return deny, ErrDocumentExtractionDenied
	}
	claims, err := w.Repo.ListVerificationClaims(ctx, caseID)
	if err != nil {
		return deny, ErrDocumentExtractionDenied
	}
	var claim verification.Claim
	found := 0
	for _, item := range claims {
		if item.ClaimID == claimID {
			found++
			claim = item
		}
	}
	if found != 1 || claim.Status != verification.ClaimUnderCheck ||
		claim.Subject != c.Subject || claim.Purpose != c.Purpose ||
		claim.AssertedBy == actor.ActorID {
		return deny, ErrDocumentExtractionDenied
	}
	associated := false
	for _, id := range claim.EvidenceIDs {
		if id == evidenceID {
			associated = true
			break
		}
	}
	if !associated {
		return deny, ErrDocumentExtractionDenied
	}
	e, err := w.Repo.GetEvidence(ctx, evidenceID)
	now := w.Now().UTC()
	if err != nil || e.Status != verification.EvidenceAvail ||
		e.EvidenceType != "DOCUMENT" || e.ArtifactID == "" ||
		e.Subject != c.Subject || e.Purpose != c.Purpose ||
		e.ExpiresAt != nil && !e.ExpiresAt.After(now) {
		return deny, ErrDocumentExtractionDenied
	}
	doc, err := w.Custody.OpenScanned(ctx, e, actor)
	if err != nil || doc.ScanOutcome != "CLEAN" || doc.ScanAttestation == "" ||
		doc.ScannedAt.IsZero() || doc.ScannedAt.After(now) ||
		doc.EvidenceID != evidenceID || doc.ArtifactID != e.ArtifactID ||
		len(doc.Bytes) < 8 || len(doc.Bytes) > 8*1024*1024 {
		return deny, ErrDocumentExtractionDenied
	}
	sum := sha256.Sum256(doc.Bytes)
	expectedDigest := "sha256:" + hex.EncodeToString(sum[:])
	if doc.SHA256 != expectedDigest {
		return deny, ErrDocumentExtractionDenied
	}
	receipt, err := w.Processor.Extract(ctx, evidenceID, e.Status, doc.MediaType, doc.Bytes)
	if err != nil || receipt.EvidenceID != evidenceID ||
		receipt.DocumentDigest != expectedDigest ||
		receipt.Standing != "DOCUMENT_CANDIDATE_ONLY" ||
		receipt.ProcessorID == "" || receipt.ProcessorVersion == "" ||
		receipt.ObservedAt.IsZero() || receipt.ObservedAt.After(w.Now().UTC().Add(time.Second)) {
		return deny, ErrDocumentExtractionDenied
	}
	attempt := DocumentCandidateAttempt{
		CaseID: caseID, ClaimID: claimID, EvidenceID: evidenceID,
		ArtifactID: e.ArtifactID, ActorID: actor.ActorID, Receipt: receipt,
	}
	// Backend must revalidate the current evidence state under lock and
	// seal candidate values independently of the metadata/index database.
	stored, err := w.Repo.RecordDocumentCandidates(ctx, attempt, idempotencyKey, actor)
	if err != nil || stored.CaseID != caseID || stored.ClaimID != claimID ||
		stored.EvidenceID != evidenceID || stored.ArtifactID != e.ArtifactID ||
		stored.ActorID != actor.ActorID ||
		stored.Receipt.DocumentDigest != receipt.DocumentDigest ||
		stored.Receipt.Standing != "DOCUMENT_CANDIDATE_ONLY" {
		return deny, ErrDocumentExtractionDenied
	}
	return stored, nil
}
