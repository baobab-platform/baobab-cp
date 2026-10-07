package eventingress

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type keyFile struct {
	KeyID     string `json:"key_id"`
	Sender    string `json:"sender"`
	SecretB64 string `json:"secret_b64"`
	Revoked   bool   `json:"revoked,omitempty"`
}

// ParseKeys reads a key registry: a JSON array of {key_id, sender, secret_b64, revoked}. It is strict, because a misread registry is
// an authentication hole: ids must be well formed and unique, secrets standard base64 of at least 32 bytes, and each sender one of
// allowedSenders (the producers the accepted-event list names).
func ParseKeys(raw []byte, allowedSenders []string) (map[string]Key, error) {
	var entries []keyFile
	decoder := json.NewDecoder(bytesReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("event delivery keys: %w", err)
	}
	// Exactly one document: anything after it (a second array, garbage) means a malformed rotation, never "the first one wins".
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("event delivery keys: unexpected data after the registry")
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("event delivery keys: at least one key is required")
	}
	keys := make(map[string]Key, len(entries))
	for index, entry := range entries {
		if !keyIDPattern.MatchString(entry.KeyID) {
			return nil, fmt.Errorf("event delivery keys: entry %d has a malformed key_id", index)
		}
		if _, duplicate := keys[entry.KeyID]; duplicate {
			return nil, fmt.Errorf("event delivery keys: key_id %q is listed twice", entry.KeyID)
		}
		allowed := false
		for _, sender := range allowedSenders {
			allowed = allowed || sender == entry.Sender
		}
		if !allowed {
			return nil, fmt.Errorf("event delivery keys: key %q names a sender that no accepted event has as producer", entry.KeyID)
		}
		secret, err := base64.StdEncoding.DecodeString(entry.SecretB64)
		if err != nil || len(secret) < MinSecretBytes {
			return nil, fmt.Errorf("event delivery keys: key %q needs a standard base64 secret of at least %d bytes", entry.KeyID, MinSecretBytes)
		}
		keys[entry.KeyID] = Key{ID: entry.KeyID, Sender: entry.Sender, Secret: secret, Revoked: entry.Revoked}
	}
	return keys, nil
}

// FileKeys serves keys from a registry file and notices when it changes, so a key can be added, rotated or revoked without a
// restart. A changed file that does not parse keeps the last valid registry and reports the error to OnError; a revocation only
// takes effect through a valid file.
type FileKeys struct {
	Path    string
	Senders []string
	OnError func(error)

	mu       sync.Mutex
	loadedAt time.Time
	modTime  time.Time
	keys     map[string]Key
}

// NewFileKeys loads the registry once and fails if it is not valid, so a deployment that cannot authenticate anyone does not start.
func NewFileKeys(path string, senders []string, onError func(error)) (*FileKeys, error) {
	f := &FileKeys{Path: path, Senders: senders, OnError: onError}
	if err := f.reload(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *FileKeys) reload() error {
	info, err := os.Stat(f.Path)
	if err != nil {
		return fmt.Errorf("event delivery keys: %w", err)
	}
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return fmt.Errorf("event delivery keys: %w", err)
	}
	keys, err := ParseKeys(raw, f.Senders)
	if err != nil {
		return err
	}
	f.keys, f.modTime, f.loadedAt = keys, info.ModTime(), time.Now()
	return nil
}

// Lookup returns the registered key, re-reading the file first when it has changed on disk.
func (f *FileKeys) Lookup(keyID string) (Key, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if info, err := os.Stat(f.Path); err == nil && !info.ModTime().Equal(f.modTime) {
		if err := f.reload(); err != nil {
			f.modTime = info.ModTime() // report once per change, keep serving the last valid registry
			if f.OnError != nil {
				f.OnError(err)
			}
		}
	}
	key, ok := f.keys[keyID]
	return key, ok
}
