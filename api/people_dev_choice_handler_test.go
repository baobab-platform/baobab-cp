package api

import (
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/repository"
)

func TestPeopleDevChoiceOnlyInDevelopmentOrIntegration(t *testing.T) {
	for env, allow := range map[string]bool{
		"development": true, "integration": true,
		"test": false, "sandbox": false, "staging": false,
		"production": false, "": false, "DEVELOPMENT": false,
	} {
		if got := peopleDevChoicePermitted(env); got != allow {
			t.Fatalf("People choice mutation in %q = %t; want %t", env, got, allow)
		}
	}
}

func TestPeopleDevChoiceClosedProposalNoVendorSelfAuthority(t *testing.T) {
	normal := `{"tenant_id":"tn_demo","legal_employer_id":"ACME-ZA","jurisdiction":"ZA",
	"capability_key":"payroll.run.read","binding_id":"11111111-1111-4111-8111-111111111111",
	"certification_id":"22222222-2222-4222-8222-222222222222",
	"choice_evidence_ref":"cp/development/qualified/source/receipt","effective_from":"2026-11-12T12:00:00Z"}`
	parsed, ok := parsePeopleDevChoice([]byte(normal))
	if !ok || parsed.BindingID == "" {
		t.Fatal("closed development proposal denied")
	}
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	if err := parsed.Validate(now); err != nil {
		t.Fatalf("valid bounded proposal: %v", err)
	}
	for label, body := range map[string]string{
		"provider is asserted": normal[:len(normal)-1] + `,"provider_id":"provider_forged"}`,
		"approver is asserted": normal[:len(normal)-1] + `,"approved_by":"another_user"}`,
		"employee data":       normal[:len(normal)-1] + `,"employee":"PVT"}`,
		"certification forgery": normal[:len(normal)-1] + `,"status":"CERTIFIED"}`,
		"duplicate employer":  normal[:len(normal)-1] + `,"legal_employer_id":"OTHER"}`,
		"missing scope":       `{"tenant_id":"tn_demo"}`,
		"wrong type":          `{"tenant_id":null}`,
		"trailing text":       normal + `{}`,
	} {
		t.Run(label, func(t *testing.T) {
			if _, ok := parsePeopleDevChoice([]byte(body)); ok {
				t.Fatal("forged, duplicated or extraneous People authority admitted")
			}
		})
	}
	for label, update := range map[string]func(*repository.PeopleDevChoiceRequest){
		"unrecognised capability": func(r *repository.PeopleDevChoiceRequest) {r.CapabilityKey = "payroll.run.finalize"},
		"cross jurisdiction": func(r *repository.PeopleDevChoiceRequest) {r.Jurisdiction = "ZAX"},
		"missing binding": func(r *repository.PeopleDevChoiceRequest) {r.BindingID = ""},
		"missing certification": func(r *repository.PeopleDevChoiceRequest) {r.CertificationID = ""},
		"expired effective time": func(r *repository.PeopleDevChoiceRequest) {r.EffectiveFrom = now.Add(-time.Hour)},
		"future effective time": func(r *repository.PeopleDevChoiceRequest) {r.EffectiveFrom = now.Add(time.Hour)},
		"wrong tenant": func(r *repository.PeopleDevChoiceRequest) {r.TenantID = "tn-foreign"},
	} {
		t.Run(label, func(t *testing.T) {
			changed := parsed
			update(&changed)
			if err := changed.Validate(now); err == nil {
				t.Fatal("unqualified provider-choice intent admitted")
			}
		})
	}
}
