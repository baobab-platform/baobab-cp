package repository

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestCX05ArtifactEncryptionRoundtripNoPlaintext(t *testing.T) {
	r := &PostgresRepository{}
	plain := []byte("organisation_id,display_name\n0,=FAKE()\n")
	if _, err := r.encryptConsoleDataset("a", plain); err == nil {
		t.Fatal("missing key accepted")
	}
	key := bytes.Repeat([]byte{0x34}, 32)
	if err := r.SetConsoleDatasetKey(key); err != nil {
		t.Fatal(err)
	}
	blob, err := r.encryptConsoleDataset("plan-digest-A", plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, plain) || !bytes.HasPrefix(blob, []byte(consoleCipherPrefix)) {
		t.Fatal("plaintext or invalid envelope")
	}
	got, err := r.decryptConsoleDataset("plan-digest-A", blob)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("roundtrip", err)
	}
	if _, err := r.decryptConsoleDataset("different-digest", blob); err == nil {
		t.Fatal("ciphertext swapped across plans")
	}
	blob[len(blob)-1] ^= 1
	if _, err := r.decryptConsoleDataset("plan-digest-A", blob); err == nil {
		t.Fatal("tamper accepted")
	}
}

func TestCX05ExpiredArtifactPurge(t *testing.T) {
	f := newOrgFixture(t)
	// Same schema as real durable operations; no plaintext payload.
	at := time.Now().UTC()
	expired := "a1e3" + string(bytes.Repeat([]byte("e"), 60))
	unexpired := "b1e3" + string(bytes.Repeat([]byte("f"), 60))
	for _, item := range []struct {
		digest  string
		expires time.Time
	}{{expired, at.Add(-time.Hour)}, {unexpired, at.Add(time.Hour)}} {
		_, err := f.admin.Exec(context.Background(), `INSERT INTO operations.console_dataset_plan
         (digest,kind,maker,idempotency_key,intent,expires_at,state,artifact)
         VALUES($1,'REDACTED_CSV_EXPORT',$1,$1,'{}'::jsonb,$2,'PENDING_APPROVAL',$3)`,
			item.digest, item.expires, append([]byte(consoleCipherPrefix), bytes.Repeat([]byte{0x5a}, 48)...))
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		f.admin.Exec(context.Background(), `DELETE FROM operations.console_dataset_plan WHERE digest=ANY($1)`, []string{expired, unexpired})
	})
	removed, err := f.repo.PurgeExpiredConsoleDatasetArtifacts(context.Background(), at, 10)
	if err != nil || removed < 1 {
		t.Fatalf("purge %d %v", removed, err)
	}
	var oldBytes, newBytes []byte
	if err := f.admin.QueryRow(context.Background(), `SELECT artifact FROM operations.console_dataset_plan WHERE digest=$1`, expired).Scan(&oldBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), `SELECT artifact FROM operations.console_dataset_plan WHERE digest=$1`, unexpired).Scan(&newBytes); err != nil {
		t.Fatal(err)
	}
	if len(oldBytes) != 0 || len(newBytes) == 0 {
		t.Fatal("expiry boundaries ignored")
	}
}
