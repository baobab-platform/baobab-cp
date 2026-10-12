package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/documentcustody"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/jackc/pgx/v5"
)

// CustodyApproval is loaded from an independently controlled private job
// mount, not from HTTP input or CLI flags. It binds the reviewed snapshot and
// expires. AWS/SQL role provisioning must separate intake and maintenance.
type CustodyApproval struct {
	ExpectedRevision  int64     `json:"expected_revision"`
	Operation         string    `json:"operation"`
	EvidenceID        string    `json:"evidence_id"`
	ArtifactID        string    `json:"artifact_id"`
	ProofHash         string    `json:"proof_hash"`
	ExpectedState     string    `json:"expected_state"`
	ExpectedVersion   string    `json:"expected_version"`
	TargetKey         string    `json:"target_key,omitempty"`
	ProposedBy        string    `json:"proposed_by"`
	ApprovedBy        string    `json:"approved_by"`
	ApprovalReference string    `json:"approval_reference"`
	EffectiveFrom     time.Time `json:"effective_from"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func (a CustodyApproval) Valid(now time.Time) bool {
	if a.EvidenceID == "" || a.ArtifactID == "" || len(a.ProofHash) != 64 || a.ExpectedState == "" || a.ExpectedRevision < 1 || a.ProposedBy == "" || a.ApprovedBy == "" || a.ProposedBy == a.ApprovedBy || a.ApprovalReference == "" || a.EffectiveFrom.IsZero() || a.EffectiveFrom.After(now) || !a.ExpiresAt.After(now) {
		return false
	}
	switch a.Operation {
	case "RECONCILE", "HOLD", "RELEASE_HOLD", "DELETE":
		return a.TargetKey == ""
	case "ROTATE":
		return a.TargetKey != ""
	default:
		return false
	}
}

// CustodyReport contains lawful operational selectors only. Never expose
// ciphertext, key envelopes, document bytes, OCR text or cloud object URLs.
type CustodyReport struct {
	Revision      int64     `json:"revision"`
	KeyID         string    `json:"key_id,omitempty"`
	EvidenceID    string    `json:"evidence_id"`
	ArtifactID    string    `json:"artifact_id"`
	ProofHash     string    `json:"proof_hash"`
	State         string    `json:"state"`
	VersionID     string    `json:"version_id"`
	PolicyID      string    `json:"policy_id"`
	PolicyVersion string    `json:"policy_version"`
	RetainUntil   time.Time `json:"retain_until"`
	DeleteAfter   time.Time `json:"delete_after"`
	OrphanAfter   time.Time `json:"orphan_after"`
	LegalHold     bool      `json:"legal_hold"`
}

func reportCustody(o authority.CustodyObject) CustodyReport {
	return CustodyReport{EvidenceID: o.EvidenceID, ArtifactID: o.ArtifactID, ProofHash: o.ProofHash, State: o.State, VersionID: o.VersionID, PolicyID: o.PolicyID, PolicyVersion: o.PolicyVersion, RetainUntil: o.RetainUntil, DeleteAfter: o.DeleteAfter, OrphanAfter: o.OrphanAfter, LegalHold: o.LegalHold, KeyID: o.KeyID, Revision: o.Revision}
}

// Reports paginate by an exact composite cursor; a scheduled read-only job
// can inspect every row without permitting bulk destructive actions.
func (d DurableDocumentVault) Reports(ctx context.Context, afterEvidence, afterArtifact string, limit int) ([]CustodyReport, error) {
	if !d.ready(ctx) || limit < 1 || limit > 100 {
		return nil, authority.ErrDocumentCustody
	}
	rows, err := d.Repository.pool.Query(ctx, `SELECT evidence_id,artifact_id,proof_hash,state,version_id,policy_id,policy_version,retain_until,delete_after,orphan_after,legal_hold,key_id,revision FROM evidence.document_custody WHERE (evidence_id,artifact_id)>($1,$2) ORDER BY evidence_id,artifact_id LIMIT $3`, afterEvidence, afterArtifact, limit)
	if err != nil {
		return nil, authority.ErrDocumentCustody
	}
	defer rows.Close()
	out := []CustodyReport{}
	for rows.Next() {
		var r CustodyReport
		if rows.Scan(&r.EvidenceID, &r.ArtifactID, &r.ProofHash, &r.State, &r.VersionID, &r.PolicyID, &r.PolicyVersion, &r.RetainUntil, &r.DeleteAfter, &r.OrphanAfter, &r.LegalHold, &r.KeyID, &r.Revision) != nil {
			return nil, authority.ErrDocumentCustody
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d DurableDocumentVault) Maintain(ctx context.Context, a CustodyApproval, actor AuditActor) (CustodyReport, error) {
	var empty CustodyReport
	if !d.ready(ctx) || !a.Valid(d.Now()) || actor.ActorID == "" || actor.ActorType != "workload" || actor.ClientID != "baobab-document-custody-maintenance" || actor.CorrelationID == "" {
		return empty, authority.ErrDocumentCustody
	}
	limited, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := d.Repository.pool.Begin(limited)
	if err != nil {
		return empty, authority.ErrDocumentCustody
	}
	defer tx.Rollback(context.Background())
	if lockCustody(limited, tx, a.EvidenceID, a.ArtifactID) != nil {
		return empty, authority.ErrDocumentCustody
	}
	// No case/claim lock is acquired here: maintenance needs no verification
	// authority. Taking evidence before custody preserves the reader's order.
	var raw []byte
	er := tx.QueryRow(limited, `SELECT document FROM evidence.record WHERE evidence_id=$1 FOR UPDATE`, a.EvidenceID).Scan(&raw)
	var e verification.Evidence
	if er == nil {
		e, er = decodeDocument[verification.Evidence](raw)
	}
	if er != nil && !errors.Is(er, pgx.ErrNoRows) {
		return empty, authority.ErrDocumentCustody
	}
	o, err := scanCustody(tx.QueryRow(limited, custodySelect+` FOR UPDATE`, a.EvidenceID, a.ArtifactID))
	if err != nil || a.ProofHash != o.ProofHash || a.ExpectedState != o.State || a.ExpectedVersion != o.VersionID || a.ExpectedRevision != o.Revision || o.State == "DELETED" {
		return empty, authority.ErrDocumentCustody
	}
	policy, err := d.Policies.CustodyPolicy(limited, o.PolicyID, o.PolicyVersion)
	if err != nil || !policy.Valid() || policy.ID != o.PolicyID || policy.Version != o.PolicyVersion || o.PolicyHash != policyDigest(policy) || er == nil && e.RetentionPolicy != o.PolicyID {
		return empty, authority.ErrDocumentCustody
	}
	observed, statErr := d.Backend.Stat(limited, o)
	if statErr != nil && !errors.Is(statErr, documentcustody.ErrAbsent) {
		return empty, authority.ErrDocumentCustody
	}
	if statErr == nil {
		if o.VersionID != "" && observed.VersionID != o.VersionID || observed.ProofHash != o.ProofHash || observed.CipherDigest != o.CipherDigest || observed.RetainUntil.Before(o.RetainUntil) {
			return empty, authority.ErrDocumentCustody
		}
		o.VersionID = observed.VersionID
	}
	sourceKey := o.KeyID
	now := d.Now().UTC()
	if !a.Valid(now) {
		return empty, authority.ErrDocumentCustody
	}
	switch a.Operation {
	case "RECONCILE":
		if o.State == "DELETING" {
			break
		}
		if o.State != "PENDING" || o.OrphanAfter.After(now) {
			return empty, authority.ErrDocumentCustody
		}
		var p authority.ScanProof
		pe := tx.QueryRow(limited, `SELECT evidence_id,artifact_id,media_type,document_digest,scanner_id,scanner_version,scan_outcome,scanned_at,signature FROM evidence.scanned_artifact WHERE evidence_id=$1 AND artifact_id=$2`, o.EvidenceID, o.ArtifactID).Scan(&p.EvidenceID, &p.ArtifactID, &p.MediaType, &p.Digest, &p.ScannerID, &p.ScannerVersion, &p.Outcome, &p.ScannedAt, &p.Signature)
		if pe == nil {
			// A proof proves a committed reference, not current verification. Never
			// delete it as an orphan. Hash drift/missing bytes need investigation.
			if authority.CustodyProofHash(p) != o.ProofHash || statErr != nil {
				return empty, authority.ErrDocumentCustody
			}
			o.State = "ATTACHED"
		} else if errors.Is(pe, pgx.ErrNoRows) {
			o.State = "QUARANTINED"
			o.QuarantinedAt = now
		} else {
			return empty, authority.ErrDocumentCustody
		}
	case "HOLD", "RELEASE_HOLD":
		if statErr != nil || o.State == "DELETING" {
			return empty, authority.ErrDocumentCustody
		}
		on := a.Operation == "HOLD"
		if d.Backend.Hold(limited, o, on) != nil {
			return empty, authority.ErrDocumentCustody
		}
		check, he := d.Backend.Stat(limited, o)
		if he != nil || check.LegalHold != on {
			return empty, authority.ErrDocumentCustody
		}
		o.LegalHold = on
	case "ROTATE":
		if statErr != nil || o.State != "ATTACHED" {
			return empty, authority.ErrDocumentCustody
		}
		wrapped, re := d.Backend.RewrapKey(limited, o, a.TargetKey)
		if re != nil || len(wrapped) == 0 {
			return empty, authority.ErrDocumentCustody
		}
		// ReEncrypt never exposes the data key. Preserve nonce/ciphertext/version
		// and independent scan signature; old KMS keys remain until dependency
		// inventories AND retained backups no longer need their envelopes.
		o.WrappedKey = wrapped
		o.KeyID = a.TargetKey
	case "DELETE":
		if statErr == nil && a.ExpectedVersion != observed.VersionID {
			return empty, authority.ErrDocumentCustody
		}
		if o.State != "ATTACHED" && o.State != "QUARANTINED" && o.State != "DELETING" || o.LegalHold || statErr == nil && observed.LegalHold || o.RetainUntil.After(now) || statErr == nil && observed.RetainUntil.After(now) {
			return empty, authority.ErrDocumentCustody
		}
		if o.State == "QUARANTINED" || o.State == "DELETING" && o.QuarantinedAt.Year() > 1970 {
			if o.QuarantinedAt.Add(time.Duration(policy.QuarantineHours) * time.Hour).After(now) {
				return empty, authority.ErrDocumentCustody
			}
			var referenced bool
			if tx.QueryRow(limited, `SELECT EXISTS(SELECT 1 FROM evidence.scanned_artifact WHERE evidence_id=$1 AND artifact_id=$2)`, o.EvidenceID, o.ArtifactID).Scan(&referenced) != nil || referenced {
				return empty, authority.ErrDocumentCustody
			}
		} else if o.DeleteAfter.After(now) {
			return empty, authority.ErrDocumentCustody
		}
		if o.State != "DELETING" {
			// Commit a read/intake tombstone BEFORE any irreversible remote action.
			// A crash/timeout leaves DELETING, which needs a newly reviewed exact
			// snapshot approval. It can never return plaintext or be reattached.
			o.State = "DELETING"
			if persistCustody(limited, tx, &o) != nil || custodyAudit(limited, tx, actor, a, o, sourceKey) != nil || !a.Valid(d.Now()) || tx.Commit(limited) != nil {
				return empty, authority.ErrDocumentCustody
			}
			// This invocation stops at the concrete reviewable cutoff. A separate
			// approved DELETE of DELETING performs the remote action and receipt.
			return reportCustody(o), nil
		}
		if statErr == nil {
			o.RetainUntil = observed.RetainUntil
			if d.Backend.Delete(limited, o) != nil {
				return empty, authority.ErrDocumentCustody
			}
		}
		// If an earlier Delete succeeded but the DB commit failed, exact-version
		// absence lets this approved retry finish. Never recreate deleted bytes.
		if _, err = tx.Exec(limited, `DELETE FROM evidence.document_candidate_attempt WHERE evidence_id=$1 AND artifact_id=$2`, o.EvidenceID, o.ArtifactID); err != nil {
			return empty, authority.ErrDocumentCustody
		}
		if er == nil && e.ArtifactID == o.ArtifactID {
			e.Status = "DESTROYED"
			e.Version++
			doc, de := json.Marshal(e)
			if de != nil {
				return empty, authority.ErrDocumentCustody
			}
			if _, err = tx.Exec(limited, `UPDATE evidence.record SET status='DESTROYED',document=$2::jsonb,version=version+1 WHERE evidence_id=$1`, e.EvidenceID, doc); err != nil {
				return empty, authority.ErrDocumentCustody
			}
		}
		o.State = "DELETED"
		o.KeyID = ""
		o.WrappedKey = []byte{0}
	}
	if persistCustody(limited, tx, &o) != nil || custodyAudit(limited, tx, actor, a, o, sourceKey) != nil || !a.Valid(d.Now()) || tx.Commit(limited) != nil {
		return empty, authority.ErrDocumentCustody
	}
	return reportCustody(o), nil
}
func persistCustody(ctx context.Context, tx pgx.Tx, o *authority.CustodyObject) error {
	var q any
	if !o.QuarantinedAt.IsZero() && o.QuarantinedAt.Year() > 1970 {
		q = o.QuarantinedAt
	}
	err := tx.QueryRow(ctx, `UPDATE evidence.document_custody SET version_id=$3,key_id=$4,wrapped_key=$5,state=$6,quarantined_at=$7,legal_hold=$8,revision=revision+1,updated_at=clock_timestamp() WHERE evidence_id=$1 AND artifact_id=$2 RETURNING revision`, o.EvidenceID, o.ArtifactID, o.VersionID, o.KeyID, o.WrappedKey, o.State, q, o.LegalHold).Scan(&o.Revision)
	return err
}
func custodyAudit(ctx context.Context, tx pgx.Tx, actor AuditActor, a CustodyApproval, o authority.CustodyObject, sourceKey string) error {
	return insertProvisioningAudit(ctx, tx, actor, "", "verification.document.custody."+a.Operation, "evidence/"+o.EvidenceID, map[string]any{"evidence_id": o.EvidenceID, "artifact_id": o.ArtifactID, "version_id": o.VersionID, "state": o.State, "policy_id": o.PolicyID, "policy_version": o.PolicyVersion, "legal_hold": o.LegalHold, "key_id": o.KeyID, "revision": o.Revision, "target_key_id": a.TargetKey, "source_key_id": sourceKey, "proposed_by": a.ProposedBy, "approved_by": a.ApprovedBy, "approval_reference": a.ApprovalReference})
}
