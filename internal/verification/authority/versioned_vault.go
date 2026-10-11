package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SealVersioned stores an independent immutable source response for each
// authenticated verification attempt. EvidenceID alone is not an observation
// identity: a later official lookup is allowed to yield changed issuer data.
// The attempt digest is derived server-side from case, claim, evidence,
// workload principal and idempotency key, never supplied by an applicant.
func (v SealedVault) SealVersioned(ctx context.Context, evidenceID, attemptDigest string, plaintext []byte) (string, error) {
	key, err := observationCustodyKey(evidenceID, attemptDigest)
	if err != nil {
		return "", ErrCustody
	}
	if _, err := v.Seal(ctx, key, plaintext); err != nil {
		return "", err
	}
	// The opaque reference contains no document, legal-entity or client IDs.
	// Consumers MUST authorise access against the immutable observation
	// before resolving this reference: possession is not permission.
	return "sealed-v2:" + strings.TrimPrefix(key, "obs_"), nil
}

// OpenVersioned must only be called after CP has authorised the observation,
// subject, purpose, tenant and evidence reference. It cannot be exposed by a
// generic raw-file API and does not itself grant access.
func (v SealedVault) OpenVersioned(ctx context.Context, ref string) ([]byte, error) {
	const prefix = "sealed-v2:"
	if !strings.HasPrefix(ref, prefix) {
		return nil, ErrCustody
	}
	part := strings.TrimPrefix(ref, prefix)
	if len(part) != 64 {
		return nil, ErrCustody
	}
	for _, ch := range part {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return nil, ErrCustody
		}
	}
	return v.Open(ctx, "obs_"+part)
}

func observationCustodyKey(evidenceID, attemptDigest string) (string, error) {
	if len(evidenceID) < 8 || len(evidenceID) > 96 ||
		len(attemptDigest) != len("sha256:")+64 ||
		!strings.HasPrefix(attemptDigest, "sha256:") {
		return "", ErrCustody
	}
	for _, ch := range strings.TrimPrefix(attemptDigest, "sha256:") {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return "", ErrCustody
		}
	}
	sum := sha256.Sum256([]byte("baobab-cipc-response/v2\x00" + evidenceID + "\x00" + attemptDigest))
	return "obs_" + hex.EncodeToString(sum[:]), nil
}
