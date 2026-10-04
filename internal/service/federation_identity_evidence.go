package service

import (
	"context"
	"reflect"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// FederationCanonicalEvidence is IAM's PRIVATE canonical authority projection,
// not a new public identity model. Its mapping reference must still be approved
// by IAM for the consuming trust/revision/scope before a login can succeed.
type FederationCanonicalEvidence struct {
	Issuer, Subject, PrincipalID, ExternalIdentityID, MappingReference string
	PrincipalStatus, ExternalIdentityStatus, ActorType, MappingBasis   string
	ValidUntil                                                         time.Time
}

type FederationIdentityEvidenceService struct {
	Repository                    repository.FederationIdentityEvidenceReader
	Environment, EngineInstanceID string
	Now                           func() time.Time
}

func (s FederationIdentityEvidenceService) Resolve(ctx context.Context, issuer, subject string) (FederationCanonicalEvidence, error) {
	if s.Repository == nil || ctx == nil || ctx.Err() != nil || s.Now == nil || !domain.ValidEngineInstanceID(s.EngineInstanceID) {
		return FederationCanonicalEvidence{}, ErrFederationIdentityUnavailable
	}
	if value := reflect.ValueOf(s.Repository); value.Kind() == reflect.Pointer && value.IsNil() {
		return FederationCanonicalEvidence{}, ErrFederationIdentityUnavailable
	}
	if !validFederationEvidenceEnvironment(s.Environment) || !validFederationEvidenceSubject(issuer, subject) {
		return FederationCanonicalEvidence{}, ErrFederationIdentityDenied
	}
	value, err := s.Repository.ReadFederationIdentityEvidence(ctx, issuer, subject, s.Environment, s.EngineInstanceID)
	if err != nil || ctx.Err() != nil {
		return FederationCanonicalEvidence{}, ErrFederationIdentityUnavailable
	}
	p, e, x := value.Identity.Principal, value.Identity.ExternalIdentity, value.Reference
	now := s.Now().UTC()
	if !federationUUID.MatchString(p.ID) || !federationUUID.MatchString(e.ID) || p.ID != e.PrincipalID || p.Status != "ACTIVE" || e.Status != "ACTIVE" || p.ActorType != "human" || e.Issuer != issuer || e.Subject != subject || !domain.ValidExternalReferenceID(x.ID) || x.SystemNamespace != "baobab_cp" || x.EngineID != "baobab-cp" || x.Environment != s.Environment || x.EngineInstanceID != s.EngineInstanceID || x.NativeEntityType != "canonical_identity_mapping" || x.NativeID != e.ID || x.SourceAuthority != "engine" && x.SourceAuthority != "reconciliation" || x.Status != "active" || x.Fingerprint != repository.FederationIdentityDigest(value.Identity) || x.LastVerifiedAt == nil || x.LastVerifiedAt.After(now) || !now.Before(x.LastVerifiedAt.Add(5*time.Minute)) {
		return FederationCanonicalEvidence{}, ErrFederationIdentityDenied
	}
	until := now.Add(time.Minute)
	if bound := x.LastVerifiedAt.Add(5 * time.Minute); bound.Before(until) {
		until = bound
	}
	return FederationCanonicalEvidence{issuer, subject, p.ID, e.ID, x.ID, p.Status, e.Status, p.ActorType, "ISSUER_SUBJECT", until}, nil
}
