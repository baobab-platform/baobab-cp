package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nabhold/baobab-cp/internal/auth"
	"github.com/nabhold/baobab-cp/internal/metrics"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// TestOrganisationObservabilityRoutesAreScoped covers ADR-BCP-018 gate
// ORG-15's HTTP authority: drift and audit lineage are platform-admin only,
// and /metrics serves only a workload holding metrics:read.
func TestOrganisationObservabilityRoutesAreScoped(t *testing.T) {
	registry := metrics.NewRegistry()
	registry.NewCounterVec("probe_total", "Probe.").Inc()
	deps := func(admin, workload auth.Principal) Dependencies {
		// A nil repository is enough: every refusal happens before it is used.
		return Dependencies{Store: &fakeStore{}, AdminVerifier: fakeVerifier{principal: admin}, WorkloadVerifier: fakeVerifier{principal: workload},
			OrganisationObservability: (*repository.PostgresRepository)(nil), Metrics: registry}
	}

	tenantAdmin := New(deps(tenantAdminPrincipal(), workloadPrincipal()))
	for _, path := range []string{"/v1/organisation-drift", "/v1/organisations/0190a1b2-c3d4-7e5f-8071-8293a4b5c6d1/audit"} {
		if response := adminRequest(t, tenantAdmin, http.MethodGet, path, ""); response.Code != http.StatusForbidden {
			t.Errorf("a tenant administrator must not read %s: %d", path, response.Code)
		}
	}
	platformAdmin := New(deps(adminPrincipal(), workloadPrincipal()))
	if response := adminRequest(t, platformAdmin, http.MethodGet, "/v1/organisation-drift?limit=0", ""); response.Code != http.StatusBadRequest {
		t.Errorf("an out-of-range page size must be refused before evaluation: %d", response.Code)
	}

	scrape := func(handler http.Handler) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := scrape(New(deps(adminPrincipal(), workloadPrincipal()))); w.Code != http.StatusForbidden {
		t.Errorf("a workload without metrics:read must not scrape: %d", w.Code)
	}
	scraper := workloadPrincipal()
	scraper.Scopes = map[string]struct{}{"metrics:read": {}}
	w := scrape(New(deps(adminPrincipal(), scraper)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "probe_total 1") {
		t.Fatalf("a scraper with metrics:read reads the exposition: %d %s", w.Code, w.Body.String())
	}
	unauthenticated := httptest.NewRecorder()
	New(deps(adminPrincipal(), scraper)).ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("an unauthenticated scrape must be refused: %d", unauthenticated.Code)
	}
}
