package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// TestEngineReleaseRoutes drives the ADR-BCP-025 gate ER-02 routes: release
// tooling records under engine-release:record (201, then 200 for a
// byte-identical replay), a conflicting version and a digest another
// release owns are 409 with their registered codes, another engine's
// provider is 422, the scopes are enforced, and an administrator reads and
// lists releases. Every response conforms to the Shared contract.
func TestEngineReleaseRoutes(t *testing.T) {
	ctx, admin, repo := activationDatabase(t)
	short := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine := "baobab-apirelease" + short
	capabilityKey := "test.apirelease" + short + ".perform"
	cleanup := func() {
		tx, err := admin.Begin(ctx)
		if err == nil {
			// Releases are never deleted in operation; this test-only
			// cleanup turns the immutability triggers off for itself.
			tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
			for _, stmt := range []string{
				`DELETE FROM topology.engine_release_artifact WHERE engine_release_id IN (SELECT r.engine_release_id FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`,
				`DELETE FROM topology.engine_release_provider_support WHERE engine_release_id IN (SELECT r.engine_release_id FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`,
				`DELETE FROM topology.engine_release WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`,
			} {
				tx.Exec(ctx, stmt, engine)
			}
			tx.Commit(ctx)
		}
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, engine+".engine")
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	_, err := admin.Exec(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1)`, engine)
	mustNoError(t, err)
	_, err = admin.Exec(ctx, `INSERT INTO capability.capability_provider (provider_key, name, provider_type, engine_id, status)
		SELECT $1, $1, 'BAOBAB_ENGINE', engine_id, 'DRAFT' FROM topology.engine WHERE code = $2`, engine+".engine", engine)
	mustNoError(t, err)
	_, err = repo.SyncCapabilityCatalogue(ctx, []repository.CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "API release test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/api-release-test", Digest: "sha256:" + strings.Repeat("6", 64)}})
	mustNoError(t, err)

	identities := repository.NewInMemoryRepository()
	human := func(subject string, scopes ...string) auth.Principal {
		p := domain.Principal{ID: domain.NewPrincipalID(), ActorType: "human", Status: "ACTIVE"}
		mustNoError(t, identities.CreateIdentity(ctx, p))
		mustNoError(t, identities.LinkExternalIdentity(ctx, domain.ExternalIdentity{ID: domain.NewExternalIdentityID(),
			PrincipalID: p.ID, Issuer: testRealm, Subject: subject + "-" + short, Status: "ACTIVE"}))
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: subject + "-" + short, Issuer: testRealm, ActorType: "human", TokenID: "er-" + subject, Scopes: set,
			Roles: map[string]struct{}{RolePlatformAdmin: {}}}
	}
	workload := func(client string, scopes ...string) auth.Principal {
		set := map[string]struct{}{}
		for _, s := range scopes {
			set[s] = struct{}{}
		}
		return auth.Principal{Subject: client, Issuer: testRealm, ActorType: "workload", ClientID: client, TokenID: "er-" + client, Scopes: set}
	}
	h := activationHarness{t: t, dir: contracttest.SharedDir(t), handler: New(Dependencies{Store: &fakeStore{}, Identities: identities,
		EngineReleases: repo,
		AdminVerifier: tokenVerifier{
			"operator": human("operator", "topology:read", "topology:write"),
			"reader":   human("reader", "topology:read"),
		},
		WorkloadVerifier: tokenVerifier{
			"tooling":  workload("release-tooling", "engine-release:record"),
			"stranger": workload("other-workload", "context:resolve"),
		}})}

	digest := func(seed string) string { return "sha256:" + strings.Repeat(seed, 52) + short }
	body := func(version, seed, provider string) map[string]any {
		return map[string]any{"engine_id": engine, "release_version": version,
			"artifacts":                              []map[string]any{{"artifact_type": "OCI_IMAGE", "repository": "ghcr.io/baobab-platform/" + engine, "digest": digest(seed)}},
			"provider_support":                       []map[string]any{{"provider_key": provider, "capability_key": capabilityKey, "contract_versions": []int{1}}},
			"capability_provider_declaration_digest": "sha256:" + strings.Repeat("d", 64), "source_revision": strings.Repeat("a", 40),
			"reason": "Built from main."}
	}
	post := func(who string, b map[string]any) *httptest.ResponseRecorder {
		return h.call(http.MethodPost, "/v1/engine-releases", who, nil, b)
	}
	conforms := func(definition string, w *httptest.ResponseRecorder) {
		t.Helper()
		var v any
		mustNoError(t, json.Unmarshal(w.Body.Bytes(), &v))
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, h.dir, "topology/v1/release.schema.json#/$defs/"+definition), v)
	}

	created := post("tooling", body("1.0.0", "a", engine+".engine"))
	h.expect(created, http.StatusCreated, "", "record")
	conforms("EngineRelease", created)
	var rel release.Release
	mustNoError(t, json.Unmarshal(created.Body.Bytes(), &rel))
	if rel.Status != release.StatusCandidate || rel.RecordedBy != "workload:release-tooling" || created.Header().Get("Location") != "/v1/engine-releases/"+rel.ReleaseID {
		t.Fatalf("recorded: %+v %s", rel, created.Header().Get("Location"))
	}
	h.expect(post("tooling", body("1.0.0", "a", engine+".engine")), http.StatusOK, rel.ReleaseID, "replay")
	changed := body("1.0.0", "a", engine+".engine")
	changed["source_revision"] = strings.Repeat("b", 40)
	h.expect(post("tooling", changed), http.StatusConflict, release.ReasonVersionConflict, "other content")
	h.expect(post("tooling", body("1.1.0", "a", engine+".engine")), http.StatusConflict, release.ReasonDigestConflict, "an owned digest")
	h.expect(post("tooling", body("1.1.0", "b", "baobab-elsewhere.engine")), http.StatusUnprocessableEntity, release.ReasonProviderNotOwned, "a foreign provider")
	tagOnly := body("1.1.0", "b", engine+".engine")
	tagOnly["artifacts"] = []map[string]any{{"artifact_type": "OCI_IMAGE", "repository": "ghcr.io/baobab-platform/" + engine, "display_tag": "1.1.0"}}
	h.expect(post("tooling", tagOnly), http.StatusBadRequest, "VALIDATION_FAILED", "a tag-only artifact")
	supplied := body("1.1.0", "b", engine+".engine")
	supplied["status"] = "APPROVED"
	h.expect(post("tooling", supplied), http.StatusBadRequest, "VALIDATION_FAILED", "a caller-supplied status")
	h.expect(post("stranger", body("1.1.0", "b", engine+".engine")), http.StatusForbidden, "AUTHORIZATION_DENIED", "a workload without engine-release:record")
	h.expect(post("reader", body("1.1.0", "b", engine+".engine")), http.StatusForbidden, "AUTHORIZATION_DENIED", "an administrator without topology:write")

	// An administrator records too, under their own principal.
	byOperator := post("operator", body("1.1.0", "b", engine+".engine"))
	h.expect(byOperator, http.StatusCreated, "", "record by an administrator")
	var second release.Release
	mustNoError(t, json.Unmarshal(byOperator.Body.Bytes(), &second))
	if second.RecordedBy == "" || strings.HasPrefix(second.RecordedBy, "workload:") {
		t.Fatalf("an administrator's release must name their principal, not a workload: %q", second.RecordedBy)
	}

	got := h.call(http.MethodGet, "/v1/engine-releases/"+rel.ReleaseID, "reader", nil, nil)
	h.expect(got, http.StatusOK, rel.ReleaseID, "get")
	conforms("EngineRelease", got)
	h.expect(h.call(http.MethodGet, "/v1/engine-releases/erl_00000000000000000000000000000000", "reader", nil, nil), http.StatusNotFound,
		"ENGINE_RELEASE_NOT_FOUND", "an unknown release")
	list := h.call(http.MethodGet, "/v1/engine-releases?engine_id="+engine+"&status=CANDIDATE", "reader", nil, nil)
	h.expect(list, http.StatusOK, second.ReleaseID, "list")
	conforms("EngineReleasePage", list)
	h.expect(h.call(http.MethodGet, "/v1/engine-releases?status=LIVE", "reader", nil, nil), http.StatusBadRequest, "VALIDATION_FAILED", "an unknown status")
	h.expect(h.call(http.MethodGet, "/v1/engine-releases", "tooling", nil, nil), http.StatusUnauthorized, "", "a workload reading releases")
}
