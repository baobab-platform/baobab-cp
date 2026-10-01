package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// removeEngineReleases deletes a test engine's releases. Releases are never
// deleted in operation (migration 000082 refuses it), so this test-only
// cleanup turns triggers off for its own transaction.
func removeEngineReleases(ctx context.Context, admin *pgxpool.Pool, engine string) {
	tx, err := admin.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // cleanup
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		return
	}
	for _, table := range []string{"engine_release_artifact", "engine_release_provider_support"} {
		tx.Exec(ctx, `DELETE FROM topology.`+table+` WHERE engine_release_id IN (SELECT r.engine_release_id
			FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id WHERE e.code = $1)`, engine)
	}
	tx.Exec(ctx, `DELETE FROM topology.engine_release WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
	tx.Commit(ctx) //nolint:errcheck // cleanup
}

// TestEngineReleaseRecord covers ADR-BCP-025 gate ER-02 on PostgreSQL:
// recording a CANDIDATE release, a byte-identical replay, every refusal
// (version conflict, digest owned by another release, a provider of another
// engine, an uncatalogued capability or contract major, an unknown
// engine), reading and listing with filters and pages, and the database's
// own immutability: identity never changes, status moves only along the
// release lifecycle, and nothing is deleted. Every release conforms to the
// Shared contract.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestEngineReleaseRecord(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	engine, other := "baobab-release"+suffix, "baobab-otherrelease"+suffix
	capabilityKey := "test.release" + suffix + ".perform"
	cleanup := func() {
		removeEngineReleases(ctx, admin, engine)
		removeEngineReleases(ctx, admin, other)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code IN ($1, $2)`, engine, other)
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, code := range []string{engine, other} {
		if _, err := admin.Exec(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1)`, code); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "Release test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/release-test", Digest: "sha256:" + strings.Repeat("5", 64)}}); err != nil {
		t.Fatal(err)
	}
	schema := contracts.MustSchema("topology/v1/release.schema.json#/$defs/EngineRelease")
	pageSchema := contracts.MustSchema("topology/v1/release.schema.json#/$defs/EngineReleasePage")
	actor := AuditActor{ActorID: "release-tooling", ActorType: "workload", ClientID: "release-tooling", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	digest := func(seed string) string { return "sha256:" + strings.Repeat(seed, 52) + suffix }
	// Two artifacts, each with its own digest seed.
	request := func(version, amd64, arm64 string) release.RecordRequest {
		return release.RecordRequest{EngineID: engine, ReleaseVersion: version,
			Artifacts: []release.Artifact{
				{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine, Digest: digest(amd64), Platform: "linux/amd64", DisplayTag: version},
				{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine, Digest: digest(arm64), Platform: "linux/arm64"},
			},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: engine + ".engine", CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("c", 40),
			Provenance: &release.Provenance{AttestationURI: "oci://ghcr.io/baobab-platform/" + engine + "@" + digest("9"),
				AttestationDigest: digest("9"), BuilderID: "https://github.com/baobab-platform/shared/.github/workflows/release.yml"},
			Reason: "Built from main."}
	}
	first := request("1.0.0", "a", "b")

	recorded, replay, err := repo.RecordEngineRelease(ctx, first, "workload:release-tooling", now, actor)
	if err != nil || replay {
		t.Fatalf("record: %v replay=%v", err, replay)
	}
	if recorded.Status != release.StatusCandidate || !release.ValidID(recorded.ReleaseID) || recorded.RecordedBy != "workload:release-tooling" ||
		len(recorded.Artifacts) != 2 || recorded.Artifacts[1].Platform != "linux/arm64" || recorded.Provenance == nil {
		t.Fatalf("recorded: %+v", recorded)
	}
	if err := contracts.ValidateValue(schema, recorded); err != nil {
		t.Fatalf("the release does not conform: %v", err)
	}

	// A byte-identical replay, even with another reason, returns it.
	again := first
	again.Reason = "Retried by the pipeline."
	replayed, replay, err := repo.RecordEngineRelease(ctx, again, "workload:release-tooling", now.Add(time.Minute), actor)
	if err != nil || !replay || replayed.ReleaseID != recorded.ReleaseID || replayed.Reason != first.Reason {
		t.Fatalf("replay: %+v %v %v", replayed, replay, err)
	}

	// Refusals.
	changed := request("1.0.0", "a", "b")
	changed.SourceRevision = strings.Repeat("f", 40)
	if _, _, err := repo.RecordEngineRelease(ctx, changed, "workload:release-tooling", now, actor); !errors.Is(err, ErrEngineReleaseVersionConflict) {
		t.Fatalf("other content for a recorded version: %v", err)
	}
	if _, _, err := repo.RecordEngineRelease(ctx, request("1.1.0", "a", "e"), "workload:release-tooling", now, actor); !errors.Is(err, ErrEngineReleaseDigestConflict) {
		t.Fatalf("a digest another release owns: %v", err)
	}
	foreign := request("1.2.0", "c", "e")
	foreign.ProviderSupport[0].ProviderKey = other + ".engine"
	var invalid *release.ErrInvalid
	if _, _, err := repo.RecordEngineRelease(ctx, foreign, "workload:release-tooling", now, actor); !errors.As(err, &invalid) || invalid.Code != release.ReasonProviderNotOwned {
		t.Fatalf("another engine's provider: %v", err)
	}
	uncatalogued := request("1.2.0", "c", "e")
	uncatalogued.ProviderSupport[0].ContractVersions = []int{1, 2}
	if _, _, err := repo.RecordEngineRelease(ctx, uncatalogued, "workload:release-tooling", now, actor); !errors.As(err, &invalid) || invalid.Code != release.ReasonCapabilityNotCatalogue {
		t.Fatalf("an uncatalogued contract major: %v", err)
	}
	unknown := request("1.2.0", "c", "e")
	unknown.EngineID = "baobab-neverregistered" + suffix
	unknown.ProviderSupport = nil
	if _, _, err := repo.RecordEngineRelease(ctx, unknown, "workload:release-tooling", now, actor); !errors.Is(err, ErrEngineReleaseEngineUnknown) {
		t.Fatalf("an unregistered engine: %v", err)
	}
	duplicate := request("1.2.0", "c", "c")
	if _, _, err := repo.RecordEngineRelease(ctx, duplicate, "workload:release-tooling", now, actor); !errors.Is(err, release.ErrDuplicateDigest) {
		t.Fatalf("one digest listed twice: %v", err)
	}
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM topology.engine_release r JOIN topology.engine e ON e.engine_id = r.engine_id
		WHERE e.code = $1`, engine).Scan(&count); err != nil || count != 1 {
		t.Fatalf("refused records left releases behind: %d %v", count, err)
	}

	// Read and list, filtered and paged.
	second, _, err := repo.RecordEngineRelease(ctx, request("1.1.0", "c", "e"), "workload:release-tooling", now.Add(time.Second), actor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetEngineRelease(ctx, recorded.ReleaseID)
	if err != nil || got.ReleaseID != recorded.ReleaseID || got.ProviderSupport[0].ContractVersions[0] != 1 {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := repo.GetEngineRelease(ctx, "erl_00000000000000000000000000000000"); !errors.Is(err, ErrEngineReleaseNotFound) {
		t.Fatalf("an unknown release: %v", err)
	}
	page, next, err := repo.ListEngineReleases(ctx, EngineReleaseFilter{EngineID: engine, Limit: 1})
	if err != nil || len(page) != 1 || page[0].ReleaseID != second.ReleaseID || next == "" {
		t.Fatalf("first page: %+v %q %v", page, next, err)
	}
	if err := contracts.ValidateValue(pageSchema, release.Page{Items: page, NextCursor: &next}); err != nil {
		t.Fatalf("the page does not conform: %v", err)
	}
	page, next, err = repo.ListEngineReleases(ctx, EngineReleaseFilter{EngineID: engine, Limit: 1, PageToken: next})
	if err != nil || len(page) != 1 || page[0].ReleaseID != recorded.ReleaseID || next != "" {
		t.Fatalf("second page: %+v %q %v", page, next, err)
	}
	if page, _, err := repo.ListEngineReleases(ctx, EngineReleaseFilter{EngineID: engine, Status: release.StatusApproved}); err != nil || len(page) != 0 {
		t.Fatalf("approved releases: %+v %v", page, err)
	}
	if _, _, err := repo.ListEngineReleases(ctx, EngineReleaseFilter{PageToken: "garbage"}); !errors.Is(err, ErrEngineReleaseMalformedPageToken) {
		t.Fatalf("a malformed page token: %v", err)
	}

	// The database keeps releases immutable whatever the caller.
	refused := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("the database accepted %s", label)
		}
	}
	refused("a changed version", `UPDATE topology.engine_release SET release_version = '9.9.9' WHERE release_key = $1`, recorded.ReleaseID)
	refused("a deleted release", `DELETE FROM topology.engine_release WHERE release_key = $1`, recorded.ReleaseID)
	refused("a changed artifact", `UPDATE topology.engine_release_artifact SET repository = 'ghcr.io/x/y' WHERE digest = $1`, first.Artifacts[0].Digest)
	refused("a deleted artifact", `DELETE FROM topology.engine_release_artifact WHERE digest = $1`, first.Artifacts[0].Digest)
	refused("CANDIDATE to DEPRECATED", `UPDATE topology.engine_release SET status = 'DEPRECATED', status_changed_by = 'p', status_changed_at = now(),
		status_reason = 'Old.' WHERE release_key = $1`, recorded.ReleaseID)
	if _, err := admin.Exec(ctx, `UPDATE topology.engine_release SET status = 'REVOKED', status_changed_by = 'prn_x', status_changed_at = now(),
		status_reason = 'Withdrawn.' WHERE release_key = $1`, second.ReleaseID); err != nil {
		t.Fatalf("revoking a candidate: %v", err)
	}
	refused("leaving REVOKED", `UPDATE topology.engine_release SET status = 'APPROVED' WHERE release_key = $1`, second.ReleaseID)
}
