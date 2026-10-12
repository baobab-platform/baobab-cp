// Package scannerruntime composes the private scanner boundary. Public trust,
// custody keys and introspection credentials are CP dependencies. The scanner
// signing key is deliberately absent from CPConfig.
package scannerruntime

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrConfiguration = errors.New("protected scanner configuration unavailable")

// Mounted files are re-read at point of use, bounded and symlink-refused.
// Deployments must atomically replace approved files to rotate/revoke them.
func privateFile(path string, max int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrConfiguration
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 1 || info.Size() > max {
		return nil, ErrConfiguration
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrConfiguration
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) || current.Mode().Perm()&0077 != 0 {
		return nil, ErrConfiguration
	}
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil || int64(len(raw)) > max {
		return nil, ErrConfiguration
	}
	return raw, nil
}

func privateJSON(path string, out any) error {
	raw, err := privateFile(path, 32768)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF {
		return ErrConfiguration
	}
	return nil
}

func secret(path string) (string, error) {
	raw, err := privateFile(path, 8192)
	value := strings.TrimSpace(string(raw))
	if err != nil || value == "" || strings.ContainsAny(value, "\r\n") {
		return "", ErrConfiguration
	}
	return value, nil
}

type TrustRecord struct {
	ScannerID         string    `json:"scanner_id"`
	ScannerVersion    string    `json:"scanner_version"`
	PublicKeyHex      string    `json:"public_key_hex"`
	ProposedBy        string    `json:"proposed_by"`
	ApprovedBy        string    `json:"approved_by"`
	ApprovalReference string    `json:"approval_reference"`
	EffectiveFrom     time.Time `json:"effective_from"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type MountedScannerTrust struct {
	Path string
	Now  func() time.Time
}

func (m MountedScannerTrust) records(ctx context.Context) ([]TrustRecord, error) {
	if ctx == nil || ctx.Err() != nil || m.Now == nil {
		return nil, ErrConfiguration
	}
	var records struct {
		Keys []TrustRecord `json:"keys"`
	}
	if privateJSON(m.Path, &records) != nil || len(records.Keys) == 0 || len(records.Keys) > 100 {
		return nil, ErrConfiguration
	}
	seen := map[string]bool{}
	for _, r := range records.Keys {
		key, err := hex.DecodeString(r.PublicKeyHex)
		identity := r.ScannerID + "\x00" + r.ScannerVersion
		now := m.Now().UTC()
		if r.ScannerID == "" || r.ScannerVersion == "" || seen[identity] || err != nil || len(key) != ed25519.PublicKeySize ||
			r.ProposedBy == "" || r.ApprovedBy == "" || r.ProposedBy == r.ApprovedBy || r.ApprovalReference == "" ||
			r.EffectiveFrom.IsZero() || r.EffectiveFrom.After(now) || !r.ExpiresAt.After(now) {
			return nil, ErrConfiguration
		}
		seen[identity] = true
	}
	return records.Keys, nil
}

// Validate rejects an empty, stale or self-approved trust set at startup;
// ScannerKey repeats that validation at each operation/revocation boundary.
func (m MountedScannerTrust) Validate(ctx context.Context) error {
	_, err := m.records(ctx)
	return err
}

func (m MountedScannerTrust) ScannerKey(ctx context.Context, id, version string) (ed25519.PublicKey, error) {
	records, err := m.records(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.ScannerID == id && r.ScannerVersion == version {
			key, _ := hex.DecodeString(r.PublicKeyHex)
			return ed25519.PublicKey(key), nil
		}
	}
	return nil, ErrConfiguration
}

// ScannerProofKey binds the independently signed timestamp to the SAME
// current approval record used for key selection. Trust granted later cannot
// retroactively admit proofs prepared before that approval's effective window.
func (m MountedScannerTrust) ScannerProofKey(ctx context.Context, p authority.ScanProof) (ed25519.PublicKey, error) {
	records, err := m.records(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.ScannerID != p.ScannerID || r.ScannerVersion != p.ScannerVersion {
			continue
		}
		if p.ScannedAt.IsZero() || p.ScannedAt.Before(r.EffectiveFrom) || !p.ScannedAt.Before(r.ExpiresAt) {
			return nil, ErrConfiguration
		}
		key, _ := hex.DecodeString(r.PublicKeyHex)
		return ed25519.PublicKey(key), nil
	}
	return nil, ErrConfiguration
}
