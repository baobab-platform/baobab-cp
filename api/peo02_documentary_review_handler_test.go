package api

import (
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 basestore "github.com/baobab-platform/baobab-cp/internal/store"
 "github.com/baobab-platform/baobab-cp/internal/store/postgres"
)
type inertDocumentaryReviewWriter struct{}
func(inertDocumentaryReviewWriter) ProposeDocumentaryReview(context.Context,string,
 basestore.RequestMetadata,string,string,int,[]byte)(postgres.ReviewReceipt,error){
 return postgres.ReviewReceipt{},errors.New("unexpected unauthenticated proposal")
}
func(inertDocumentaryReviewWriter) DecideDocumentaryReview(context.Context,string,
 basestore.RequestMetadata,string,string,postgres.ReviewDecisionInput)(postgres.ReviewReceipt,error){
 return postgres.ReviewReceipt{},errors.New("unexpected unauthenticated decision")
}
func(inertDocumentaryReviewWriter) FileDocumentaryReviewAppeal(context.Context,string,
 basestore.RequestMetadata,string,string,postgres.ReviewAppealInput)(postgres.ReviewReceipt,error){
 return postgres.ReviewReceipt{},errors.New("unexpected unauthenticated appeal")
}
func(inertDocumentaryReviewWriter) DecideDocumentaryReviewAppeal(context.Context,string,
 basestore.RequestMetadata,string,string,postgres.ReviewAppealDecisionInput)(postgres.ReviewReceipt,error){
 return postgres.ReviewReceipt{},errors.New("unexpected unauthenticated appeal decision")
}
func TestPEO02SReviewAppealRoutesAreHumanAuthenticatedAndProductionOff(t *testing.T){
 paths:=[]string{
  "/v2/documentary-reviews/deferrals/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/milestones/6/propose",
  "/v2/documentary-reviews/intents/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/decision",
  "/v2/documentary-reviews/decisions/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/appeals",
  "/v2/documentary-reviews/appeals/0190a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b/decision",
 }
 cases:=[]struct{
  name string
  d Dependencies
  expected int
 }{
  {"default off",Dependencies{Store:&fakeStore{}},http.StatusNotFound},
  {"production disabled",Dependencies{Store:&fakeStore{},Environment:"production",
    DocumentaryReviewEnabled:true,DocumentaryReview:inertDocumentaryReviewWriter{}},http.StatusNotFound},
  {"unrecognised environment disabled",Dependencies{Store:&fakeStore{},Environment:"STAGING",
    DocumentaryReviewEnabled:true,DocumentaryReview:inertDocumentaryReviewWriter{}},http.StatusNotFound},
  {"staging still denies unauthenticated principal",Dependencies{
    Store:&fakeStore{},Environment:"staging",DocumentaryReviewEnabled:true,
    DocumentaryReview:inertDocumentaryReviewWriter{},
    AdminVerifier:fakeVerifier{err:errors.New("expired token")},
  },http.StatusUnauthorized},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   h:=New(tc.d)
   for _,path:=range paths{
    req:=httptest.NewRequest(http.MethodPost,path,strings.NewReader("{}"))
    req.Header.Set("Authorization","Bearer invalid")
    w:=httptest.NewRecorder()
    h.ServeHTTP(w,req)
    if w.Code!=tc.expected{t.Errorf("%s returned %d instead of %d: %s",path,w.Code,tc.expected,w.Body.String())}
   }
  })
 }
}
