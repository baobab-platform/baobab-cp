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

// RetentionPolicy is a versioned purpose/jurisdiction/classification policy,
// independently approved outside intake. Evidence expiry is NOT retention.
// Durations are anchored to the canonical Evidence.CreatedAt, not each retry.
type RetentionPolicy struct {
	ID               string `json:"id"`
	Version          string `json:"version"`
	Purpose          string `json:"purpose"`
	Jurisdiction     string `json:"jurisdiction"`
	Classification   string `json:"classification"`
	MinimumHours     int64  `json:"minimum_hours"`
	MaximumHours     int64  `json:"maximum_hours"`
	OrphanGraceHours int64  `json:"orphan_grace_hours"`
	QuarantineHours  int64  `json:"quarantine_hours"`
}

func (p RetentionPolicy) Valid() bool {
	return p.ID != "" && p.Version != "" && p.Purpose != "" && p.Jurisdiction != "" && p.Classification != "" && p.MinimumHours > 0 && p.MaximumHours >= p.MinimumHours && p.MaximumHours <= 876000 && p.OrphanGraceHours >= 1 && p.OrphanGraceHours <= 8760 && p.QuarantineHours >= 1 && p.QuarantineHours <= 8760
}

// Pin policy content as well as its name/version; editing a mounted policy in
// place cannot shorten grace or silently change a retained object's rules.
func policyDigest(p RetentionPolicy) string {
	raw, _ := json.Marshal(p)
	return authority.CipherDigest(raw)
}

type RetentionPolicyReader interface {
	CustodyPolicy(context.Context, string, string) (RetentionPolicy, error)
}

type DurableDocumentVault struct {
	Repository *PostgresRepository
	Backend    authority.CustodyBackend
	Policies   RetentionPolicyReader
	Prefix     string
	Now        func() time.Time
}

func (d DurableDocumentVault) ready(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == nil && d.Repository != nil && d.Repository.pool != nil && d.Backend != nil && d.Policies != nil && d.Now != nil && d.Prefix != ""
}

// Every writer and maintenance job takes the same transaction-scoped artifact
// fence BEFORE canonical locks. This excludes intake still uploading or
// committing from orphan decisions, including after a lost commit response.
func lockCustody(ctx context.Context, tx pgx.Tx, evidence, artifact string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 124))`, evidence+"\n"+artifact)
	return err
}

const custodySelect = `SELECT evidence_id,artifact_id,proof_hash,object_key,version_id,key_id,wrapped_key,nonce,cipher_digest,policy_id,policy_version,retain_until,delete_after,orphan_after,COALESCE(quarantined_at,'epoch'::timestamptz),legal_hold,state,revision,policy_hash FROM evidence.document_custody WHERE evidence_id=$1 AND artifact_id=$2`

func scanCustody(row pgx.Row) (authority.CustodyObject, error) {
	var o authority.CustodyObject
	err := row.Scan(&o.EvidenceID, &o.ArtifactID, &o.ProofHash, &o.ObjectKey, &o.VersionID, &o.KeyID, &o.WrappedKey, &o.Nonce, &o.CipherDigest, &o.PolicyID, &o.PolicyVersion, &o.RetainUntil, &o.DeleteAfter, &o.OrphanAfter, &o.QuarantinedAt, &o.LegalHold, &o.State, &o.Revision, &o.PolicyHash)
	return o, err
}

// Seal requires the private live admission lease. A second connection commits
// only the intent metadata before S3: no foreign key/row lock on canonical
// evidence is taken by that connection. Proof, attachment and audit use the
// admission transaction and therefore either all commit or all roll back.
func (d DurableDocumentVault) Seal(ctx context.Context, p authority.ScanProof, document []byte) error {
	if !d.ready(ctx) {
		return authority.ErrDocumentCustody
	}
	lease, ok := ctx.Value(scanAdmissionKey{}).(*scanAdmissionLease)
	if !ok || lease == nil || !lease.active || lease.repository != d.Repository || lease.evidenceID != p.EvidenceID || lease.artifactID != p.ArtifactID || validateScanAdmission(ctx, lease.tx, lease, d.Now()) != nil {
		return authority.ErrDocumentCustody
	}
	var raw []byte
	if lease.tx.QueryRow(ctx, `SELECT document FROM evidence.record WHERE evidence_id=$1`, p.EvidenceID).Scan(&raw) != nil {
		return authority.ErrDocumentCustody
	}
	e, err := decodeDocument[verification.Evidence](raw)
	cl, clErr := lockClaim(ctx, lease.tx, lease.caseID, lease.claimID)
	o, lookupErr := scanCustody(lease.tx.QueryRow(ctx, custodySelect, p.EvidenceID, p.ArtifactID))
	version := ""
	if lookupErr == nil {
		version = o.PolicyVersion
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return authority.ErrDocumentCustody
	}
	policy, pe := d.Policies.CustodyPolicy(ctx, e.RetentionPolicy, version)
	if err != nil || clErr != nil || pe != nil || !policy.Valid() || policy.ID != e.RetentionPolicy || policy.Purpose != e.Purpose || policy.Jurisdiction != cl.Jurisdiction || policy.Classification != e.Classification || e.CreatedAt.IsZero() || e.CreatedAt.After(d.Now()) {
		return authority.ErrDocumentCustody
	}
	err = lookupErr
	var key []byte
	if errors.Is(err, pgx.ErrNoRows) {
		hash := authority.CustodyProofHash(p)
		var wrapped []byte
		var keyID string
		key, wrapped, keyID, err = d.Backend.GenerateKey(ctx, hash)
		if err != nil {
			return authority.ErrDocumentCustody
		}
		defer authority.ClearKey(key)
		nonce, ne := authority.NewCustodyNonce()
		if ne != nil {
			return authority.ErrDocumentCustody
		}
		sealed, se := authority.EncryptCustody(p, document, key, nonce)
		if se != nil {
			return authority.ErrDocumentCustody
		}
		o = authority.CustodyObject{
			EvidenceID: p.EvidenceID, ArtifactID: p.ArtifactID, ProofHash: hash,
			ObjectKey: d.Prefix + hash, KeyID: keyID, WrappedKey: wrapped, Nonce: nonce,
			CipherDigest: authority.CipherDigest(sealed),
			PolicyID:     policy.ID, PolicyVersion: policy.Version, PolicyHash: policyDigest(policy),
			RetainUntil: e.CreatedAt.Add(time.Duration(policy.MinimumHours) * time.Hour).UTC().Truncate(time.Microsecond),
			DeleteAfter: e.CreatedAt.Add(time.Duration(policy.MaximumHours) * time.Hour).UTC().Truncate(time.Microsecond),
			OrphanAfter: d.Now().Add(time.Duration(policy.OrphanGraceHours) * time.Hour).UTC().Truncate(time.Microsecond),
			State:       "PENDING",
		}
		if !o.RetainUntil.After(d.Now()) {
			return authority.ErrDocumentCustody
		}
		_, err = d.Repository.pool.Exec(ctx, `INSERT INTO evidence.document_custody(evidence_id,artifact_id,proof_hash,object_key,key_id,wrapped_key,nonce,cipher_digest,policy_id,policy_version,retain_until,delete_after,orphan_after,policy_hash,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'PENDING')`, o.EvidenceID, o.ArtifactID, o.ProofHash, o.ObjectKey, o.KeyID, o.WrappedKey, o.Nonce, o.CipherDigest, o.PolicyID, o.PolicyVersion, o.RetainUntil, o.DeleteAfter, o.OrphanAfter, o.PolicyHash)
		if err != nil {
			return authority.ErrDocumentCustody
		}
	} else if err != nil {
		return authority.ErrDocumentCustody
	}
	if o.ProofHash != authority.CustodyProofHash(p) || o.PolicyID != policy.ID || o.PolicyVersion != policy.Version || o.PolicyHash != policyDigest(policy) || (o.State != "PENDING" && o.State != "ATTACHED") || !o.DeleteAfter.After(d.Now()) {
		return authority.ErrDocumentCustody
	}
	if key == nil {
		key, err = d.Backend.UnwrapKey(ctx, o)
		if err != nil {
			return authority.ErrDocumentCustody
		}
		defer authority.ClearKey(key)
	}
	sealed, err := authority.EncryptCustody(p, document, key, o.Nonce)
	if err != nil || authority.CipherDigest(sealed) != o.CipherDigest {
		return authority.ErrDocumentCustody
	}
	observed, err := d.Backend.Stat(ctx, o)
	if errors.Is(err, documentcustody.ErrAbsent) && o.State == "PENDING" && o.VersionID == "" {
		version, ce := d.Backend.Create(ctx, o, sealed)
		// Conditional collision and timeout are both recovered by HEAD. Only the
		// recorded ciphertext hash/proof/retention may be attached, never latest
		// bytes simply because their object key happens to match.
		if ce == nil {
			o.VersionID = version
		}
		observed, err = d.Backend.Stat(ctx, o)
	}
	if err != nil || observed.VersionID == "" || observed.ProofHash != o.ProofHash || observed.CipherDigest != o.CipherDigest || observed.RetainUntil.Before(o.RetainUntil) || o.LegalHold && !observed.LegalHold {
		return authority.ErrDocumentCustody
	}
	currentPolicy, policyErr := d.Policies.CustodyPolicy(ctx, e.RetentionPolicy, policy.Version)
	if policyErr != nil || currentPolicy != policy {
		return authority.ErrDocumentCustody
	}
	if o.State == "ATTACHED" {
		return nil
	}
	err = lease.tx.QueryRow(ctx, `UPDATE evidence.document_custody SET version_id=$3,state='ATTACHED',revision=revision+1,updated_at=clock_timestamp() WHERE evidence_id=$1 AND artifact_id=$2 RETURNING revision`, p.EvidenceID, p.ArtifactID, observed.VersionID).Scan(&o.Revision)
	if err != nil {
		return authority.ErrDocumentCustody
	}
	return insertProvisioningAudit(ctx, lease.tx, lease.actor, "", "verification.document.custody.attached", "evidence/"+p.EvidenceID, map[string]any{"evidence_id": p.EvidenceID, "artifact_id": p.ArtifactID, "version_id": observed.VersionID, "key_id": o.KeyID, "revision": o.Revision, "policy_id": o.PolicyID, "policy_version": o.PolicyVersion})
}

type custodyReadKey struct{}
type custodyReadLease struct {
	tx                 pgx.Tx
	repo               *PostgresRepository
	evidence, artifact string
	active             bool
}

// Open deliberately denies direct calls. Plaintext is delivered only while
// WithLockedDocument holds canonical locks; maintenance waits on evidence.
func (d DurableDocumentVault) Open(ctx context.Context, p authority.ScanProof) ([]byte, error) {
	if !d.ready(ctx) {
		return nil, authority.ErrDocumentCustody
	}
	l, ok := ctx.Value(custodyReadKey{}).(*custodyReadLease)
	if !ok || l == nil || !l.active || l.repo != d.Repository || l.evidence != p.EvidenceID || l.artifact != p.ArtifactID {
		return nil, authority.ErrDocumentCustody
	}
	o, err := scanCustody(l.tx.QueryRow(ctx, custodySelect, p.EvidenceID, p.ArtifactID))
	if err != nil || o.State != "ATTACHED" || o.ProofHash != authority.CustodyProofHash(p) || !o.DeleteAfter.After(d.Now()) {
		return nil, authority.ErrDocumentCustody
	}
	sealed, err := d.Backend.Read(ctx, o)
	if err != nil || authority.CipherDigest(sealed) != o.CipherDigest {
		return nil, authority.ErrDocumentCustody
	}
	key, err := d.Backend.UnwrapKey(ctx, o)
	if err != nil {
		return nil, authority.ErrDocumentCustody
	}
	defer authority.ClearKey(key)
	return authority.DecryptCustody(p, sealed, key, o.Nonce)
}
