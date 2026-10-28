package tmsauthority

import (
 "slices"
 "testing"
 "time"
)
func completeCandidate(now time.Time) TrustedInput{
 return TrustedInput{
  Scope:CanonicalScope{TenantID:"tn_synthetica",OperatingOrganisationID:"org-a",
   LegalEntityID:"legal-a",MarketID:"market-za",Environment:"development"},
  ActorIssuer:"https://synthetic-iam.invalid",ActorSubject:"subject-a",
  PrincipalID:"principal-a",WorkloadClientID:"synthetic-tms-validator",
  TokenID:"token-ref-a",IAMCurrent:true,ContextCurrent:true,
  LegalMandateCurrent:true,ProviderBindingCurrent:true,
  ProviderReleaseCurrent:true,CapabilityCurrent:true,
  Operation:BookingSubmit,AggregateID:"booking-a",
  IntentDigest:"sha256:"+stringFill("a",64),BindingID:"binding-a",
  BindingRevision:"3",ProviderReleaseDigest:"sha256:"+stringFill("b",64),
  FencingGeneration:2,LeaseValidUntil:now.Add(5*time.Second),
 }
}
func stringFill(s string,n int)string{out:="";for i:=0;i<n;i++{out+=s};return out}
func TestAllNativeEffectsHeldEvenWithPerfectSyntheticInputs(t *testing.T){
 now:=time.Date(2026,10,28,12,0,0,0,time.UTC)
 for op:=range operations{
  in:=completeCandidate(now);in.Operation=op
  d:=AssessCandidate(in,now)
  if d.Status!=StatusHeld||d.Executable||d.ActivatesProvider||
   !slices.Contains(d.Reasons,"TMS_NATIVE_EXECUTION_CONTRACT_NOT_ACCEPTED"){
   t.Fatalf("%s incorrectly admitted: %+v",op,d)
  }
 }
}
func TestMissingCurrentFactsAndIdentityCannotMintGrant(t *testing.T){
 now:=time.Date(2026,10,28,12,0,0,0,time.UTC)
 tests:=[]struct{name string; mutate func(*TrustedInput);reason string}{
  {"revoked IAM",func(i *TrustedInput){i.IAMCurrent=false},"IAM_IDENTITY_NOT_CURRENT"},
  {"expired context",func(i *TrustedInput){i.ContextCurrent=false},"CP_CONTEXT_NOT_CURRENT"},
  {"unverified legal",func(i *TrustedInput){i.LegalMandateCurrent=false},"LEGAL_MANDATE_OR_HOLD"},
  {"legal hold",func(i *TrustedInput){i.LegalHold=true},"LEGAL_MANDATE_OR_HOLD"},
  {"wrong binding",func(i *TrustedInput){i.ProviderBindingCurrent=false},"PROVIDER_BINDING_OR_RELEASE_REVOKED"},
  {"revoked release",func(i *TrustedInput){i.NativeProviderRevoked=true},"PROVIDER_BINDING_OR_RELEASE_REVOKED"},
  {"no capability",func(i *TrustedInput){i.CapabilityCurrent=false},"CAPABILITY_NOT_CURRENT"},
  {"cross tenant",func(i *TrustedInput){i.Scope.TenantID=""},"CANONICAL_SCOPE_MISSING"},
  {"wrong legal",func(i *TrustedInput){i.Scope.LegalEntityID=""},"CANONICAL_SCOPE_MISSING"},
  {"no binding",func(i *TrustedInput){i.BindingID=""},"RESOURCE_OR_INTENT_NOT_PINNED"},
  {"malformed digest",func(i *TrustedInput){i.IntentDigest="sha256:unknown"},"RESOURCE_OR_INTENT_NOT_PINNED"},
  {"stale lease",func(i *TrustedInput){i.FencingGeneration=0},"OPERATION_FENCE_NOT_CURRENT"},
  {"expired lease",func(i *TrustedInput){i.LeaseValidUntil=now.Add(-1*time.Second)},"OPERATION_FENCE_NOT_CURRENT"},
  {"long lease",func(i *TrustedInput){i.LeaseValidUntil=now.Add(time.Minute)},"OPERATION_FENCE_NOT_CURRENT"},
  {"unknown operation",func(i *TrustedInput){i.Operation="native.admin"},"OPERATION_NOT_DEFINED"},
 }
 for _,tt:=range tests{t.Run(tt.name,func(t *testing.T){
  in:=completeCandidate(now);tt.mutate(&in)
  d:=AssessCandidate(in,now)
  if d.Executable||!slices.Contains(d.Reasons,tt.reason){
   t.Fatalf("candidate not held for %s: %+v",tt.name,d)
  }
 })}
}
func TestZeroInputFailsClosed(t *testing.T){
 d:=AssessCandidate(TrustedInput{},time.Time{})
 if d.Executable||d.Status!="HELD"||len(d.Reasons)<4{t.Fatalf("fail-open: %+v",d)}
}
