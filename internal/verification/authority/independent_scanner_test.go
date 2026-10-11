package authority

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func syntheticClamd(t *testing.T, reply []byte, expected []byte) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "clamd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		command := make([]byte, len("zINSTREAM\x00"))
		if _, readErr := io.ReadFull(conn, command); readErr != nil || string(command) != "zINSTREAM\x00" {
			return
		}
		var contents []byte
		for {
			var header [4]byte
			if _, readErr := io.ReadFull(conn, header[:]); readErr != nil {
				return
			}
			n := binary.BigEndian.Uint32(header[:])
			if n == 0 {
				break
			}
			if n > 32*1024 {
				return
			}
			buf := make([]byte, n)
			if _, readErr := io.ReadFull(conn, buf); readErr != nil {
				return
			}
			contents = append(contents, buf...)
		}
		if !bytes.Equal(contents, expected) {
			return
		}
		_, _ = conn.Write(reply)
	}()
	return socket
}

func TestPEO03LIndependentClamdScanAndMountedSignature(t *testing.T) {
	document := []byte("%PDF-1.7\nsynthetic non-authorising scanner document")
	keyPublic, keyPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "scanner-only-ed25519.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(keyPrivate)), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attester := IndependentDocumentAttester{
		Scanner: ClamdUnixScanner{SocketPath: syntheticClamd(t, []byte("stream: OK\x00"), document)},
		Signer: MountedScanSigningKey{PrivateKeyPath: keyFile},
		ScannerID: "synthetic-clamd-independent", ScannerVersion: "clamav-fixture-v1",
		Now: func() time.Time { return now },
	}
	proof, err := attester.ScanAndAttest(context.Background(), "evr_scanner123", "eart_scanner123", "application/pdf", document)
	if err != nil || proof.Outcome != "CLEAN" ||
		VerifyScanProof(proof, keyPublic, now.Add(time.Second)) != nil {
		t.Fatalf("independent clean scan or proof invalid: %+v error=%v", proof, err)
	}
	if proof.ScannedAt.UnixNano()%1000 != 0 {
		t.Fatal("proof signature will not survive PostgreSQL timestamp precision")
	}
	wrongKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyScanProof(proof, wrongKey, now.Add(time.Second)) == nil {
		t.Fatal("untrusted scanner accepted")
	}
	proof.ScannerVersion = "forged-scanner-v2"
	if VerifyScanProof(proof, keyPublic, now.Add(time.Second)) == nil {
		t.Fatal("scanner version was not covered by signature")
	}
}

func TestPEO03LNoSignatureForInfectedOrUnavailableScan(t *testing.T) {
	document := []byte("%PDF-1.7\nsynthetic scanner negative document")
	_, keyPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scanner.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(keyPrivate)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		reply []byte
	}{
		{"infected", []byte("stream: Eicar-Test-Signature FOUND\x00")},
		{"scanner error", []byte("stream: scan failed ERROR\x00")},
		{"no scan response", nil},
		{"malformed clean", []byte("stream: OK\n")},
	}{
		t.Run(tc.name, func(t *testing.T) {
			attester := IndependentDocumentAttester{
				Scanner: ClamdUnixScanner{SocketPath: syntheticClamd(t, tc.reply, document)},
				Signer: MountedScanSigningKey{PrivateKeyPath: path},
				ScannerID: "synthetic-independent", ScannerVersion: "v1",
				Now: time.Now,
			}
			if _, err := attester.ScanAndAttest(context.Background(), "evr_scanner123", "eart_scanner123", "application/pdf", document);
				!errors.Is(err, ErrIndependentScanUnavailable) {
				t.Fatalf("untrusted scan produced signing proof: %v", err)
			}
		})
	}
}

func TestPEO03LRejectWorldReadableOrSymlinkSigner(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scanner.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(private)), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := (MountedScanSigningKey{PrivateKeyPath: path}).Sign([]byte("test")); err == nil {
		t.Fatal("world-readable signing key accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (MountedScanSigningKey{PrivateKeyPath: path}).Sign([]byte("test")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "symlink.key")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (MountedScanSigningKey{PrivateKeyPath: link}).Sign([]byte("test")); err == nil {
		t.Fatal("symlink signing key accepted")
	}
}
