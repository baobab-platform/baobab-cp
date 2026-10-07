package eventingress

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"time"
)

const (
	// Recipient is the consumer name bound into every signature, so a signature made for another consumer is worthless here.
	Recipient    = "baobab-control-plane"
	signingLabel = "baobab-event-delivery-v1"
	// ReplayWindow is how far a delivery's timestamp may be from this service's clock, either way.
	ReplayWindow = 300 * time.Second
	// MinSecretBytes is the smallest delivery secret accepted.
	MinSecretBytes = 32
	// MaxBodyBytes is the largest event accepted.
	MaxBodyBytes = 1 << 20

	signaturePrefix = "hmac-sha256="
	timestampLayout = "2006-01-02T15:04:05Z"
)

var (
	keyIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
	timestampPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T([01]\d|2[0-3]):[0-5]\d:[0-5]\dZ$`)
	signaturePattern = regexp.MustCompile(`^hmac-sha256=[0-9a-f]{64}$`)
)

// ErrUnauthenticated is every authentication failure, deliberately without a reason: an unknown or revoked key, a stale or future
// timestamp and a wrong signature are indistinguishable to the sender.
var ErrUnauthenticated = errors.New("the event delivery could not be authenticated")

// ErrMalformed is a header set that is not well formed. It is judged before anything secret is touched, so it reveals nothing.
var ErrMalformed = errors.New("the event delivery headers are malformed")

// Headers are the three signature headers of one delivery.
type Headers struct {
	KeyID     string
	Timestamp string
	Signature string
}

// Well reports whether the headers have the closed syntax the contract defines.
func (h Headers) Well() error {
	if !keyIDPattern.MatchString(h.KeyID) || !timestampPattern.MatchString(h.Timestamp) || !signaturePattern.MatchString(h.Signature) {
		return ErrMalformed
	}
	if _, err := time.Parse(timestampLayout, h.Timestamp); err != nil {
		return ErrMalformed
	}
	return nil
}

// Key is one registered delivery key. Sender is the producer (a repository name such as baobab-erp) the key may deliver for: a key
// can never deliver an event whose producer is another.
type Key struct {
	ID      string
	Sender  string
	Secret  []byte
	Revoked bool
}

// String never shows the secret.
func (k Key) String() string { return "Key{" + k.ID + " for " + k.Sender + ", secret redacted}" }

// SigningString is the exact text that is signed.
func SigningString(keyID, timestamp string, body []byte) []byte {
	digest := sha256.Sum256(body)
	return []byte(signingLabel + "\n" + Recipient + "\n" + keyID + "\n" + timestamp + "\n" + hex.EncodeToString(digest[:]))
}

// Sign is the signature a sender makes (used by tests and by any sender in this repository).
func Sign(key Key, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, key.Secret)
	mac.Write(SigningString(key.ID, timestamp, body))
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify proves the delivery in the contract's order: the key is registered and not revoked, the timestamp is inside the replay
// window, and the signature matches (constant time). It returns the key on success and ErrUnauthenticated otherwise.
func Verify(lookup func(keyID string) (Key, bool), h Headers, body []byte, now time.Time) (Key, error) {
	if err := h.Well(); err != nil {
		return Key{}, err
	}
	key, ok := lookup(h.KeyID)
	if !ok || key.Revoked || len(key.Secret) < MinSecretBytes {
		return Key{}, ErrUnauthenticated
	}
	signedAt, _ := time.Parse(timestampLayout, h.Timestamp)
	if delta := now.Sub(signedAt); delta > ReplayWindow || delta < -ReplayWindow {
		return Key{}, ErrUnauthenticated
	}
	if !hmac.Equal([]byte(Sign(key, h.Timestamp, body)), []byte(h.Signature)) {
		return Key{}, ErrUnauthenticated
	}
	return key, nil
}
