package authority

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSealedVaultRefusesSharedFilesystemDirectories(t *testing.T) {
	shared := []string{string(filepath.Separator), os.TempDir()}
	for _, directory := range shared {
		if safeCustodyDirectory(directory) {
			t.Fatalf("unsafe shared directory permitted: %s", directory)
		}
		vault := SealedVault{Directory: directory, Key: bytes.Repeat([]byte{0x33}, 32)}
		if _, err := vault.Seal(context.Background(), "evr_synthetic0001", []byte("synthetic record")); !errors.Is(err, ErrCustody) {
			t.Fatalf("shared directory seal not refused: %s: %v", directory, err)
		}
	}
	loose := filepath.Join(t.TempDir(), "unsafe-permissions")
	if err := os.Mkdir(loose, 0755); err != nil {
		t.Fatal(err)
	}
	looseVault := SealedVault{Directory: loose, Key: bytes.Repeat([]byte{0x33}, 32)}
	if _, err := looseVault.Seal(context.Background(), "evr_synthetic0001", []byte("synthetic record")); err != nil {
		t.Fatalf("private custody child inside permissive mount rejected: %v", err)
	}
	child, childErr := os.Stat(filepath.Join(loose, ".baobab-source-custody"))
	if childErr != nil || child.Mode().Perm()&0077 != 0 {
		t.Fatalf("private custody leaf must be 0700: %v", childErr)
	}
	info, err := os.Stat(loose)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("custody adapter mutated permissions of unrelated directory: %v", err)
	}
	vault := SealedVault{Directory: t.TempDir(), Key: bytes.Repeat([]byte{0x33}, 32)}
	if _, err := vault.Seal(context.Background(), "evr_synthetic0001", []byte("synthetic record")); err != nil {
		t.Fatalf("dedicated vault directory unexpectedly blocked: %v", err)
	}
}
