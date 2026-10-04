package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

type mp2cWorkloadRegistry struct {
	active   map[string]bool
	scopes   map[string]map[string]bool
	observer map[string]auth.ReporterScope
	reporter map[string]auth.ReporterScope
}

func (r *mp2cWorkloadRegistry) IsActive(clientID string) bool {
	return r != nil && r.active[clientID]
}
func (r *mp2cWorkloadRegistry) AllowsScope(clientID, scope string) bool {
	return r != nil && r.active[clientID] && r.scopes[clientID][scope]
}
func (r *mp2cWorkloadRegistry) IdentityRuntimeObserver(clientID string) (auth.ReporterScope, bool) {
	scope, ok := r.observer[clientID]
	return scope, ok && r.active[clientID]
}
func (r *mp2cWorkloadRegistry) Reporter(clientID string) (auth.ReporterScope, bool) {
	scope, ok := r.reporter[clientID]
	return scope, ok && r.active[clientID]
}

type runtimeProfileRepoStub struct {
	calls       int
	profile     repository.IdentityRuntimeProfile
	source      string
	environment string
	regions     []string
	replay      bool
	err         error
	snapshot    repository.FederationPlatformSnapshot
	readErr     error
	readCalls   int
	readFacet   string
	readConfig  string
	onRead      func()
}

func (s *runtimeProfileRepoStub) RecordIdentityRuntimeProfile(
	_ context.Context,
	profile repository.IdentityRuntimeProfile,
	source string,
	environment string,
	regions []string,
	_ time.Time,
) (bool, error) {
	s.calls++
	s.profile = profile
	s.source = source
	s.environment = environment
	s.regions = append([]string(nil), regions...)
	return s.replay, s.err
}

func (s *runtimeProfileRepoStub) ReadFederationPlatformSnapshot(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	runtimeCapability string,
	configurationReference string,
	_ string,
	_ time.Time,
) (repository.FederationPlatformSnapshot, error) {
	s.readCalls++
	s.readFacet = runtimeCapability
	s.readConfig = configurationReference
	if s.onRead != nil {
		s.onRead()
	}
	return s.snapshot, s.readErr
}

func mp2cRuntimeProfileBody(t *testing.T, now time.Time) string {
	t.Helper()
	digest := "sha256:" + strings.Repeat("a", 64)
	profile := repository.IdentityRuntimeProfile{
		ProviderID:              "provider_aaaaaaaa",
		EngineInstanceID:        "ei_aaaaaaaa",
		ConfigurationReference:  "ref_config",
		SecurityDomainReference: "ref_domain",
		ArtifactDigest:          digest,
		Revision:                1,
		PublishedAt:             now,
		CapabilityObservations: []repository.IdentityRuntimeCapabilityObservation{{
			Capability:         "OIDC_FEDERATION",
			VerificationStatus: "VERIFIED",
			Evidence: &repository.IdentityRuntimeEvidence{
				EvidenceReference: "ref_support",
				ArtifactDigest:    digest,
				ObservedAt:        now.Add(-time.Minute),
				ExpiresAt:         now.Add(time.Hour),
			},
		}},
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestIdentityRuntimeProfileRouteRequiresTokenAndRegistryScope(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		registryScope bool
		active        bool
		wantStatus    int
		wantCalls     int
	}{
		{name: "registered", registryScope: true, active: true, wantStatus: http.StatusCreated, wantCalls: 1},
		{name: "scope only in token", registryScope: false, active: true, wantStatus: http.StatusForbidden},
		{name: "inactive registry workload", registryScope: true, active: false, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &runtimeProfileRepoStub{}
			scopes := map[string]map[string]bool{"baobab-deployment-controller-staging": {}}
			if tc.registryScope {
				scopes["baobab-deployment-controller-staging"][auth.IdentityRuntimeObserveScope] = true
			}
			registry := &mp2cWorkloadRegistry{
				active: map[string]bool{"baobab-deployment-controller-staging": tc.active},
				scopes: scopes,
				observer: map[string]auth.ReporterScope{
					"baobab-deployment-controller-staging": {Environment: "staging", Regions: []string{"af-south-1"}},
				},
			}
			principal := auth.Principal{
				Issuer:    "https://issuer.test",
				Subject:   "runtime-observer",
				ActorType: "workload",
				ClientID:  "baobab-deployment-controller-staging",
				Scopes:    map[string]struct{}{auth.IdentityRuntimeObserveScope: {}},
			}
			handler := New(Dependencies{
				Store:                   &fakeStore{},
				WorkloadVerifier:        tokenVerifier{"caller": principal},
				WorkloadRegistry:        registry,
				IdentityRuntimeProfiles: repo,
				Environment:             "staging",
			})
			req := httptest.NewRequest(http.MethodPost, "/internal/identity-runtime/v1/profiles", strings.NewReader(mp2cRuntimeProfileBody(t, now)))
			req.Header.Set("Authorization", "Bearer caller")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", w.Code, w.Body.String(), tc.wantStatus)
			}
			if repo.calls != tc.wantCalls {
				t.Fatalf("repository calls = %d, want %d", repo.calls, tc.wantCalls)
			}
			if tc.wantCalls == 1 {
				if repo.source != "workload:baobab-deployment-controller-staging" || repo.environment != "staging" ||
					len(repo.regions) != 1 || repo.regions[0] != "af-south-1" {
					t.Fatalf("wrong reporter provenance: source=%q environment=%q regions=%v", repo.source, repo.environment, repo.regions)
				}
			}
		})
	}
}
