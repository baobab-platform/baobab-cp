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

var ErrCaptureDenied = errors.New("authoritative lookup or evidence binding unavailable")

type CaptureRepository interface {
	GetVerificationCase(context.Context, string) (verification.Case, error)
	ListVerificationClaims(context.Context, string) ([]verification.Claim, error)
	GetEvidence(context.Context, string) (verification.Evidence, error)
	RecordAuthoritativeSourceObservation(context.Context, string, verification.SourceObservation,
		string, string, time.Time, repository.AuditActor, bool) (string, error)
	GetAuthoritativeObservationByKey(context.Context, string, string) (authority.CaptureReceipt, error)
}

// CIPCIntake is only instantiated by the server after explicit certified
// specification/secret/custody configuration. An applicant cannot supply the
// source, response, outcome, lookup host, or certification.
type CIPCIntake struct {
	Repo           CaptureRepository
	AllowedClientID string // exact IAM-registered source verifier, never applicant-selected
	Transport      authority.CIPCTransport
	Vault          authority.SealedVault
	Registrations  verification.VerifiedRegistrationResolver
	Now            func() time.Time
	AllowSynthetic bool
}

type custodiedRegistry struct {
	transport     authority.CIPCTransport
	vault         authority.SealedVault
	evidenceID    string
	attemptDigest string
	ttl           time.Duration
}

func (c custodiedRegistry) LookupCompany(ctx context.Context, number string) (verification.CompanyRecord, error) {
	record, err := c.transport.Lookup(ctx, number)
	if err != nil {
		return verification.CompanyRecord{}, ErrCaptureDenied
	}
	uri, err := c.vault.SealVersioned(ctx, c.evidenceID, c.attemptDigest, record.RawResponse)
	if err != nil {
		return verification.CompanyRecord{}, ErrCaptureDenied
	}
	return verification.CompanyRecord{
		RegistrationNumber: record.RegistrationNumber, LegalName: record.LegalName,
		EntityStatus: record.Status, SourceReference: record.SourceReference,
		ProvenanceRef: uri, ResponseDigest: record.Digest,
		ObservedAt: record.ObservedAt, FreshnessUntil: record.ObservedAt.Add(c.ttl),
		Synthetic: c.transport.AllowLoopback,
	}, nil
}

func (s CIPCIntake) Capture(ctx context.Context, caseID, claimID, evidenceID, key string, actor repository.AuditActor) (authority.CaptureReceipt, error) {
	empty := authority.CaptureReceipt{}
	if s.Repo == nil || ctx == nil || s.Now == nil || caseID == "" || claimID == "" ||
		evidenceID == "" || len(key) < 16 || len(key) > 128 || actor.ActorType != "workload" ||
		s.AllowedClientID == "" || actor.ClientID != s.AllowedClientID || actor.ActorID == "" {
		return empty, ErrCaptureDenied
	}
	// Replays are looked up before calling CIPC, but may NOT return old
	// provenance while the source evidence, case or claim has been revoked.
	prior, err := s.Repo.GetAuthoritativeObservationByKey(ctx, actor.ActorID, key)
	replay := err == nil
	if replay {
		if prior.CaseID != caseID || prior.ClaimID != claimID || prior.EvidenceID != evidenceID {
			return empty, ErrCaptureDenied
		}
	} else if !errors.Is(err, repository.ErrVerificationNotFound) {
		return empty, ErrCaptureDenied
	}
	c, err := s.Repo.GetVerificationCase(ctx, caseID)
	if err != nil || c.Status != verification.CaseVerifying || c.Subject.SubjectType != "ORGANISATION" {
		return empty, ErrCaptureDenied
	}
	claims, err := s.Repo.ListVerificationClaims(ctx, caseID)
	if err != nil {
		return empty, ErrCaptureDenied
	}
	var claim verification.Claim
	count := 0
	for _, candidate := range claims {
		if candidate.ClaimID == claimID {
			claim = candidate
			count++
		}
	}
	if count != 1 || claim.Status != verification.ClaimUnderCheck ||
		claim.Subject != c.Subject || claim.Purpose != c.Purpose ||
		claim.Jurisdiction != "ZA" {
		return empty, ErrCaptureDenied
	}
	evidence, err := s.Repo.GetEvidence(ctx, evidenceID)
	if err != nil || evidence.Status != verification.EvidenceAvail ||
		evidence.SourceID != "esrc_cipc" || evidence.Subject != c.Subject ||
		evidence.Purpose != c.Purpose || evidence.SourceRecordReference == "" {
		return empty, ErrCaptureDenied
	}
	now := s.Now().UTC()
	if evidence.ExpiresAt != nil && !evidence.ExpiresAt.After(now) {
		return empty, ErrCaptureDenied
	}
	if replay {
		// The exact still-AVAILABLE issuer evidence and open review case are
		// required on every replay. No network request or custody rewrite.
		if prior.SourceID != "esrc_cipc" || prior.AuthorityGranted ||
			!prior.FreshnessUntil.After(now) {
			return empty, ErrCaptureDenied
		}
		return prior, nil
	}
	// Every authenticated source lookup gets one immutable custody object.
	// Re-verification with a new request key may reveal a changed official fact
	// and must never overwrite the historical issuer response.
	sum := sha256.Sum256([]byte("peo03g-cipc/v2\x00" + actor.ActorID + "\x00" +
		actor.ClientID + "\x00" + caseID + "\x00" + claimID + "\x00" +
		evidenceID + "\x00" + key))
	attemptDigest := "sha256:" + hex.EncodeToString(sum[:])
	registry := custodiedRegistry{transport: s.Transport, vault: s.Vault,
		evidenceID: evidenceID, attemptDigest: attemptDigest, ttl: 24 * time.Hour}
	adapter := verification.CIPCCompaniesAdapter{Registry: registry,
		Registrations: s.Registrations, Certified: true,
		AllowSynthetic: s.AllowSynthetic, AdapterID: "baobab.cipc.certified"}
	observation, err := adapter.LookupCIPC(ctx, caseID, claim, now)
	if err != nil || observation.SourceReference != evidence.SourceRecordReference {
		return empty, ErrCaptureDenied
	}
	id, err := s.Repo.RecordAuthoritativeSourceObservation(ctx, caseID, observation,
		evidenceID, key, now, actor, s.AllowSynthetic)
	if err != nil {
		return empty, ErrCaptureDenied
	}
	return authority.CaptureReceipt{ObservationID: id, EvidenceID: evidenceID, CaseID: caseID,
		ClaimID: claimID, SourceID: "esrc_cipc", ObservedAt: observation.ObservedAt,
		FreshnessUntil: observation.FreshnessUntil, AuthorityGranted: false}, nil
}
