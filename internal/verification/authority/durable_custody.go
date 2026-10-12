package authority

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// CustodyObject identifies one immutable ciphertext version. WrappedKey is
// KMS ciphertext, never plaintext key material. Object keys and encryption
// context use hashes, so cloud access logs do not disclose evidence selectors.
type CustodyObject struct {
	Revision                                                        int64
	EvidenceID, ArtifactID, ProofHash, ObjectKey, VersionID         string
	KeyID, CipherDigest, PolicyID, PolicyVersion, PolicyHash, State string
	WrappedKey, Nonce                                               []byte
	RetainUntil, DeleteAfter, OrphanAfter, QuarantinedAt            time.Time
	LegalHold                                                       bool
}

func CustodyProofHash(p ScanProof) string {
	raw := append(p.SigningPayload(), p.Signature...)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// CustodyBackend never accepts an unversioned read/delete or a governance
// bypass. Create is conditional; an uncertain upload is recovered by Stat.
type CustodyBackend interface {
	GenerateKey(context.Context, string) (plain, wrapped []byte, keyID string, err error)
	UnwrapKey(context.Context, CustodyObject) ([]byte, error)
	RewrapKey(context.Context, CustodyObject, string) ([]byte, error)
	Create(context.Context, CustodyObject, []byte) (string, error)
	Stat(context.Context, CustodyObject) (CustodyObject, error)
	Read(context.Context, CustodyObject) ([]byte, error)
	Hold(context.Context, CustodyObject, bool) error
	Delete(context.Context, CustodyObject) error
}

// EncryptCustody uses a random per-object data key and nonce. A recorded
// nonce may be reused ONLY for exact retries of the same immutable proof and
// digest. Never call this function for changed input under an existing intent.
func EncryptCustody(p ScanProof, document, key, nonce []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 12 || len(document) < 8 || len(document) > MaxScannedDocumentBytes || !validDocumentSignature(p.MediaType, document) {
		return nil, ErrDocumentCustody
	}
	h := sha256.Sum256(document)
	if p.Digest != "sha256:"+hex.EncodeToString(h[:]) {
		return nil, ErrDocumentCustody
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDocumentCustody
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, ErrDocumentCustody
	}
	return g.Seal(nil, nonce, document, []byte(CustodyProofHash(p))), nil
}

func DecryptCustody(p ScanProof, sealed, key, nonce []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 12 || len(sealed) < 24 || len(sealed) > MaxScannedDocumentBytes+16 {
		return nil, ErrDocumentCustody
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDocumentCustody
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, ErrDocumentCustody
	}
	plain, err := g.Open(nil, nonce, sealed, []byte(CustodyProofHash(p)))
	if err != nil {
		return nil, ErrDocumentCustody
	}
	// Digest and MIME checks are repeated after authenticated decryption.
	if _, err = EncryptCustody(p, plain, key, nonce); err != nil {
		ClearKey(plain)
		return nil, ErrDocumentCustody
	}
	return plain, nil
}

func NewCustodyNonce() ([]byte, error) { n := make([]byte, 12); _, err := rand.Read(n); return n, err }
func CipherDigest(raw []byte) string   { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func ClearKey(key []byte) {
	for i := range key {
		key[i] = 0
	}
}
