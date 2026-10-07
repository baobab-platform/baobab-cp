package eventingress

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	testSecret = func() []byte {
		b := make([]byte, 32)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}()
	testKey = Key{ID: "erp-delivery-2026-10", Sender: "baobab-erp", Secret: testSecret}
	instant = time.Date(2026, 10, 7, 17, 30, 0, 0, time.UTC)
)

func lookup(keys ...Key) func(string) (Key, bool) {
	return func(id string) (Key, bool) {
		for _, k := range keys {
			if k.ID == id {
				return k, true
			}
		}
		return Key{}, false
	}
}

// ERP's test suite pins this exact vector (baobab-erp tests/conformance/test_provisioning_changed.py): the sender and the receiver
// of signed delivery are two implementations of one contract, and these bytes are how they are proven to agree.
func TestTheSignatureMatchesTheSendersFixedVector(t *testing.T) {
	body := []byte(`{"specversion":"1.0"}`)
	const want = "hmac-sha256=f8fb9e93e325a379d3692202629c81bc8f94654521bb6f5073e12b324ae3adea"
	if got := Sign(testKey, "2026-10-07T17:30:00Z", body); got != want {
		t.Fatalf("signature %s does not match the sender's vector %s", got, want)
	}
	text := string(SigningString(testKey.ID, "2026-10-07T17:30:00Z", body))
	if !strings.HasPrefix(text, "baobab-event-delivery-v1\nbaobab-control-plane\nerp-delivery-2026-10\n2026-10-07T17:30:00Z\n") || strings.HasSuffix(text, "\n") {
		t.Fatalf("signing string is not the documented one: %q", text)
	}
}

func signed(key Key, ts string, body []byte) Headers {
	return Headers{KeyID: key.ID, Timestamp: ts, Signature: Sign(key, ts, body)}
}

func TestAGenuineDeliveryVerifies(t *testing.T) {
	body := []byte(`{"a":1}`)
	key, err := Verify(lookup(testKey), signed(testKey, "2026-10-07T17:30:00Z", body), body, instant)
	if err != nil || key.ID != testKey.ID || key.Sender != "baobab-erp" {
		t.Fatalf("key=%v err=%v", key, err)
	}
}

func TestEveryBindingMatters(t *testing.T) {
	body := []byte(`{"a":1}`)
	h := signed(testKey, "2026-10-07T17:30:00Z", body)
	other := Key{ID: testKey.ID, Sender: "baobab-erp", Secret: bytes.Repeat([]byte{7}, 32)}
	for name, c := range map[string]struct {
		keys    func(string) (Key, bool)
		headers Headers
		body    []byte
	}{
		"another body":      {lookup(testKey), h, []byte(`{"a":2}`)},
		"another timestamp": {lookup(testKey), Headers{h.KeyID, "2026-10-07T17:30:01Z", h.Signature}, body},
		"another secret":    {lookup(other), h, body},
		"an unknown key":    {lookup(), h, body},
		"a revoked key":     {lookup(Key{ID: testKey.ID, Sender: "baobab-erp", Secret: testSecret, Revoked: true}), h, body},
		"a short secret":    {lookup(Key{ID: testKey.ID, Sender: "baobab-erp", Secret: testSecret[:31]}), h, body},
	} {
		if _, err := Verify(c.keys, c.headers, c.body, instant); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestTheReplayWindowIs300SecondsEitherWay(t *testing.T) {
	body := []byte(`{}`)
	h := signed(testKey, "2026-10-07T17:30:00Z", body)
	for offset, ok := range map[time.Duration]bool{-300 * time.Second: true, 300 * time.Second: true, 301 * time.Second: false, -301 * time.Second: false, 0: true, 24 * time.Hour: false} {
		_, err := Verify(lookup(testKey), h, body, instant.Add(offset))
		if (err == nil) != ok {
			t.Errorf("offset %s: err=%v want ok=%v", offset, err, ok)
		}
	}
}

// Header syntax is judged before any key is touched, with the closed grammar of signed-delivery.schema.json, and is the one
// failure that is not "unauthenticated".
func TestMalformedHeadersAreRefusedBeforeAnyKeyIsLookedUp(t *testing.T) {
	good := signed(testKey, "2026-10-07T17:30:00Z", []byte(`{}`))
	called := false
	probe := func(string) (Key, bool) { called = true; return testKey, true }
	for name, h := range map[string]Headers{
		"empty key id":          {"", good.Timestamp, good.Signature},
		"uppercase key id":      {"ERP-KEY", good.Timestamp, good.Signature},
		"key id with a slash":   {"erp/key", good.Timestamp, good.Signature},
		"short key id":          {"ab", good.Timestamp, good.Signature},
		"offset timestamp":      {good.KeyID, "2026-10-07T17:30:00+02:00", good.Signature},
		"fractional timestamp":  {good.KeyID, "2026-10-07T17:30:00.5Z", good.Signature},
		"impossible date":       {good.KeyID, "2026-02-31T17:30:00Z", good.Signature},
		"bare hex signature":    {good.KeyID, good.Timestamp, strings.Repeat("0", 64)},
		"unknown algorithm":     {good.KeyID, good.Timestamp, "none=" + strings.Repeat("0", 64)},
		"uppercase hex":         {good.KeyID, good.Timestamp, "hmac-sha256=" + strings.Repeat("A", 64)},
		"truncated signature":   {good.KeyID, good.Timestamp, "hmac-sha256=" + strings.Repeat("0", 63)},
		"signature with suffix": {good.KeyID, good.Timestamp, good.Signature + "0"},
	} {
		if _, err := Verify(probe, h, []byte(`{}`), instant); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if called {
		t.Fatal("a key was looked up for a malformed delivery")
	}
}

func TestAKeyNeverShowsItsSecret(t *testing.T) {
	if s := testKey.String(); strings.Contains(s, "\x01") || !strings.Contains(s, "redacted") {
		t.Fatalf("String shows the secret: %q", s)
	}
}
