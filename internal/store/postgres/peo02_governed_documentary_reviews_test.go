package postgres

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"
 "time"

 "github.com/baobab-platform/baobab-cp/internal/domain"
 basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func syntheticAssessment(actor, deferral, org string, month int, now time.Time) []byte {
 payload := map[string]any{
  "deferral_id":deferral,"organisation_id":org,"milestone_month":month,
  "review_reference":"staging/peo02s/synthetic-only",
  "reviewed_by":actor,"reviewed_at":now.Format(time.RFC3339Nano),
  "applicability_assessments":[]map[string]any{{
    "assessment_reference":"staging/assessment/synthetic-v1",
    "organisation_id":org,
    "requirement_id":"platform.founding.trading-name-proof",
    "authority":"PLATFORM_DOCUMENTARY",
    "applies":"NEEDS_ASSESSMENT",
    "policy_reference":"policy/peo02/approved-synthetic-only",
    "policy_version":"1",
    "context_reference":"staging/synthetic-context/1",
    "market":"ZA",
    "capability":"trade.checkout",
    "evaluated_at":now.Format(time.RFC3339Nano),
    "valid_until":now.Add(time.Minute*5).Format(time.RFC3339Nano),
  }},
 }
 raw,_:=json.Marshal(payload)
 return raw
}
func TestPEO02SGovernedReviewRejectsCrossActorAndUnverifiedPayload(t *testing.T){
 now:=time.Now().UTC()
 actor:=domain.NewUUIDv7()
 grant:=domain.NewUUIDv7()
 org:=domain.NewUUIDv7()
 good:=syntheticAssessment(actor,grant,org,6,now)
 if _,err:=validateReviewAssessment(good,actor,grant,6,now);err!=nil{
  t.Fatalf("Shared canonical fixture validation failed: %v",err)
 }
 tests:=[]struct{
  name string
  change func(map[string]any)
 }{
  {"untrusted reviewer",func(m map[string]any){m["reviewed_by"]=domain.NewUUIDv7()}},
  {"another deferral",func(m map[string]any){m["deferral_id"]=domain.NewUUIDv7()}},
  {"other milestone",func(m map[string]any){m["milestone_month"]=12}},
  {"unbounded future review",func(m map[string]any){m["reviewed_at"]=now.Add(time.Hour).Format(time.RFC3339Nano)}},
  {"unknown outcome",func(m map[string]any){items:=m["applicability_assessments"].([]any);items[0].(map[string]any)["applies"]="WAIVED"}},
  {"mismatched organisation",func(m map[string]any){items:=m["applicability_assessments"].([]any);items[0].(map[string]any)["organisation_id"]=domain.NewUUIDv7()}},
  {"missing provenance",func(m map[string]any){items:=m["applicability_assessments"].([]any);delete(items[0].(map[string]any),"policy_reference")}},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   m:=map[string]any{}
   if err:=json.Unmarshal(good,&m);err!=nil{t.Fatal(err)}
   tc.change(m)
   raw,_:=json.Marshal(m)
   if _,err:=validateReviewAssessment(raw,actor,grant,6,now);!errors.Is(err,ErrDocumentaryReviewDenied){
    t.Fatalf("invalid review accepted: %v",err)
   }
  })
 }
}
func TestPEO02SRestrictionScopesExcludeWildcardCapability(t *testing.T){
 for _,x:=range []ReviewRestriction{
  {RequirementID:"platform.founding.doc",CapabilityKey:"*",MarketCode:"ZA"},
  {RequirementID:"platform.founding.doc",CapabilityKey:"trade.checkout",MarketCode:"*"},
  {RequirementID:"platform.founding.doc",CapabilityKey:"trade.checkout",MarketCode:"zA"},
 }{
  if validateRestrictionScope(x){t.Fatalf("unscoped remediation accepted: %+v",x)}
 }
 if !validateRestrictionScope(ReviewRestriction{
  RequirementID:"platform.founding.doc",CapabilityKey:"trade.checkout",MarketCode:"ZA",
 }){t.Fatal("exact named capability restriction was rejected")}
}
func TestPEO02SPersistenceRejectsUnapprovedReviewWithoutSideEffects(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL")
 if dsn==""{t.Skip("PostgreSQL 17 integration DB not configured")}
 ctx:=context.Background()
 db,err:=Open(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 if err=db.ApplyMigrations(ctx);err!=nil{t.Fatal(err)}
 actor:=domain.NewUUIDv7()
 grant:=domain.NewUUIDv7()
 org:=domain.NewUUIDv7()
 // A well-formed review is NOT sufficient: a month-6 checkpoint must
 // already derive from an independently approved original deferral.
 meta:=basestore.RequestMetadata{ActorID:actor,ActorType:"human",CorrelationID:domain.NewUUIDv7()}
 _,err=db.ProposeDocumentaryReview(ctx,
  "peo02s-negative-"+domain.NewUUIDv7(),meta,actor,grant,6,syntheticAssessment(actor,grant,org,6,time.Now().UTC()))
 if err==nil{t.Fatal("a self-declared review created authority or a review record")}
 var count int
 if err=db.pool.QueryRow(ctx,`SELECT count(*) FROM admission.documentary_remediation_restriction
  WHERE organisation_id=$1::uuid`,org).Scan(&count);err!=nil||count!=0{
  t.Fatalf("invalid review wrote a scoped restriction or grant: count=%d err=%v",count,err)
 }
}
