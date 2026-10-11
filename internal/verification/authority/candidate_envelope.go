package authority

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

var ErrCandidateEnvelope = errors.New("document candidate envelope denied")

// ValidateCandidateReceipt is checked AGAIN at persistence, not just at the
// OCR HTTP edge. No processor response can become a VERIFIED claim.
func ValidateCandidateReceipt(a DocumentCandidateAttempt) error {
	r := a.Receipt
	if a.CaseID == "" || a.ClaimID == "" || a.EvidenceID == "" || a.ArtifactID == "" || a.ActorID == "" ||
		r.EvidenceID != a.EvidenceID || r.Standing != "DOCUMENT_CANDIDATE_ONLY" ||
		r.ProcessorID == "" || r.ProcessorVersion == "" ||
		r.ObservedAt.IsZero() || r.ObservedAt.After(time.Now().UTC().Add(time.Minute)) ||
		len(r.DocumentDigest) != 71 || !strings.HasPrefix(r.DocumentDigest, "sha256:") ||
		len(r.Candidates) > 32 {
		return ErrCandidateEnvelope
	}
	for _, c := range r.Candidates {
		if !documentCandidateAllowed(c.Field) || c.Value == "" || len(c.Value) > 256 ||
			strings.ContainsAny(c.Value, "\x00\r\n") || math.IsNaN(c.Confidence) || math.IsInf(c.Confidence, 0) ||
			c.Confidence < 0 || c.Confidence > 1 || c.Page < 1 || c.Page > 1000 {
			return ErrCandidateEnvelope
		}
	}
	return nil
}

func candidateAAD(a DocumentCandidateAttempt, clientID, key string) []byte {
	return []byte(a.CaseID + "\n" + a.ClaimID + "\n" + a.EvidenceID + "\n" + a.ArtifactID +
		"\n" + a.ActorID + "\n" + clientID + "\n" + key + "\n" + a.Receipt.DocumentDigest)
}

// EncryptCandidates stores only ciphertext in PostgreSQL. The key must come
// from a separately mounted KMS-derived secret with rotation governance.
func EncryptCandidates(key []byte, a DocumentCandidateAttempt, clientID, idempotencyKey string) ([]byte, error) {
	if len(key) != 32 || clientID == "" || len(idempotencyKey) < 16 || len(idempotencyKey) > 128 ||
		ValidateCandidateReceipt(a) != nil {
		return nil, ErrCandidateEnvelope
	}
	raw, err := json.Marshal(a.Receipt.Candidates)
	if err != nil || len(raw) > 48*1024 {
		return nil, ErrCandidateEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCandidateEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCandidateEnvelope
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, ErrCandidateEnvelope
	}
	return aead.Seal(nonce, nonce, raw, candidateAAD(a, clientID, idempotencyKey)), nil
}

// DecryptCandidates authenticates every metadata selector as AEAD additional
// data, so swapping encrypted rows across actors or evidence fails closed.
func DecryptCandidates(key, ciphertext []byte, a DocumentCandidateAttempt, clientID, idempotencyKey string) ([]DocumentCandidate, error) {
	if len(key) != 32 || len(ciphertext) < 29 || len(ciphertext) > 65536 {
		return nil, ErrCandidateEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCandidateEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrCandidateEnvelope
	}
	raw, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], candidateAAD(a, clientID, idempotencyKey))
	if err != nil || len(raw) > 48*1024 {
		return nil, ErrCandidateEnvelope
	}
	var candidates []DocumentCandidate
	if err = json.Unmarshal(raw, &candidates); err != nil {
		return nil, ErrCandidateEnvelope
	}
	a.Receipt.Candidates = candidates
	if ValidateCandidateReceipt(a) != nil {
		return nil, ErrCandidateEnvelope
	}
	return candidates, nil
}
