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
	vault := SealedVault{Directory: t.TempDir(), Key: bytes.Repeat([]byte{0x33}, 32)}
	if _, err := vault.Seal(context.Background(), "evr_synthetic0001", []byte("synthetic record")); err != nil {
		t.Fatalf("dedicated vault directory unexpectedly blocked: %v", err)
	}
}
