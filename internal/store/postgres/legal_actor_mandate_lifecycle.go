// ADR-BCP-027 LA-04D: governed legal-actor mandate lifecycle.
package postgres

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "time"
 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/baobab-platform/baobab-cp/internal/service/legalactor"
 basestore "github.com/baobab-platform/baobab-cp/internal/store"
 "github.com/jackc/pgx/v5"
)

func legalActorTarget(action string) (string,string) {
 switch action {
 case "ACTIVATE": return "ACTIVE","MandateActivated"
 case "SUSPEND": return "SUSPENDED","MandateSuspended"
 case "REVOKE": return "REVOKED","MandateRevoked"
 case "EXPIRE": return "EXPIRED","MandateExpired"
 }
 return "",""
}

func (s *Store) TransitionOperatingLegalActorMandate(ctx context.Context,key string,meta basestore.RequestMetadata,
 actorID,mandateID string,cmd legalactor.LifecycleCommand)(legalactor.LifecycleReceipt,error){
 var empty legalactor.LifecycleReceipt
 target,definition:=legalActorTarget(cmd.Action)
 if target=="" || len(key)<16 || !domain.IsUUID(actorID) || !domain.IsUUID(mandateID) ||
 !domain.IsUUID(meta.CorrelationID) || meta.ActorID!=actorID || meta.ActorType!="human" ||
 cmd.AuthorityBasisReference=="" || len(cmd.EvidenceReferences)==0 {
   return empty,ErrLegalActorMandateAuthority
 }
 digest,err:=mandateHash(struct {
  Actor, Mandate string
  Command legalactor.LifecycleCommand
 }{actorID,mandateID,cmd})
 if err!=nil{return empty,err}
 tx,err:=s.pool.Begin(ctx)
 if err!=nil{return empty,err}
 defer tx.Rollback(ctx)
 // The LA-04C ledger guarantees stable receipts across command retries.
 _,err=tx.Exec(ctx,"SELECT pg_advisory_xact_lock(hashtextextended($1,0))","la04c:LIFECYCLE:"+key)
 if err!=nil{return empty,err}
 var hash,principal string
 var encoded []byte
 err=tx.QueryRow(ctx,`SELECT request_digest,actor_id::text,response
 FROM registry.operating_legal_actor_mandate_command
 WHERE command_kind='LIFECYCLE' AND idempotency_key=$1`,key).
 Scan(&hash,&principal,&encoded)
 if err==nil {
  if hash!=digest || principal!=actorID {
   return empty,fmt.Errorf("%w: %w",basestore.ErrIdempotencyConflict,ErrLegalActorMandateConflict)
  }
  var prior legalactor.LifecycleReceipt
  if err=json.Unmarshal(encoded,&prior);err!=nil{return empty,err}
  return prior,nil
 }
 if !errors.Is(err,pgx.ErrNoRows){return empty,err}
 if err=mandateHuman(ctx,tx,actorID);err!=nil{return empty,err}
 var tenantID,orgID,oldState,maker string
 var end *time.Time
 err=tx.QueryRow(ctx,`SELECT tenant_id,operating_organisation_id::text,status,created_by::text,effective_to
 FROM registry.operating_legal_actor_mandate WHERE mandate_id=$1::uuid FOR UPDATE`,mandateID).
 Scan(&tenantID,&orgID,&oldState,&maker,&end)
 if errors.Is(err,pgx.ErrNoRows){return empty,ErrLegalActorMandateConflict}
 if err!=nil{return empty,err}
 switch cmd.Action {
 case "ACTIVATE":
  if oldState!="PENDING"||actorID==maker{return empty,ErrLegalActorMandateAuthority}
 case "SUSPEND":
  if oldState!="ACTIVE"{return empty,ErrLegalActorMandateAuthority}
 case "REVOKE":
  if oldState!="PENDING"&&oldState!="ACTIVE"&&oldState!="SUSPENDED"{return empty,ErrLegalActorMandateAuthority}
 case "EXPIRE":
  if end==nil||time.Now().UTC().Before(*end)||
   (oldState!="PENDING"&&oldState!="ACTIVE"&&oldState!="SUSPENDED"){
    return empty,ErrLegalActorMandateAuthority
   }
 }
 _,err=tx.Exec(ctx,`INSERT INTO registry.operating_legal_actor_mandate_transition
 (mandate_id,action,from_status,to_status,authority_basis_reference,evidence_references,performed_by)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7::uuid)`,mandateID,cmd.Action,oldState,target,
 cmd.AuthorityBasisReference,cmd.EvidenceReferences,actorID)
 if err!=nil{return empty,err}
 var actual string
 err=tx.QueryRow(ctx,`UPDATE registry.operating_legal_actor_mandate m
 SET status=$2,
 approved_by=CASE WHEN $2='ACTIVE' THEN d.decided_by ELSE m.approved_by END,
 approved_at=CASE WHEN $2='ACTIVE' THEN d.decided_at ELSE m.approved_at END,
 legal_actor_verification_reference=CASE WHEN $2='ACTIVE' THEN d.legal_actor_verification_reference
 ELSE m.legal_actor_verification_reference END,
 revoked_at=CASE WHEN $2='REVOKED' THEN clock_timestamp() ELSE m.revoked_at END
 FROM registry.operating_legal_actor_mandate_decision d
 WHERE m.mandate_id=$1::uuid AND d.mandate_id=m.mandate_id
 RETURNING m.status`,mandateID,target).Scan(&actual)
 if errors.Is(err,pgx.ErrNoRows)&&cmd.Action!="ACTIVATE" {
  err=tx.QueryRow(ctx,`UPDATE registry.operating_legal_actor_mandate SET status=$2,
 revoked_at=CASE WHEN $2='REVOKED' THEN clock_timestamp() ELSE revoked_at END
 WHERE mandate_id=$1::uuid RETURNING status`,mandateID,target).Scan(&actual)
 }
 if err!=nil{return empty,fmt.Errorf("guarded transition: %w",err)}
 if actual!=target{return empty,ErrLegalActorMandateConflict}
 return s.finishMandateLifecycle(ctx,tx,key,meta,actorID,mandateID,
 tenantID,orgID,oldState,target,definition,digest,cmd)
}
