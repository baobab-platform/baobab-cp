package development

// Subscriptions fixture operations belong to CP, never to the engine or the
// orchestrator. This separate development binary refuses protected/unmarked
// stores and requires a current existing bootstrap lease before any write.
import (
 "context"
 "encoding/json"
 "errors"
 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

type SubscriptionFixture struct {
 TenantID string `json:"tenant_id"`
 OtherTenantID string `json:"other_tenant_id"`
 OrganisationID string `json:"organisation_id"`
 SubscriptionID string `json:"product_subscription_id"`
 ClassificationID string `json:"classification_id"`
 Reference string `json:"classification_reference"`
 LegalEntityID string `json:"legal_entity_id"`
 Revision int64 `json:"authoritative_revision"`
 Status string `json:"status"`
}

func SubscriptionFixtures(ctx context.Context, pool *pgxpool.Pool, cfg Config, phase string) (SubscriptionFixture,error) {
 var out SubscriptionFixture
 if err:=cfg.Validate();err!=nil{return out,err}
 if phase!="prepare"&&phase!="advance"&&phase!="suspend"&&phase!="read"{return out,errors.New("unsupported subscription fixture phase")}
 if err:=CheckDatabase(ctx,pool);err!=nil{return out,err}
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.Serializable});if err!=nil{return out,err};defer tx.Rollback(ctx)
 var raw []byte
 err=tx.QueryRow(ctx,`SELECT result FROM development.bootstrap_run WHERE run_id=$1 AND created_at<=$2 AND expires_at>$2 FOR UPDATE`,cfg.RunID,cfg.Now).Scan(&raw)
 if err!=nil{return out,err}
 var run Result
 if err=json.Unmarshal(raw,&run);err!=nil{return out,err}
 if !run.SyntheticOnly||run.ProtectedAcceptance||run.RunID!=cfg.RunID||run.ExpiresAt.Before(cfg.Now){return out,errors.New("synthetic run required")}
 for _,e:=range run.Enterprises{if e.Key=="nabhold"{out.TenantID=e.TenantID;out.OrganisationID=e.OrganisationID;out.LegalEntityID=e.LegalEntityID};if e.Key=="external-client"{out.OtherTenantID=e.TenantID}}
 if out.TenantID==""||out.OtherTenantID==""||out.TenantID==out.OtherTenantID{return out,errors.New("distinct fixture tenants required")}
 out.Reference="development:"+cfg.RunID+":subscriptions"
 var id,classification string
 if phase=="prepare" {
  var exists bool
  if err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM product.product_subscription WHERE tenant_id=$1 AND product_id='dev-subscriptions')`,out.TenantID).Scan(&exists);err!=nil{return out,err}
  if !exists {
   id,classification=domain.NewUUIDv7(),domain.NewUUIDv7()
   if _,err=tx.Exec(ctx,`INSERT INTO product.product_subscription(subscription_id,tenant_id,product_id,status,source,source_reference,effective_to) VALUES($1,$2,'dev-subscriptions','ACTIVE','MANUAL_APPROVAL',$3,$4)`,id,out.TenantID,out.Reference,run.ExpiresAt);err!=nil{return out,err}
   // COMMERCIAL is an inert read/projection fixture. It confers no payer,
   // INTERNAL eligibility, capability, legal verification or provider grant.
   if _,err=tx.Exec(ctx,`INSERT INTO product.subscription_classification(classification_id,subscription_id,tenant_id,subscription_type,classification_source,classification_reference,reason,classified_at,classified_by) VALUES($1,$2,$3,'COMMERCIAL','MANUAL_GOVERNANCE',$4,'ADR-SHARED-043 synthetic source fixture; not commercial approval',$5,$6)`,classification,id,out.TenantID,out.Reference,cfg.Now,run.MakerID);err!=nil{return out,err}
   if _,err=tx.Exec(ctx,`UPDATE product.product_subscription SET subscription_type='COMMERCIAL',classification_id=$1 WHERE subscription_id=$2`,classification,id);err!=nil{return out,err}
  }
 }
 // Restrict changes to the exact run-owned tuple. Never accepts a caller ID
 // or SQL statement, and never touches another subscription or classification.
 if phase=="advance"||phase=="suspend" {
  expected,next,status:=int64(1),int64(2),"ACTIVE"
  if phase=="suspend"{expected,next,status=2,3,"SUSPENDED"}
  tag,e:=tx.Exec(ctx,`UPDATE product.product_subscription SET version=$1,status=$2 WHERE tenant_id=$3 AND product_id='dev-subscriptions' AND source_reference=$4 AND version=$5`,next,status,out.TenantID,out.Reference,expected)
  if e!=nil{return out,e};if tag.RowsAffected()!=1{return out,errors.New("fixture transition out of order")}
 }
 err=tx.QueryRow(ctx,`SELECT subscription_id::text,classification_id::text,version,status FROM product.product_subscription WHERE tenant_id=$1 AND product_id='dev-subscriptions' AND source_reference=$2`,out.TenantID,out.Reference).Scan(&id,&classification,&out.Revision,&out.Status)
 if err!=nil{return out,err}
 out.SubscriptionID,err=domain.FormatResourceID(domain.ProductSubscriptionIDPrefix,id);if err!=nil{return out,err}
 out.ClassificationID,err=domain.FormatResourceID(domain.SubscriptionClassificationIDPrefix,classification);if err!=nil{return out,err}
 return out,tx.Commit(ctx)
}
