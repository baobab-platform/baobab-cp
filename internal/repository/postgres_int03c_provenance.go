package repository

// CP-only attestation of a separately registered, disposable synthetic
// canonical Principal. Two distinct ACTIVE canonical human administrators
// must verify an existing exact IAM issuer/subject mapping before a
// deactivation can reference a run. Neither operation creates a Principal.
import (
 "context"
 "errors"
 "time"

 "github.com/jackc/pgx/v5"
)

type Int03CProvenance struct {
 ProvenanceID string `json:"provenance_id"`
 PrincipalID string `json:"principal_id"`
 RunID string `json:"run_id"`
 SourceSHA string `json:"source_sha"`
 SourceEvidenceDigest string `json:"source_evidence_digest"`
 Status string `json:"status"`
 ExpiresAt time.Time `json:"expires_at"`
}

func (r *PostgresRepository) ProposeInt03CProvenance(ctx context.Context,principal,issuer,subject,run,sha,digest,maker string)(Int03CProvenance,error){
 if r==nil||r.pool==nil||!int03cUUID.MatchString(principal)||!int03cUUID.MatchString(maker)||
  principal==maker||issuer!=int03CIssuer||!validInt03CSubject(subject)||
  !int03cHex.MatchString(run)||!int03cSHA.MatchString(sha)||!int03cProofDigest.MatchString(digest){
  return Int03CProvenance{},ErrInt03CNotPermitted
 }
 tx,err:=r.pool.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.Serializable})
 if err!=nil{return Int03CProvenance{},err}
 defer tx.Rollback(ctx)
 var matched bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(
    SELECT 1 FROM identity.principal p
    JOIN identity.external_identity e ON e.principal_id=p.principal_id
    WHERE p.principal_id=$1::uuid AND p.actor_type='workload' AND p.status='ACTIVE'
      AND e.issuer=$2 AND e.subject=$3 AND e.status='ACTIVE')
    AND EXISTS(SELECT 1 FROM identity.principal h WHERE h.principal_id=$4::uuid
      AND h.actor_type='human' AND h.status='ACTIVE')`,
    principal,issuer,subject,maker).Scan(&matched)
 if err!=nil{return Int03CProvenance{},err}
 if !matched{return Int03CProvenance{},ErrInt03CNotPermitted}
 var out Int03CProvenance
 err=tx.QueryRow(ctx,`INSERT INTO identity.int03c_principal_provenance
 (principal_id,issuer,subject,run_id,source_sha,source_evidence_digest,maker_id,expires_at)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7::uuid,clock_timestamp()+interval '90 minutes')
 ON CONFLICT DO NOTHING
 RETURNING provenance_id::text,principal_id::text,run_id,source_sha,source_evidence_digest,expires_at`,
 principal,issuer,subject,run,sha,digest,maker).
 Scan(&out.ProvenanceID,&out.PrincipalID,&out.RunID,&out.SourceSHA,&out.SourceEvidenceDigest,&out.ExpiresAt)
 if errors.Is(err,pgx.ErrNoRows){
  // On a collision recover only the original exact intent. Never update
  // expiration or replace the maker/proof digest on a replay.
  var originalMaker string
  err=tx.QueryRow(ctx,`SELECT provenance_id::text,principal_id::text,run_id,source_sha,
   source_evidence_digest,expires_at,maker_id::text
   FROM identity.int03c_principal_provenance
   WHERE issuer=$1 AND subject=$2 AND run_id=$3 FOR UPDATE`,issuer,subject,run).
   Scan(&out.ProvenanceID,&out.PrincipalID,&out.RunID,&out.SourceSHA,&out.SourceEvidenceDigest,&out.ExpiresAt,&originalMaker)
  if err!=nil{return Int03CProvenance{},ErrInt03CNotPermitted}
  if originalMaker!=maker{return Int03CProvenance{},ErrInt03CNotPermitted}
 }else if err!=nil{return Int03CProvenance{},err}
 if out.PrincipalID!=principal||out.RunID!=run||out.SourceSHA!=sha||
  out.SourceEvidenceDigest!=digest||!out.ExpiresAt.After(time.Now().UTC()){
  return Int03CProvenance{},ErrInt03CNotPermitted
 }
 if err=tx.Commit(ctx);err!=nil{return Int03CProvenance{},err}
 out.Status="AWAITING_INDEPENDENT_CHECKER"
 return out,nil
}

func (r *PostgresRepository) ApproveInt03CProvenance(ctx context.Context,id,checker,expectedDigest string)(Int03CProvenance,error){
 if r==nil||r.pool==nil||!int03cUUID.MatchString(id)||!int03cUUID.MatchString(checker)||!int03cProofDigest.MatchString(expectedDigest){
  return Int03CProvenance{},ErrInt03CNotPermitted
 }
 tx,err:=r.pool.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.Serializable})
 if err!=nil{return Int03CProvenance{},err}
 defer tx.Rollback(ctx)
 var out Int03CProvenance
 var maker,issuer,subject string
 err=tx.QueryRow(ctx,`SELECT provenance_id::text,principal_id::text,run_id,source_sha,
   source_evidence_digest,expires_at,maker_id::text,issuer,subject
   FROM identity.int03c_principal_provenance
   WHERE provenance_id=$1::uuid FOR UPDATE`,id).
   Scan(&out.ProvenanceID,&out.PrincipalID,&out.RunID,&out.SourceSHA,&out.SourceEvidenceDigest,&out.ExpiresAt,&maker,&issuer,&subject)
 if err!=nil{return Int03CProvenance{},ErrInt03CNotPermitted}
 if out.SourceEvidenceDigest!=expectedDigest||maker==checker||!out.ExpiresAt.After(time.Now().UTC())||issuer!=int03CIssuer||
  !validInt03CSubject(subject){return Int03CProvenance{},ErrInt03CNotPermitted}
 var active bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(
  SELECT 1 FROM identity.principal h WHERE h.principal_id=$1::uuid
  AND h.actor_type='human' AND h.status='ACTIVE')
  AND EXISTS(
  SELECT 1 FROM identity.principal p JOIN identity.external_identity e
  ON e.principal_id=p.principal_id
  WHERE p.principal_id=$2::uuid AND p.actor_type='workload'
    AND p.status='ACTIVE' AND e.status='ACTIVE'
    AND e.issuer=$3 AND e.subject=$4)`,checker,out.PrincipalID,issuer,subject).Scan(&active)
 if err!=nil{return Int03CProvenance{},err}
 if !active{return Int03CProvenance{},ErrInt03CNotPermitted}
 tag,err:=tx.Exec(ctx,`INSERT INTO identity.int03c_provenance_approval(provenance_id,checker_id)
 VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`,id,checker)
 if err!=nil{return Int03CProvenance{},err}
 if tag.RowsAffected()!=1{return Int03CProvenance{},ErrInt03CNotPermitted}
 if err=tx.Commit(ctx);err!=nil{return Int03CProvenance{},err}
 out.Status="INDEPENDENTLY_APPROVED_UNATTESTED"
 return out,nil
}
