package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
)

// fakeIamOrganisations is an in-memory IamOrganisationRepository with the
// same link rules as the PostgreSQL implementation.
type fakeIamOrganisations struct {
	refs   []domain.IamOrganisationReference
	orgs   map[string]bool
	actors []repository.AuditActor
}

func (f *fakeIamOrganisations) LinkIamOrganisation(_ context.Context, ref domain.IamOrganisationReference, actor repository.AuditActor) (string, bool, error) {
	if err := ref.Validate(); err != nil {
		return "", false, err
	}
	if !f.orgs[ref.OrganisationID] {
		return "", false, repository.ErrCanonicalEntityNotFound
	}
	for _, existing := range f.refs {
		if existing.Status == domain.IamReferenceActive && existing.Evidence() == ref.Evidence() {
			if existing.OrganisationID != ref.OrganisationID {
				return "", false, repository.ErrIamOrganisationAlreadyLinked
			}
			return existing.ID, false, nil
		}
	}
	ref.ID = fmt.Sprintf("iamorg_%032x", len(f.refs)+1)
	f.refs = append(f.refs, ref)
	f.actors = append(f.actors, actor)
	return ref.ID, true, nil
}

func (f *fakeIamOrganisations) RetireIamOrganisationReference(_ context.Context, id string, at time.Time, reason string, _ repository.AuditActor) error {
	for i := range f.refs {
		if f.refs[i].ID == id && f.refs[i].Status == domain.IamReferenceActive && reason != "" {
			f.refs[i].Status, f.refs[i].EffectiveTo = domain.IamReferenceRetired, &at
			return nil
		}
	}
	return fmt.Errorf("iam organisation reference %s is not retirable", id)
}

func (f *fakeIamOrganisations) ListIamOrganisationReferences(_ context.Context, organisationID string) ([]domain.IamOrganisationReference, error) {
	var out []domain.IamOrganisationReference
	for _, ref := range f.refs {
		if ref.OrganisationID == organisationID {
			out = append(out, ref)
		}
	}
	return out, nil
}

func (f *fakeIamOrganisations) ResolveIamOrganisation(_ context.Context, ev domain.IamOrganisationEvidence, at time.Time) (string, error) {
	if err := ev.Validate(); err != nil {
		return "", err
	}
	for _, ref := range f.refs {
		if ref.Evidence() == ev && ref.Resolves(at) {
			return ref.OrganisationID, nil
		}
	}
	return "", repository.ErrIamOrganisationNotLinked
}

const testRealm = "https://id.baobab-platform.test/realms/baobab"

func adminRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// TestIamOrganisationRoutes covers the ADR-BCP-018 ORG-10 admin surface:
// linking is idempotent and attributed to the administrator, an IAM
// organisation held by another Organisation is a conflict, links are
// retired rather than deleted, and the lookup fails closed.
func TestIamOrganisationRoutes(t *testing.T) {
	iam := &fakeIamOrganisations{orgs: map[string]bool{"org-1": true, "org-2": true}}
	handler := New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: adminPrincipal()}, IamOrganisations: iam})
	body := `{"provider":"keycloak","issuer":"` + testRealm + `","provider_organisation_id":"kc-1"}`

	response := adminRequest(t, handler, http.MethodPost, "/v1/canonical-entities/org-1/iam-organisations", body)
	if response.Code != http.StatusCreated || response.Header().Get("Location") == "" {
		t.Fatalf("link: %d %s", response.Code, response.Body.String())
	}
	var linked domain.IamOrganisationReference
	mustNoError(t, json.Unmarshal(response.Body.Bytes(), &linked))
	if linked.SourceAuthority != iamOrganisationSourceAuthority || linked.Status != domain.IamReferenceActive || iam.actors[0].ActorID != "admin-123" {
		t.Fatalf("link = %+v, actor = %+v", linked, iam.actors[0])
	}
	if response := adminRequest(t, handler, http.MethodPost, "/v1/canonical-entities/org-1/iam-organisations", body); response.Code != http.StatusOK {
		t.Fatalf("replayed link: %d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(t, handler, http.MethodPost, "/v1/canonical-entities/org-2/iam-organisations", body); response.Code != http.StatusConflict {
		t.Fatalf("link held by another organisation: %d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(t, handler, http.MethodPost, "/v1/canonical-entities/org-9/iam-organisations", body); response.Code != http.StatusNotFound {
		t.Fatalf("unknown organisation: %d %s", response.Code, response.Body.String())
	}
	for name, bad := range map[string]string{
		"source authority from the client": `{"provider":"keycloak","issuer":"` + testRealm + `","provider_organisation_id":"kc-2","source_authority":"applicant"}`,
		"non-https issuer":                 `{"provider":"keycloak","issuer":"http://id.example/realms/x","provider_organisation_id":"kc-2"}`,
		"unsupported provider":             `{"provider":"okta","issuer":"` + testRealm + `","provider_organisation_id":"kc-2"}`,
	} {
		if response := adminRequest(t, handler, http.MethodPost, "/v1/canonical-entities/org-1/iam-organisations", bad); response.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}

	lookup := "/v1/iam-organisations?provider=keycloak&issuer=" + testRealm + "&provider_organisation_id=kc-1"
	if response := adminRequest(t, handler, http.MethodGet, lookup, ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"organisation_id":"org-1"`) {
		t.Fatalf("lookup: %d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(t, handler, http.MethodPost, "/v1/iam-organisation-references/"+linked.ID+"/retire", `{"reason":"realm decommissioned"}`); response.Code != http.StatusNoContent {
		t.Fatalf("retire: %d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(t, handler, http.MethodGet, lookup, ""); response.Code != http.StatusNotFound {
		t.Fatalf("lookup of a retired link: %d %s", response.Code, response.Body.String())
	}
	if response := adminRequest(t, handler, http.MethodGet, "/v1/canonical-entities/org-1/iam-organisations", ""); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"status":"RETIRED"`) {
		t.Fatalf("list keeps retired links: %d %s", response.Code, response.Body.String())
	}
}

// TestPlatformContextHandlerIamOrganization covers iam_organization on the
// platform-context endpoint: it resolves through a link and is attested like
// organisation_id; unlinked evidence is a distinct 403; both at once is a 400.
func TestPlatformContextHandlerIamOrganization(t *testing.T) {
	repo := repository.NewInMemoryRepository()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities["org-1"] = domain.CanonicalEntity{ID: "org-1", EntityType: domain.EntityTypeBuyerOrganisation, Status: "ACTIVE", OwnerTenantID: "tenant-123"}
	iam := &fakeIamOrganisations{orgs: map[string]bool{"org-1": true}}
	_, _, err := iam.LinkIamOrganisation(context.Background(), domain.IamOrganisationReference{OrganisationID: "org-1", Provider: "keycloak", Issuer: testRealm,
		ProviderOrganisationID: "kc-1", Status: domain.IamReferenceActive, EffectiveFrom: time.Now().Add(-time.Hour), SourceAuthority: "test"}, repository.AuditActor{})
	mustNoError(t, err)
	handler := PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity: service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:  &fakeStore{}, Canonical: canonical, Mappings: noOrganisationMappings{}, IamOrganisations: iam,
		},
		Contexts: repo,
	}
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", bytes.NewReader([]byte(body)))
		principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.nabhold.com/realms/baobab", ActorType: "workload", ClientID: "baobab-trade", TokenID: "token-123", Scopes: map[string]struct{}{"context:resolve": {}}}
		req = req.WithContext(auth.WithPrincipal(context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000010"), principal))
		w := httptest.NewRecorder()
		handler.Resolve(w, req)
		return w
	}
	evidence := func(kc string) string {
		return `"iam_organization":{"provider":"keycloak","issuer":"` + testRealm + `","provider_organisation_id":"` + kc + `"}`
	}

	w := call(`{"tenant_id":"tenant-123",` + evidence("kc-1") + `}`)
	if w.Code != http.StatusOK {
		t.Fatalf("linked evidence: %d %s", w.Code, w.Body.String())
	}
	var response platformContextResolveResponse
	mustNoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	stored, err := repo.GetContext(context.Background(), response.ContextID)
	mustNoError(t, err)
	if stored.OrganisationID != "org-1" || stored.Provenance["organisation_id"].Source != "baobab-cp:iam-organisation-reference" {
		t.Fatalf("stored context: organisation=%q provenance=%+v", stored.OrganisationID, stored.Provenance["organisation_id"])
	}
	if w := call(`{"tenant_id":"tenant-123",` + evidence("kc-unknown") + `}`); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "ORGANISATION_NOT_RESOLVED") {
		t.Fatalf("unlinked evidence: %d %s", w.Code, w.Body.String())
	}
	if w := call(`{"tenant_id":"tenant-123","organisation_id":"org-1",` + evidence("kc-1") + `}`); w.Code != http.StatusBadRequest {
		t.Fatalf("both identifiers: %d %s", w.Code, w.Body.String())
	}
}
