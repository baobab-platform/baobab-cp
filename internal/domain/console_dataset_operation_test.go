package domain

import (
 "context"
 "encoding/csv"
 "strings"
 "testing"
 "time"
)
func TestCX05GovernedDatasetDeniesByDefault(t *testing.T){
 now:=time.Date(2026,10,12,12,0,0,0,time.UTC)
 id:="0199d000-1111-7777-8000-111111111111"
 rows:=[]ConsoleDatasetRecord{{OrganisationID:id,DisplayName:"=SUM(2+2)",Status:"ACTIVE",VerificationState:"UNVERIFIED",SourceAuthority:"CP"}}
 source,err:=DatasetSourceFingerprint(rows);if err!=nil{t.Fatal(err)}
 intent:=ConsoleDatasetIntent{Kind:ConsoleExport,MakerPrincipalID:"prn_maker",Purpose:"reviewed disclosure",
  RecipientReference:"ref_01",IdempotencyKey:"cx05-synthetic-idempotency-example",SourceFingerprint:source,TargetIDs:[]string{id},ValidUntil:now.Add(time.Hour)}
 digest,err:=intent.CanonicalDigest(now);if err!=nil{t.Fatal(err)}
 approval:=ConsoleDatasetApproval{PlanDigest:digest,CheckerPrincipalID:"prn_checker",ApprovedAt:now,EvidenceReference:"case_1"}
 allow:=func(_ string,p string)bool{return p=="organisation.view"||p=="organisation.export"}
 csvBytes,err:=BuildApprovedRedactedCSV(intent,approval,rows,now,allow,true);if err!=nil{t.Fatal(err)}
 parsed,err:=csv.NewReader(strings.NewReader(string(csvBytes))).ReadAll();if err!=nil||len(parsed)!=2||parsed[1][1]!="'=SUM(2+2)"{t.Fatalf("unsafe export %q err %v",csvBytes,err)}
 r:=append([]ConsoleDatasetRecord{},rows...);r[0].Status="SUSPENDED"
 if _,err=BuildApprovedRedactedCSV(intent,approval,r,now,allow,true);err==nil{t.Fatal("stale source allowed")}
 if _,err=BuildApprovedRedactedCSV(intent,approval,rows,now,allow,false);err==nil{t.Fatal("unreserved disclosure allowed")}
 if _,err=BuildApprovedRedactedCSV(intent,approval,rows,now,func(string,string)bool{return false},true);err==nil{t.Fatal("unauthorised export allowed")}
 approval.CheckerPrincipalID=intent.MakerPrincipalID
 if _,err=BuildApprovedRedactedCSV(intent,approval,rows,now,allow,true);err==nil{t.Fatal("self approval allowed")}
}
func TestCX05BulkValuesVersionsAndRightsBoundToChecker(t *testing.T){
 now:=time.Date(2026,10,12,12,0,0,0,time.UTC)
 id:="0199d000-1111-7777-8000-111111111111"
 rows:=[]ConsoleDatasetRecord{{OrganisationID:id,DisplayName:"Nabhold",Status:"ACTIVE",VerificationState:"PENDING_REVIEW",SourceAuthority:"CP"}}
 source,_:=DatasetSourceFingerprint(rows)
 changes:=[]ConsoleMetadataChange{{OrganisationID:id,ExpectedVersion:4,InternalLabel:"Board review label"}}
 changehash,_:=BulkChangeFingerprint(changes)
 intent:=ConsoleDatasetIntent{Kind:ConsoleBulk,Action:"organisation.metadata.update",MakerPrincipalID:"prn_maker",Purpose:"governed bulk update",
  RecipientReference:"ref_02",IdempotencyKey:"cx05-synthetic-idempotency-example",SourceFingerprint:source,ChangeFingerprint:changehash,
  TargetIDs:[]string{id},ValidUntil:now.Add(time.Hour)}
 digest,err:=intent.CanonicalDigest(now);if err!=nil{t.Fatal(err)}
 approval:=ConsoleDatasetApproval{PlanDigest:digest,CheckerPrincipalID:"prn_checker",ApprovedAt:now,EvidenceReference:"case_2"}
 allow:=func(_ string,p string)bool{return strings.HasPrefix(p,"organisation.")}
 calls:=0
 commit:=func(_ context.Context,h string,items []ConsoleMetadataChange)error{if h!=digest||len(items)!=1{t.Fatal("incorrect transaction inputs")};calls++;return nil}
 if err:=ApplyApprovedMetadataBulk(context.Background(),intent,approval,rows,changes,now,allow,true,commit);err!=nil{t.Fatal(err)}
 altered:=append([]ConsoleMetadataChange(nil),changes...);altered[0].InternalLabel="change after approval"
 if err:=ApplyApprovedMetadataBulk(context.Background(),intent,approval,rows,altered,now,allow,true,commit);err==nil{t.Fatal("post-approval tampering allowed")}
 if calls!=1{t.Fatal("unapproved commit occurred")}
 if err:=ApplyApprovedMetadataBulk(context.Background(),intent,approval,rows,changes,now,func(_ string,p string)bool{return p!="organisation.bulk.approve"},true,commit);err==nil{t.Fatal("checker right absent")}
 if err:=ApplyApprovedMetadataBulk(context.Background(),intent,approval,rows,changes,now,allow,true,nil);err==nil{t.Fatal("missing durable TX accepted")}
 if err:=ApplyApprovedMetadataBulk(context.Background(),intent,approval,rows,changes,now.Add(2*time.Hour),allow,true,commit);err==nil{t.Fatal("expired approval reused")}
}
