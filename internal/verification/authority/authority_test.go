package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fixtureSecrets struct{}
func (fixtureSecrets) OAuthCredentials(context.Context) (string,string,string,error) {
	return "synthetic-client","synthetic-secret","synthetic-subscription",nil
}

func fixtureTransport(t *testing.T) (CIPCTransport,func()) {
	t.Helper()
	now:=time.Date(2026,10,11,8,0,0,0,time.UTC)
	server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path=="/synthetic-token"{
			if r.Method!=http.MethodPost || r.Header.Get("Content-Type")!="application/x-www-form-urlencoded" {
				t.Error("token request is not form-urlencoded")
			}
			w.Header().Set("Content-Type","application/json")
			w.Write([]byte(`{"access_token":"synthetic-secret-token","token_type":"Bearer"}`))
			return
		}
		if r.URL.Path=="/synthetic-companies/2020%2F123456%2F07" || r.URL.Path=="/synthetic-companies/2020/123456/07"{
			if r.Header.Get("Authorization")!="Bearer synthetic-secret-token" ||
				r.Header.Get("X-Synthetic-Subscription")!="synthetic-subscription" {
				t.Error("missing test IAM bearer or subscription")
			}
			w.Header().Set("Content-Type","application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"reg":"2020/123456/07","name":"Synthetic Test Company",
				"status":"ACTIVE","ref":"synthetic-issuer-ref-0001"})
			return
		}
		http.NotFound(w,r)
	}))
	spec:=Specification{SourceID:"esrc_cipc",OfficialOrigin:server.URL,
		OAuthTokenURL:server.URL+"/synthetic-token",
		CompanyLookupURL:server.URL+"/synthetic-companies/{registration_number}",
		HTTPMethod:http.MethodGet,SubscriptionField:"X-Synthetic-Subscription",
		IDField:"reg",NameField:"name",StatusField:"status",ReferenceField:"ref",
		Version:"synthetic-approved-test-v1"}
	cert:=Certification{SourceID:"esrc_cipc",SpecDigest:Digest(spec),ProposedBy:"prn_maker",
		ApprovedBy:"prn_checker",ApprovedAt:now.Add(-time.Hour),ExpiresAt:now.Add(time.Hour),
		ApprovalRecord:"synthetic-approved-fixture"}
	return CIPCTransport{Spec:spec,Certification:cert,Secrets:fixtureSecrets{},
		Client:server.Client(),AllowLoopback:true,Now:func() time.Time {return now}},server.Close
}

func TestPEO03FCIPCTLSOAuthAndImmutableSourceDigest(t *testing.T){
	c,closeServer:=fixtureTransport(t);defer closeServer()
	record,err:=c.Lookup(context.Background(),"2020/123456/07")
	if err!=nil {t.Fatal(err)}
	if record.RegistrationNumber!="2020/123456/07" || record.LegalName!="Synthetic Test Company" ||
		record.SourceReference!="synthetic-issuer-ref-0001" {t.Fatalf("wrong company mapping: %+v",record)}
	d:=sha256.Sum256(record.RawResponse)
	if record.Digest!="sha256:"+hex.EncodeToString(d[:]) {t.Fatal("provider response digest mismatch")}
	if strings.Contains(c.String(),"synthetic-secret") {t.Fatal("OAuth credentials exposed")}
}

func TestPEO03FNoUnapprovedSourceOrURL(t *testing.T){
	c,stop:=fixtureTransport(t);defer stop()
	for _,tc:=range []struct{name string;change func(*CIPCTransport)}{
		{"expired",func(c *CIPCTransport){c.Certification.ExpiresAt=c.Now().Add(-time.Second)}},
		{"self-approved",func(c *CIPCTransport){c.Certification.ApprovedBy=c.Certification.ProposedBy}},
		{"changed-spec",func(c *CIPCTransport){c.Spec.CompanyLookupURL="https://127.0.0.1/fake/{registration_number}"}},
		{"source-confusion",func(c *CIPCTransport){c.Spec.SourceID="esrc_sarstcs"}},
		{"http-origin",func(c *CIPCTransport){c.Spec.OfficialOrigin="http://example.com"}},
		{"unexpected-host",func(c *CIPCTransport){c.Spec.OAuthTokenURL="https://other.example.com/token"}},
		{"missing-credential",func(c *CIPCTransport){c.Secrets=nil}},
		{"no-loopback-production",func(c *CIPCTransport){c.AllowLoopback=false}},
	}{
		t.Run(tc.name,func(t *testing.T){bad:=c;tc.change(&bad)
			_,err:=bad.Lookup(context.Background(),"2020/123456/07")
			if !errors.Is(err,ErrUncertified){t.Fatalf("unapproved config accepted: %v",err)}
		})
	}
	for _,bad:=range []string{"../secret","2020/123456/07?admin=true","company#other","",strings.Repeat("a",81)}{
		if _,err:=c.Lookup(context.Background(),bad);!errors.Is(err,ErrUncertified){
			t.Fatalf("untrusted company reference accepted: %q %v",bad,err)
		}
	}
}

func TestPEO03FResponseMismatchRejects(t *testing.T){
	c,closeServer:=fixtureTransport(t);defer closeServer()
	_,err:=c.Lookup(context.Background(),"2020/123456/08")
	if !errors.Is(err,ErrUnavailable){t.Fatalf("unexpected company lookup passed: %v",err)}
}

func TestPEO03FSealedCustodyTamperAndIdentityBinding(t *testing.T){
	key:=make([]byte,32)
	for i:=range key {key[i]=byte(i+1)}
	vault:=SealedVault{Directory:t.TempDir(),Key:key}
	data:=[]byte(`{"official_source":"synthetic-only","company":"Test"}`)
	ref,err:=vault.Seal(context.Background(),"evr_fixture00000001",data)
	if err!=nil || !strings.HasPrefix(ref,"sealed-sha256:"){t.Fatalf("seal: %s %v",ref,err)}
	got,err:=vault.Open(context.Background(),"evr_fixture00000001")
	if err!=nil || string(got)!=string(data){t.Fatalf("open: %v",err)}
	if _,err=vault.Seal(context.Background(),"evr_fixture00000001",data);!errors.Is(err,ErrCustody){
		t.Fatalf("immutable file was overwritten: %v",err)
	}
	if _,err=vault.Open(context.Background(),"evr_some_other_id");!errors.Is(err,ErrCustody){
		t.Fatalf("foreign evidence ID read source contents: %v",err)
	}
	bad:=vault;bad.Key=make([]byte,32)
	if _,err=bad.Open(context.Background(),"evr_fixture00000001");!errors.Is(err,ErrCustody){
		t.Fatalf("wrong key opened source response: %v",err)
	}
}
