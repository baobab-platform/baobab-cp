package verificationservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
)

type syntheticCaptureRepo struct {
	c verification.Case
	claim verification.Claim
	evidence verification.Evidence
	stored verification.SourceObservation
	recorded int
}
func (r *syntheticCaptureRepo) GetVerificationCase(context.Context,string)(verification.Case,error){return r.c,nil}
func (r *syntheticCaptureRepo) ListVerificationClaims(context.Context,string)([]verification.Claim,error){
	return []verification.Claim{r.claim},nil
}
func (r *syntheticCaptureRepo) GetEvidence(context.Context,string)(verification.Evidence,error){return r.evidence,nil}
func (r *syntheticCaptureRepo) RecordAuthoritativeSourceObservation(_ context.Context,_ string,obs verification.SourceObservation,_ string,_ string,_ time.Time,_ repository.AuditActor,_ bool)(string,error){
	r.recorded++;r.stored=obs;return "aobs_synthetictest1",nil
}
func (r *syntheticCaptureRepo) GetAuthoritativeObservationByKey(context.Context,string,string)(authority.CaptureReceipt,error){
	return authority.CaptureReceipt{},repository.ErrVerificationNotFound
}
type staticCaptureCredentials struct{}
func (staticCaptureCredentials) OAuthCredentials(context.Context)(string,string,string,error){
	return "test-id","test-secret","test-subscription",nil
}

func TestPEO03GLocalTLSSourceToEncryptedCustodyAndNonPositiveReceipt(t *testing.T){
	now:=time.Date(2026,10,11,8,0,0,0,time.UTC)
	server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path=="/token"{
			w.Header().Set("Content-Type","application/json")
			w.Write([]byte(`{"access_token":"synthetic-token","token_type":"Bearer"}`))
			return
		}
		if !strings.HasPrefix(r.URL.Path,"/companies/")||r.Header.Get("Authorization")!="Bearer synthetic-token"{
			http.Error(w,"invalid synthetic official request",403);return
		}
		w.Header().Set("Content-Type","application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"number":"2020/123456/07","name":"Synthetic Company Ltd",
			"status":"ACTIVE","reference":"synthetic-official-0001"})
	}));defer server.Close()
	spec:=authority.Specification{SourceID:"esrc_cipc",OfficialOrigin:server.URL,
		OAuthTokenURL:server.URL+"/token",
		CompanyLookupURL:server.URL+"/companies/{registration_number}",
		HTTPMethod:"GET",SubscriptionField:"X-Subscription-Key",
		IDField:"number",NameField:"name",StatusField:"status",ReferenceField:"reference",Version:"synthetic-v1"}
	cert:=authority.Certification{SourceID:"esrc_cipc",SpecDigest:authority.Digest(spec),
		ProposedBy:"prn_maker",ApprovedBy:"prn_checker",
		ApprovedAt:now.Add(-time.Minute),ExpiresAt:now.Add(time.Hour),
		ApprovalRecord:"synthetic-approval"}
	subject:=verification.Subject{SubjectType:"ORGANISATION",SubjectID:"org_synthetic"}
	caseID,claimID,evidenceID:="vcase_test1","ecl_test1","evr_test1"
	repo:=&syntheticCaptureRepo{
		c:verification.Case{CaseID:caseID,Status:verification.CaseVerifying,
			Subject:subject,Purpose:"ORGANISATION_ADMISSION"},
		claim:verification.Claim{ClaimID:claimID,Subject:subject,Status:verification.ClaimUnderCheck,
			ClaimType:"REGISTRATION_IDENTIFIER",Jurisdiction:"ZA",
			ClaimedValue:verification.ClaimedValue{Value:"2020/123456/07"},Purpose:"ORGANISATION_ADMISSION"},
		evidence:verification.Evidence{EvidenceID:evidenceID,SourceID:"esrc_cipc",Subject:subject,
			Status:verification.EvidenceAvail,SourceRecordReference:"synthetic-official-0001",
			Purpose:"ORGANISATION_ADMISSION"},
	}
	key:=make([]byte,32);for i:=range key{key[i]=byte(i+1)}
	vault:=authority.SealedVault{Directory:t.TempDir(),Key:key}
	service:=CIPCIntake{Repo:repo,Transport:authority.CIPCTransport{Spec:spec,Certification:cert,
		Secrets:staticCaptureCredentials{},Client:server.Client(),AllowLoopback:true,Now:func()time.Time{return now}},
		Vault:vault,Now:func()time.Time{return now},AllowSynthetic:true}
	actor:=repository.AuditActor{ActorID:"prn_workload",ActorType:"workload",ClientID:"test-verifier",
		CorrelationID:"019a0707-0202-7777-8000-111111111111"}
	res,err:=service.Capture(context.Background(),caseID,claimID,evidenceID,"synthetic-source-capture-test",actor)
	if err!=nil {t.Fatalf("locally captured source rejected: %v",err)}
	if res.AuthorityGranted||res.SourceID!="esrc_cipc"||repo.recorded!=1 ||
		repo.stored.Synthetic!=true {t.Fatalf("positive authority or missing synthetic ledger: %+v %+v",res,repo.stored)}
	raw,err:=vault.Open(context.Background(),evidenceID)
	if err!=nil||!strings.Contains(string(raw),"Synthetic Company Ltd"){
		t.Fatalf("official proof not encrypted and recoverable: %v",err)
	}
	if replay,err:=service.Capture(context.Background(),caseID,claimID,evidenceID,
		"another-synthetic-source-capture",actor);err!=nil||replay.AuthorityGranted{
		t.Fatalf("identical issuer response could not be replayed safely: %+v %v",replay,err)
	}
}
