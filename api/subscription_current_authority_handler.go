package api

// ADR-SHARED-044: current ProductSubscription workload reads are separate
// from financial authorization. CP does not own pricing, payer or Kill Bill.
import (
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "regexp"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/auth"
    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/repository"
    "github.com/baobab-platform/baobab-cp/internal/service/subscription"
    "github.com/go-chi/chi/v5"
)

const currentSubscriptionPolicy = "ADR-SHARED-011/ADR-BCP-017/ADR-SUB-0003/current:v1"
const commercialPreflightPolicy = "ADR-SHARED-011/ADR-BCP-017/ADR-SUB-0015/financial-preflight:v1"

var commercialUUID = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")

type subscriptionCurrentHandler struct {
 reader repository.CurrentProductSubscriptionReader
 organisations subscription.Organisations
 registry auth.WorkloadRegistry
 clientID string
 now func()time.Time
}
type subscriptionCurrentClassification struct {
 ID string `json:"classification_id"`
 Type string `json:"subscription_type"`
 Source string `json:"classification_source"`
 Reference string `json:"classification_reference"`
}
type subscriptionCurrentSnapshot struct {
 TenantID string `json:"tenant_id"`
 SubscriptionID string `json:"product_subscription_id"`
 ProductID string `json:"product_id"`
 Status string `json:"status"`
 Revision int64 `json:"authoritative_revision"`
 Classification *subscriptionCurrentClassification `json:"classification"`
 PrimaryOrganisationID *string `json:"primary_organisation_id"`
 EvaluatedAt time.Time `json:"evaluated_at"`
 ExpiresAt time.Time `json:"expires_at"`
 PolicyReference string `json:"policy_reference"`
}
type subscriptionPreflightRequest struct {
 TenantID string `json:"tenant_id"`
 SubscriptionID string `json:"product_subscription_id"`
 Revision int64 `json:"authoritative_revision"`
 OperationID string `json:"operation_id"`
 OperationKind string `json:"operation_kind"`
 ProviderKind string `json:"provider_kind"`
}
type subscriptionPreflightDecision struct {
 TenantID string `json:"tenant_id"`
 SubscriptionID string `json:"product_subscription_id"`
 Revision int64 `json:"authoritative_revision"`
 OperationID string `json:"operation_id"`
 Outcome string `json:"outcome"`
 ReasonCode string `json:"reason_code"`
 EvaluatedAt time.Time `json:"evaluated_at"`
 ExpiresAt time.Time `json:"expires_at"`
 PolicyReference string `json:"policy_reference"`
}
func (h subscriptionCurrentHandler) at()time.Time {if h.now!=nil{return h.now().UTC()};return time.Now().UTC()}
func (h subscriptionCurrentHandler) caller(w http.ResponseWriter,r *http.Request,scope string)bool{
 w.Header().Set("Cache-Control","no-store, max-age=0")
 w.Header().Set("Pragma","no-cache")
 p,ok:=auth.PrincipalFromContext(r.Context())
 scopes,scoped:=h.registry.(auth.WorkloadScopeRegistry)
 if !ok||p.ActorType!="workload"||p.ClientID==""||p.ClientID!=h.clientID||
 h.registry==nil||!h.registry.IsActive(p.ClientID)||!p.HasScope(scope)||!scoped||!scopes.AllowsScope(p.ClientID,scope){
 problem(w,r,http.StatusForbidden,"SUBSCRIPTION_WORKLOAD_DENIED","a current registered subscription workload is required",false)
 return false
 }
 return true
}
func (h subscriptionCurrentHandler) current(r *http.Request,tenantID,subID string)(subscriptionCurrentSnapshot,error){
 now:=h.at()
 out:=subscriptionCurrentSnapshot{TenantID:tenantID,SubscriptionID:subID,
 EvaluatedAt:now,ExpiresAt:now.Add(3*time.Second),PolicyReference:currentSubscriptionPolicy}
 if h.reader==nil||h.organisations==nil{return out,errors.New("current-state dependencies unavailable")}
 row,err:=h.reader.ReadCurrentProductSubscription(r.Context(),tenantID,subID)
 if err!=nil{return out,err}
 if row.TenantID!=tenantID||row.SubscriptionID!=subID||row.Revision<1 {
 return out,errors.New("current subscription tenant or revision mismatch")
 }
 out.ProductID,out.Status,out.Revision=row.ProductID,row.Status,row.Revision
 if row.ClassificationID!=""{
 out.Classification=&subscriptionCurrentClassification{ID:row.ClassificationID,Type:row.SubscriptionType,
 Source:row.ClassificationSource,Reference:row.ClassificationReference}
 }
 live,err:=h.organisations.ListLiveTenantOrganisationMappings(r.Context(),tenantID)
 if err!=nil{return out,err}
 count:=0
 for _,m:=range live{
 if m.MappingRole==domain.TenantOrgRolePrimary&&m.Status==domain.RelationshipStatusActive&&m.InEffect(now) {
 count++
 id:=m.OrganisationID
 out.PrimaryOrganisationID=&id
 }
 }
 if count!=1 {out.PrimaryOrganisationID=nil}
 return out,nil
}
func (h subscriptionCurrentHandler) read(w http.ResponseWriter,r *http.Request){
 if !h.caller(w,r,"subscription:current-workload-read"){return}
 tenantID,subID:=chi.URLParam(r,"tenantID"),chi.URLParam(r,"subscriptionID")
 if tenantID==""{problem(w,r,400,"INVALID_SUBSCRIPTION_QUERY","tenant is required",false);return}
 if _,err:=domain.ParseResourceID(domain.ProductSubscriptionIDPrefix,subID);err!=nil{
 problem(w,r,400,"INVALID_SUBSCRIPTION_QUERY","canonical subscription id required",false);return}
 p,_:=auth.PrincipalFromContext(r.Context())
 if _,ok:=resolveWorkloadTenant(p.TenantID,tenantID);!ok{
 problem(w,r,404,"PRODUCT_SUBSCRIPTION_NOT_FOUND","subscription unavailable",false);return}
 out,err:=h.current(r,tenantID,subID)
 if errors.Is(err,repository.ErrProductSubscriptionNotFound){
 problem(w,r,404,"PRODUCT_SUBSCRIPTION_NOT_FOUND","subscription unavailable",false);return}
 if err!=nil{problem(w,r,503,"CURRENT_SUBSCRIPTION_UNAVAILABLE","fresh snapshot unavailable",true);return}
 writeJSON(w,200,out)
}
func (h subscriptionCurrentHandler) preflight(w http.ResponseWriter,r *http.Request){
 if !h.caller(w,r,"subscription:commercial-authority"){return}
 tenantID,subID:=chi.URLParam(r,"tenantID"),chi.URLParam(r,"subscriptionID")
 var req subscriptionPreflightRequest
 decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,8192))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&req);err!=nil{problem(w,r,400,"INVALID_COMMERCIAL_AUTHORITY_REQUEST","invalid request",false);return}
 // No second JSON document may hide an alternate request from audit.
 var trailing json.RawMessage
 if err:=decoder.Decode(&trailing);err!=io.EOF{
 problem(w,r,400,"INVALID_COMMERCIAL_AUTHORITY_REQUEST","trailing payload forbidden",false);return}
 if req.TenantID!=tenantID||req.SubscriptionID!=subID||req.Revision<1||
 req.OperationKind!="ENSURE_SUBSCRIPTION"||req.ProviderKind!="KILL_BILL"||
 !commercialUUID.MatchString(req.OperationID) {
 problem(w,r,400,"INVALID_COMMERCIAL_AUTHORITY_REQUEST","invalid operation or revision",false);return}
 if _,err:=domain.ParseResourceID(domain.ProductSubscriptionIDPrefix,subID);err!=nil{
 problem(w,r,400,"INVALID_COMMERCIAL_AUTHORITY_REQUEST","canonical subscription required",false);return}
 p,_:=auth.PrincipalFromContext(r.Context())
 if _,ok:=resolveWorkloadTenant(p.TenantID,tenantID);!ok{
 problem(w,r,404,"PRODUCT_SUBSCRIPTION_NOT_FOUND","subscription unavailable",false);return}
 snapshot,err:=h.current(r,tenantID,subID)
 if errors.Is(err,repository.ErrProductSubscriptionNotFound){
 problem(w,r,404,"PRODUCT_SUBSCRIPTION_NOT_FOUND","subscription unavailable",false);return}
 if err!=nil{problem(w,r,503,"CURRENT_SUBSCRIPTION_UNAVAILABLE","fresh snapshot unavailable",true);return}
 reason:="COMMERCIAL_TERMS_NOT_ATTESTED"
 switch {
 case snapshot.Revision!=req.Revision:reason="REVISION_STALE"
 case snapshot.Status!="ACTIVE":reason="SUBSCRIPTION_NOT_ACTIVE"
 case snapshot.Classification==nil||snapshot.Classification.Type!="COMMERCIAL":reason="CLASSIFICATION_NOT_COMMERCIAL"
 case snapshot.PrimaryOrganisationID==nil:reason="CURRENT_ORGANISATION_UNVERIFIED"
 }
 // v1 has no positive outcome. Prices, payer mandate, versioned plans
 // and provider account belong to other authorities.
 writeJSON(w,200,subscriptionPreflightDecision{TenantID:tenantID,SubscriptionID:subID,
 Revision:snapshot.Revision,OperationID:req.OperationID,Outcome:"DENIED",
 ReasonCode:reason,EvaluatedAt:snapshot.EvaluatedAt,ExpiresAt:snapshot.ExpiresAt,
 PolicyReference:commercialPreflightPolicy})
}
