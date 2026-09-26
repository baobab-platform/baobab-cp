package api

import (
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service/application"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	"github.com/baobab-platform/baobab-cp/internal/service/subscription"
)

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// operation is "METHOD /v1/path" with path parameters reduced to {}.
func operation(method, path string) string {
	return strings.ToUpper(method) + " " + pathParam.ReplaceAllString(path, "{}")
}

// fullRouter registers every optional route family. Handlers are never
// invoked, so the dependencies only need to be non-nil.
func fullRouter(t *testing.T) chi.Routes {
	t.Helper()
	h := New(Dependencies{
		IamOrganisations: struct {
			repository.IamOrganisationRepository
		}{},
		OrganisationAdmission: struct {
			repository.OrganisationAdmissionRepository
		}{},
		Counterparties: struct {
			repository.CounterpartyRepository
		}{},
		OrganisationObservability: struct {
			repository.OrganisationObservabilityRepository
		}{},
		PlatformAccounts: struct {
			repository.PlatformAccountRepository
		}{},
		ExternalReferences: struct {
			repository.ExternalReferenceRepository
		}{},
		Provisioning:    struct{ ProvisioningRepository }{},
		Onboarding:      &onboarding.Service{},
		Applications:    &application.Service{},
		Classifications: &subscription.Classifier{},
		Metrics:         metrics.NewRegistry(),
	})
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("New returned %T, not a chi router", h)
	}
	return routes
}

// implemented lists every operation the router serves under /v1.
func implemented(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := chi.Walk(fullRouter(t), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/v1/") {
			out[operation(method, route)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// described lists every operation the pinned Shared OpenAPI describes,
// under its /v1 server base.
func described(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := contracts.ReadEmbedded("control-plane/v1/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for path, item := range doc.Paths {
		for method := range item {
			switch method {
			case "get", "put", "post", "patch", "delete":
				out[operation(method, "/v1"+path)] = true
			}
		}
	}
	return out
}

// undescribed are served but not yet described by Shared's
// control-plane/v1 OpenAPI (CP Console FE-00 gap B2). The list may only
// shrink: describe a route in Shared and remove it here.
var undescribed = []string{
	"GET /v1/external-references",
	"GET /v1/tenants/{}/provisioning",
	"GET /v1/tenants/{}/provisioning/{}",
	"GET /v1/tenants/{}/provisioning/{}/drift",
	"GET /v1/tenants/{}/provisioning/{}/readiness",
	"POST /v1/canonical-entities/{}/external-references",
	"POST /v1/capabilities/resolve",
	"POST /v1/capabilities/resolve-batch",
	"POST /v1/platform-context/resolve",
	"POST /v1/resolve",
	"POST /v1/tenants/{}/provisioning",
	"POST /v1/tenants/{}/provisioning/{}/apply",
	"POST /v1/tenants/{}/provisioning/{}/cancel",
	"POST /v1/tenants/{}/provisioning/{}/retry",
}

// unimplemented are described by Shared but not served (FE-00 gap G2).
var unimplemented = []string{
	"GET /v1/mappings/{}",
	"GET /v1/markets/{}",
	"PATCH /v1/mappings/{}",
	"PATCH /v1/markets/{}",
	"POST /v1/mappings",
	"POST /v1/mappings/{}/activate",
	"POST /v1/mappings/{}/retire",
	"POST /v1/markets",
	"POST /v1/markets/{}/activate",
	"POST /v1/resolution/mappings",
}

// TestOpenAPIDescribesTheRouter: every /v1 route is described by the
// pinned Shared OpenAPI or listed as undescribed, every described
// operation is served or listed as unimplemented, and neither list holds a
// stale entry. A generated Console client therefore never calls a route
// that does not exist, and a new route cannot ship without a description.
func TestOpenAPIDescribesTheRouter(t *testing.T) {
	served, spec := implemented(t), described(t)
	var missing, phantom []string
	for op := range served {
		if !spec[op] && !slices.Contains(undescribed, op) {
			missing = append(missing, op)
		}
	}
	for op := range spec {
		if !served[op] && !slices.Contains(unimplemented, op) {
			phantom = append(phantom, op)
		}
	}
	sort.Strings(missing)
	sort.Strings(phantom)
	for _, op := range missing {
		t.Errorf("%s is served but not described by Shared control-plane/v1 openapi.yaml", op)
	}
	for _, op := range phantom {
		t.Errorf("%s is described by Shared but not served", op)
	}
	for _, op := range undescribed {
		if spec[op] {
			t.Errorf("%s is now described; remove it from undescribed", op)
		} else if !served[op] {
			t.Errorf("%s is listed as undescribed but not served", op)
		}
	}
	for _, op := range unimplemented {
		if served[op] {
			t.Errorf("%s is now served; remove it from unimplemented", op)
		} else if !spec[op] {
			t.Errorf("%s is listed as unimplemented but not described", op)
		}
	}
}
