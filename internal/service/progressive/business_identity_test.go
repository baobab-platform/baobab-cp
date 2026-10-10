package progressive

import (
  "strings"
  "testing"
)

func TestPEO03PinnedBusinessIdentityRejectsFalseCorporateAttestation(t *testing.T){
  examples:=[]struct{name string;payload string;permitted bool}{
    {"pre-incorporation operating business",`{"operating_name":"ZuriBeans Synthetic","organisation_form":"UNINCORPORATED_ORGANISATION","operating_country":"ZA","incorporation_claim":"NOT_INCORPORATED","authorised_representative":{"full_name":"Test Applicant","role":"owner"}}`,true},
    {"sole proprietor",`{"operating_name":"Local Stall Synthetic","organisation_form":"SOLE_PROPRIETOR","operating_country":"UG","incorporation_claim":"NOT_INCORPORATED","authorised_representative":{"full_name":"Test Applicant","role":"owner"}}`,true},
    {"incorporated claim missing registration proof",`{"operating_name":"Example","organisation_form":"COMPANY","operating_country":"ZA","incorporation_claim":"INCORPORATED_CLAIMED","legal_name":"Example Ltd","jurisdiction_of_incorporation":"ZA","authorised_representative":{"full_name":"Test Applicant","role":"director"}}`,false},
    {"forged verified flag",`{"operating_name":"Example","organisation_form":"COMPANY","operating_country":"ZA","incorporation_claim":"INCORPORATED_CLAIMED","verification_state":"VERIFIED","authorised_representative":{"full_name":"Test Applicant","role":"owner"}}`,false},
    {"non incorporated pretend jurisdiction",`{"operating_name":"Example","organisation_form":"UNINCORPORATED_ORGANISATION","operating_country":"ZA","incorporation_claim":"NOT_INCORPORATED","jurisdiction_of_incorporation":"ZA","authorised_representative":{"full_name":"Test Applicant","role":"owner"}}`,false},
  }
  for _,example:=range examples {t.Run(example.name,func(t *testing.T){
    claim,err:=ValidateProgressiveBusinessIdentity([]byte(example.payload))
    if (err==nil)!=example.permitted {t.Fatalf("permit=%v, err=%v",example.permitted,err)}
    if example.permitted && (strings.TrimSpace(claim.OperatingName)=="" || claim.IncorporationClaim!="NOT_INCORPORATED"){t.Fatal("lost applicant's unverified claim")}
  })}
}
