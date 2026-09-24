package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
)

func kindAttestationHandler(t *testing.T, entities ...domain.CanonicalEntity) PlatformContextHandler {
	t.Helper()
	repo := repository.NewInMemoryRepository()
	canonical := repository.NewCanonicalRepository()
	iam := &fakeIamOrganisations{orgs: map[string]bool{}}
	for _, entity := range entities {
		canonical.Entities[entity.ID] = entity
		iam.orgs[entity.ID] = true
		_, _, err := iam.LinkIamOrganisation(context.Background(), domain.IamOrganisationReference{
			OrganisationID: entity.ID, Provider: "keycloak", Issuer: testRealm, ProviderOrganisationID: "kc-" + entity.ID,
			Status: domain.IamReferenceActive, EffectiveFrom: time.Now().Add(-time.Hour), SourceAuthority: "test",
		}, repository.AuditActor{})
		mustNoError(t, err)
	}
	return PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity:         service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:          &fakeStore{},
			Canonical:        canonical,
			Mappings:         noOrganisationMappings{},
			IamOrganisations: iam,
		},
		Contexts: repo,
	}
}

func resolveKind(handler PlatformContextHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/platform-context/resolve", bytes.NewReader([]byte(body)))
	principal := auth.Principal{
		Subject:   "baobab-trade",
		Issuer:    "https://iam.baobab-platform.com/realms/baobab",
		ActorType: "workload",
		ClientID:  "baobab-trade",
		TokenID:   "token-123",
		Scopes:    map[string]struct{}{"context:resolve": {}},
	}
	requestContext := context.WithValue(context.Background(), correlationKey{}, "00000000-0000-4000-8000-000000000018")
	w := httptest.NewRecorder()
	handler.Resolve(w, req.WithContext(auth.WithPrincipal(requestContext, principal)))
	return w
}

func iamEvidence(organisationID string) string {
	return `"iam_organization":{"provider":"keycloak","issuer":"` + testRealm + `","provider_organisation_id":"kc-` + organisationID + `"}`
}

var (
	kindBuyer    = domain.CanonicalEntity{ID: "org-buyer-1", EntityType: domain.EntityTypeBuyerOrganisation, Status: "ACTIVE", OwnerTenantID: "tenant-123"}
	kindSupplier = domain.CanonicalEntity{ID: "org-supplier-1", EntityType: domain.EntityTypeSupplierOrganisation, Status: "ACTIVE", OwnerTenantID: "tenant-123"}
	// kindForeignSupplier belongs to another tenant: attestation must fail
	// before its kind is compared, so the error is the same as for any
	// unattested organisation.
	kindForeignSupplier = domain.CanonicalEntity{ID: "org-supplier-2", EntityType: domain.EntityTypeSupplierOrganisation, Status: "ACTIVE", OwnerTenantID: "tenant-other"}
)

func TestPlatformContextHandlerAttestsExactBuyerOrganisationKind(t *testing.T) {
	handler := kindAttestationHandler(t, kindBuyer)
	for name, body := range map[string]string{
		"organisation_id":  `{"tenant_id":"tenant-123","organisation_id":"org-buyer-1","expected_organisation_type":"BUYER_ORGANISATION"}`,
		"iam_organization": `{"tenant_id":"tenant-123",` + iamEvidence("org-buyer-1") + `,"expected_organisation_type":"BUYER_ORGANISATION"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := resolveKind(handler, body)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
			}
			var response platformContextResolveResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.OrganisationID != "org-buyer-1" || response.OrganisationType != domain.EntityTypeBuyerOrganisation {
				t.Fatalf("unexpected organisation attestation: %+v", response)
			}
		})
	}
}

func TestPlatformContextHandlerOmitsKindWithoutExpectation(t *testing.T) {
	w := resolveKind(kindAttestationHandler(t, kindSupplier), `{"tenant_id":"tenant-123","organisation_id":"org-supplier-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("generic organisation resolution must stay compatible, got %d body=%s", w.Code, w.Body.String())
	}
	var response platformContextResolveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OrganisationID != "org-supplier-1" || response.OrganisationType != "" {
		t.Fatalf("a kind must only be attested when requested: %+v", response)
	}
}

func TestPlatformContextHandlerRejectsWrongOrganisationKind(t *testing.T) {
	handler := kindAttestationHandler(t, kindSupplier, kindForeignSupplier)
	for name, body := range map[string]string{
		"supplier as buyer":             `{"tenant_id":"tenant-123","organisation_id":"org-supplier-1","expected_organisation_type":"BUYER_ORGANISATION"}`,
		"supplier as buyer via iam":     `{"tenant_id":"tenant-123",` + iamEvidence("org-supplier-1") + `,"expected_organisation_type":"BUYER_ORGANISATION"}`,
		"foreign supplier":              `{"tenant_id":"tenant-123","organisation_id":"org-supplier-2","expected_organisation_type":"BUYER_ORGANISATION"}`,
		"unregistered kind":             `{"tenant_id":"tenant-123","organisation_id":"org-supplier-1","expected_organisation_type":"TENANT"}`,
		"kind without organisation":     `{"tenant_id":"tenant-123","expected_organisation_type":"BUYER_ORGANISATION"}`,
		"iam evidence not linked, kind": `{"tenant_id":"tenant-123",` + iamEvidence("org-unknown") + `,"expected_organisation_type":"BUYER_ORGANISATION"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := resolveKind(handler, body)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// The kind of an organisation the tenant is not attested for must not be
// observable: a foreign supplier requested as a buyer fails exactly like a
// foreign supplier requested with no kind.
func TestPlatformContextHandlerDoesNotRevealForeignOrganisationKind(t *testing.T) {
	handler := kindAttestationHandler(t, kindForeignSupplier)
	withKind := resolveKind(handler, `{"tenant_id":"tenant-123","organisation_id":"org-supplier-2","expected_organisation_type":"BUYER_ORGANISATION"}`)
	withoutKind := resolveKind(handler, `{"tenant_id":"tenant-123","organisation_id":"org-supplier-2"}`)
	var a, b map[string]any
	if err := json.Unmarshal(withKind.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := json.Unmarshal(withoutKind.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if withKind.Code != withoutKind.Code || a["code"] != b["code"] || a["detail"] != b["detail"] {
		t.Fatalf("responses differ: %d %v vs %d %v", withKind.Code, a, withoutKind.Code, b)
	}
}
