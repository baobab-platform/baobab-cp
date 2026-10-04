package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type federationEvidenceReader struct {
	value repository.FederationIdentityEvidence
	err   error
	calls int
}

func (r *federationEvidenceReader) ReadFederationIdentityEvidence(context.Context, string, string, string, string) (repository.FederationIdentityEvidence, error) {
	r.calls++
	return r.value, r.err
}

func TestFederationCanonicalEvidenceRequiresCurrentExactReference(t *testing.T) {
	now := time.Now().UTC()
	p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
	e := domain.ExternalIdentity{ID: domain.NewExternalIdentityID(), PrincipalID: p.ID, Issuer: "https://upstream.example", Subject: "stable-human", Status: "ACTIVE"}
	identity := repository.FederationIdentity{Principal: p, ExternalIdentity: e}
	reference := domain.ExternalReference{ID: "ref_testcanonical", SystemNamespace: "baobab_cp", EngineID: "baobab-cp", EngineInstanceID: "ei_testcanonical", Environment: "development", NativeEntityType: "canonical_identity_mapping", NativeID: e.ID, SourceAuthority: "reconciliation", Fingerprint: repository.FederationIdentityDigest(identity), Status: "active", LastVerifiedAt: &now}
	for _, mode := range []string{"valid", "manual-import", "unverified", "stale", "future", "digest-drift", "instance", "environment", "principal-revoked", "external-revoked", "native-target", "provider-namespace", "storage-down"} {
		t.Run(mode, func(t *testing.T) {
			reader := &federationEvidenceReader{value: repository.FederationIdentityEvidence{Identity: identity, Reference: reference}}
			switch mode {
			case "manual-import":
				reader.value.Reference.SourceAuthority = "manual-import"
			case "unverified":
				reader.value.Reference.Status = "unverified"
			case "stale":
				value := now.Add(-5 * time.Minute)
				reader.value.Reference.LastVerifiedAt = &value
			case "future":
				value := now.Add(time.Second)
				reader.value.Reference.LastVerifiedAt = &value
			case "digest-drift":
				reader.value.Identity.ExternalIdentity.PrincipalID = domain.NewPrincipalID()
			case "instance":
				reader.value.Reference.EngineInstanceID = "ei_otherinstance"
			case "environment":
				reader.value.Reference.Environment = "production"
			case "principal-revoked":
				reader.value.Identity.Principal.Status = "REVOKED"
			case "external-revoked":
				reader.value.Identity.ExternalIdentity.Status = "REVOKED"
			case "native-target":
				reader.value.Reference.NativeID = domain.NewExternalIdentityID()
			case "provider-namespace":
				reader.value.Reference.SystemNamespace = "keycloak"
			case "storage-down":
				reader.err = errors.New("private storage details")
			}
			source := FederationIdentityEvidenceService{Repository: reader, Environment: "development", EngineInstanceID: reference.EngineInstanceID, Now: func() time.Time { return now }}
			out, err := source.Resolve(context.Background(), e.Issuer, e.Subject)
			if mode == "valid" {
				if err != nil || out.PrincipalID != p.ID || out.MappingReference != reference.ID || !out.ValidUntil.Equal(now.Add(time.Minute)) {
					t.Fatal(out, err)
				}
			} else if err == nil || out.PrincipalID != "" {
				t.Fatal("unverified authority accepted", out, err)
			}
		})
	}
	var missing *federationEvidenceReader
	source := FederationIdentityEvidenceService{Repository: missing, Environment: "development", EngineInstanceID: reference.EngineInstanceID, Now: time.Now}
	if _, err := source.Resolve(context.Background(), e.Issuer, e.Subject); !errors.Is(err, ErrFederationIdentityUnavailable) {
		t.Fatal("nil backend", err)
	}
}
