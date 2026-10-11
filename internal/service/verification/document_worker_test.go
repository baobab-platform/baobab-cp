package verificationservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type extractionRepositoryFixture struct {
	c        verification.Case
	claim    verification.Claim
	e        verification.Evidence
	existing *DocumentCandidateAttempt
	writes   int
}

func (f *extractionRepositoryFixture) GetVerificationCase(context.Context, string) (verification.Case, error) {
	return f.c, nil
}
func (f *extractionRepositoryFixture) ListVerificationClaims(context.Context, string) ([]verification.Claim, error) {
	return []verification.Claim{f.claim}, nil
}
func (f *extractionRepositoryFixture) GetEvidence(context.Context, string) (verification.Evidence, error) {
	return f.e, nil
}
func (f *extractionRepositoryFixture) GetDocumentCandidateByKey(context.Context, string, string) (DocumentCandidateAttempt, error) {
	if f.existing == nil {
		return DocumentCandidateAttempt{}, repository.ErrVerificationNotFound
	}
	return *f.existing, nil
}
func (f *extractionRepositoryFixture) RecordDocumentCandidates(_ context.Context, a DocumentCandidateAttempt, _ string, _ repository.AuditActor) (DocumentCandidateAttempt, error) {
	f.writes++
	f.existing = &a
	return a, nil
}

type extractionCustodyFixture struct {
	doc   ScannedDocument
	reads int
}

func (c *extractionCustodyFixture) OpenScanned(_ context.Context, _ verification.Evidence, _ repository.AuditActor) (ScannedDocument, error) {
	c.reads++
	return c.doc, nil
}

type extractionProcessorFixture struct {
	count    int
	now      time.Time
	override func(*authority.DocumentExtractionReceipt)
}

func (p *extractionProcessorFixture) Extract(_ context.Context, id, status, mediaType string, data []byte) (authority.DocumentExtractionReceipt, error) {
	p.count++
	if status != verification.EvidenceAvail || mediaType != "application/pdf" {
		return authority.DocumentExtractionReceipt{}, errors.New("invalid input")
	}
	sum := sha256.Sum256(data)
	out := authority.DocumentExtractionReceipt{
		EvidenceID: id, DocumentDigest: "sha256:" + hex.EncodeToString(sum[:]),
		ProcessorID: "synthetic-certified-ocr", ProcessorVersion: "synthetic-v1",
		Standing: "DOCUMENT_CANDIDATE_ONLY", ObservedAt: p.now,
		Candidates: []authority.DocumentCandidate{{Field: "legal_name", Value: "Example Pty Ltd", Confidence: 0.8, Page: 1}},
	}
	if p.override != nil {
		p.override(&out)
	}
	return out, nil
}

func documentWorkerFixture() (DocumentExtractionWorker, *extractionRepositoryFixture, *extractionCustodyFixture, *extractionProcessorFixture, repository.AuditActor) {
	now := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	subject := verification.Subject{SubjectType: "ORGANISATION", SubjectID: "org_synthetic_1"}
	caseID, claimID, evidenceID := "vcase_synthetic1", "ecl_synthetic1", "evr_synthetic1"
	content := []byte("%PDF-1.7\nsynthetic scanned document")
	digest := sha256.Sum256(content)
	repo := &extractionRepositoryFixture{
		c: verification.Case{CaseID: caseID, Subject: subject, Status: verification.CaseVerifying, Purpose: "ORGANISATION_ADMISSION"},
		claim: verification.Claim{ClaimID: claimID, Subject: subject, Status: verification.ClaimUnderCheck,
			ClaimType: "LEGAL_NAME", Jurisdiction: "ZA", AssertedBy: "prn_applicant",
			Purpose: "ORGANISATION_ADMISSION", EvidenceIDs: []string{evidenceID}},
		e: verification.Evidence{EvidenceID: evidenceID, EvidenceType: "DOCUMENT",
			Subject: subject, Purpose: "ORGANISATION_ADMISSION", SourceID: "esrc_cipc",
			ArtifactID: "eart_synthetic1", Status: verification.EvidenceAvail},
	}
	custody := &extractionCustodyFixture{doc: ScannedDocument{
		EvidenceID: evidenceID, ArtifactID: "eart_synthetic1", MediaType: "application/pdf",
		Bytes: content, SHA256: "sha256:" + hex.EncodeToString(digest[:]),
		ScanOutcome: "CLEAN", ScanAttestation: "scan-attestation-synthetic", ScannedAt: now.Add(-time.Minute),
	}}
	processor := &extractionProcessorFixture{now: now}
	worker := DocumentExtractionWorker{Repo: repo, Custody: custody, Processor: processor,
		AllowedClientID: "baobab-document-extractor-staging", Now: func() time.Time { return now }}
	actor := repository.AuditActor{ActorID: "prn_extractor", ActorType: "workload",
		ClientID: "baobab-document-extractor-staging"}
	return worker, repo, custody, processor, actor
}

func TestPEO03JOnlyScannedGovernedDocumentReachesOCR(t *testing.T) {
	w, repo, custody, processor, actor := documentWorkerFixture()
	a, err := w.Extract(context.Background(), repo.c.CaseID, repo.claim.ClaimID, repo.e.EvidenceID,
		"synthetic-idempotency-key-0001", actor)
	if err != nil || a.Receipt.Standing != "DOCUMENT_CANDIDATE_ONLY" || repo.writes != 1 || custody.reads != 1 || processor.count != 1 {
		t.Fatalf("verified boundary refused valid fixture or granted authority: %+v err=%v", a, err)
	}
	// A committed replay must not reopen sensitive bytes or call the provider.
	replay, err := w.Extract(context.Background(), repo.c.CaseID, repo.claim.ClaimID, repo.e.EvidenceID,
		"synthetic-idempotency-key-0001", actor)
	if err != nil || replay.Receipt.DocumentDigest != a.Receipt.DocumentDigest ||
		repo.writes != 1 || custody.reads != 1 || processor.count != 1 {
		t.Fatalf("replay reprocessed sealed document: %+v err=%v", replay, err)
	}
	if _, err := w.Extract(context.Background(), "vcase_other", repo.claim.ClaimID, repo.e.EvidenceID,
		"synthetic-idempotency-key-0001", actor); !errors.Is(err, ErrDocumentExtractionDenied) {
		t.Fatalf("idempotency binding stolen: %v", err)
	}
}

func TestPEO03JDenyUnboundEvidenceAndUnscannedDocuments(t *testing.T) {
	tests := []struct {
		name string
		edit func(*DocumentExtractionWorker, *extractionRepositoryFixture, *extractionCustodyFixture, *extractionProcessorFixture, *repository.AuditActor)
	}{
		{"wrong IAM client", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, a *repository.AuditActor) {
			a.ClientID = "generic-admin"
		}},
		{"browser actor", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, a *repository.AuditActor) {
			a.ActorType = "human"
		}},
		{"claim not in verification", func(_ *DocumentExtractionWorker, r *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			r.claim.Status = "SELF_ASSERTED"
		}},
		{"evidence not linked to claim", func(_ *DocumentExtractionWorker, r *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			r.claim.EvidenceIDs = nil
		}},
		{"subject mismatch", func(_ *DocumentExtractionWorker, r *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			r.e.Subject.SubjectID = "org_other"
		}},
		{"no artifact", func(_ *DocumentExtractionWorker, r *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			r.e.ArtifactID = ""
		}},
		{"restricted evidence", func(_ *DocumentExtractionWorker, r *extractionRepositoryFixture, _ *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			r.e.Status = "RESTRICTED"
		}},
		{"quarantined artifact", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, c *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			c.doc.ScanOutcome = "QUARANTINED"
		}},
		{"missing scan attestation", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, c *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			c.doc.ScanAttestation = ""
		}},
		{"wrong artifact binding", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, c *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			c.doc.ArtifactID = "eart_other"
		}},
		{"tampered document", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, c *extractionCustodyFixture, _ *extractionProcessorFixture, _ *repository.AuditActor) {
			c.doc.Bytes = append(c.doc.Bytes, 'X')
		}},
		{"positive authority claim", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, _ *extractionCustodyFixture, p *extractionProcessorFixture, _ *repository.AuditActor) {
			p.override = func(x *authority.DocumentExtractionReceipt) { x.Standing = "VERIFIED" }
		}},
		{"model response wrong digest", func(_ *DocumentExtractionWorker, _ *extractionRepositoryFixture, _ *extractionCustodyFixture, p *extractionProcessorFixture, _ *repository.AuditActor) {
			p.override = func(x *authority.DocumentExtractionReceipt) { x.DocumentDigest = "sha256:tampered" }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, r, c, p, a := documentWorkerFixture()
			tt.edit(&w, r, c, p, &a)
			if _, err := w.Extract(context.Background(), r.c.CaseID, r.claim.ClaimID, r.e.EvidenceID,
				"synthetic-idempotency-key-0002", a); !errors.Is(err, ErrDocumentExtractionDenied) {
				t.Fatalf("untrusted evidence accepted: %v", err)
			}
			if r.writes != 0 {
				t.Fatal("invalid extraction persisted candidates")
			}
		})
	}
}
