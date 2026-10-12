package scannerruntime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

func TestIsolatedScannerExactReplayAndRedactedHTTPSReceipt(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte("%PDF-1.7\nsynthetic private document")
	attester := authority.IndependentDocumentAttester{Scanner: fixtureClean{}, Signer: fixtureSigner{key: key}, ScannerID: "synthetic-av", ScannerVersion: "v1", Now: time.Now}
	proof, err := attester.ScanAndAttest(context.Background(), "evr_synthetic", "eart_synthetic", "application/pdf", doc)
	if err != nil {
		t.Fatal(err)
	}
	job := signedJob{CaseID: "vcase_synthetic", ClaimID: "ecl_synthetic", Proof: proof}
	jobRaw, _ := json.Marshal(job)
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != authority.ScannerIntakePath || r.Header.Get("Authorization") != "Bearer synthetic-workload-token" {
			t.Error("wrong target/token")
		}
		var input authority.ScanSubmission
		if json.NewDecoder(r.Body).Decode(&input) != nil || string(input.Document) != string(doc) || hex.EncodeToString(input.Proof.Signature) != hex.EncodeToString(proof.Signature) {
			t.Error("exact signed replay changed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(authority.ScanReceipt{CaseID: job.CaseID, ClaimID: job.ClaimID, EvidenceID: proof.EvidenceID, ArtifactID: proof.ArtifactID, Digest: proof.Digest, Standing: "SCAN_PROOF_RECORDED"})
	}))
	defer server.Close()
	cfg := ScannerConfig{Environment: "integration", CPBaseURL: server.URL, TokenFile: privateFixture(t, "token", []byte("synthetic-workload-token")),
		SigningKeyFile: privateFixture(t, "signer", []byte(hex.EncodeToString(key))), ClamdSocket: filepath.Join(t.TempDir(), "unused-clamd.sock"),
		ScannerID: proof.ScannerID, ScannerVersion: proof.ScannerVersion, CaseID: job.CaseID, ClaimID: job.ClaimID, EvidenceID: proof.EvidenceID, ArtifactID: proof.ArtifactID,
		MediaType: proof.MediaType, DocumentFile: privateFixture(t, "document", doc), SignedJobFile: privateFixture(t, "signed-job", jobRaw)}
	encoded, _ := json.Marshal(cfg)
	config := privateFixture(t, "operator.json", encoded)
	for i := 0; i < 2; i++ {
		receipt, err := RunScanner(context.Background(), config, server.Client())
		if err != nil || receipt.AuthorityGranted || receipt.Standing != "SCAN_PROOF_RECORDED" {
			t.Fatalf("isolated replay denied: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("expected explicit replay, got %d requests", calls)
	}
	before := calls
	if err = os.WriteFile(cfg.DocumentFile, []byte("%PDF-1.7\nchanged private document"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = RunScanner(context.Background(), config, server.Client()); err == nil || calls != before {
		t.Fatal("changed bytes replay reached network")
	}
}

type fixtureClean struct{}

func (fixtureClean) ScanDocument(context.Context, []byte) error { return nil }

type fixtureSigner struct{ key ed25519.PrivateKey }

func (s fixtureSigner) Sign(raw []byte) ([]byte, error) { return ed25519.Sign(s.key, raw), nil }

func TestMountedTrustIndependentApprovalAndRevocation(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := TrustRecord{ScannerID: "synthetic-av", ScannerVersion: "v1", PublicKeyHex: hex.EncodeToString(public), ProposedBy: "synthetic-maker", ApprovedBy: "synthetic-checker", ApprovalReference: "synthetic-approval", EffectiveFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	write := func(r TrustRecord) []byte {
		raw, _ := json.Marshal(map[string]any{"keys": []TrustRecord{r}})
		return raw
	}
	path := privateFixture(t, "trust.json", write(record))
	trust := MountedScannerTrust{Path: path, Now: func() time.Time { return now }}
	if _, err = trust.ScannerKey(context.Background(), record.ScannerID, record.ScannerVersion); err != nil {
		t.Fatal(err)
	}
	bad := record
	bad.ApprovedBy = bad.ProposedBy
	if err = os.WriteFile(path, write(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = trust.ScannerKey(context.Background(), record.ScannerID, record.ScannerVersion); err == nil {
		t.Fatal("self-approved scanner key admitted")
	}
	if err = os.WriteFile(path, []byte(`{"keys":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = trust.ScannerKey(context.Background(), record.ScannerID, record.ScannerVersion); err == nil {
		t.Fatal("removed scanner trust stayed cached")
	}
	link := filepath.Join(t.TempDir(), "symlink")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = privateFile(link, 32768); err == nil {
		t.Fatal("symlink secret/trust accepted")
	}
}

// This test exercises the actual independent executable composition with the
// Unix clamd protocol and TLS intake. The AV process is synthetic; no real
// malware-provider certification or shared workload activation is inferred.
func TestIsolatedScannerUnixScanThenExactDurableReplay(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "clamd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		command := make([]byte, 10)
		if _, err = io.ReadFull(c, command); err != nil {
			done <- err
			return
		}
		if string(command) != "zINSTREAM\x00" {
			done <- errors.New("incorrect clamd command")
			return
		}
		for {
			var length [4]byte
			if _, err = io.ReadFull(c, length[:]); err != nil {
				done <- err
				return
			}
			n := binary.BigEndian.Uint32(length[:])
			if n == 0 {
				break
			}
			if n > 32768 {
				done <- errors.New("unbounded clamd frame")
				return
			}
			if _, err = io.CopyN(io.Discard, c, int64(n)); err != nil {
				done <- err
				return
			}
		}
		_, err = c.Write([]byte("stream: OK\x00"))
		done <- err
	}()
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var in authority.ScanSubmission
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Error("bad scanner wire")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(authority.ScanReceipt{CaseID: in.CaseID, ClaimID: in.ClaimID, EvidenceID: in.Proof.EvidenceID, ArtifactID: in.Proof.ArtifactID, Digest: in.Proof.Digest, Standing: "SCAN_PROOF_RECORDED"})
	}))
	defer server.Close()
	cfg := ScannerConfig{Environment: "integration", CPBaseURL: server.URL, TokenFile: privateFixture(t, "token", []byte("synthetic-workload-token")), SigningKeyFile: privateFixture(t, "signer", []byte(hex.EncodeToString(key))), ClamdSocket: socket,
		ScannerID: "synthetic-av", ScannerVersion: "v1", CaseID: "vcase_synthetic", ClaimID: "ecl_synthetic", EvidenceID: "evr_synthetic", ArtifactID: "eart_synthetic", MediaType: "application/pdf", DocumentFile: privateFixture(t, "input", []byte("%PDF-1.7\nsynthetic")), SignedJobFile: filepath.Join(dir, "signed-job.json")}
	raw, _ := json.Marshal(cfg)
	config := privateFixture(t, "config", raw)
	if _, err = RunScanner(context.Background(), config, server.Client()); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(cfg.SignedJobFile)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close() // replay must not need the AV runtime or a new signature.
	if _, err = RunScanner(context.Background(), config, server.Client()); err != nil {
		t.Fatal(err)
	}
	replayed, err := os.ReadFile(cfg.SignedJobFile)
	if err != nil || string(saved) != string(replayed) || requests != 2 {
		t.Fatal("durable signed job changed or transport retried implicitly")
	}
}

type configJWTFixture struct{}

func (configJWTFixture) Verify(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, nil
}

func TestCPCompositionRequiresCurrentIndependentConfigurationAndNoSigner(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	trust, _ := json.Marshal(map[string]any{"keys": []TrustRecord{{ScannerID: "synthetic-av", ScannerVersion: "v1", PublicKeyHex: hex.EncodeToString(pub), ProposedBy: "synthetic-maker", ApprovedBy: "synthetic-checker", ApprovalReference: "synthetic-approval", EffectiveFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}})
	cfg := CPConfig{TrustFile: privateFixture(t, "trust", trust), CustodyDirectory: t.TempDir(), CustodyKeyFile: privateFixture(t, "custody", make([]byte, 32)), IntrospectionURL: "https://synthetic.example.invalid/introspect", IntrospectionClientIDFile: privateFixture(t, "id", []byte("synthetic-id")), IntrospectionClientSecretFile: privateFixture(t, "credential", []byte("synthetic-credential"))}
	raw, _ := json.Marshal(cfg)
	path := privateFixture(t, "cp-config", raw)
	registry := privateFixture(t, "registry", []byte("workloads:\n  "+authority.ScannerClientID+":\n    status: ACTIVE\n    environment: integration\n    credential_type: federated_workload_token\n    allowed_audiences: [baobab-control-plane]\n    allowed_scopes: [verification:scan-attest]\n"))
	repo := &repository.PostgresRepository{}
	if _, _, err = ComposeCP(context.Background(), "integration", path, registry, configJWTFixture{}, repo); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"", "production", "unknown"} {
		if _, _, err = ComposeCP(context.Background(), environment, path, registry, configJWTFixture{}, repo); err == nil {
			t.Fatalf("%s runtime admitted", environment)
		}
	}
	injected := append([]byte(`{"signing_key_file":"/must-not-mount",`), raw[1:]...)
	if err = os.WriteFile(path, injected, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ComposeCP(context.Background(), "integration", path, registry, configJWTFixture{}, repo); err == nil {
		t.Fatal("CP accepted scanner private-key configuration")
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(cfg.CustodyKeyFile, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ComposeCP(context.Background(), "integration", path, registry, configJWTFixture{}, repo); err == nil {
		t.Fatal("world-readable custody key admitted")
	}
}
