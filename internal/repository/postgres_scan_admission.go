package repository

import (
	"context"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/jackc/pgx/v5"
)

// The lease is deliberately private: neither a bearer scope nor an exported
// Evidence snapshot permits custody writes. Only this ledger can establish
// the current, row-locked case -> claim -> evidence relationship.
type scanAdmissionKey struct{}
type scanAdmissionLease struct {
	actor                         AuditActor
	tx                            pgx.Tx
	repository                    *PostgresRepository
	caseID, claimID, evidenceID   string
	artifactID, actorID, clientID string
	active, registered            bool
}

// WithScanAdmission holds canonical OEV-03 locks during scan, seal and proof
// registration. RegisterScannedArtifact MUST reuse this transaction rather
// than opening another connection (which would deadlock on these locks).
// The bounded callback must never promote evidence status or verify a claim.
func (d DocumentLedger) WithScanAdmission(ctx context.Context, caseID, claimID, evidenceID, artifactID string, actor AuditActor, run func(context.Context) error) error {
	if ctx == nil || ctx.Err() != nil || d.PostgresRepository == nil || d.Now == nil ||
		d.ScannerClientID == "" || actor.ActorType != "workload" || actor.ActorID == "" ||
		actor.ClientID != d.ScannerClientID || caseID == "" || claimID == "" ||
		evidenceID == "" || artifactID == "" || run == nil {
		return ErrDocumentLedgerDenied
	}
	limited, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := d.pool.Begin(limited)
	if err != nil {
		return ErrDocumentLedgerDenied
	}
	defer tx.Rollback(context.Background())
	if lockCustody(limited, tx, evidenceID, artifactID) != nil {
		return ErrDocumentLedgerDenied
	}
	lease := &scanAdmissionLease{tx: tx, repository: d.PostgresRepository,
		caseID: caseID, claimID: claimID, evidenceID: evidenceID, artifactID: artifactID, actor: actor,
		actorID: actor.ActorID, clientID: actor.ClientID, active: true}
	// A retained callback context is unusable after return, even if its
	// transaction object remains reachable. It is not a renewable permit.
	defer func() { lease.active = false }()
	if err = validateScanAdmission(limited, tx, lease, d.Now().UTC()); err != nil {
		return ErrDocumentLedgerDenied
	}
	if err = run(context.WithValue(limited, scanAdmissionKey{}, lease)); err != nil ||
		limited.Err() != nil || !lease.registered {
		return ErrDocumentLedgerDenied
	}
	// Locks prevent lifecycle mutation, but the clock can cross expiry while
	// antivirus/custody runs. Recheck before committing the proof and audit.
	if validateScanAdmission(limited, tx, lease, d.Now().UTC()) != nil || tx.Commit(limited) != nil {
		return ErrDocumentLedgerDenied
	}
	return nil
}

func validateScanAdmission(ctx context.Context, tx pgx.Tx, lease *scanAdmissionLease, now time.Time) error {
	c, err := lockCase(ctx, tx, lease.caseID, -1)
	if err != nil || c.CaseID != lease.caseID || c.Status != verification.CaseVerifying ||
		c.Subject.SubjectID == "" || c.Subject.SubjectType == "" || c.Purpose == "" {
		return ErrDocumentLedgerDenied
	}
	cl, err := lockClaim(ctx, tx, lease.caseID, lease.claimID)
	if err != nil || cl.ClaimID != lease.claimID || cl.Status != verification.ClaimUnderCheck ||
		cl.Subject != c.Subject || cl.Purpose != c.Purpose || cl.AssertedBy == lease.actorID {
		return ErrDocumentLedgerDenied
	}
	linked := false
	for _, id := range cl.EvidenceIDs {
		linked = linked || id == lease.evidenceID
	}
	if !linked {
		return ErrDocumentLedgerDenied
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT document FROM evidence.record WHERE evidence_id=$1 FOR SHARE`, lease.evidenceID).Scan(&raw); err != nil {
		return ErrDocumentLedgerDenied
	}
	e, err := decodeDocument[verification.Evidence](raw)
	if err != nil || e.EvidenceID != lease.evidenceID || e.ArtifactID != lease.artifactID ||
		e.Subject != c.Subject || e.Purpose != c.Purpose || e.EvidenceType != "DOCUMENT" ||
		(e.Status != verification.EvidenceAvail && e.Status != "QUARANTINED") ||
		e.ExpiresAt != nil && !e.ExpiresAt.After(now) {
		return ErrDocumentLedgerDenied
	}
	return nil
}
