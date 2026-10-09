package api

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/auth"
 "github.com/baobab-platform/baobab-cp/internal/domain"
 "github.com/baobab-platform/baobab-cp/internal/repository"
 "github.com/baobab-platform/baobab-cp/internal/service/legalactor"
)
type legalActorAssessorRegistry struct{ allowed bool }
func (r legalActorAssessorRegistry) IsActive(string)bool{return r.allowed}
func (r legalActorAssessorRegistry) AllowsScope(clientID,scope string)bool{
 return r.allowed && clientID=="baobab-trade"&&scope=="legal-actor:assess"
}
type legalActorAssessorFixture struct{ calls int; outcome legalactor.Resolution }
func (f *legalActorAssessorFixture) ResolveOperatingLegalActor(_ context.Context, _ legalactor.Request)(legalactor.Resolution,error){
 f.calls++
 return f.outcome,nil
}
func TestLA05AssessmentRouteDefaultOffAndProductionLocked(t *testing.T){
 repo:=repository.NewInMemoryRepository()
 for _,deps:=range []Dependencies{
  {Store:&fakeStore{},Environment:"integration",Contexts:repo,Identities:repo,
   WorkloadRegistry:legalActorAssessorRegistry{allowed:true},LegalActorAssessment:&legalActorAssessorFixture{}},
  {Store:&fakeStore{},Environment:"production",Contexts:repo,Identities:repo,
   WorkloadRegistry:legalActorAssessorRegistry{allowed:true},
   LegalActorAssessmentEnabled:true,LegalActorAssessment:&legalActorAssessorFixture{}},
 }{
  h:=New(deps)
  w:=httptest.NewRecorder()
  h.ServeHTTP(w,httptest.NewRequest(http.MethodPost,"/internal/legal-actor/v1/assess",nil))
  if w.Code!=http.StatusNotFound{t.Fatalf("assessment must remain opt-in and nonproduction, got %d",w.Code)}
 }
}
func TestLA05AssessmentRejectsUntrustedAndCrossContext(t *testing.T){
 cases:=[]struct{name string; scope bool; market string; runtime bool; expired bool; owner bool; expected int}{
  {"valid-but-denied",true,"ZA",true,false,true,http.StatusOK},
  {"wrong-market",true,"UG",true,false,true,http.StatusForbidden},
  {"expired",true,"ZA",true,true,true,http.StatusForbidden},
  {"unowned-context",true,"ZA",true,false,false,http.StatusNotFound},
  {"unscoped-workload",false,"ZA",true,false,true,http.StatusForbidden},
  {"provisioning-context",true,"ZA",false,false,true,http.StatusForbidden},
 }
 for _,tc:=range cases {
  t.Run(tc.name,func(t *testing.T){
   repo:=repository.NewInMemoryRepository()
   principal:=domain.Principal{ID:domain.NewPrincipalID(),ActorType:"workload",Status:"ACTIVE"}
   if err:=repo.CreateIdentity(context.Background(),principal);err!=nil{t.Fatal(err)}
   issuer:="https://iam.example.test"
   subject:="baobab-trade"
   if err:=repo.LinkExternalIdentity(context.Background(),domain.ExternalIdentity{
    ID:domain.NewExternalIdentityID(),PrincipalID:principal.ID,Issuer:issuer,
    Subject:subject,Status:"ACTIVE"});err!=nil{t.Fatal(err)}
   now:=time.Now().UTC()
   expiry:=now.Add(time.Minute)
   if tc.expired{expiry=now.Add(-time.Minute)}
   org:=domain.NewUUIDv7()
   ctx:=domain.Context{ID:domain.NewUUIDv7(),PrincipalID:principal.ID,
    TenantID:"tn_la05a",OrganisationID:org,CountryCode:"ZA",
    MarketID:"market-za",ResolvedAt:now.Add(-time.Minute),ExpiresAt:&expiry}
   if !tc.runtime{ctx.AuthorityPurpose=domain.ContextPurposeTenantProvisioning}
   if !tc.owner{ctx.PrincipalID=domain.NewPrincipalID()}
   if err:=repo.CreateContext(context.Background(),ctx);err!=nil{t.Fatal(err)}
   stub:=&legalActorAssessorFixture{outcome:legalactor.Resolution{
    Outcome:legalactor.NoApplicableMandate,EvaluatedAt:now,PolicyReference:legalactor.PolicyReference}}
   handler:=New(Dependencies{
    Store:&fakeStore{tenant:domain.Tenant{TenantID:ctx.TenantID,ObservedState:"active"}},
    Environment:"integration",LegalActorAssessmentEnabled:true,
    WorkloadVerifier:fakeVerifier{principal:auth.Principal{
     Issuer:issuer,Subject:subject,ActorType:"workload",ClientID:"baobab-trade",
     TenantID:ctx.TenantID,TokenID:"token-la05",
     Scopes:map[string]struct{}{"context:resolve":{},"legal-actor:assess":{}}}},
    WorkloadRegistry:legalActorAssessorRegistry{allowed:tc.scope},
    LegalActorAssessment:stub,Contexts:repo,Identities:repo,
   })
   body,_:=json.Marshal(map[string]any{"context_id":ctx.ID,"role":"SELLER_OF_RECORD",
    "activity":"B2B_COFFEE_SALE","market":tc.market,"operation_reference":"order-123"})
   req:=httptest.NewRequest(http.MethodPost,"/internal/legal-actor/v1/assess",bytes.NewReader(body))
   req.Header.Set("Authorization","Bearer verified-stub")
   req.Header.Set("Content-Type","application/json")
   out:=httptest.NewRecorder()
   handler.ServeHTTP(out,req)
   if out.Code!=tc.expected{
    t.Fatalf("%s: status=%d want=%d body=%s",tc.name,out.Code,tc.expected,out.Body.String())
   }
   if tc.expected==http.StatusOK {
    var result assessLegalActorResponse
    if err:=json.Unmarshal(out.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
    if result.LegalActorResolution.Outcome!=legalactor.NoApplicableMandate ||
     result.ProviderPermissionsGranted || result.LegalActorResolution.ResponsibleLegalEntityID!="" {
     t.Fatalf("denial leaked actor or granted provider permission: %+v",result)
    }
    if out.Header().Get("Cache-Control")!="no-store"{t.Fatal("legal actor assessment could be cached")}
    if stub.calls!=1{t.Fatalf("expected one trusted resolver call, got %d",stub.calls)}
   }else if stub.calls!=0{t.Fatalf("untrusted context reached legal actor resolver")}
  })
 }
}
