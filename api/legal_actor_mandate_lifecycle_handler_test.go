package api

import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/baobab-platform/baobab-cp/internal/service/legalactor"
 basestore "github.com/baobab-platform/baobab-cp/internal/store"
)
type stubMandateLifecycle struct{}
func (stubMandateLifecycle) TransitionOperatingLegalActorMandate(context.Context,string,
 basestore.RequestMetadata,string,string,legalactor.LifecycleCommand)(legalactor.LifecycleReceipt,error){
 return legalactor.LifecycleReceipt{},errors.New("must never reach unverified repository")
}
func TestLA04DLifecycleRoutesDefaultOffAndProductionLocked(t *testing.T){
 urls:=[]string{
  "/v2/legal-actor-mandates/0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6a/activate",
  "/v2/legal-actor-mandates/0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6a/terminate",
 }
 for _,deps:=range []Dependencies{
  {Store:&fakeStore{}},
  {Store:&fakeStore{},Environment:"production",LegalActorLifecycleEnabled:true,
   LegalActorLifecycle:stubMandateLifecycle{}},
 } {
  h:=New(deps)
  for _,url:=range urls {
   out:=httptest.NewRecorder()
   h.ServeHTTP(out,httptest.NewRequest(http.MethodPost,url,strings.NewReader("{}")))
   if out.Code!=http.StatusNotFound{t.Fatalf("route unexpectedly enabled in production/default: %s code %d",url,out.Code)}
  }
 }
}
func TestLA04DLifecycleRequiresAuthenticatedHuman(t *testing.T){
 h:=New(Dependencies{Store:&fakeStore{},Environment:"integration",
  LegalActorLifecycleEnabled:true,LegalActorLifecycle:stubMandateLifecycle{},
  AdminVerifier:fakeVerifier{err:errors.New("bad token")}})
 for _,url:=range []string{
 "/v2/legal-actor-mandates/0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6a/activate",
 "/v2/legal-actor-mandates/0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6a/terminate",
 }{
  req:=httptest.NewRequest(http.MethodPost,url,strings.NewReader("{}"))
  req.Header.Set("Authorization","Bearer invalid")
  out:=httptest.NewRecorder()
  h.ServeHTTP(out,req)
  if out.Code!=http.StatusUnauthorized{t.Fatalf("unauthorised lifecycle action %s returned %d",url,out.Code)}
 }
}
