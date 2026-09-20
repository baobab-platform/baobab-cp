package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
	"github.com/nabhold/baobab-cp/internal/service"
)

func kindAttestationHandler(entity domain.CanonicalEntity) PlatformContextHandler {
	repo := repository.NewInMemoryRepository()
	canonical := repository.NewCanonicalRepository()
	canonical.Entities[entity.ID] = entity
	return PlatformContextHandler{
		ContextResolution: service.ContextResolutionService{
			Identity:  service.IdentityService{Repository: repo, Provision: service.WorkloadOnlyProvisioningPolicy},
			Tenants:   &fakeStore{},
			Canonical: canonical,
		},
		Contexts: repo,
	}
}

func kindAttestationRequest(body string) *http.Request {
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/platform-context/resolve",
		bytes.NewReader([]byte(body)),
	)
	principal := auth.Principal{
		Subject:   "baobab-trade",
		Issuer:    "https://iam.baobab-platform.com/realms/baobab",
		ActorType: "workload",
		ClientID:  "baobab-trade",
		TokenID:   "token-123",
		Scopes:    map[string]struct{}{"context:resolve": {}},
	}
	requestContext := context.WithValue(
		context.Background(),
		correlationKey{},
		"00000000-0000-4000-8000-000000000018",
	)
	return req.WithContext(auth.WithPrincipal(requestContext, principal))
}

func TestPlatformContextHandlerAttestsExactBuyerOrganisationKind(t *testing.T) {
	handler := kindAttestationHandler(domain.CanonicalEntity{
		ID:            "org-buyer-1",
		EntityType:    domain.EntityTypeBuyerOrganisation,
		Status:        "ACTIVE",
		OwnerTenantID: "tenant-123",
	})
	req := kindAttestationRequest(
		`{"tenant_id":"tenant-123","organisation_id":"org-buyer-1","expected_organisation_type":"BUYER_ORGANISATION"}`,
	)
	w := httptest.NewRecorder()

	handler.Resolve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var response platformContextResolveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OrganisationID != "org-buyer-1" ||
		response.OrganisationType != domain.EntityTypeBuyerOrganisation {
		t.Fatalf("unexpected organisation attestation: %+v", response)
	}
}

func TestPlatformContextHandlerRejectsSupplierForBuyerAttestation(t *testing.T) {
	handler := kindAttestationHandler(domain.CanonicalEntity{
		ID:            "org-supplier-1",
		EntityType:    domain.EntityTypeSupplierOrganisation,
		Status:        "ACTIVE",
		OwnerTenantID: "tenant-123",
	})
	req := kindAttestationRequest(
		`{"tenant_id":"tenant-123","organisation_id":"org-supplier-1","expected_organisation_type":"BUYER_ORGANISATION"}`,
	)
	w := httptest.NewRecorder()

	handler.Resolve(w, req)

	if w.Code != http.StatusForbidden ||
		!bytes.Contains(w.Body.Bytes(), []byte("CONTEXT_DENIED")) {
		t.Fatalf("expected wrong-kind denial, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformContextHandlerRejectsExpectedKindWithoutOrganisation(t *testing.T) {
	handler := kindAttestationHandler(domain.CanonicalEntity{
		ID:            "org-buyer-1",
		EntityType:    domain.EntityTypeBuyerOrganisation,
		Status:        "ACTIVE",
		OwnerTenantID: "tenant-123",
	})
	req := kindAttestationRequest(
		`{"tenant_id":"tenant-123","expected_organisation_type":"BUYER_ORGANISATION"}`,
	)
	w := httptest.NewRecorder()

	handler.Resolve(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected missing organisation denial, got %d body=%s", w.Code, w.Body.String())
	}
}
