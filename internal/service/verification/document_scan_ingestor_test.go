package verificationservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type syntheticScanAttester struct {
	proof authority.ScanProof
	calls int
	err   error
}

func (s *syntheticScanAttester) ScanAndAttest(context.Context, string, string, string, []byte) (authority.ScanProof, error) {
	s.calls++
	return s.proof, s.err
}

type syntheticScannedWriter struct{ calls int; err error }
func (s *syntheticScannedWriter) Seal(context.Context, authority.ScanProof, []byte) error {
	s.calls++
	return s.err
}

type syntheticScanRegistrar struct{ calls int; err error }
func (s *syntheticScanRegistrar) RegisterScannedArtifact(context.Context, authority.ScanProof, repository.AuditActor) error {
	s.calls++
	return s.err
}

func TestPEO03LScanPipelineCannotRegisterUnscannedOrWrongClient(t *testing.T) {
	attester := &syntheticScanAttester{proof: authority.ScanProof{
		EvidenceID: "evr_scannerdoc1", ArtifactID: "eart_scannerdoc1",
		MediaType: "application/pdf", Outcome: "CLEAN", ScannedAt: time.Now().UTC(),
	}}
	vault := &syntheticScannedWriter{}
	ledger := &syntheticScanRegistrar{}
	svc := ScannedArtifactIngestor{Attester: attester, Vault: vault, Ledger: ledger,
		AllowedClientID: "baobab-independent-scanner-staging"}
	actor := repository.AuditActor{ActorType: "workload", ActorID: "prn_scanner",
		ClientID: "baobab-independent-scanner-staging"}
	b := []byte("%PDF-1.7\nsynthetic scanned input")
	if _, err := svc.Ingest(context.Background(), attester.proof.EvidenceID,
		attester.proof.ArtifactID, "application/pdf", b, actor); err != nil {
		t.Fatalf("valid synthetic sequence denied: %v", err)
	}
	if attester.calls != 1 || vault.calls != 1 || ledger.calls != 1 {
		t.Fatalf("incorrect scan-seal-register sequence: %d %d %d", attester.calls, vault.calls, ledger.calls)
	}
	actor.ClientID = "baobab-document-extractor-staging"
	if _, err := svc.Ingest(context.Background(), attester.proof.EvidenceID,
		attester.proof.ArtifactID, "application/pdf", b, actor); !errors.Is(err, ErrScanIngestionDenied) {
		t.Fatalf("extractor impersonated independent scanner: %v", err)
	}
	if attester.calls != 1 || vault.calls != 1 || ledger.calls != 1 {
		t.Fatal("unauthorised scan actor reached scanner, custody or ledger")
	}
	actor.ClientID = svc.AllowedClientID
	attester.err = errors.New("malware scanner denied input")
	if _, err := svc.Ingest(context.Background(), attester.proof.EvidenceID,
		attester.proof.ArtifactID, "application/pdf", b, actor); !errors.Is(err, ErrScanIngestionDenied) {
		t.Fatalf("failed scan was treated as CLEAN: %v", err)
	}
	if vault.calls != 1 || ledger.calls != 1 {
		t.Fatal("unclean artifact reached custody or ledger")
	}
	attester.err = nil
	attester.proof.Outcome = "INFECTED"
	if _, err := svc.Ingest(context.Background(), attester.proof.EvidenceID,
		attester.proof.ArtifactID, "application/pdf", b, actor); err == nil {
		t.Fatal("infected proof was registered")
	}
	if vault.calls != 1 || ledger.calls != 1 {
		t.Fatal("INFECTED document was persisted")
	}
	attester.proof.Outcome = "CLEAN"
	vault.err = errors.New("mounted custody unavailable")
	if _, err := svc.Ingest(context.Background(), attester.proof.EvidenceID,
		attester.proof.ArtifactID, "application/pdf", b, actor); err == nil {
		t.Fatal("unavailable custody yielded a scan registration")
	}
	if ledger.calls != 1 {
		t.Fatal("unsealed document acquired registration")
	}
}
