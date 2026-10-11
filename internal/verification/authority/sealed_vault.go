package authority

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrCustody = errors.New("authoritative-source evidence custody refused")

// SealedVault is a local/dev or encrypted volume custody adapter. The key
// comes from an independently mounted 32-byte secret, not an environment
// string or source record. Production must use a certified custody backend
// with key lifecycle, access logging, retention and destruction policies.
type SealedVault struct {
	Directory string
	Key       []byte
}

// Seal commits encrypted ciphertext before a source observation is linked to
// it. No plaintext, taxpayer PIN or bearer token is written to the evidence
// metadata database, logfile or source ID.
func (v SealedVault) Seal(ctx context.Context, evidenceID string, plaintext []byte) (string, error) {
	if ctx == nil || ctx.Err() != nil || v.Directory == "" || len(v.Key) != 32 ||
		len(plaintext) == 0 || len(plaintext) > 128*1024 ||
		len(evidenceID) < 8 || len(evidenceID) > 96 {
		return "", ErrCustody
	}
	block, err := aes.NewCipher(v.Key)
	if err != nil { return "", ErrCustody }
	aead, err := cipher.NewGCM(block)
	if err != nil { return "", ErrCustody }
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil { return "", ErrCustody }
	ciphertext := aead.Seal(nonce, nonce, plaintext, []byte(evidenceID))
	if err := os.MkdirAll(v.Directory, 0700); err != nil { return "", fmt.Errorf("%w: directory creation failed", ErrCustody) }
	dir, err := os.Open(v.Directory)
	if err != nil { return "", ErrCustody }
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 { return "", fmt.Errorf("%w: directory permissions rejected", ErrCustody) }
	name := sha256.Sum256([]byte(evidenceID))
	path := filepath.Join(v.Directory, hex.EncodeToString(name[:])+".sealed")
	// O_EXCL avoids replacing an existing immutable source response; a retry
	// must reconcile its existing evidence binding rather than overwrite it.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil { return "", ErrCustody }
	_, err = file.Write(ciphertext)
	if err == nil { err = file.Sync() }
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		return "", ErrCustody
	}
	if ctx.Err() != nil { return "", ErrCustody }
	return "sealed-sha256:" + hex.EncodeToString(name[:]), nil
}

// Open requires the original internal evidence ID; the returned bytes must
// never be served to an unauthorised user or logged as HTTP response material.
func (v SealedVault) Open(ctx context.Context, evidenceID string) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || len(v.Key) != 32 || evidenceID == "" { return nil, ErrCustody }
	name := sha256.Sum256([]byte(evidenceID))
	path := filepath.Join(v.Directory, hex.EncodeToString(name[:])+".sealed")
	f, err := os.Open(path)
	if err != nil { return nil, ErrCustody }
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128*1024+256 {
		return nil, ErrCustody
	}
	contents, err := io.ReadAll(io.LimitReader(f, 128*1024+257))
	if err != nil { return nil, ErrCustody }
	block, err := aes.NewCipher(v.Key)
	if err != nil { return nil, ErrCustody }
	aead, err := cipher.NewGCM(block)
	if err != nil || len(contents) < aead.NonceSize()+aead.Overhead() { return nil, ErrCustody }
	plain, err := aead.Open(nil, contents[:aead.NonceSize()], contents[aead.NonceSize():], []byte(evidenceID))
	if err != nil { return nil, fmt.Errorf("%w: authentication failed", ErrCustody) }
	return plain, nil
}
