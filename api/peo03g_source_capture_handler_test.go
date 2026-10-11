package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type sourceCaptureRegistry struct{ active,scope bool }
func (s sourceCaptureRegistry) IsActive(id string) bool {return s.active && id=="baobab-cipc-verifier"}
func (s sourceCaptureRegistry) AllowsScope(id,scope string) bool {
	return s.scope && s.IsActive(id) && scope==officialSourceScope
}
type captureFixture struct{ called int; err error }
func (f *captureFixture) Capture(_ context.Context,caseID,claimID,evidenceID,key string,actor repository.AuditActor) (authority.CaptureReceipt,error) {
	f.called++
	if f.err!=nil{return authority.CaptureReceipt{},f.err}
	if caseID!="vcase_test"||claimID!="ecl_test"||evidenceID!="evr_test"||
		key!="synthetic-source-request-001"||actor.ActorType!="workload"{
		return authority.CaptureReceipt{},errors.New("unbound request")
	}
	now:=time.Now().UTC()
	return authority.CaptureReceipt{ObservationID:"aobs_synthetic",CaseID:caseID,
		ClaimID:claimID,EvidenceID:evidenceID,SourceID:"esrc_cipc",
		ObservedAt:now,FreshnessUntil:now.Add(time.Minute)},nil
}

func TestPEO03GSourceCaptureDefaultOffAndProductionDenied(t *testing.T){
	for _,deps:=range []Dependencies{
		{Store:&fakeStore{},Environment:"integration",SourceCapture:&captureFixture{}},
		{Store:&fakeStore{},Environment:"production",SourceCaptureEnabled:true,
			SourceCapture:&captureFixture{},SourceCaptureClientIDs:[]string{"baobab-cipc-verifier"},
			WorkloadRegistry:sourceCaptureRegistry{active:true,scope:true}},
		{Store:&fakeStore{},Environment:"integration",SourceCaptureEnabled:true,
			SourceCapture:&captureFixture{},WorkloadRegistry:sourceCaptureRegistry{active:true,scope:true}},
	}{
		req:=httptest.NewRequest(http.MethodPost,"/internal/evidence/v1/source-observations/cipc",strings.NewReader("{}"))
		w:=httptest.NewRecorder()
		New(deps).ServeHTTP(w,req)
		if w.Code!=http.StatusNotFound{t.Fatalf("unapproved source route mounted: %d",w.Code)}
	}
}

func TestPEO03GWorkloadVerifierAndRegistryDenyCrossClient(t *testing.T){
	cases:=[]struct{name string;client string;scopes bool;active bool;valid bool;body string;want int}{
		{"valid workload","baobab-cipc-verifier",true,true,true,`{"verification_case_id":"vcase_test","claim_id":"ecl_test","evidence_id":"evr_test"}`,201},
		{"cross client","unregistered-client",true,true,true,`{"verification_case_id":"vcase_test","claim_id":"ecl_test","evidence_id":"evr_test"}`,403},
		{"missing scope","baobab-cipc-verifier",false,true,true,`{"verification_case_id":"vcase_test","claim_id":"ecl_test","evidence_id":"evr_test"}`,403},
		{"revoked workload","baobab-cipc-verifier",true,false,true,`{"verification_case_id":"vcase_test","claim_id":"ecl_test","evidence_id":"evr_test"}`,403},
		{"bad JWT","baobab-cipc-verifier",true,true,false,`{"verification_case_id":"vcase_test","claim_id":"ecl_test","evidence_id":"evr_test"}`,401},
		{"forged source","baobab-cipc-verifier",true,true,true,`{"verification_case_id":"vcase_test","claim_id":"ecl_test","evidence_id":"evr_test","outcome":"MATCHED","source_id":"esrc_cipc"}`,400},
	}
	for _,tc:=range cases{
		t.Run(tc.name,func(t *testing.T){
			f:=&captureFixture{}
			scopes:=map[string]struct{}{}
			if tc.scopes{scopes[officialSourceScope]=struct{}{}}
			principal:=auth.Principal{Issuer:"https://iam.example.test",Subject:"registered-verifier-workload",
				ClientID:tc.client,ActorType:"workload",Scopes:scopes}
			v:=fakeVerifier{principal:principal}
			if !tc.valid{v.err=errors.New("synthetic invalid signature")}
			h:=New(Dependencies{Store:&fakeStore{},Environment:"integration",
				WorkloadVerifier:v,WorkloadRegistry:sourceCaptureRegistry{active:tc.active,scope:tc.scopes},
				SourceCaptureEnabled:true,SourceCapture:f,SourceCaptureClientIDs:[]string{"baobab-cipc-verifier"}})
			req:=httptest.NewRequest(http.MethodPost,"/internal/evidence/v1/source-observations/cipc",strings.NewReader(tc.body))
			req.Header.Set("Authorization","Bearer synthetic-token")
			req.Header.Set("Idempotency-Key","synthetic-source-request-001")
			w:=httptest.NewRecorder();h.ServeHTTP(w,req)
			if w.Code!=tc.want{t.Fatalf("want %d got %d: %s",tc.want,w.Code,w.Body.String())}
			if tc.want==201 && (!strings.Contains(w.Body.String(),`"authority_granted":false`)||f.called!=1){
				t.Fatalf("successful source capture granted authority or did not call backend: %s",w.Body.String())
			}
			if tc.want!=201 && f.called!=0{t.Fatalf("denied caller executed source lookup")}
		})
	}
}
