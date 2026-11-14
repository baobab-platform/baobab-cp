package api

import (
	"testing"
)

func TestPeopleSelectionRequestRejectsForgedAuthorityAndDuplicates(t *testing.T) {
	good := `{"context_id":"ctx_opaque","capability_key":"payroll.run.read","correlation_id":"12345678-1234-1234-1234-123456789012","required_contract_version":1}`
	if _, ok := parsePeopleSelectionRequest([]byte(good)); !ok {
		t.Fatal("valid intent denied")
	}
	for name, bad := range map[string]string{
		"tenant":         good[:len(good)-1] + `,"tenant_id":"tn_forged"}`,
		"legal_employer": good[:len(good)-1] + `,"legal_employer_id":"OTHER"}`,
		"provider":       good[:len(good)-1] + `,"provider_id":"provider_forged"}`,
		"binding":        good[:len(good)-1] + `,"binding_id":"bind_forged"}`,
		"principal":      good[:len(good)-1] + `,"principal_id":"principal_forged"}`,
		"duplicate":      good[:len(good)-1] + `,"context_id":"ctx_other"}`,
		"unsupported":    `{"context_id":"ctx_opaque","capability_key":"payroll.run.manage","correlation_id":"12345678-1234-1234-1234-123456789012","required_contract_version":1}`,
		"wrong-contract": `{"context_id":"ctx_opaque","capability_key":"payroll.run.read","correlation_id":"12345678-1234-1234-1234-123456789012","required_contract_version":2}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := parsePeopleSelectionRequest([]byte(bad)); ok {
				t.Fatal("untrusted field accepted")
			}
		})
	}
}

func TestPeopleProviderSelectionNeverMountsInProtectedEnvironments(t *testing.T) {
	for env, allowed := range map[string]bool{
		"development": true, "test": true, "integration": true,
		"sandbox": false, "staging": false, "production": false,
		"": false, "PRODUCTION": false, "unknown": false,
	} {
		t.Run(env, func(t *testing.T) {
			if got := peopleProviderSelectionPermittedIn(env); got != allowed {
				t.Fatalf("People provider route environment %q allowed=%v, want %v", env, got, allowed)
			}
		})
	}
}
