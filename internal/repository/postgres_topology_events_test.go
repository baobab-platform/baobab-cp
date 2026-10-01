package repository

import (
	"context"
	"encoding/json"
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

// TestTopologyEvents covers ADR-BCP-025 section 2.10: a recorded release,
// a release changing status and an instance's desired release changing are
// each published once through the outbox in the change's own transaction, as
// platform-scoped events that conform to the Shared payload schemas. A replay
// publishes nothing. A revocation publishes the status change together with
// the desired-release change of every instance it moved, and a cleared
// desired release is null.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestTopologyEvents(t *testing.T) {
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
	engine := "baobab-events" + suffix
	capabilityKey := "test.events" + suffix + ".perform"
	var aggregates []string
	cleanup := func() {
		for _, id := range aggregates {
			admin.Exec(ctx, `DELETE FROM messaging.outbox WHERE aggregate_id = $1`, id)
		}
		admin.Exec(ctx, `UPDATE topology.engine_instance SET desired_release_id = NULL WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		removeEngineReleases(ctx, admin, engine)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var engineID string
	must(admin.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) RETURNING engine_id::text`, engine).Scan(&engineID))
	_, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "Events test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/events-test", Digest: "sha256:" + strings.Repeat("6", 64)}})
	must(err)
	var instance string
	must(admin.QueryRow(ctx, `INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_key`, engineID).Scan(&instance))
	aggregates = append(aggregates, instance)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actor := AuditActor{ActorID: "prn_tool" + suffix, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	seed := 0
	recordRelease := func(version string) (release.Release, bool) {
		t.Helper()
		seed++
		req := release.RecordRequest{EngineID: engine, ReleaseVersion: version,
			Artifacts:                           []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine, Digest: "sha256:" + strings.Repeat(string(rune('a'+seed)), 56) + suffix[:8]}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: engine + ".engine", CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("e", 40), Reason: "Built from main."}
		rel, replay, err := repo.RecordEngineRelease(ctx, req, "workload:release-tooling", now, actor)
		must(err)
		if !replay {
			aggregates = append(aggregates, rel.ReleaseID)
		}
		return rel, replay
	}
	approve := func(key string) {
		t.Helper()
		_, err := admin.Exec(ctx, `UPDATE topology.engine_release SET status = 'APPROVED', status_changed_by = 'prn_approver', status_changed_at = $2,
			status_reason = 'Approved for the test.' WHERE release_key = $1`, key, now)
		must(err)
	}
	type published struct {
		Type, Scope, Subject string
		Data                 map[string]any
	}
	eventsOf := func(aggregate, eventType string) []published {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT payload FROM messaging.outbox WHERE aggregate_id = $1 AND event_type = $2 ORDER BY occurred_at, aggregate_version`, aggregate, eventType)
		must(err)
		defer rows.Close()
		var out []published
		for rows.Next() {
			var raw []byte
			must(rows.Scan(&raw))
			var env struct {
				Type        string         `json:"type"`
				Subject     string         `json:"subject"`
				BaobabScope string         `json:"baobabscope"`
				Data        map[string]any `json:"data"`
			}
			must(json.Unmarshal(raw, &env))
			out = append(out, published{env.Type, env.BaobabScope, env.Subject, env.Data})
		}
		return out
	}
	conform := func(def string, p published) {
		t.Helper()
		must(contracts.ValidateValue(contracts.MustSchema("topology/v1/events.schema.json#/$defs/"+def), p.Data))
		if p.Scope != "platform" {
			t.Fatalf("%s must be platform-scoped, got %q", p.Type, p.Scope)
		}
	}

	// Recording publishes once; a byte-identical replay publishes nothing.
	v1, replay := recordRelease("1.0.0")
	if replay {
		t.Fatal("first record was a replay")
	}
	recorded := eventsOf(v1.ReleaseID, EventEngineReleaseRecorded)
	if len(recorded) != 1 || recorded[0].Subject != v1.ReleaseID || recorded[0].Data["status"] != "CANDIDATE" || recorded[0].Data["engine_id"] != engine {
		t.Fatalf("recorded event: %+v", recorded)
	}
	conform("EngineReleaseRecorded", recorded[0])
	seed--
	if again, replayed := recordRelease("1.0.0"); !replayed || again.ReleaseID != v1.ReleaseID {
		t.Fatalf("replay: %v %+v", replayed, again)
	}
	if n := len(eventsOf(v1.ReleaseID, EventEngineReleaseRecorded)); n != 1 {
		t.Fatalf("a replay published %d recorded events", n)
	}

	// Desiring a release publishes the change with its artifacts.
	v2, _ := recordRelease("1.1.0")
	approve(v1.ReleaseID)
	approve(v2.ReleaseID)
	tx, err := repo.pool.Begin(ctx)
	must(err)
	ok, err := setDesiredRelease(ctx, tx, repo.eventSource(), instance, v1.ReleaseID, 1, "cs_events"+suffix, now)
	must(err)
	if !ok {
		t.Fatal("setDesiredRelease refused a current version")
	}
	must(tx.Commit(ctx))
	desired := eventsOf(instance, EventDesiredReleaseChanged)
	if len(desired) != 1 {
		t.Fatalf("desired-release events: %+v", desired)
	}
	d := desired[0].Data
	artifacts, _ := d["desired_artifacts"].([]any)
	if d["previous_release_id"] != nil || d["desired_release_id"] != v1.ReleaseID || d["desired_release_version"] != "1.0.0" || len(artifacts) != 1 || d["version"] != float64(2) {
		t.Fatalf("desired-release event: %+v", d)
	}
	conform("EngineInstanceDesiredReleaseChanged", desired[0])

	// A stale plan changes nothing and publishes nothing.
	tx, err = repo.pool.Begin(ctx)
	must(err)
	stale, err := setDesiredRelease(ctx, tx, repo.eventSource(), instance, v2.ReleaseID, 1, "", now)
	must(err)
	must(tx.Rollback(ctx))
	if stale || len(eventsOf(instance, EventDesiredReleaseChanged)) != 1 {
		t.Fatal("a stale desired-release change must not apply or publish")
	}

	// Revoking the desired release replaces it: the desired-release change of
	// the instance it moved and the status change are both published.
	_, err = repo.ChangeEngineReleaseStatus(ctx, v1.ReleaseID, release.StatusChangeRequest{TargetStatus: release.StatusRevoked,
		Reason: "Withdrawn.", DesiredReleaseDispositions: []release.Disposition{{EngineInstanceID: instance, Action: release.DispositionReplace, ReplacementReleaseID: v2.ReleaseID}}},
		"prn_admin"+suffix, now, actor)
	must(err)
	status := eventsOf(v1.ReleaseID, EventEngineReleaseStatusChanged)
	if len(status) != 1 || status[0].Data["previous_status"] != "APPROVED" || status[0].Data["status"] != "REVOKED" {
		t.Fatalf("status-changed event: %+v", status)
	}
	conform("EngineReleaseStatusChanged", status[0])
	desired = eventsOf(instance, EventDesiredReleaseChanged)
	if len(desired) != 2 || desired[1].Data["previous_release_id"] != v1.ReleaseID || desired[1].Data["desired_release_id"] != v2.ReleaseID {
		t.Fatalf("revocation's desired-release event: %+v", desired)
	}
	conform("EngineInstanceDesiredReleaseChanged", desired[1])

	// Clearing the desired release publishes null and no artifacts.
	tx, err = repo.pool.Begin(ctx)
	must(err)
	cleared, err := setDesiredRelease(ctx, tx, repo.eventSource(), instance, "", 3, "", now.Add(time.Second))
	must(err)
	if !cleared {
		t.Fatal("clearing was refused")
	}
	must(tx.Commit(ctx))
	desired = eventsOf(instance, EventDesiredReleaseChanged)
	if len(desired) != 3 || desired[2].Data["desired_release_id"] != nil || desired[2].Data["previous_release_id"] != v2.ReleaseID {
		t.Fatalf("clear event: %+v", desired)
	}
	if _, has := desired[2].Data["desired_artifacts"]; has {
		t.Fatal("a cleared desired release carries no artifacts")
	}
	conform("EngineInstanceDesiredReleaseChanged", desired[2])
}
