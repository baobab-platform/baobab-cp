package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCustodyEnvelopeBindsExactProofDigestAndMedia(t *testing.T) {
	doc := []byte("%PDF-1.7\nsynthetic custody proof")
	h := sha256.Sum256(doc)
	p := ScanProof{EvidenceID: "evr_synthetic", ArtifactID: "eart_synthetic", MediaType: "application/pdf", Digest: "sha256:" + hex.EncodeToString(h[:]), Signature: bytes.Repeat([]byte{7}, 64)}
	key := bytes.Repeat([]byte{8}, 32)
	nonce := bytes.Repeat([]byte{9}, 12)
	sealed, err := EncryptCustody(p, doc, key, nonce)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptCustody(p, sealed, key, nonce)
	if err != nil || !bytes.Equal(plain, doc) {
		t.Fatal("exact envelope unavailable")
	}
	for _, mutate := range []func(*ScanProof){func(p *ScanProof) { p.EvidenceID = "evr_foreign" }, func(p *ScanProof) { p.ArtifactID = "eart_foreign" }, func(p *ScanProof) { p.MediaType = "image/png" }, func(p *ScanProof) { p.ScannerVersion = "other" }, func(p *ScanProof) { p.Signature = bytes.Repeat([]byte{5}, 64) }} {
		changed := p
		mutate(&changed)
		if _, err := DecryptCustody(changed, sealed, key, nonce); err == nil {
			t.Fatal("swapped proof decrypted")
		}
	}
	corrupted := append([]byte(nil), sealed...)
	corrupted[len(corrupted)-1] ^= 1
	if _, err := DecryptCustody(p, corrupted, key, nonce); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	if _, err := EncryptCustody(p, []byte("not a PDF"), key, nonce); err == nil {
		t.Fatal("changed bytes sealed under retry nonce")
	}
	ClearKey(key)
	if !bytes.Equal(key, make([]byte, 32)) {
		t.Fatal("key was not cleared")
	}
}
