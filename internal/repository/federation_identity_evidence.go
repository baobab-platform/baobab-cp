package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

// FederationIdentityEvidence is an atomic CP-owned identity relationship plus
// its registered immutable native evidence reference. A reference is not IAM
// approval: the IAM consumer separately checks its governed approval receipt.
type FederationIdentityEvidence struct {
	Identity  FederationIdentity
	Reference domain.ExternalReference
}

type FederationIdentityEvidenceReader interface {
	ReadFederationIdentityEvidence(context.Context, string, string, string, string) (FederationIdentityEvidence, error)
}

// FederationIdentityDigest describes only the existing issuer/subject actor
// relationship. No email, business CanonicalEntity, role or provider label is
// involved. Lifecycle is checked currently rather than frozen into the digest.
func FederationIdentityDigest(identity FederationIdentity) string {
	data, _ := json.Marshal(struct{ Issuer, Subject, PrincipalID, ExternalIdentityID string }{
		identity.ExternalIdentity.Issuer, identity.ExternalIdentity.Subject, identity.Principal.ID, identity.ExternalIdentity.ID})
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}
