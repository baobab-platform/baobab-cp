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

	"github.com/baobab-platform/baobab-cp/internal/capability/certification"
	capabilitydomain "github.com/baobab-platform/baobab-cp/internal/capability/domain"
	"github.com/baobab-platform/baobab-cp/internal/changeset"
	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/topology/release"
)

// removeChangesetsFor deletes every changeset that targets id, with its
// plans, decisions, operations and outcome. Test-only cleanup.
func removeChangesetsFor(ctx context.Context, admin *pgxpool.Pool, id string) {
	admin.Exec(ctx, `DELETE FROM changeset.outcome WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, id)
	admin.Exec(ctx, `DELETE FROM changeset.approval WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, id)
	admin.Exec(ctx, `UPDATE changeset.changeset SET current_plan_id = NULL WHERE target_id = $1`, id)
	admin.Exec(ctx, `DELETE FROM operations.execution_operation WHERE subject_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, id)
	admin.Exec(ctx, `DELETE FROM changeset.plan WHERE changeset_id IN (SELECT changeset_id FROM changeset.changeset WHERE target_id = $1)`, id)
	admin.Exec(ctx, `DELETE FROM changeset.changeset WHERE target_id = $1`, id)
}

// releaseApproval drives ENGINE_RELEASE_APPROVAL changesets in tests.
type releaseApproval struct {
	t                   *testing.T
	ctx                 context.Context
	repo                *PostgresRepository
	requester, approver string
	actor               AuditActor
	now                 time.Time
}

// draft records a DRAFT approval of the release id.
func (a releaseApproval) draft(id, key string) changeset.Changeset {
	a.t.Helper()
	desired := changeset.DesiredChange{Kind: changeset.KindReleaseApproval, ReleaseID: id}
	base, found, err := a.repo.TargetRevision(a.ctx, desired)
	if err != nil || !found || base != 1 {
		a.t.Fatalf("release revision: %d %v %v", base, found, err)
	}
	c, err := changeset.Draft(changeset.CreateRequest{Title: "Approve release", Reason: "Qualified in staging.", DesiredChange: desired},
		domain.NewResourceID("cs"), a.requester, "API", a.actor.CorrelationID, base, a.now)
	if err != nil {
		a.t.Fatal(err)
	}
	if c, err = a.repo.CreateChangeset(a.ctx, c, key, "hash-"+key, a.actor); err != nil {
		a.t.Fatal(err)
	}
	return c
}

// submit submits c, requiring the state and blockers it reaches, and
// returns it.
func (a releaseApproval) submit(c changeset.Changeset, wantState string, wantCodes ...string) changeset.Changeset {
	a.t.Helper()
	c, err := a.repo.SubmitChangeset(a.ctx, c.ChangesetID, c.Revision, domain.NewResourceID("plan"), a.now, a.actor)
	if err != nil {
		a.t.Fatal(err)
	}
	var codes []string
	for _, b := range c.BlockingReasons {
		codes = append(codes, b.Code)
	}
	if c.State != wantState || !slices.Equal(codes, wantCodes) {
		a.t.Fatalf("submit: state %s, blockers %v; want %s %v", c.State, codes, wantState, wantCodes)
	}
	if err := contracts.ValidateValue(contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/Changeset"), c); err != nil {
		a.t.Fatalf("changeset does not conform: %v", err)
	}
	plan, err := a.repo.CurrentChangesetPlan(a.ctx, c.ChangesetID)
	if err != nil {
		a.t.Fatal(err)
	}
	if err := contracts.ValidateValue(contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetPlan"), plan); err != nil {
		a.t.Fatalf("plan does not conform: %v", err)
	}
	if len(plan.ReadinessRequirements) != len(changeset.Kinds()[changeset.KindReleaseApproval].PlanChecks) {
		a.t.Fatalf("the plan reports %d of the plan checks", len(plan.ReadinessRequirements))
	}
	return c
}

// approve has the approver decide c's current plan and applies it.
func (a releaseApproval) approve(c changeset.Changeset) {
	a.t.Helper()
	plan, err := a.repo.CurrentChangesetPlan(a.ctx, c.ChangesetID)
	if err != nil {
		a.t.Fatal(err)
	}
	decision := changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}
	if _, err := a.repo.DecideChangeset(a.ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), a.approver, "", a.now, a.actor); err != nil {
		a.t.Fatal(err)
	}
	if c, err = a.repo.GetChangeset(a.ctx, c.ChangesetID); err != nil {
		a.t.Fatal(err)
	}
	if _, _, err := a.repo.ApplyChangeset(a.ctx, c.ChangesetID, c.Revision, domain.NewResourceID("op"), "apply-"+c.ChangesetID, "h",
		a.requester, a.now, a.actor); err != nil {
		a.t.Fatal(err)
	}
}

// TestEngineReleaseApprovalChangeset covers ADR-BCP-025 section 2.4: a
// recorded CANDIDATE release is APPROVED only through an
// ENGINE_RELEASE_APPROVAL changeset. Its plan blocks while the release's
// support is no longer catalogued, and while an environment it may serve
// (an instance's, or the Control Plane's own) requires provenance it lacks.
// Neither the requester nor the principal who recorded the release
// approves; another approver does, and applying it approves the release
// under the approver's name. Approving it again is a state conflict. Every
// record conforms to the Shared contract.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestEngineReleaseApprovalChangeset(t *testing.T) {
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
	engine := "baobab-approval" + suffix
	providerKey := engine + ".engine"
	capabilityKey := "test.approval" + suffix + ".perform"
	var releases []string
	cleanup := func() {
		for _, id := range releases {
			removeChangesetsFor(ctx, admin, id)
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

	var engineID string
	if err := admin.QueryRow(ctx, `INSERT INTO topology.engine (code, name) VALUES ($1, $1) RETURNING engine_id::text`, engine).Scan(&engineID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "Approval test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/approval-test", Digest: "sha256:" + strings.Repeat("7", 64)}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RegisterEngine(ctx, EngineRegistrationRecord{
		Repository: engine,
		Capabilities: []capabilitydomain.Capability{{
			Key: capabilityKey, Name: "Approval test", DomainKey: "test",
			Lifecycle: capabilitydomain.CapabilityLifecycleActive,
			Maturity: capabilitydomain.CapabilityMaturitySupported,
		}},
		Provider: EngineRegistrationProvider{
			ProviderKey: providerKey, Name: "Approval provider",
			ProviderType: "BAOBAB_ENGINE", EngineKey: "engine",
			Lifecycle: "DRAFT", Ownership: engine, ProductionPermitted: true,
		},
		Support: []EngineRegistrationSupport{{CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
	}); err != nil {
		t.Fatal(err)
	}
	var providerID string
	if err := admin.QueryRow(ctx,
		`SELECT canonical_provider_id FROM capability.capability_provider WHERE provider_key = $1`,
		providerKey,
	).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	staging := domain.NewUUIDv7()
	exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status)
		VALUES ($1, $2, 'af-south-1', 'staging', 'ACTIVE')`, staging, engineID)

	recorder, requester, approver := "prn_recorder"+suffix, "prn_requester"+suffix, "prn_approver"+suffix
	actor := AuditActor{ActorID: requester, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := func(version string, seed byte, provenance *release.Provenance) string {
		t.Helper()
		rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: engine, ReleaseVersion: version,
			Artifacts: []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engine,
				Digest: "sha256:" + strings.Repeat(string(rune('a'+seed)), 56) + suffix[:8]}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: providerKey, CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("e", 40),
			Provenance: provenance, Reason: "Built from main."}, recorder, now, actor)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, rel.ReleaseID)
		return rel.ReleaseID
	}
	approvals := releaseApproval{t: t, ctx: ctx, repo: repo, requester: requester, approver: approver, actor: actor, now: now}

	// A release without provenance blocks while an engine instance that is
	// not retired runs in production, which requires provenance.
	candidate := record("2.0.0", 0, nil)
	production := domain.NewUUIDv7()
	exec(`INSERT INTO topology.engine_instance(engine_instance_id, engine_id, region, environment, status)
		VALUES ($1, $2, 'af-south-1', 'production', 'PROVISIONING')`, production, engineID)
	c := approvals.draft(candidate, "key-approve-"+suffix)
	c = approvals.submit(c, changeset.StateBlocked, "RELEASE_PROVENANCE_MISSING")
	exec(`UPDATE topology.engine_instance SET status = 'RETIRED' WHERE engine_instance_id = $1`, production)

	// Support the catalogue no longer defines blocks.
	exec(`UPDATE capability.capability SET contract_versions = '{2}' WHERE code = $1`, capabilityKey)
	c = approvals.submit(c, changeset.StateBlocked, "RELEASE_SUPPORT_NOT_CATALOGUED")
	exec(`UPDATE capability.capability SET contract_versions = '{1}' WHERE code = $1`, capabilityKey)

	// A production Control Plane requires provenance whatever the engine's
	// instances; a release that names its attestation meets it.
	productionCP, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(productionCP.Close)
	productionCP.Environment = "production"
	inProduction := approvals
	inProduction.repo = productionCP
	c = inProduction.submit(c, changeset.StateBlocked, "RELEASE_PROVENANCE_MISSING", "RELEASE_NOT_CERTIFIED")
	attested := record("2.1.0", 1, &release.Provenance{AttestationURI: "https://attestations.example/" + engine + "/2.1.0",
		AttestationDigest: "sha256:" + strings.Repeat("f", 64), BuilderID: "https://github.com/baobab-platform/actions/builder"})
	attestedChange := inProduction.submit(
		inProduction.draft(attested, "key-attested-"+suffix),
		changeset.StateBlocked,
		"RELEASE_NOT_CERTIFIED",
	)
	certActor := AuditActor{ActorID: approver, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	if _, replay, err := repo.RecordProviderCapabilityCertification(ctx, certification.RecordRequest{
		ProviderID: providerID, CapabilityKey: capabilityKey, ContractVersion: 1,
		ReleaseID: attested, QualificationProfile: "ea-09/release-approval-test-v1",
		Evidence: []certification.Evidence{{
			Type: "INTEGRATION_TEST",
			URI: "https://github.com/baobab-platform/baobab-cp/actions/runs/1",
			Digest: "sha256:" + strings.Repeat("a", 64),
		}},
		Reason: "Production qualification passed.",
	}, approver, now, certActor); err != nil || replay {
		t.Fatalf("certify attested release: replay=%v err=%v", replay, err)
	}
	attestedChange = inProduction.submit(attestedChange, changeset.StateAwaitingApproval)

	// Nothing blocks the candidate any more; the plan approves exactly it.
	c = approvals.submit(c, changeset.StateAwaitingApproval)
	plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RiskClass != "HIGH" || plan.Steps[0].Operation != changeset.OpApproveRelease || plan.Steps[0].Resources.ReleaseID != candidate ||
		plan.Steps[0].Resources.FromStatus != release.StatusCandidate || plan.Steps[1].Operation != changeset.OpVerifyRelease {
		t.Fatalf("plan: %s %+v", plan.RiskClass, plan.Steps)
	}

	// Neither the requester nor the release's recorder approves.
	decision := changeset.DecisionRequest{PlanID: plan.PlanID, PlanVersion: plan.PlanVersion, PlanDigest: plan.PlanDigest, Decision: changeset.DecisionApproved}
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), requester, "", now, actor); !errors.Is(err, ErrChangesetSelfApproval) {
		t.Fatalf("approval by the requester: %v", err)
	}
	if _, err := repo.DecideChangeset(ctx, c.ChangesetID, c.Revision, decision, domain.NewResourceID("apd"), recorder, "", now, actor); !errors.Is(err, ErrEngineReleaseSelfApproval) {
		t.Fatalf("approval by the recorder: %v", err)
	}
	approvals.approve(c)

	got, err := repo.GetEngineRelease(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != release.StatusApproved || got.StatusChangedBy != approver || got.StatusReason != releaseStatusReason(c.ChangesetID, c.Reason) {
		t.Fatalf("approved release: %s by %q", got.Status, got.StatusChangedBy)
	}
	if err := contracts.ValidateValue(contracts.MustSchema("topology/v1/release.schema.json#/$defs/EngineRelease"), got); err != nil {
		t.Fatalf("approved release does not conform: %v", err)
	}
	outcome, err := repo.GetChangeOutcome(ctx, c.ChangesetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.ValidateValue(contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangeOutcome"), outcome); err != nil {
		t.Fatalf("outcome does not conform: %v", err)
	}
	if len(outcome.AffectedResources) != 1 || outcome.AffectedResources[0].ResourceType != changeset.TargetRelease ||
		outcome.AffectedResources[0].Before != release.StatusCandidate || outcome.AffectedResources[0].After != release.StatusApproved {
		t.Fatalf("outcome: %+v", outcome)
	}

	// An APPROVED release is not approved again.
	approvals.submit(approvals.draft(candidate, "key-again-"+suffix), changeset.StateBlocked, changeset.BlockTargetStateConflict)
}

// TestReleaseStatusReason: an approved release's status reason always fits
// engine_release.status_reason (3 to 500 characters), whatever the length
// of the changeset reason (1 to 1000), and names the changeset.
func TestReleaseStatusReason(t *testing.T) {
	for _, reason := range []string{"x", strings.Repeat("é", 1000), strings.Repeat("a", 480)} {
		got := releaseStatusReason("cs_0199a1b2c3d47ea4", reason)
		if n := len([]rune(got)); n < 3 || n > maxReleaseStatusReason || !strings.HasPrefix(got, "Approved by changeset cs_0199a1b2c3d47ea4: ") {
			t.Fatalf("status reason for a %d-rune reason: %d runes %q", len([]rune(reason)), n, got)
		}
	}
}
