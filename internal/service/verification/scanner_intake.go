package verificationservice

import (
	"context"
	"crypto/ed25519"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

// ScannerKeyReader must read current independently approved public trust,
// never a public key supplied in the document request. It is invoked again
// before commit so removing trust denies even an in-flight custody operation.
type ScannerKeyReader interface {
	ScannerKey(context.Context, string, string) (ed25519.PublicKey, error)
}

type ScannedArtifactIntake struct {
	Repository *repository.PostgresRepository
	Vault      SealedScannedArtifactWriter
	Trust      ScannerKeyReader
	Now        func() time.Time
}

// Receive is the CP side of the split runtime. CP receives independently
// signed bytes; it has no scanner, CLEAN verdict generator or private signer.
// current rechecks this request's IAM token and exact workload registry.
func (s ScannedArtifactIntake) Receive(ctx context.Context, in authority.ScanSubmission, actor repository.AuditActor, current func(context.Context) error) (authority.ScanReceipt, error) {
	var empty authority.ScanReceipt
	if ctx == nil || ctx.Err() != nil || s.Repository == nil || s.Vault == nil || s.Trust == nil || s.Now == nil ||
		current == nil || actor.ActorID == "" || actor.TokenID == "" || actor.ActorType != "workload" ||
		actor.ClientID != authority.ScannerClientID || authority.ValidateScanSubmission(in) != nil {
		return empty, authority.ErrScannerIntake
	}
	check := func(c context.Context) error {
		if current(c) != nil {
			return authority.ErrScannerIntake
		}
		key, err := s.Trust.ScannerKey(c, in.Proof.ScannerID, in.Proof.ScannerVersion)
		if err != nil || authority.VerifyScanProof(in.Proof, key, s.Now()) != nil {
			return authority.ErrScannerIntake
		}
		return nil
	}
	if check(ctx) != nil {
		return empty, authority.ErrScannerIntake
	}
	key, err := s.Trust.ScannerKey(ctx, in.Proof.ScannerID, in.Proof.ScannerVersion)
	if err != nil {
		return empty, authority.ErrScannerIntake
	}
	ledger := repository.DocumentLedger{PostgresRepository: s.Repository, ScannerClientID: authority.ScannerClientID,
		ScannerKeys: map[string]ed25519.PublicKey{in.Proof.ScannerID + "/" + in.Proof.ScannerVersion: key}, Now: s.Now}
	err = ledger.WithScanAdmission(ctx, in.CaseID, in.ClaimID, in.Proof.EvidenceID, in.Proof.ArtifactID, actor, func(admitted context.Context) error {
		if check(admitted) != nil {
			return authority.ErrScannerIntake
		}
		if err := s.Vault.Seal(admitted, in.Proof, in.Document); err != nil {
			return authority.ErrScannerIntake
		}
		// Use the SAME admission transaction for proof/audit registration.
		if err := ledger.RegisterScannedArtifact(admitted, in.Proof, actor); err != nil {
			return authority.ErrScannerIntake
		}
		return check(admitted)
	})
	if err != nil {
		return empty, authority.ErrScannerIntake
	}
	return authority.ScanReceipt{CaseID: in.CaseID, ClaimID: in.ClaimID, EvidenceID: in.Proof.EvidenceID,
		ArtifactID: in.Proof.ArtifactID, Digest: in.Proof.Digest, Standing: "SCAN_PROOF_RECORDED"}, nil
}
