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

// TestReleaseDrift covers ADR-BCP-025 gate ER-05's drift. Drift opens only
// after its grace period, measured from when the condition first held (or
// from when the release became desired); opening, or changing reason, publishes
// one event through the outbox and a re-evaluation publishes none; a
// converged instance has no drift; a tenant sees the drift of the instances
// its active bindings name and nobody else's; and nothing here touches
// resolution. Events conform to the Shared schema.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestReleaseDrift(t *testing.T) {
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
	engine := "baobab-drift" + suffix
	capabilityKey := "test.drift" + suffix + ".perform"
	tenantBound, tenantOther := "tn_drift"+suffix[:8], "tn_driftother"+suffix[:8]
	var instances []string
	cleanup := func() {
		removeDeploymentObservations(ctx, admin, instances)
		for _, id := range instances {
			admin.Exec(ctx, `DELETE FROM messaging.outbox WHERE aggregate_id = $1`, id)
			admin.Exec(ctx, `DELETE FROM topology.engine_instance_release_drift WHERE engine_instance_key = $1`, id)
		}
		admin.Exec(ctx, `DELETE FROM capability.capability_binding WHERE engine_instance_id IN
			(SELECT engine_instance_id FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1))`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability_scope WHERE tenant_id IN ($1, $2)`, tenantBound, tenantOther)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, engine+".engine")
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
		Name: "Drift test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/drift-test", Digest: "sha256:" + strings.Repeat("7", 64)}})
	must(err)
	var instance, instanceID string
	must(admin.QueryRow(ctx, `INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE') RETURNING engine_instance_key, engine_instance_id::text`, engineID).Scan(&instance, &instanceID))
	instances = append(instances, instance)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
	at := func(d time.Duration) time.Time { return base.Add(d) }
	actor := AuditActor{ActorID: "prn_tool" + suffix, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	digests := map[string]string{}
	record := func(version string) string {
		t.Helper()
		digest := "sha256:" + strings.Repeat(string(rune('a'+len(digests))), 56) + suffix[:8]
		rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: engine, ReleaseVersion: version,
			Artifacts:                           []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine, Digest: digest}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: engine + ".engine", CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("e", 40),
			Reason: "Built from main."}, "workload:release-tooling", base, actor)
		must(err)
		digests[rel.ReleaseID] = digest
		_, err = admin.Exec(ctx, `UPDATE topology.engine_release SET status = 'APPROVED', status_changed_by = 'prn_approver', status_changed_at = $2,
			status_reason = 'Approved for the test.' WHERE release_key = $1`, rel.ReleaseID, base)
		must(err)
		return rel.ReleaseID
	}
	v1, v2 := record("1.0.0"), record("1.1.0")
	desire := func(releaseKey string, when time.Time) {
		t.Helper()
		_, err := admin.Exec(ctx, `UPDATE topology.engine_instance SET desired_release_id = (SELECT engine_release_id FROM topology.engine_release WHERE release_key = $1),
			desired_release_updated_at = $2 WHERE engine_instance_key = $3`, releaseKey, when, instance)
		must(err)
	}
	observe := func(observedAt time.Time, env, region string, ds ...string) {
		t.Helper()
		req := release.ObservationSubmission{EngineInstanceID: instance, Environment: env, Region: region, ObservedAt: observedAt, ExpiresAt: observedAt.Add(time.Hour)}
		for _, d := range ds {
			req.Artifacts = append(req.Artifacts, release.ObservedArtifact{Digest: d})
		}
		_, err := repo.RecordDeploymentObservation(ctx, req, "workload:deploy-controller", observedAt)
		must(err)
	}
	evaluate := func(when time.Time) {
		t.Helper()
		must(repo.EvaluateReleaseDrift(ctx, instance, when))
	}
	state := func() (reason string, open bool, found bool) {
		t.Helper()
		var opened *time.Time
		err := admin.QueryRow(ctx, `SELECT reason_code, opened_at FROM topology.engine_instance_release_drift WHERE engine_instance_key = $1`, instance).Scan(&reason, &opened)
		if err != nil {
			return "", false, false
		}
		return reason, opened != nil, true
	}
	driftEvents := func() []map[string]any {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT payload FROM messaging.outbox WHERE aggregate_id = $1 AND event_type = $2 ORDER BY occurred_at, aggregate_version`, instance, EventReleaseDriftDetected)
		must(err)
		defer rows.Close()
		var out []map[string]any
		for rows.Next() {
			var raw []byte
			must(rows.Scan(&raw))
			var env map[string]any
			must(json.Unmarshal(raw, &env))
			out = append(out, env)
		}
		return out
	}
	eventSchema := contracts.MustSchema("topology/v1/events.schema.json#/$defs/EngineInstanceReleaseDriftDetected")
	expectState := func(label, wantReason string, wantOpen bool) {
		t.Helper()
		reason, open, found := state()
		if wantReason == "" {
			if found {
				t.Fatalf("%s: want no drift, got %s open=%v", label, reason, open)
			}
			return
		}
		if !found || reason != wantReason || open != wantOpen {
			t.Fatalf("%s: got %q open=%v found=%v, want %s open=%v", label, reason, open, found, wantReason, wantOpen)
		}
	}

	// Nothing desired, nothing observed: no drift.
	evaluate(at(0))
	expectState("nothing desired", "", false)

	// Desired but never observed: a condition within its 15-minute grace is
	// not yet drift; after it, it is, and announced once.
	desire(v1, at(0))
	evaluate(at(time.Minute))
	expectState("unobserved, within grace", release.DriftReleaseUnobserved, false)
	evaluate(at(14 * time.Minute))
	expectState("unobserved, still within grace", release.DriftReleaseUnobserved, false)
	if n := len(driftEvents()); n != 0 {
		t.Fatalf("%d events before drift opened", n)
	}
	evaluate(at(16 * time.Minute))
	expectState("unobserved, past grace", release.DriftReleaseUnobserved, true)
	events := driftEvents()
	if len(events) != 1 {
		t.Fatalf("opening drift must publish one event, got %d", len(events))
	}
	evaluate(at(17 * time.Minute))
	evaluate(at(18 * time.Minute))
	if n := len(driftEvents()); n != 1 {
		t.Fatalf("a re-evaluation must not publish again, got %d events", n)
	}
	data, _ := events[0]["data"].(map[string]any)
	must(contracts.ValidateValue(eventSchema, data))
	if events[0]["type"] != EventReleaseDriftDetected || events[0]["baobabscope"] != "platform" || events[0]["subject"] != instance ||
		data["drift_reason"] != release.DriftReleaseUnobserved || data["observed_state"] != release.StateUnknown || data["desired_release_id"] != v1 {
		t.Fatalf("event: %v", events[0])
	}

	// Observed running the desired release: converged, drift gone.
	observe(at(19*time.Minute), "staging", "af-south-1", digests[v1])
	expectState("converged", "", false)

	// A newly desired release is mid-rollout until its grace (30 minutes)
	// passes, counted from when it became desired.
	desire(v2, at(20*time.Minute))
	evaluate(at(21 * time.Minute))
	expectState("new desired release, within grace", release.DriftReleaseMismatch, false)
	evaluate(at(49 * time.Minute))
	expectState("new desired release, still within grace", release.DriftReleaseMismatch, false)
	evaluate(at(51 * time.Minute))
	expectState("new desired release, past grace", release.DriftReleaseMismatch, true)
	if n := len(driftEvents()); n != 2 {
		t.Fatalf("want 2 events, got %d", n)
	}

	// A critical reason has no grace and replaces the open one: one event.
	observe(at(52*time.Minute), "staging", "af-south-1", "sha256:"+strings.Repeat("0", 64))
	expectState("unrecorded artifact", release.DriftUnknownArtifactRunning, true)
	if n := len(driftEvents()); n != 3 {
		t.Fatalf("a change of reason must publish, got %d events", n)
	}
	observe(at(53*time.Minute), "production", "eu-west-1", digests[v2])
	expectState("wrong location", release.DriftDeploymentLocationMismatch, true)
	if n := len(driftEvents()); n != 4 {
		t.Fatalf("want 4 events, got %d", n)
	}

	// Running a revoked release is critical even when the instance
	// desires it. (The database refuses revoking a desired release; revoke
	// a release the instance only runs.)
	desire(v1, at(54*time.Minute))
	_, err = admin.Exec(ctx, `UPDATE topology.engine_release SET status = 'REVOKED', status_changed_by = 'prn_approver', status_changed_at = $2,
		status_reason = 'Revoked for the test.' WHERE release_key = $1`, v2, at(55*time.Minute))
	must(err)
	observe(at(56*time.Minute), "staging", "af-south-1", digests[v2])
	expectState("revoked release", release.DriftRevokedReleaseRunning, true)
	if n := len(driftEvents()); n != 5 {
		t.Fatalf("want 5 events, got %d", n)
	}
	for i, e := range driftEvents() {
		must(contracts.ValidateValue(eventSchema, e["data"]))
		if i == 0 {
			continue
		}
	}

	// Back to the desired release: it converges, and drift opening again
	// announces again.
	observe(at(57*time.Minute), "staging", "af-south-1", digests[v1])
	expectState("converged again", "", false)
	observe(at(58*time.Minute), "staging", "af-south-1", "sha256:"+strings.Repeat("1", 64))
	expectState("unrecorded artifact again", release.DriftUnknownArtifactRunning, true)
	if n := len(driftEvents()); n != 6 {
		t.Fatalf("reopening drift must announce again, got %d events", n)
	}

	// The sweep re-evaluates and clears what converged without a new
	// observation: once the last observation expires nothing is known.
	swept, err := repo.SweepReleaseDrift(ctx, at(58*time.Minute))
	must(err)
	if swept < 1 {
		t.Fatalf("sweep evaluated %d instances", swept)
	}
	expectState("sweep, still drifting", release.DriftUnknownArtifactRunning, true)
	if n := len(driftEvents()); n != 6 {
		t.Fatalf("a sweep must not republish, got %d events", n)
	}

	// A tenant sees the open drift of the instances its ACTIVE bindings
	// name, and only those.
	var capabilityID string
	must(admin.QueryRow(ctx, `SELECT capability_id::text FROM capability.capability WHERE code = $1`, capabilityKey).Scan(&capabilityID))
	var scopeID string
	must(admin.QueryRow(ctx, `INSERT INTO capability.capability_scope(tenant_id) VALUES ($1) RETURNING scope_id::text`, tenantBound).Scan(&scopeID))
	var providerID string
	must(admin.QueryRow(ctx, `INSERT INTO capability.capability_provider(provider_key, name, provider_type, engine_id, status)
		VALUES ($1, $1, 'BAOBAB_ENGINE', $2::uuid, 'ACTIVE') RETURNING provider_id::text`, engine+".engine", engineID).Scan(&providerID))
	_, err = admin.Exec(ctx, `INSERT INTO capability.capability_binding(capability_id, engine_instance_id, scope_id, binding_mode, status, contract_version, effective_from, provider_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'PRIMARY', 'ACTIVE', '1', now() - interval '1 day', $4::uuid)`, capabilityID, instanceID, scopeID, providerID)
	must(err)
	bound, err := repo.ListReleaseDrift(ctx, tenantBound)
	must(err)
	if len(bound) != 1 || bound[0].EngineInstanceID != instance || bound[0].Reason != release.DriftUnknownArtifactRunning || bound[0].Severity != "CRITICAL" {
		t.Fatalf("bound tenant's drift: %+v", bound)
	}
	if other, err := repo.ListReleaseDrift(ctx, tenantOther); err != nil || len(other) != 0 {
		t.Fatalf("an unbound tenant must see no drift: %+v %v", other, err)
	}
	_, err = admin.Exec(ctx, `UPDATE capability.capability_binding SET effective_to = now() - interval '1 hour' WHERE engine_instance_id = $1::uuid`, instanceID)
	must(err)
	if ended, err := repo.ListReleaseDrift(ctx, tenantBound); err != nil || len(ended) != 0 {
		t.Fatalf("an ended binding must not count: %+v %v", ended, err)
	}

	// The topology gauges describe it, with only the labels and names
	// Shared allows (topologyMetric, topologyMetricLabel).
	families, err := TopologyMetricsCollector{Repo: repo, Now: func() time.Time { return at(58 * time.Minute) }}.Collect(ctx)
	must(err)
	schemaDir := os.Getenv("SHARED_CONTRACTS_DIR")
	allowedNames, allowedLabels := map[string]bool{}, map[string]bool{}
	if schemaDir != "" {
		raw, err := os.ReadFile(schemaDir + "/contracts/topology/v1/domain.schema.json")
		must(err)
		var doc struct {
			Defs map[string]struct {
				Enum []string `json:"enum"`
			} `json:"$defs"`
		}
		must(json.Unmarshal(raw, &doc))
		for _, n := range doc.Defs["topologyMetric"].Enum {
			allowedNames[n] = true
		}
		for _, l := range doc.Defs["topologyMetricLabel"].Enum {
			allowedLabels[l] = true
		}
	}
	drifting := 0.0
	for _, f := range families {
		if schemaDir != "" && !allowedNames[f.Name] {
			t.Errorf("metric %s is not in Shared's topologyMetric", f.Name)
		}
		for _, sample := range f.Samples {
			for label := range sample.Labels {
				if schemaDir != "" && !allowedLabels[label] {
					t.Errorf("%s carries label %s, which Shared does not allow", f.Name, label)
				}
			}
			if f.Name == "engine_instance_release_drift_total" && sample.Labels["drift_reason"] == release.DriftUnknownArtifactRunning {
				drifting = sample.Value
			}
		}
	}
	if len(families) != 4 || drifting < 1 {
		t.Fatalf("topology metrics: %d families, %v instances drifting with an unrecorded artifact", len(families), drifting)
	}
}
