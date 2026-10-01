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

// TestDeploymentObservations covers ADR-BCP-025 gate ER-04. Observations are
// appended with a Control Plane-minted id, recorded_at and a strictly
// increasing ingestion_sequence; the table is append-only; a window outside
// the policy and an unknown instance are refused and nothing is stored; the
// current observation is the latest observed_at, then the highest sequence;
// and the observed release is derived from the digests: a release, MIXED,
// UNKNOWN_ARTIFACT, FOREIGN_ARTIFACT, or UNKNOWN once the current
// observation expires. Every record conforms to the Shared schemas.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestDeploymentObservations(t *testing.T) {
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
	engine, other := "baobab-observed"+suffix, "baobab-observedother"+suffix
	capabilityKey := "test.observed" + suffix + ".perform"
	var instances []string
	cleanup := func() {
		removeDeploymentObservations(ctx, admin, instances)
		removeEngineReleases(ctx, admin, engine)
		removeEngineReleases(ctx, admin, other)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code IN ($1, $2))`, engine, other)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code IN ($1, $2)`, engine, other)
	}
	cleanup()
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	engineIDs := map[string]string{}
	for _, code := range []string{engine, other} {
		var id string
		must(admin.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) RETURNING engine_id::text`, code).Scan(&id))
		engineIDs[code] = id
	}
	_, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "Observed test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL", Owner: engine, Source: "fixtures/observed-test",
		Digest: "sha256:" + strings.Repeat("8", 64)}})
	must(err)
	var instance string
	must(admin.QueryRow(ctx, `INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_key`, engineIDs[engine]).Scan(&instance))
	instances = append(instances, instance)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actor := AuditActor{ActorID: "prn_tool" + suffix, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	digests := map[string]string{}
	record := func(engineCode, version string) string {
		t.Helper()
		digest := "sha256:" + strings.Repeat(string(rune('a'+len(digests))), 56) + suffix[:8]
		rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: engineCode, ReleaseVersion: version,
			Artifacts:                           []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engineCode, Digest: digest}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: engineCode + ".engine", CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("e", 40),
			Reason: "Built from main."}, "workload:release-tooling", now, actor)
		must(err)
		digests[rel.ReleaseID] = digest
		return rel.ReleaseID
	}
	v1, v2, foreign := record(engine, "1.0.0"), record(engine, "1.1.0"), record(other, "9.0.0")

	submit := func(observedAt time.Time, ttl time.Duration, ds ...string) (release.Observation, error) {
		req := release.ObservationSubmission{EngineInstanceID: instance, Environment: "staging", Region: "af-south-1",
			ObservedAt: observedAt, ExpiresAt: observedAt.Add(ttl)}
		for _, d := range ds {
			req.Artifacts = append(req.Artifacts, release.ObservedArtifact{Digest: d, Platform: "linux/amd64"})
		}
		return repo.RecordDeploymentObservation(ctx, req, "workload:deploy-controller", now)
	}
	observationSchema := contracts.MustSchema("topology/v1/deployment-observation.schema.json#/$defs/DeploymentObservation")
	observedSchema := contracts.MustSchema("topology/v1/deployment-observation.schema.json#/$defs/ObservedRelease")
	pageSchema := contracts.MustSchema("topology/v1/deployment-observation.schema.json#/$defs/DeploymentObservationPage")
	conforms := func(label string, schema *contracts.Schema, v any) {
		t.Helper()
		if err := contracts.ValidateValue(schema, v); err != nil {
			t.Fatalf("%s does not conform: %v", label, err)
		}
	}
	observed := func(at time.Time) release.ObservedRelease {
		t.Helper()
		got, err := repo.GetObservedRelease(ctx, instance, at)
		must(err)
		conforms("observed release", observedSchema, got)
		return got
	}

	// No observation: UNKNOWN, with no observation_id.
	if got := observed(now); got.State != release.StateUnknown || got.ObservationID != "" {
		t.Fatalf("before any observation: %+v", got)
	}

	// An accepted observation: minted id, CP-assigned recorded_at and sequence.
	first, err := submit(now.Add(-time.Minute), 10*time.Minute, digests[v1])
	must(err)
	conforms("recorded observation", observationSchema, first)
	if !strings.HasPrefix(first.ObservationID, "dob_") || first.IngestionSequence < 1 || !first.RecordedAt.Equal(now) ||
		first.Source != "workload:deploy-controller" {
		t.Fatalf("recorded observation: %+v", first)
	}
	if got := observed(now); got.State != release.StateRelease || got.ReleaseID != v1 || got.ObservationID != first.ObservationID {
		t.Fatalf("one release: %+v want %s", got, v1)
	}

	// Sequences strictly increase, and at equal observed_at the later one is current.
	second, err := submit(now.Add(-time.Minute), 10*time.Minute, digests[v2])
	must(err)
	if second.IngestionSequence <= first.IngestionSequence {
		t.Fatalf("ingestion_sequence did not increase: %d then %d", first.IngestionSequence, second.IngestionSequence)
	}
	if got := observed(now); got.ReleaseID != v2 || got.ObservationID != second.ObservationID {
		t.Fatalf("tie not broken by ingestion_sequence: %+v", got)
	}
	// A later-accepted observation of an earlier moment is not current.
	if _, err := submit(now.Add(-time.Hour), 3*time.Hour/2, digests[v1]); err == nil {
		t.Fatal("a 90-minute time-to-live is outside the policy and must be refused")
	}
	stale, err := submit(now.Add(-5*time.Minute), 10*time.Minute, digests[v1])
	must(err)
	if got := observed(now); got.ObservationID != second.ObservationID {
		t.Fatalf("a stale observation accepted later became current: %+v (stale %s)", got, stale.ObservationID)
	}

	// Rolling upgrade: both digests, one observation.
	mixed, err := submit(now, 10*time.Minute, digests[v1], digests[v2])
	must(err)
	if got := observed(now); got.State != release.StateMixed || len(got.ReleaseIDs) != 2 || got.ObservationID != mixed.ObservationID {
		t.Fatalf("mixed: %+v", got)
	}
	// Unrecorded and foreign artifacts.
	unknown, err := submit(now.Add(time.Second), 10*time.Minute, digests[v1], "sha256:"+strings.Repeat("0", 64))
	must(err)
	if got := observed(now.Add(time.Second)); got.State != release.StateUnknownArtifact || got.ObservationID != unknown.ObservationID {
		t.Fatalf("unknown artifact: %+v", got)
	}
	if _, err := submit(now.Add(2*time.Second), 10*time.Minute, digests[foreign]); err != nil {
		t.Fatal(err)
	}
	if got := observed(now.Add(2 * time.Second)); got.State != release.StateForeignArtifact {
		t.Fatalf("foreign artifact: %+v", got)
	}
	// Expiry: UNKNOWN, and an older live observation does not stand in.
	if got := observed(now.Add(2*time.Second + 10*time.Minute)); got.State != release.StateUnknown {
		t.Fatalf("expired: %+v", got)
	}
	// Future-dated: UNKNOWN until its time.
	future, err := submit(now.Add(time.Hour), 10*time.Minute, digests[v2])
	must(err)
	if got := observed(now); got.State != release.StateUnknown {
		t.Fatalf("future-dated newest must be UNKNOWN, got %+v", got)
	}
	if got := observed(now.Add(time.Hour)); got.ReleaseID != v2 || got.ObservationID != future.ObservationID {
		t.Fatalf("future observation once due: %+v", got)
	}

	// Refusals store nothing.
	var count int
	count = observationCount(t, ctx, admin, instance)
	for name, window := range map[string]time.Duration{"empty": 0, "below policy": 30 * time.Second, "above policy": 2 * time.Hour} {
		_, err := submit(now, window, digests[v1])
		var invalid *release.ErrInvalid
		if !errors.As(err, &invalid) || invalid.Code != release.ReasonObservationWindowInvalid {
			t.Fatalf("%s window: %v", name, err)
		}
	}
	_, err = repo.RecordDeploymentObservation(ctx, release.ObservationSubmission{EngineInstanceID: "ei_doesnotexist" + suffix,
		Artifacts: []release.ObservedArtifact{{Digest: digests[v1]}}, Environment: "staging", Region: "af-south-1",
		ObservedAt: now, ExpiresAt: now.Add(time.Minute * 5)}, "workload:deploy-controller", now)
	var invalid *release.ErrInvalid
	if !errors.As(err, &invalid) || invalid.Code != release.ReasonObservationInstanceUnknown {
		t.Fatalf("unknown instance: %v", err)
	}
	if after := observationCount(t, ctx, admin, instance); after != count {
		t.Fatalf("a refused observation was stored: %d -> %d", count, after)
	}
	if _, err := repo.GetObservedRelease(ctx, "ei_doesnotexist"+suffix, now); !errors.Is(err, ErrEngineInstanceNotFound) {
		t.Fatalf("observed release of an unknown instance: %v", err)
	}

	// A location mismatch is stored as reported, never an update to the instance.
	moved := release.ObservationSubmission{EngineInstanceID: instance, Environment: "production", Region: "eu-west-1",
		Artifacts: []release.ObservedArtifact{{Digest: digests[v1]}}, ObservedAt: now.Add(3 * time.Hour), ExpiresAt: now.Add(3*time.Hour + 5*time.Minute)}
	stored, err := repo.RecordDeploymentObservation(ctx, moved, "workload:deploy-controller", now)
	must(err)
	if stored.Environment != "production" || stored.Region != "eu-west-1" {
		t.Fatalf("location was not stored as reported: %+v", stored)
	}
	var env, region string
	must(admin.QueryRow(ctx, `SELECT environment, region FROM topology.engine_instance WHERE engine_instance_key = $1`, instance).Scan(&env, &region))
	if env != "staging" || region != "af-south-1" {
		t.Fatalf("an observation changed the instance: %s %s", env, region)
	}

	// Append-only: the database refuses update, delete and truncate.
	for _, sql := range []string{
		`UPDATE topology.deployment_observation SET source = 'x' WHERE observation_key = '` + first.ObservationID + `'`,
		`DELETE FROM topology.deployment_observation WHERE observation_key = '` + first.ObservationID + `'`,
		`TRUNCATE topology.deployment_observation`,
	} {
		if _, err := admin.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%q must be refused as append-only, got %v", sql, err)
		}
	}

	// Listing: newest first, paged, complete, conforming.
	var listed []release.Observation
	token := ""
	for {
		items, next, err := repo.ListDeploymentObservations(ctx, instance, token, 2)
		must(err)
		listed = append(listed, items...)
		page := release.ObservationPage{Items: items}
		if next != "" {
			page.NextCursor = &next
		}
		conforms("observation page", pageSchema, page)
		if next == "" {
			break
		}
		token = next
	}
	if len(listed) != observationCount(t, ctx, admin, instance) {
		t.Fatalf("listing returned %d observations", len(listed))
	}
	for i := 1; i < len(listed); i++ {
		if release.Newer(listed[i], listed[i-1]) {
			t.Fatalf("listing is not newest first at %d: %s before %s", i, listed[i-1].ObservationID, listed[i].ObservationID)
		}
	}
	if _, _, err := repo.ListDeploymentObservations(ctx, instance, "garbage", 2); !errors.Is(err, ErrObservationMalformedPageToken) {
		t.Fatalf("malformed page token: %v", err)
	}
	if _, _, err := repo.ListDeploymentObservations(ctx, "ei_doesnotexist"+suffix, "", 2); !errors.Is(err, ErrEngineInstanceNotFound) {
		t.Fatalf("listing an unknown instance: %v", err)
	}
}

func observationCount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, instance string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM topology.deployment_observation WHERE engine_instance_key = $1`, instance).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// removeDeploymentObservations deletes test observations. The table is
// append-only, so cleanup switches the triggers off for its own transaction,
// as removeEngineReleases does for releases.
func removeDeploymentObservations(ctx context.Context, admin *pgxpool.Pool, instances []string) {
	tx, err := admin.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // cleanup
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		return
	}
	for _, id := range instances {
		tx.Exec(ctx, `DELETE FROM topology.deployment_observation WHERE engine_instance_key = $1`, id)
	}
	tx.Commit(ctx) //nolint:errcheck // cleanup
}
