package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// TestDeploymentObservationRoutes drives the ADR-BCP-025 gate ER-04 routes.
// Only a registered, ACTIVE reporter workload holding deployment:observe
// submits, and only for the environment and regions its registry entry
// names; anything else is refused and stores nothing. The request carries no
// field the Control Plane assigns. Administrators read under topology:read;
// no workload reads. Every response conforms to the Shared contract.
func TestDeploymentObservationRoutes(t *testing.T) {
	ctx, admin, repo := activationDatabase(t)
	short := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine := "baobab-apiobserve" + short
	var instance string
	cleanup := func() {
		if instance != "" {
			if tx, err := admin.Begin(ctx); err == nil {
				tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
				tx.Exec(ctx, `DELETE FROM topology.deployment_observation WHERE engine_instance_key = $1`, instance)
				tx.Commit(ctx)
			}
		}
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	var engineID string
	mustNoError(t, admin.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) RETURNING engine_id::text`, engine).Scan(&engineID))
	mustNoError(t, admin.QueryRow(ctx, `INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_key`, engineID).Scan(&instance))

	registry := filepath.Join(t.TempDir(), "workload-registry.yaml")
	mustNoError(t, os.WriteFile(registry, []byte(`
schema: {name: baobab-platform-workload-registry, version: "1.0"}
workloads:
  staging-deploy-controller:
    environment: staging
    allowed_scopes: ["deployment:observe"]
    deployment_regions: [af-south-1, eu-west-1]
    status: ACTIVE
  suspended-deploy-controller:
    environment: staging
    allowed_scopes: ["deployment:observe"]
    deployment_regions: [af-south-1]
    status: SUSPENDED
  no-regions-controller:
    environment: staging
    allowed_scopes: ["deployment:observe"]
    status: ACTIVE
  plain-workload:
    environment: staging
    allowed_scopes: ["context:resolve"]
    status: ACTIVE
`), 0o600))
	workloads, err := auth.LoadWorkloadRegistryFile(registry)
	mustNoError(t, err)

	identities := repository.NewInMemoryRepository()
	reader := func() auth.Principal {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: "reader-" + short, Status: "ACTIVE"}))
		return auth.Principal{Subject: "reader-" + short, Issuer: testRealm, ActorType: "human", TokenID: "do-reader",
			Scopes: map[string]struct{}{"topology:read": {}, "topology:write": {}}, Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}()
	workload := func(client string, scopes ...string) auth.Principal {
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: client, Issuer: testRealm, ActorType: "workload", ClientID: client, TokenID: "do-" + client, Scopes: set}
	}
	workloadTokens := tokenVerifier{
		"reporter":  workload("staging-deploy-controller", "deployment:observe"),
		"suspended": workload("suspended-deploy-controller", "deployment:observe"),
		"noregions": workload("no-regions-controller", "deployment:observe"),
		"plain":     workload("plain-workload", "context:resolve"),
		"unlisted":  workload("unlisted-controller", "deployment:observe"),
		"readerwl":  workload("staging-deploy-controller", "desired-release:read"),
	}
	adminTokens := tokenVerifier{"admin": reader, "noscope": auth.Principal{Subject: "x", Issuer: testRealm, ActorType: "human", TokenID: "x", Scopes: map[string]struct{}{}}}
	build := func(withRegistry bool) activationHarness {
		deps := Dependencies{Store: &fakeStore{}, Identities: identities, DeploymentObservations: repo, AdminVerifier: adminTokens, WorkloadVerifier: workloadTokens}
		if withRegistry {
			deps.WorkloadRegistry = workloads
		}
		return activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(deps)}
	}
	h := build(true)

	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:" + strings.Repeat("a", 64)
	body := func(mutate func(map[string]any)) map[string]any {
		m := map[string]any{"engine_instance_id": instance, "environment": "staging", "region": "af-south-1",
			"artifacts":   []map[string]any{{"digest": digest, "platform": "linux/amd64"}},
			"observed_at": now.Format(time.RFC3339), "expires_at": now.Add(10 * time.Minute).Format(time.RFC3339)}
		if mutate != nil {
			mutate(m)
		}
		return m
	}
	stored := func() int {
		var n int
		mustNoError(t, admin.QueryRow(ctx, `SELECT count(*) FROM topology.deployment_observation WHERE engine_instance_key = $1`, instance).Scan(&n))
		return n
	}
	schema := func(def string) *jsonschema.Schema {
		return contracttest.CompileSchema(t, h.dir, "topology/v1/deployment-observation.schema.json#/$defs/"+def)
	}
	conforms := func(def string, w *bytes.Buffer) {
		var v any
		mustNoError(t, json.Unmarshal(w.Bytes(), &v))
		contracttest.ValidateJSON(t, schema(def), v)
	}
	const submit = "/v1/deployment-observations"

	// A registered reporter, inside its registration.
	ok := h.call(http.MethodPost, submit, "reporter", nil, body(nil))
	h.expect(ok, http.StatusCreated, "", "registered reporter")
	conforms("DeploymentObservation", ok.Body)
	var recorded release.Observation
	mustNoError(t, json.Unmarshal(ok.Body.Bytes(), &recorded))
	if recorded.Source != "workload:staging-deploy-controller" || recorded.IngestionSequence < 1 || !strings.HasPrefix(recorded.ObservationID, "dob_") {
		t.Fatalf("the Control Plane must assign id, sequence and source: %+v", recorded)
	}
	// Its other registered region is also accepted.
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["region"] = "eu-west-1" })), http.StatusCreated, "", "second registered region")
	accepted := stored()

	// Refusals: each stores nothing.
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["region"] = "us-east-1" })),
		http.StatusForbidden, "DEPLOYMENT_OBSERVATION_OUT_OF_SCOPE", "unregistered region")
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["environment"] = "production" })),
		http.StatusForbidden, "DEPLOYMENT_OBSERVATION_OUT_OF_SCOPE", "unregistered environment")
	h.expect(h.call(http.MethodPost, submit, "suspended", nil, body(nil)), http.StatusForbidden, "AUTHORIZATION_DENIED", "suspended reporter")
	h.expect(h.call(http.MethodPost, submit, "unlisted", nil, body(nil)), http.StatusForbidden, "AUTHORIZATION_DENIED", "reporter absent from the registry")
	h.expect(h.call(http.MethodPost, submit, "noregions", nil, body(nil)), http.StatusForbidden, "DEPLOYMENT_OBSERVATION_OUT_OF_SCOPE", "reporter with no region")
	h.expect(h.call(http.MethodPost, submit, "plain", nil, body(nil)), http.StatusForbidden, "AUTHORIZATION_DENIED", "workload without deployment:observe")
	h.expect(h.call(http.MethodPost, submit, "admin", nil, body(nil)), http.StatusUnauthorized, "", "an administrator is not a reporter")
	h.expect(h.call(http.MethodPost, submit, "", nil, body(nil)), http.StatusUnauthorized, "", "no token")
	h.expect(build(false).call(http.MethodPost, submit, "reporter", nil, body(nil)), http.StatusForbidden, "DEPLOYMENT_OBSERVATION_OUT_OF_SCOPE", "no registry configured")
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["engine_instance_id"] = "ei_doesnotexist" + short })),
		http.StatusNotFound, "DEPLOYMENT_OBSERVATION_INSTANCE_UNKNOWN", "unknown instance")
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["expires_at"] = now.Add(5 * time.Second).Format(time.RFC3339) })),
		http.StatusUnprocessableEntity, "DEPLOYMENT_OBSERVATION_WINDOW_INVALID", "window below policy")
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["expires_at"] = now.Add(-time.Minute).Format(time.RFC3339) })),
		http.StatusUnprocessableEntity, "DEPLOYMENT_OBSERVATION_WINDOW_INVALID", "window ends before it starts")
	for _, field := range []string{"recorded_at", "ingestion_sequence", "source", "observation_id"} {
		w := h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m[field] = "x" }))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("a request naming %s must be refused (400), got %d %s", field, w.Code, w.Body.String())
		}
	}
	h.expect(h.call(http.MethodPost, submit, "reporter", nil, body(func(m map[string]any) { m["artifacts"] = []map[string]any{{"digest": "latest"}} })),
		http.StatusBadRequest, "", "a tag is not an artifact identity")
	if after := stored(); after != accepted {
		t.Fatalf("a refused observation was stored: %d -> %d", accepted, after)
	}

	// Reads are administrators', under topology:read.
	list := "/v1/engine-instances/" + instance + "/deployment-observations"
	page := h.call(http.MethodGet, list, "admin", nil, nil)
	h.expect(page, http.StatusOK, "", "list observations")
	conforms("DeploymentObservationPage", page.Body)
	var got release.ObservationPage
	mustNoError(t, json.Unmarshal(page.Body.Bytes(), &got))
	if len(got.Items) != accepted {
		t.Fatalf("listed %d observations, stored %d", len(got.Items), accepted)
	}
	observed := h.call(http.MethodGet, "/v1/engine-instances/"+instance+"/observed-release", "admin", nil, nil)
	h.expect(observed, http.StatusOK, "", "observed release")
	conforms("ObservedRelease", observed.Body)
	var state release.ObservedRelease
	mustNoError(t, json.Unmarshal(observed.Body.Bytes(), &state))
	if state.State != release.StateUnknownArtifact {
		t.Fatalf("a digest no release owns must be UNKNOWN_ARTIFACT, got %+v", state)
	}
	for _, path := range []string{list, "/v1/engine-instances/" + instance + "/observed-release"} {
		h.expect(h.call(http.MethodGet, path, "noscope", nil, nil), http.StatusForbidden, "", "read without topology:read")
		h.expect(h.call(http.MethodGet, path, "readerwl", nil, nil), http.StatusUnauthorized, "", "a workload does not read observations")
	}
	h.expect(h.call(http.MethodGet, "/v1/engine-instances/ei_doesnotexist"+short+"/observed-release", "admin", nil, nil), http.StatusNotFound, "ENGINE_INSTANCE_NOT_FOUND", "unknown instance read")
	h.expect(h.call(http.MethodGet, list+"?page_token=garbage", "admin", nil, nil), http.StatusBadRequest, "", "malformed page token")
}
