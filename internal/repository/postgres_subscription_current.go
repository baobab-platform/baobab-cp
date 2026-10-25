package repository

import (
 "context"
 "errors"

 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/jackc/pgx/v5"
)

type CurrentProductSubscription struct {
 TenantID string
 SubscriptionID string
 ProductID string
 Status string
 Revision int64
 ClassificationID string
 SubscriptionType string
 ClassificationSource string
 ClassificationReference string
}

type CurrentProductSubscriptionReader interface {
 ReadCurrentProductSubscription(context.Context,string,string) (CurrentProductSubscription,error)
}
var _ CurrentProductSubscriptionReader = (*PostgresRepository)(nil)

// A single query joins the immutable classification pointed to by the
// subscription revision; no stale event timestamp or provider state is used.
func (r *PostgresRepository) ReadCurrentProductSubscription(ctx context.Context, tenantID, subscriptionID string) (CurrentProductSubscription,error) {
 var out CurrentProductSubscription
 id,err:=domain.ParseResourceID(domain.ProductSubscriptionIDPrefix,subscriptionID)
 if err!=nil || tenantID=="" {return out,ErrProductSubscriptionNotFound}
 var rawID,rawClassification string
 err=r.pool.QueryRow(ctx,`SELECT s.subscription_id::text, s.tenant_id, s.product_id, s.status, s.version,
 COALESCE(s.classification_id::text,''), COALESCE(s.subscription_type,''),
 COALESCE(c.classification_source,''), COALESCE(c.classification_reference,'')
 FROM product.product_subscription s
 LEFT JOIN product.subscription_classification c
 ON c.classification_id=s.classification_id AND c.subscription_id=s.subscription_id
 AND c.tenant_id=s.tenant_id AND c.subscription_type=s.subscription_type
 WHERE s.subscription_id=$1::uuid AND s.tenant_id=$2`,id,tenantID).Scan(
 &rawID,&out.TenantID,&out.ProductID,&out.Status,&out.Revision,&rawClassification,
 &out.SubscriptionType,&out.ClassificationSource,&out.ClassificationReference)
 if errors.Is(err,pgx.ErrNoRows){return out,ErrProductSubscriptionNotFound}
 if err!=nil{return out,err}
 out.SubscriptionID,err=domain.FormatResourceID(domain.ProductSubscriptionIDPrefix,rawID)
 if err!=nil{return CurrentProductSubscription{},err}
 if rawClassification!=""{
 out.ClassificationID,err=domain.FormatResourceID(domain.SubscriptionClassificationIDPrefix,rawClassification)
 if err!=nil{return CurrentProductSubscription{},err}
 if out.ClassificationSource==""||out.ClassificationReference==""||out.SubscriptionType==""{
 return CurrentProductSubscription{},errors.New("current classification pointer does not resolve")}
 }else if out.SubscriptionType!=""||out.ClassificationSource!=""||out.ClassificationReference!=""{
 return CurrentProductSubscription{},errors.New("inconsistent unclassified subscription")}
 return out,nil
}
