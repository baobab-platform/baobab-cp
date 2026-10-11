package authority

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVersionedSourceCustodyPreservesChangedOfficialResponses(t *testing.T) {
	v := SealedVault{Directory: t.TempDir(), Key: bytes.Repeat([]byte{0x42}, 32)}
	ctx := context.Background()
	evidence := "evr_validdocument01"
	fingerprint := func(value string) string {
		d := sha256.Sum256([]byte(value))
		return "sha256:" + hex.EncodeToString(d[:])
	}
	first := []byte(`{"reg":"2020/123456/07","status":"ACTIVE"}`)
	changed := []byte(`{"reg":"2020/123456/07","status":"IN_BUSINESS_RESCUE"}`)
	a, err := v.SealVersioned(ctx, evidence, fingerprint("attempt-1"), first)
	if err != nil {
		t.Fatalf("first response: %v", err)
	}
	b, err := v.SealVersioned(ctx, evidence, fingerprint("attempt-2"), changed)
	if err != nil || a == b {
		t.Fatalf("changed response should have independent immutable custody; err=%v", err)
	}
	replay, err := v.SealVersioned(ctx, evidence, fingerprint("attempt-1"), first)
	if err != nil || replay != a {
		t.Fatalf("identical replay must retain the first custody address; err=%v", err)
	}
	if _, err := v.SealVersioned(ctx, evidence, fingerprint("attempt-1"), changed); err == nil {
		t.Fatal("same attempt identity overwrote historical response")
	}
	aPlain, err := v.OpenVersioned(ctx, a)
	if err != nil || !bytes.Equal(aPlain, first) {
		t.Fatalf("first response lost: %v", err)
	}
	bPlain, err := v.OpenVersioned(ctx, b)
	if err != nil || !bytes.Equal(bPlain, changed) {
		t.Fatalf("changed response lost: %v", err)
	}
	if _, err := v.OpenVersioned(ctx, "sealed-v2:../unsafe"); err == nil {
		t.Fatal("untrusted custody reference accepted")
	}
	if _, err := v.SealVersioned(ctx, evidence, "not-a-digest", changed); err == nil {
		t.Fatal("unbound attempt identifier accepted")
	}
}
