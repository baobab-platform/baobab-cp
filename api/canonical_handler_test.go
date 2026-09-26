package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

const canonicalCreateBody = `{"canonical_key":"supplier:thamani_global:sup_01k4p8q2r3s4","entity_type":"SUPPLIER_ORGANISATION",` +
	`"display_name":"Thamani Global Supplies","owner_tenant_id":"tn_01k4thamani","authority":"thamani","classification":"TENANT_CONFIDENTIAL"}`

type canonicalRoutes struct {
	t       *testing.T
	handler http.Handler
	schema  *jsonschema.Schema
}

func newCanonicalRoutes(t *testing.T) canonicalRoutes {
	t.Helper()
	canonical := service.CanonicalEntityService{Repository: repository.NewCanonicalRepository()}
	routes := canonicalRoutes{t: t, handler: New(Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: adminPrincipal()}, Canonical: canonical})}
	// Behaviour is tested everywhere; conformance whenever a Shared checkout
	// is available (CI always sets SHARED_CONTRACTS_DIR).
	if os.Getenv("SHARED_CONTRACTS_DIR") != "" {
		routes.schema = contracttest.CompileSchema(t, contracttest.SharedDir(t), "control-plane/v1/canonical-entity.schema.json#/$defs/CanonicalEntity")
	}
	return routes
}

func (c canonicalRoutes) do(method, path, ifMatch, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	request.Header.Set("Authorization", "Bearer admin-token")
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	return response
}

// entity asserts a CanonicalEntity response: status, ETag, and conformance.
func (c canonicalRoutes) entity(response *httptest.ResponseRecorder, status int, etag string) map[string]any {
	c.t.Helper()
	if response.Code != status || response.Header().Get("ETag") != etag {
		c.t.Fatalf("got %d with ETag %q, want %d with %q: %s", response.Code, response.Header().Get("ETag"), status, etag, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		c.t.Fatal(err)
	}
	if c.schema != nil {
		contracttest.ValidateJSON(c.t, c.schema, body)
	}
	return body
}

func (c canonicalRoutes) refused(response *httptest.ResponseRecorder, status int, code string) {
	c.t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != status || body.Code != code {
		c.t.Fatalf("got %d %s, want %d %s: %s", response.Code, body.Code, status, code, response.Body.String())
	}
}

// TestCanonicalEntityRoutesFollowTheContract drives the Canonical registry
// operations of Shared's control-plane/v1 OpenAPI: a registration always
// starts DRAFT under a Control Plane identifier, and each lifecycle command
// needs the current version in If-Match (ADR-BCP-022 sections 123-130).
func TestCanonicalEntityRoutesFollowTheContract(t *testing.T) {
	c := newCanonicalRoutes(t)

	created := c.do(http.MethodPost, "/v1/canonical-entities", "", canonicalCreateBody)
	entity := c.entity(created, http.StatusCreated, `"1"`)
	id, _ := entity["id"].(string)
	if !domain.IsUUID(id) || entity["status"] != "DRAFT" || entity["version"] != float64(1) || entity["display_name"] != "Thamani Global Supplies" {
		t.Fatalf("registration must start DRAFT at version 1 under a minted id: %v", entity)
	}
	if created.Header().Get("Location") != "/v1/canonical-entities/"+id {
		t.Fatalf("Location %q", created.Header().Get("Location"))
	}
	path := "/v1/canonical-entities/" + id
	c.entity(c.do(http.MethodGet, path, "", ""), http.StatusOK, `"1"`)

	c.refused(c.do(http.MethodPost, path+"/validate", "", ""), http.StatusPreconditionRequired, "IF_MATCH_REQUIRED")
	c.refused(c.do(http.MethodPost, path+"/validate", `W/"1"`, ""), http.StatusBadRequest, "INVALID_IF_MATCH")
	if validated := c.entity(c.do(http.MethodPost, path+"/validate", `"1"`, ""), http.StatusOK, `"2"`); validated["status"] != "VALIDATED" {
		t.Fatalf("validate: %v", validated)
	}
	// A stale version is reported as such even though the transition would
	// also be refused; the current version with a prohibited transition is
	// a lifecycle conflict.
	c.refused(c.do(http.MethodPost, path+"/validate", `"1"`, ""), http.StatusPreconditionFailed, "CANONICAL_ENTITY_VERSION_MISMATCH")
	c.refused(c.do(http.MethodPost, path+"/validate", `"2"`, ""), http.StatusConflict, "CANONICAL_ENTITY_LIFECYCLE_CONFLICT")
	if active := c.entity(c.do(http.MethodPost, path+"/activate", `"2"`, ""), http.StatusOK, `"3"`); active["status"] != "ACTIVE" {
		t.Fatalf("activate: %v", active)
	}
	if retired := c.entity(c.do(http.MethodPost, path+"/retire", `"3"`, ""), http.StatusOK, `"4"`); retired["status"] != "RETIRED" {
		t.Fatalf("retire: %v", retired)
	}

	unknown := "/v1/canonical-entities/" + domain.NewUUIDv7()
	c.refused(c.do(http.MethodGet, unknown, "", ""), http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND")
	c.refused(c.do(http.MethodPost, unknown+"/activate", `"1"`, ""), http.StatusNotFound, "CANONICAL_ENTITY_NOT_FOUND")
}

// TestCanonicalEntityRegistrationRefusesServerOwnedFields: a registration
// cannot choose its identifier, lifecycle state, version or timestamps, so it
// cannot skip DRAFT -> VALIDATED -> ACTIVE, which ADR-BCP-016 context
// resolution relies on before trusting an organisation_id.
func TestCanonicalEntityRegistrationRefusesServerOwnedFields(t *testing.T) {
	c := newCanonicalRoutes(t)
	base := strings.TrimSuffix(canonicalCreateBody, "}")
	for name, extra := range map[string]string{
		"identifier": `,"id":"0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b"`,
		"status":     `,"status":"ACTIVE"`,
		"version":    `,"version":7`,
		"created_at": `,"created_at":"2026-09-26T09:00:00Z"`,
		"metadata":   `,"metadata":{"source":"import"}`,
	} {
		t.Run(name, func(t *testing.T) {
			c.refused(c.do(http.MethodPost, "/v1/canonical-entities", "", base+extra+"}"), http.StatusBadRequest, "INVALID_REQUEST")
		})
	}
	for name, body := range map[string]string{
		"legacy tenant id":      strings.Replace(canonicalCreateBody, "tn_01k4thamani", "tenant-123", 1),
		"key without namespace": strings.Replace(canonicalCreateBody, "supplier:thamani_global:sup_01k4p8q2r3s4", "Supplier", 1),
		"lower-case kind":       strings.Replace(canonicalCreateBody, "SUPPLIER_ORGANISATION", "supplier", 1),
		"unknown class":         strings.Replace(canonicalCreateBody, "TENANT_CONFIDENTIAL", "SECRET", 1),
	} {
		t.Run(name, func(t *testing.T) {
			c.refused(c.do(http.MethodPost, "/v1/canonical-entities", "", body), http.StatusBadRequest, "VALIDATION_FAILED")
		})
	}
	window := base + `,"effective_from":"2026-09-26T09:00:00Z","effective_to":"2026-09-25T09:00:00Z"}`
	c.refused(c.do(http.MethodPost, "/v1/canonical-entities", "", window), http.StatusUnprocessableEntity, "VALIDATION_FAILED")
}
