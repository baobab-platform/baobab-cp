package eventingress

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registry(entries ...string) []byte { return []byte("[" + strings.Join(entries, ",") + "]") }

func entry(id, sender string, secretBytes int, extra string) string {
	return fmt.Sprintf(`{"key_id":%q,"sender":%q,"secret_b64":%q%s}`, id, sender, base64.StdEncoding.EncodeToString(make([]byte, secretBytes)), extra)
}

func TestParseKeysIsStrict(t *testing.T) {
	senders := []string{"baobab-erp"}
	keys, err := ParseKeys(registry(entry("erp-delivery-2026-10", "baobab-erp", 32, ""), entry("erp-delivery-2026-11", "baobab-erp", 48, `,"revoked":true`)), senders)
	if err != nil || len(keys) != 2 || !keys["erp-delivery-2026-11"].Revoked || keys["erp-delivery-2026-10"].Revoked {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	for name, raw := range map[string][]byte{
		"empty":                registry(),
		"not json":             []byte("keys"),
		"a duplicate id":       registry(entry("erp-delivery-2026-10", "baobab-erp", 32, ""), entry("erp-delivery-2026-10", "baobab-erp", 32, "")),
		"a short secret":       registry(entry("erp-delivery-2026-10", "baobab-erp", 31, "")),
		"a bad id":             registry(entry("ERP", "baobab-erp", 32, "")),
		"a sender nobody has":  registry(entry("trade-delivery-2026", "baobab-trade", 32, "")),
		"a second document":    append(registry(entry("erp-delivery-2026-10", "baobab-erp", 32, "")), registry(entry("erp-delivery-2026-11", "baobab-erp", 32, ""))...),
		"trailing garbage":     append(registry(entry("erp-delivery-2026-10", "baobab-erp", 32, "")), []byte(" garbage")...),
		"an unknown member":    registry(entry("erp-delivery-2026-10", "baobab-erp", 32, `,"admin":true`)),
		"a non-base64 secret":  []byte(`[{"key_id":"erp-delivery-2026-10","sender":"baobab-erp","secret_b64":"!!"}]`),
		"a url-safe base64 id": []byte(`[{"key_id":"erp-delivery-2026-10","sender":"baobab-erp","secret_b64":"` + strings.Repeat("-", 44) + `"}]`),
	} {
		if _, err := ParseKeys(raw, senders); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestFileKeysPicksUpRotationAndRevocationAndSurvivesABrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	write := func(content []byte, mod time.Time) {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().Add(-time.Hour)
	write(registry(entry("erp-delivery-2026-10", "baobab-erp", 32, "")), base)
	var reported []error
	keys, err := NewFileKeys(path, []string{"baobab-erp"}, func(err error) { reported = append(reported, err) })
	if err != nil {
		t.Fatal(err)
	}
	if k, ok := keys.Lookup("erp-delivery-2026-10"); !ok || k.Revoked {
		t.Fatal("the loaded key is missing")
	}
	// Rotation: a new key appears and the old one is revoked, without a restart.
	write(registry(entry("erp-delivery-2026-10", "baobab-erp", 32, `,"revoked":true`), entry("erp-delivery-2026-11", "baobab-erp", 32, "")), base.Add(time.Minute))
	if k, _ := keys.Lookup("erp-delivery-2026-10"); !k.Revoked {
		t.Fatal("a revocation did not take effect")
	}
	if _, ok := keys.Lookup("erp-delivery-2026-11"); !ok {
		t.Fatal("a rotated-in key is not served")
	}
	// A broken file never becomes "nobody is authenticated" nor "everybody is": the last valid registry stays, and the error is reported.
	write([]byte("not json"), base.Add(2*time.Minute))
	if _, ok := keys.Lookup("erp-delivery-2026-11"); !ok || len(reported) != 1 {
		t.Fatalf("the last valid registry was lost or the error was not reported (%d)", len(reported))
	}
	if _, ok := keys.Lookup("erp-delivery-2026-11"); !ok || len(reported) != 1 {
		t.Fatal("the same broken file was reported twice")
	}
	if _, err := NewFileKeys(filepath.Join(t.TempDir(), "absent.json"), []string{"baobab-erp"}, nil); err == nil {
		t.Fatal("a missing registry must stop startup")
	}
	if _, err := NewFileKeys(path, []string{"baobab-erp"}, nil); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a broken registry must stop startup: %v", err)
	}
}
