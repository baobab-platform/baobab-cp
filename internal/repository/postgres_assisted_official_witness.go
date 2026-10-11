package repository

import (
	"context"
	"errors"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/jackc/pgx/v5"
)

var ErrAssistedWitnessDenied = errors.New("official-source witness/decision not authorised")

// ProposeAssistedWitness persists a sanitized official portal review
// performed by an IAM-authenticated human. It NEVER sets any claim VERIFIED,
// activates a suspended SARS source, or creates an OEV-03 check/result.
func (r *PostgresRepository) ProposeAssistedWitness(ctx context.Context,f authority.AssistedFinding,key string,now time.Time,actor AuditActor)(authority.AssistedFinding,error){
	empty:=authority.AssistedFinding{}
	if validateActor(actor)!=nil||actor.ActorType!="human"||len(key)<16||len(key)>128||
		f.Witness!=actor.ActorID||authority.ValidateAssistedProposal(f,now)!=nil{
		return empty,ErrAssistedWitnessDenied
	}
	expected:=map[string]string{
		"esrc_sarstcs":"TAX_COMPLIANCE_STATUS",
		"esrc_sarsvat":"VAT_REGISTRATION",
		"esrc_sarspaye":"PAYE_EMPLOYER_REGISTRATION",
		"esrc_sarsuif":"UIF_EMPLOYER_REGISTRATION",
		"esrc_uifdept":"UIF_EMPLOYER_REGISTRATION",
		"esrc_sarssdl":"SDL_EMPLOYER_REGISTRATION",
		"esrc_sarscustoms":"CUSTOMS_TRADER_REGISTRATION",
	}
	if expected[f.SourceID]=="" {return empty,ErrAssistedWitnessDenied}
	tx,err:=r.pool.Begin(ctx)
	if err!=nil{return empty,err}
	defer tx.Rollback(ctx)
	var existing authority.AssistedFinding
	err=tx.QueryRow(ctx,`SELECT intent_id,case_id,claim_id,source_id,evidence_id,
		official_reference,provenance_digest,observed_at,freshness_until,witness_actor,outcome
		FROM evidence.assisted_official_witness
		WHERE witness_actor=$1 AND idempotency_key=$2`,actor.ActorID,key).
		Scan(&existing.IntentID,&existing.CaseID,&existing.ClaimID,&existing.SourceID,
			&existing.EvidenceID,&existing.OfficialReference,&existing.ResponseDigest,
			&existing.ObservedAt,&existing.FreshnessUntil,&existing.Witness,&existing.Status)
	if err==nil {
		if existing.CaseID!=f.CaseID||existing.ClaimID!=f.ClaimID||
			existing.SourceID!=f.SourceID||existing.EvidenceID!=f.EvidenceID||
			existing.OfficialReference!=f.OfficialReference||existing.ResponseDigest!=f.ResponseDigest{
			return empty,ErrAssistedWitnessDenied
		}
		return existing,nil
	}
	if !errors.Is(err,pgx.ErrNoRows){return empty,err}
	c,err:=lockCase(ctx,tx,f.CaseID,-1)
	if err!=nil{return empty,err}
	if c.Status!=verification.CaseVerifying||c.Subject.SubjectType!="ORGANISATION"{
		return empty,ErrAssistedWitnessDenied
	}
	claim,err:=lockClaim(ctx,tx,f.CaseID,f.ClaimID)
	if err!=nil{return empty,err}
	if claim.Status!=verification.ClaimUnderCheck||claim.ClaimType!=expected[f.SourceID]||
		claim.Jurisdiction!="ZA"||claim.Subject!=c.Subject||claim.Purpose!=c.Purpose||
		claim.AssertedBy==actor.ActorID{return empty,ErrAssistedWitnessDenied}
	records,err:=evidenceByID(ctx,tx,[]string{f.EvidenceID})
	if err!=nil{return empty,err}
	proof,ok:=records[f.EvidenceID]
	if !ok||proof.Status!=verification.EvidenceAvail||proof.Subject!=claim.Subject||
		proof.Purpose!=claim.Purpose{return empty,ErrAssistedWitnessDenied}
	_,err=tx.Exec(ctx,`INSERT INTO evidence.assisted_official_witness
		(intent_id,case_id,claim_id,source_id,evidence_id,official_reference,
		 provenance_digest,observed_at,freshness_until,witness_actor,outcome,idempotency_key)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'PENDING',$11)`,
		f.IntentID,f.CaseID,f.ClaimID,f.SourceID,f.EvidenceID,f.OfficialReference,
		f.ResponseDigest,f.ObservedAt,f.FreshnessUntil,actor.ActorID,key)
	if err!=nil{return empty,err}
	if err=insertProvisioningAudit(ctx,tx,actor,"","verification.assisted_source.witnessed",
		"verification-case/"+f.CaseID,map[string]any{
			"intent_id":f.IntentID,"case_id":f.CaseID,"claim_id":f.ClaimID,
			"source_id":f.SourceID,"evidence_id":f.EvidenceID,"outcome":"PENDING",
		});err!=nil{return empty,err}
	if err=tx.Commit(ctx);err!=nil{return empty,err}
	return f,nil
}

// DecideAssistedWitness appends a distinct human's assessment of the
// witness. ATTESTED only corroborates an observed portal check; it does
// NOT override a suspended source, legal applicability or OEV result.
func (r *PostgresRepository) DecideAssistedWitness(ctx context.Context,intentID,key,outcome,reason string,now time.Time,actor AuditActor)(authority.AssistedFinding,error){
	empty:=authority.AssistedFinding{}
	if validateActor(actor)!=nil||actor.ActorType!="human"||
		len(key)<16||len(key)>128||len(reason)<12||len(reason)>512||
		(outcome!="ATTESTED"&&outcome!="REJECTED") {
		return empty,ErrAssistedWitnessDenied
	}
	tx,err:=r.pool.Begin(ctx)
	if err!=nil{return empty,err}
	defer tx.Rollback(ctx)
	var f authority.AssistedFinding
	var prevKey *string
	err=tx.QueryRow(ctx,`SELECT intent_id,case_id,claim_id,source_id,evidence_id,
		official_reference,provenance_digest,observed_at,freshness_until,
		witness_actor,checker_actor,checked_at,outcome,decision_key
		FROM evidence.assisted_official_witness WHERE intent_id=$1 FOR UPDATE`,intentID).
		Scan(&f.IntentID,&f.CaseID,&f.ClaimID,&f.SourceID,&f.EvidenceID,
			&f.OfficialReference,&f.ResponseDigest,&f.ObservedAt,&f.FreshnessUntil,
			&f.Witness,&f.Checker,&f.CheckedAt,&f.Status,&prevKey)
	if err!=nil{return empty,err}
	if f.Status!="PENDING"{
		if prevKey!=nil&&*prevKey==key&&f.Checker==actor.ActorID&&f.Status==outcome{return f,nil}
		return empty,ErrAssistedWitnessDenied
	}
	if authority.ValidateAssistedApproval(f,actor.ActorID,now)!=nil{return empty,ErrAssistedWitnessDenied}
	claim,err:=lockClaim(ctx,tx,f.CaseID,f.ClaimID)
	if err!=nil{return empty,err}
	if claim.AssertedBy==actor.ActorID||claim.Status!=verification.ClaimUnderCheck{
		return empty,ErrAssistedWitnessDenied
	}
	_,err=tx.Exec(ctx,`UPDATE evidence.assisted_official_witness
		SET checker_actor=$2,checked_at=$3,outcome=$4,decision_reason=$5,decision_key=$6
		WHERE intent_id=$1 AND outcome='PENDING'`,intentID,actor.ActorID,now,outcome,reason,key)
	if err!=nil{return empty,err}
	if err=insertProvisioningAudit(ctx,tx,actor,"","verification.assisted_source.reviewed",
		"verification-case/"+f.CaseID,map[string]any{
			"intent_id":f.IntentID,"source_id":f.SourceID,"outcome":outcome,
		});err!=nil{return empty,err}
	if err=tx.Commit(ctx);err!=nil{return empty,err}
	f.Status=outcome;f.Checker=actor.ActorID;f.CheckedAt=now
	return f,nil
}

func newAssistedIntentID() string {return domain.NewResourceID("asrs")}
