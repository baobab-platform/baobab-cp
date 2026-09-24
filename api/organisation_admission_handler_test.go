package api

import (
	"net/http"
	"testing"

	"github.com/nabhold/baobab-cp/internal/repository"
)

// TestOrganisationAdmissionRouteRejectsApplicantShapedInput covers the rules
// the HTTP layer enforces before onboarding runs (ADR-BCP-018 sections
// 69-70): fields the contract does not define, such as a self-classified
// platform relationship or a verification flag, are rejected, and so is a
// request that fails validation. Only platform administrators may call it.
func TestOrganisationAdmissionRouteRejectsApplicantShapedInput(t *testing.T) {
	// A nil repository is enough: every case here is refused before
	// onboarding touches persistence.
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: adminPrincipal()},
		OrganisationAdmission: (*repository.PostgresRepository)(nil)})
	path := "/v1/tenants/" + testTenantID + "/organisation-admission"
	applicant := `"applicant_organisation":{"legal_name":"Beta","registration_identifiers":[{"type":"COMPANY_REGISTRATION","value":"B-1"}]}`
	for name, body := range map[string]string{
		"self-classified platform relationship": `{"admission_decision_id":"adm_1",` + applicant + `,"platform_account":{"mode":"NONE"},"platform_relationship_type":"PLATFORM_GROUP_AFFILIATE"}`,
		"applicant claiming verification":       `{"admission_decision_id":"adm_1","applicant_organisation":{"legal_name":"Beta","verification_state":"VERIFIED","registration_identifiers":[{"type":"LEI","value":"X"}]},"platform_account":{"mode":"NONE"}}`,
		"no admission decision":                 `{` + applicant + `,"platform_account":{"mode":"NONE"}}`,
		"no governed identifiers":               `{"admission_decision_id":"adm_1","applicant_organisation":{"legal_name":"Beta","registration_identifiers":[]},"platform_account":{"mode":"NONE"}}`,
	} {
		if response := adminRequest(t, handler, http.MethodPost, path, body); response.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}

	tenantAdmin := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: tenantAdminPrincipal()},
		OrganisationAdmission: (*repository.PostgresRepository)(nil)})
	body := `{"admission_decision_id":"adm_1",` + applicant + `,"platform_account":{"mode":"NONE"}}`
	if response := adminRequest(t, tenantAdmin, http.MethodPost, path, body); response.Code == http.StatusOK || response.Code == http.StatusBadRequest {
		t.Fatalf("a tenant administrator must not reach organisation admission: %d %s", response.Code, response.Body.String())
	}
}
