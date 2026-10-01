package repository

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// TestProviderActivationChangeset covers EA-02D end to end, its engine
// releases approved through ENGINE_RELEASE_APPROVAL: a registered
// provider is DRAFT and carries its canonical provider_ id; a
// PROVIDER_ACTIVATION plan runs every plan check and blocks on each that
// fails (no recorded engine release, a production instance for a provider
// not permitted there, an unhealthy instance); once nothing blocks it the
// plan is approved by someone else and applied, the provider is ACTIVE,
// re-registering leaves it ACTIVE, and activating it again is a state
// conflict. Every record conforms to the Shared contract.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestProviderActivationChangeset(t *testing.T) {
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
	engine := "baobab-activation" + suffix
	providerKey := engine + ".engine"
	capabilityKey := "test.activation" + suffix + ".perform"
	var canonical string
	var releases []string
	cleanup := func() {
		for _, id := range releases {
			removeChangesetsFor(ctx, admin, id)
		}
		if canonical != "" {
			admin.Exec(ctx, `DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, canonical)
			admin.Exec(ctx, `DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, canonical)
			admin.Exec(ctx, `UPDATE changeset.changeset SET current_plan_id = NULL WHERE target_id = $1`, canonical)
			admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE subject_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, canonical)
			admin.Exec(ctx, `DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, canonical)
			admin.Exec(ctx, `DELETE FROM changeset.changeset WHERE target_id = $1`, canonical)
		}
		removeEngineReleases(ctx, admin, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key = $1`, providerKey)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code = $1)`, engine)
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code = $1`, engine)
	}
	cleanup()
	t.Cleanup(cleanup)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}

	// A catalogued capability and a registered provider.
	definition := capabilitydomain.Capability{Key: capabilityKey, Name: "Activation test", DomainKey: "test",
		Lifecycle: capabilitydomain.CapabilityLifecycleActive, Maturity: capabilitydomain.CapabilityMaturitySupported}
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: definition, ContractVersions: []int{1},
		DataClassification: "INTERNAL", Owner: engine, Source: "fixtures/activation-test", Digest: "sha256:" + strings.Repeat("3", 64)}}); err != nil {
		t.Fatal(err)
	}
	registration := EngineRegistrationRecord{Repository: engine, Capabilities: []capabilitydomain.Capability{definition},
		Provider: EngineRegistrationProvider{ProviderKey: providerKey, Name: "Activation test", ProviderType: "BAOBAB_ENGINE",
			EngineKey: "engine", Lifecycle: "DRAFT", Ownership: engine},
		Support: []EngineRegistrationSupport{{CapabilityKey: capabilityKey, ContractVersions: []int{1}}}}
	if err := repo.RegisterEngine(ctx, registration); err != nil {
		t.Fatal(err)
	}
	var providerUUID, status, engineID string
	if err := admin.QueryRow(ctx, `SELECT provider_id::text, canonical_provider_id, status, engine_id::text FROM capability.capability_provider
		WHERE provider_key = $1`, providerKey).Scan(&providerUUID, &canonical, &status, &engineID); err != nil {
		t.Fatal(err)
	}
	if status != "DRAFT" || canonical != domain.ProviderID(providerUUID) || !domain.ValidProviderID(canonical) {
		t.Fatalf("registered provider: %s %s (uuid %s)", status, canonical, providerUUID)
	}
	staging := domain.NewUUIDv7()
	exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status)
		VALUES ($1, $2, 'af-south-1', 'staging', 'ACTIVE')`, staging, engineID)

	recordSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/Changeset")
	planSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetPlan")
	outcomeSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangeOutcome")
	conforms := func(label string, schema *contracts.Schema, v any) {
		t.Helper()
		if err := contracts.ValidateValue(schema, v); err != nil {
			t.Fatalf("%s does not conform: %v", label, err)
		}
	}
	requester, approver := "prn_requester"+suffix, "prn_approver"+suffix
	actor := AuditActor{ActorID: requester, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	desired := changeset.DesiredChange{Kind: changeset.KindProviderActivation, ProviderID: canonical}
	draft := func(key string) changeset.Changeset {
		t.Helper()
		base, found, err := repo.TargetRevision(ctx, desired)
		if err != nil || !found {
			t.Fatalf("provider revision: %v %v", found, err)
		}
		c, err := changeset.Draft(changeset.CreateRequest{Title: "Activate provider", Reason: "Test.", DesiredChange: desired},
			domain.NewResourceID("cs"), requester, "API", actor.CorrelationID, base, now)
		if err != nil {
			t.Fatal(err)
		}
		created, err := repo.CreateChangeset(ctx, c, key, "hash-"+key, actor)
		if err != nil {
			t.Fatal(err)
		}
		conforms("draft", recordSchema, created)
		return created
	}
	submit := func(c changeset.Changeset, wantState string, wantCodes ...string) changeset.Changeset {
		t.Helper()
		c, err := repo.SubmitChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("plan"), now, actor)
		if err != nil {
			t.Fatal(err)
		}
		var codes []string
		for _, b := range c.BlockingReasons {
			codes = append(codes, b.Code)
		}
		if c.State != wantState || !slices.Equal(codes, wantCodes) {
			t.Fatalf("submit: state %s, blockers %v; want %s %v", c.State, codes, wantState, wantCodes)
		}
		conforms("submitted changeset", recordSchema, c)
		plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
		if err != nil {
			t.Fatal(err)
		}
		conforms("plan", planSchema, plan)
		if len(plan.ReadinessRequirements) != len(changeset.Kinds()[changeset.KindProviderActivation].PlanChecks) {
			t.Fatalf("the plan reports %d of the plan checks", len(plan.ReadinessRequirements))
		}
		return c
	}

	// Without a recorded engine release the plan is blocked, and only by it.
	c := draft("key-activate-" + suffix)
	c = submit(c, changeset.StateBlocked, "PROVIDER_NO_ELIGIBLE_RELEASE")

	// Every check that fails blocks, each with its own code.
	production := domain.NewUUIDv7()
	exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status)
		VALUES ($1, $2, 'af-south-1', 'production', 'PROVISIONING')`, production, engineID)
	exec(`INSERT INTO topology.health_observation (engine_instance_id, status, observed_at, expires_at, source, reasons)
		VALUES ($1, 'UNAVAILABLE', $2, $3, 'ACTIVE_PROBE', '{HEALTH_PROBE_FAILED}')`, staging, now.Add(-time.Minute), now.Add(time.Hour))
	c = submit(c, changeset.StateBlocked, "PROVIDER_PRODUCTION_NOT_PERMITTED", "PROVIDER_NO_ELIGIBLE_RELEASE", "PROVIDER_HEALTH_NOT_ELIGIBLE")
	exec(`UPDATE topology.engine_instance SET status = 'RETIRED' WHERE engine_instance_id = $1`, production)
	exec(`DELETE FROM topology.health_observation WHERE engine_instance_id = $1`, staging)

	// A recorded release blocks until one that supports the provider's
	// contracts is APPROVED (ADR-BCP-025 sections 2.1.2, 2.4).
	recordRelease := func(version, provider string, digestSeed byte) string {
		t.Helper()
		rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: engine, ReleaseVersion: version,
			Artifacts: []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine,
				Digest: "sha256:" + strings.Repeat(string(rune('a'+digestSeed)), 56) + suffix[:8]}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: provider, CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("e", 40),
			Reason: "Built from main."}, "workload:release-tooling", now, actor)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, rel.ReleaseID)
		return rel.ReleaseID
	}
	// Releases are approved as in operation: an ENGINE_RELEASE_APPROVAL
	// changeset, decided by someone other than its requester.
	approvals := releaseApproval{t: t, ctx: ctx, repo: repo, requester: requester, approver: approver, actor: actor, now: now}
	approve := func(id string) {
		t.Helper()
		approvals.approve(approvals.submit(approvals.draft(id, "key-release-"+id), changeset.StateAwaitingApproval))
	}
	covering := recordRelease("1.0.0", providerKey, 0)
	c = submit(c, changeset.StateBlocked, "PROVIDER_NO_ELIGIBLE_RELEASE")
	approve(recordRelease("0.9.0", engine+".other", 1))
	c = submit(c, changeset.StateBlocked, "PROVIDER_NO_ELIGIBLE_RELEASE")
	approve(covering)
	c = submit(c, changeset.StateAwaitingApproval)
	plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Operation != changeset.OpActivateProvider || plan.Steps[0].Resources.ProviderID != canonical ||
		plan.Steps[0].Resources.FromStatus != "DRAFT" {
		t.Fatalf("plan steps: %+v", plan.Steps)
	}

	// The requester never approves; another approver does, and it applies.
	decision := changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), requester, "", now, actor); !errors.Is(err, ErrChangesetSelfApproval) {
		t.Fatalf("self-approval: %v", err)
	}
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), approver, "", now, actor); err != nil {
		t.Fatal(err)
	}
	if c, err = repo.GetChangeset(ctx, c.ChangesetID); err != nil || c.State != changeset.StateApproved {
		t.Fatalf("approved: %+v %v", c, err)
	}
	if _, _, err := repo.ApplyChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-"+suffix, "h", requester, now, actor); err != nil {
		t.Fatal(err)
	}
	if c, err = repo.GetChangeset(ctx, c.ChangesetID); err != nil || c.State != changeset.StateCompleted {
		t.Fatalf("applied: %+v %v", c, err)
	}
	outcome, err := repo.GetChangeOutcome(ctx, c.ChangesetID)
	if err != nil {
		t.Fatal(err)
	}
	conforms("outcome", outcomeSchema, outcome)
	if len(outcome.AffectedResources) != 1 || outcome.AffectedResources[0].ResourceType != changeset.TargetProvider ||
		outcome.AffectedResources[0].Before != "DRAFT" || outcome.AffectedResources[0].After != "ACTIVE" {
		t.Fatalf("outcome: %+v", outcome)
	}
	readStatus := func() string {
		t.Helper()
		var s string
		if err := admin.QueryRow(ctx, `SELECT status FROM capability.capability_provider WHERE provider_key = $1`, providerKey).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := readStatus(); got != "ACTIVE" {
		t.Fatalf("provider after apply: %s", got)
	}

	// Re-registering leaves it ACTIVE (EA-02C); activating it again is a
	// state conflict.
	if err := repo.RegisterEngine(ctx, registration); err != nil {
		t.Fatal(err)
	}
	if got := readStatus(); got != "ACTIVE" {
		t.Fatalf("re-registration moved the provider to %s", got)
	}
	again := draft("key-again-" + suffix)
	submit(again, changeset.StateBlocked, changeset.BlockTargetStateConflict)
}
