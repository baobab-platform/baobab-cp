package authority

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrIndependentScanUnavailable = errors.New("independent malware scan unavailable or untrusted")

// MalwareVerdictSource MUST be a separately governed, independent antivirus
// service. Merely naming a document CLEAN is never acceptable evidence.
type MalwareVerdictSource interface {
	ScanDocument(context.Context, []byte) error
}

// ClamdUnixScanner speaks clamd's null-delimited INSTREAM protocol over a
// specifically provisioned local Unix socket. No applicant-controlled remote
// URL, shell command, or user-provided scan result is supported.
type ClamdUnixScanner struct {
	SocketPath string
}

func (s ClamdUnixScanner) ScanDocument(ctx context.Context, document []byte) error {
	if ctx == nil || ctx.Err() != nil || len(document) < 8 || len(document) > MaxScannedDocumentBytes ||
		!filepath.IsAbs(s.SocketPath) {
		return ErrIndependentScanUnavailable
	}
	info, err := os.Lstat(s.SocketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return ErrIndependentScanUnavailable
	}
	deadline := time.Now().Add(10 * time.Second)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", s.SocketPath)
	if err != nil {
		return ErrIndependentScanUnavailable
	}
	defer conn.Close()
	if err = conn.SetDeadline(deadline); err != nil {
		return ErrIndependentScanUnavailable
	}
	if err = writeScanChunk(conn, []byte("zINSTREAM\x00")); err != nil {
		return ErrIndependentScanUnavailable
	}
	// Stream fixed 32 KiB frames and an explicit final zero-length frame,
	// never an unbounded file or a size claimed by a caller.
	for position := 0; position < len(document); {
		end := position + 32*1024
		if end > len(document) {
			end = len(document)
		}
		var frame [4]byte
		binary.BigEndian.PutUint32(frame[:], uint32(end-position))
		if err = writeScanChunk(conn, frame[:]); err != nil {
			return ErrIndependentScanUnavailable
		}
		if err = writeScanChunk(conn, document[position:end]); err != nil {
			return ErrIndependentScanUnavailable
		}
		position = end
	}
	if err = writeScanChunk(conn, []byte{0, 0, 0, 0}); err != nil {
		return ErrIndependentScanUnavailable
	}
	reply, err := io.ReadAll(io.LimitReader(conn, 257))
	if err != nil || len(reply) == 0 || len(reply) > 256 {
		return ErrIndependentScanUnavailable
	}
	// The only accepted reply is an exact CLEAN result. FOUND, ERROR,
	// incomplete or unknown responses MUST NOT produce a signed proof.
	if !bytes.Equal(reply, []byte("stream: OK\x00")) {
		return ErrIndependentScanUnavailable
	}
	return nil
}

func writeScanChunk(conn net.Conn, payload []byte) error {
	for len(payload) > 0 {
		n, err := conn.Write(payload)
		if err != nil || n <= 0 {
			return ErrIndependentScanUnavailable
		}
		payload = payload[n:]
	}
	return nil
}

// MountedScanSigningKey is accessible only in the *independent scanner*
// runtime. The CP/extractor runtime must receive the corresponding public
// trust anchor, never this signing key.
type MountedScanSigningKey struct {
	PrivateKeyPath string
}

func (m MountedScanSigningKey) Sign(payload []byte) ([]byte, error) {
	info, err := os.Lstat(m.PrivateKeyPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0077 != 0 || info.Size() < 64 || info.Size() > 256 {
		return nil, ErrIndependentScanUnavailable
	}
	encoded, err := os.ReadFile(m.PrivateKeyPath)
	if err != nil {
		return nil, ErrIndependentScanUnavailable
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(secret) != ed25519.PrivateKeySize {
		return nil, ErrIndependentScanUnavailable
	}
	return ed25519.Sign(ed25519.PrivateKey(secret), payload), nil
}

type ScanProofSigner interface {
	Sign([]byte) ([]byte, error)
}

// IndependentDocumentAttester may be composed ONLY in the scanner's isolated
// workload, after independent scanner certification and mounted signing key
// approval. The DocumentLedger then independently verifies its public key.
type IndependentDocumentAttester struct {
	Scanner        MalwareVerdictSource
	Signer         ScanProofSigner
	ScannerID      string
	ScannerVersion string
	Now            func() time.Time
}

func (a IndependentDocumentAttester) ScanAndAttest(ctx context.Context, evidenceID, artifactID, mediaType string, document []byte) (ScanProof, error) {
	empty := ScanProof{}
	if ctx == nil || ctx.Err() != nil || a.Scanner == nil || a.Signer == nil || a.Now == nil ||
		len(a.ScannerID) < 3 || len(a.ScannerID) > 96 ||
		len(a.ScannerVersion) < 1 || len(a.ScannerVersion) > 96 ||
		len(document) < 8 || len(document) > MaxScannedDocumentBytes ||
		!validDocumentSignature(mediaType, document) {
		return empty, ErrIndependentScanUnavailable
	}
	// Sign only after an actual clean result. A timeout, FOUND or malformed
	// clamd reply is never treated as a non-applicable scan or clean status.
	if err := a.Scanner.ScanDocument(ctx, document); err != nil || ctx.Err() != nil {
		return empty, ErrIndependentScanUnavailable
	}
	sum := sha256.Sum256(document)
	proof := ScanProof{
		EvidenceID: evidenceID, ArtifactID: artifactID, MediaType: mediaType,
		Digest:    "sha256:" + hex.EncodeToString(sum[:]),
		ScannerID: a.ScannerID, ScannerVersion: a.ScannerVersion, Outcome: "CLEAN",
		ScannedAt: a.Now().UTC().Truncate(time.Microsecond),
	}
	signature, err := a.Signer.Sign(proof.SigningPayload())
	if err != nil || len(signature) != ed25519.SignatureSize {
		return empty, ErrIndependentScanUnavailable
	}
	proof.Signature = signature
	return proof, nil
}
