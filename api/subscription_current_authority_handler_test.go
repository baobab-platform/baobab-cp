package api

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/auth"
 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/baobab-platform/baobab-cp/internal/repository"
 "github.com/go-chi/chi/v5"
)

const currentTestSub = "sub_0190a1b2c3d4e5f60718293a4b5c6d7e"
const currentTestTenant = "tn_synthetic"
const currentTestClient = "baobab-subscriptions-workload"

type subscriptionCurrentFakeReader struct{ row repository.CurrentProductSubscription; called int }
func(f *subscriptionCurrentFakeReader) ReadCurrentProductSubscription(_ context.Context,tenant,sub string)(repository.CurrentProductSubscription,error){
 f.called++
 if f.row.TenantID!=tenant||f.row.SubscriptionID!=sub{return repository.CurrentProductSubscription{},repository.ErrProductSubscriptionNotFound}
 return f.row,nil
}
type subscriptionCurrentFakeOrgs struct{ live []domain.TenantOrganisationMapping }
func(f subscriptionCurrentFakeOrgs) ListLiveTenantOrganisationMappings(context.Context,string)([]domain.TenantOrganisationMapping,error){return f.live,nil}
func(f subscriptionCurrentFakeOrgs) ListPlatformRelationships(context.Context,string,time.Time)([]domain.PlatformRelationship,error){return nil,nil}
type subscriptionCurrentFakeRegistry struct{ active bool; scope string }
func(f subscriptionCurrentFakeRegistry) IsActive(string)bool{return f.active}
func(f subscriptionCurrentFakeRegistry) AllowsScope(_ string,scope string)bool{return scope==f.scope}

func TestSubscriptionCurrentAuthorityDenyOnlyAndCrossTenant(t *testing.T){
 at:=time.Date(2026,10,25,18,0,0,0,time.UTC)
 row:=repository.CurrentProductSubscription{TenantID:currentTestTenant,SubscriptionID:currentTestSub,
  ProductID:"synthetic-product",Status:"ACTIVE",Revision:5,ClassificationID:"subcls_0190a1b2c3d4e5f60718293a4b5c6d7e",
  SubscriptionType:"COMMERCIAL",ClassificationSource:"MANUAL_GOVERNANCE",ClassificationReference:"synthetic:approved"}
 reader:=&subscriptionCurrentFakeReader{row:row}
 scopes:=subscriptionCurrentFakeRegistry{active:true,scope:"subscription:commercial-authority"}
 handler:=subscriptionCurrentHandler{reader:reader,organisations:subscriptionCurrentFakeOrgs{
  live:[]domain.TenantOrganisationMapping{{TenantID:currentTestTenant,OrganisationID:"org_synthetic",
  MappingRole:domain.TenantOrgRolePrimary,Status:domain.RelationshipStatusActive}}},
  registry:scopes,clientID:currentTestClient,now:func()time.Time{return at}}
 path:="/internal/subscriptions/v1/tenants/"+currentTestTenant+"/product-subscriptions/"+currentTestSub+"/commercial-authority"
 router:=chi.NewRouter()
 router.Post("/internal/subscriptions/v1/tenants/{tenantID}/product-subscriptions/{subscriptionID}/commercial-authority",handler.preflight)
 reqBody:=`{"tenant_id":"tn_synthetic","product_subscription_id":"`+currentTestSub+`","authoritative_revision":5,"operation_id":"0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b","operation_kind":"ENSURE_SUBSCRIPTION","provider_kind":"KILL_BILL"}`
 request:=func(path,body string,p auth.Principal)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(http.MethodPost,path,strings.NewReader(body))
  r=r.WithContext(auth.WithPrincipal(r.Context(),p))
  w:=httptest.NewRecorder()
  router.ServeHTTP(w,r)
  return w
 }
 workload:=auth.Principal{ActorType:"workload",ClientID:currentTestClient,
  Scopes:map[string]struct{}{"subscription:commercial-authority":{}}}
 denied:=request(path,reqBody,workload)
 if denied.Code!=200{t.Fatalf("preflight %d %s",denied.Code,denied.Body)}
 var decision subscriptionPreflightDecision
 if err:=json.Unmarshal(denied.Body.Bytes(),&decision);err!=nil{t.Fatal(err)}
 if decision.Outcome!="DENIED"||decision.ReasonCode!="COMMERCIAL_TERMS_NOT_ATTESTED"||
 decision.Revision!=5||!decision.ExpiresAt.After(decision.EvaluatedAt){
 t.Fatalf("fail-closed preflight lost authority: %+v",decision)}
 if denied.Header().Get("Cache-Control")!="no-store, max-age=0"{t.Fatal("commercial authority must not be cached")}
 stale:=request(path,strings.Replace(reqBody,`"authoritative_revision":5`,`"authoritative_revision":4`,1),workload)
 if stale.Code!=200 || !strings.Contains(stale.Body.String(),"REVISION_STALE"){t.Fatal(stale.Code,stale.Body)}
 wrong:=request(strings.Replace(path,currentTestTenant,"tn_foreign",1),reqBody,workload)
 if wrong.Code!=400 || reader.called!=2{t.Fatalf("cross tenant queried canonical row: %d/%d",wrong.Code,reader.called)}
 human:=workload;human.ActorType="human"
 if w:=request(path,reqBody,human);w.Code!=403{t.Fatalf("human preflight %d",w.Code)}
 wrongClient:=workload;wrongClient.ClientID="other-workload"
 if w:=request(path,reqBody,wrongClient);w.Code!=403{t.Fatalf("wrong issuer client %d",w.Code)}
 unscoped:=handler;unscoped.registry=subscriptionCurrentFakeRegistry{active:false,scope:"subscription:commercial-authority"}
 r:=httptest.NewRequest(http.MethodPost,path,strings.NewReader(reqBody)).WithContext(auth.WithPrincipal(context.Background(),workload))
 w:=httptest.NewRecorder()
 unscopedRouter:=chi.NewRouter()
 unscopedRouter.Post("/internal/subscriptions/v1/tenants/{tenantID}/product-subscriptions/{subscriptionID}/commercial-authority",unscoped.preflight)
 unscopedRouter.ServeHTTP(w,r)
 if w.Code!=403{t.Fatalf("revoked workload reached PDP: %d",w.Code)}
}
func TestSubscriptionCurrentWorkloadReadCannotGrantPayerAuthority(t *testing.T){
 reader:=&subscriptionCurrentFakeReader{row:repository.CurrentProductSubscription{
 TenantID:currentTestTenant,SubscriptionID:currentTestSub,ProductID:"synthetic-product",
 Status:"PENDING",Revision:4}}
 handler:=subscriptionCurrentHandler{reader:reader,organisations:subscriptionCurrentFakeOrgs{},
 registry:subscriptionCurrentFakeRegistry{active:true,scope:"subscription:current-workload-read"},
 clientID:currentTestClient}
 router:=chi.NewRouter()
 router.Get("/internal/subscriptions/v1/tenants/{tenantID}/product-subscriptions/{subscriptionID}/current",handler.read)
 r:=httptest.NewRequest(http.MethodGet,"/internal/subscriptions/v1/tenants/"+currentTestTenant+
 "/product-subscriptions/"+currentTestSub+"/current",nil)
 r=r.WithContext(auth.WithPrincipal(r.Context(),auth.Principal{
 ActorType:"workload",ClientID:currentTestClient,Scopes:map[string]struct{}{"subscription:current-workload-read":{}}}))
 w:=httptest.NewRecorder();router.ServeHTTP(w,r)
 if w.Code!=200{t.Fatalf("current read: %d %s",w.Code,w.Body)}
 var snapshot subscriptionCurrentSnapshot
 if err:=json.Unmarshal(w.Body.Bytes(),&snapshot);err!=nil{t.Fatal(err)}
 if snapshot.Classification!=nil||snapshot.PrimaryOrganisationID!=nil||snapshot.Revision!=4 {
 t.Fatalf("unclassified/ungoverned current state promoted to authority: %+v",snapshot)}
}
