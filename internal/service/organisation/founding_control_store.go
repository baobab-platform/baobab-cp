package organisation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUntrustedCorporateProof = errors.New("independent corporate-control proof not accepted")

// IndependentProofApproval is a governed reviewer decision. Actual caller
// authentication, role and source-provider certification MUST be established
// by the protected composition root, not supplied by applicant HTTP input.
type IndependentProofApproval struct {
	MakerID string
	CheckerID string
	ApprovalReference string
	ApprovedAt time.Time
}

// PostgresControlProofStore is an append-only metadata ledger. It stores
// source references, digest and the third-party signed assertion, NEVER the
// sensitive company-secretarial documents, director IDs or bearer tokens.
type PostgresControlProofStore struct {
	Pool *pgxpool.Pool
	Trust map[string]IndependentControlTrust
	Now func() time.Time
}

func (s PostgresControlProofStore) Record(ctx context.Context,proof IndependentControlProof,approval IndependentProofApproval) error {
	if ctx == nil || ctx.Err() != nil || s.Pool == nil || s.Now == nil ||
		approval.MakerID == "" || approval.CheckerID == "" ||
		approval.MakerID == approval.CheckerID || approval.ApprovalReference == "" ||
		approval.ApprovedAt.IsZero() || approval.ApprovedAt.After(s.Now().UTC()) ||
		proof.RelationshipID == "" || proof.EvidenceReference == "" ||
		proof.IssuerID == approval.MakerID || proof.IssuerID == approval.CheckerID {
		return ErrUntrustedCorporateProof
	}
	trust,ok:=s.Trust[proof.SignerID]
	if !ok || !trust.verify(proof,s.Now().UTC()) ||
		approval.CheckerID == trust.ProposedBy ||
		approval.MakerID == trust.ApprovedBy {
		return ErrUntrustedCorporateProof
	}
	raw,err:=json.Marshal(proof)
	if err!=nil || len(raw)>16*1024{return ErrUntrustedCorporateProof}
	digest:=ControlProofFingerprint(proof)
	tx,err:=s.Pool.Begin(ctx)
	if err!=nil{return err}
	defer tx.Rollback(ctx)
	tag,err:=tx.Exec(ctx,`INSERT INTO evidence.founding_control_attestation
	(relationship_id,issuer_id,signer_id,evidence_ref,document_digest,
	proof_digest,proof_document,maker_principal,checker_principal,checker_approval_ref,checker_approved_at)
	VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11)
	ON CONFLICT(relationship_id,proof_digest) DO NOTHING`,
	proof.RelationshipID,proof.IssuerID,proof.SignerID,proof.EvidenceReference,
	proof.EvidenceDigest,digest,raw,approval.MakerID,approval.CheckerID,approval.ApprovalReference,approval.ApprovedAt)
	if err!=nil{return err}
	if tag.RowsAffected()==0 {
		// An idempotent retry must not silently change maker/checker evidence.
		var maker,checker,reference string
		var approvedAt time.Time
		err=tx.QueryRow(ctx,`SELECT maker_principal,checker_principal,checker_approval_ref,checker_approved_at
			FROM evidence.founding_control_attestation WHERE relationship_id=$1
			AND proof_digest=$2`,proof.RelationshipID,digest).Scan(&maker,&checker,&reference,&approvedAt)
		if err!=nil || maker!=approval.MakerID || checker!=approval.CheckerID ||
			reference!=approval.ApprovalReference || !approvedAt.Equal(approval.ApprovedAt){return ErrUntrustedCorporateProof}
	}
	return tx.Commit(ctx)
}

func (s PostgresControlProofStore) ReadIndependentControlProof(ctx context.Context,relationshipID string)(IndependentControlProof,error) {
	var empty IndependentControlProof
	if ctx==nil || ctx.Err()!=nil || s.Pool==nil || s.Now==nil || relationshipID==""{
		return empty,ErrUntrustedCorporateProof
	}
	rows,err:=s.Pool.Query(ctx,`SELECT proof_document FROM evidence.founding_control_attestation
		WHERE relationship_id=$1
          AND (proof_document->>'expires_at')::timestamptz > $2
          AND (proof_document->>'observed_at')::timestamptz <= $2
          ORDER BY recorded_at DESC,attestation_id DESC LIMIT 2`,relationshipID,s.Now().UTC())
	if err!=nil{return empty,err}
	defer rows.Close()
	count:=0
	var proof IndependentControlProof
	for rows.Next(){
		var raw []byte
		if err=rows.Scan(&raw);err!=nil{return empty,err}
		count++
		if count>1{return empty,ErrUntrustedCorporateProof}
		if err=json.Unmarshal(raw,&proof);err!=nil{return empty,ErrUntrustedCorporateProof}
	}
	if err=rows.Err();err!=nil{return empty,err}
	if count!=1 || proof.RelationshipID!=relationshipID{return empty,ErrUntrustedCorporateProof}
	return proof,nil
}

