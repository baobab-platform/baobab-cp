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

// TestDesiredReleaseLifecycle covers ADR-BCP-025 gate ER-03. An engine
// instance's desired release changes only through an
// ENGINE_INSTANCE_DESIRED_RELEASE changeset. Its plan blocks on a release
// that is not APPROVED, of another engine, or without the provenance the
// instance's environment requires, and on a change that changes nothing.
// It is set and cleared, and read back as infrastructure tooling reads it.
// A DEPRECATED release stays desired but is never newly desired. Revocation
// disposes of every desiring instance or is refused, and the database
// refuses what the Control Plane never does. Every record conforms.
//
// Set TEST_DATABASE_URL to run it; it is skipped otherwise.
func TestDesiredReleaseLifecycle(t *testing.T) {
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
	engine, other := "baobab-desired"+suffix, "baobab-desiredother"+suffix
	capabilityKey := "test.desired" + suffix + ".perform"
	var targets []string
	cleanup := func() {
		for _, id := range targets {
			removeChangesetsFor(ctx, admin, id)
		}
		admin.Exec(ctx, `UPDATE topology.engine_instance SET desired_release_id = NULL
			WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code IN ($1, $2))`, engine, other)
		removeEngineReleases(ctx, admin, engine)
		removeEngineReleases(ctx, admin, other)
		admin.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_id IN (SELECT engine_id FROM topology.engine WHERE code IN ($1, $2))`, engine, other)
		admin.Exec(ctx, `DELETE FROM capability.capability_provider WHERE provider_key IN ($1, $2)`, engine+".engine", other+".engine")
		admin.Exec(ctx, `DELETE FROM capability.capability WHERE code = $1`, capabilityKey)
		admin.Exec(ctx, `DELETE FROM topology.engine WHERE code IN ($1, $2)`, engine, other)
	}
	cleanup()
	t.Cleanup(cleanup)
	exec := func(sql string, args ...any) error {
		t.Helper()
		_, err := admin.Exec(ctx, sql, args...)
		return err
	}
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
	for _, code := range []string{engine, other} {
		mustExecResult, err := admin.Exec(ctx, `INSERT INTO capability.capability_provider
			(provider_key, name, provider_type, engine_id, status)
			VALUES ($1, $1, 'BAOBAB_ENGINE', $2::uuid, 'DRAFT')`, code+".engine", engineIDs[code])
		_ = mustExecResult
		must(err)
	}
	_, err = repo.SyncCapabilityCatalogue(ctx, []CatalogueCapability{{Capability: capabilitydomain.Capability{Key: capabilityKey,
		Name: "Desired test", DomainKey: "test", Lifecycle: capabilitydomain.CapabilityLifecycleActive,
		Maturity: capabilitydomain.CapabilityMaturitySupported}, ContractVersions: []int{1}, DataClassification: "INTERNAL",
		Owner: engine, Source: "fixtures/desired-test", Digest: "sha256:" + strings.Repeat("9", 64)}})
	must(err)
	// fixtures/desired-test provider support: certification is meaningful only
	// against support the provider actually declares.
	for _, code := range []string{engine, other} {
		_, err = admin.Exec(ctx, `INSERT INTO capability.provider_capability_support(provider_id, capability_id, contract_versions)
			SELECT p.provider_id, cap.capability_id, '{1}'::integer[]
			FROM capability.capability_provider p, capability.capability cap
			WHERE p.provider_key = $1 AND cap.code = $2`, code+".engine", capabilityKey)
		must(err)
	}
	instance := func(engineCode, environment string) string {
		t.Helper()
		var key string
		must(admin.QueryRow(ctx, `INSERT INTO topology.engine_instance(engine_id, region, environment, status)
			VALUES ($1::uuid, 'af-south-1', $2, 'ACTIVE') RETURNING engine_instance_key`, engineIDs[engineCode], environment).Scan(&key))
		targets = append(targets, key)
		return key
	}
	staging := instance(engine, "staging")

	requester, approver := "prn_requester"+suffix, "prn_approver"+suffix
	actor := AuditActor{ActorID: requester, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	approvals := releaseApproval{t: t, ctx: ctx, repo: repo, requester: requester, approver: approver, actor: actor, now: now}
	seed := byte(0)
	record := func(engineCode, version string, attested bool) string {
		t.Helper()
		var provenance *release.Provenance
		if attested {
			provenance = &release.Provenance{AttestationURI: "https://attestations.example/" + engineCode + "/" + version,
				AttestationDigest: "sha256:" + strings.Repeat("f", 64), BuilderID: "https://github.com/baobab-platform/actions/builder"}
		}
		seed++
		rel, _, err := repo.RecordEngineRelease(ctx, release.RecordRequest{EngineID: engineCode, ReleaseVersion: version,
			Artifacts: []release.Artifact{{ArtifactType: "OCI_IMAGE", Repository: "ghcr.io/baobab-platform/" + engineCode,
				Digest: "sha256:" + strings.Repeat(string(rune('a'+seed)), 56) + suffix[:8]}},
			ProviderSupport:                     []release.ProviderSupport{{ProviderKey: engineCode + ".engine", CapabilityKey: capabilityKey, ContractVersions: []int{1}}},
			CapabilityProviderDeclarationDigest: "sha256:" + strings.Repeat("d", 64), SourceRevision: strings.Repeat("e", 40),
			Provenance: provenance, Reason: "Built from main."}, "workload:release-tooling", now, actor)
		must(err)
		targets = append(targets, rel.ReleaseID)
		return rel.ReleaseID
	}
	approve := func(id string) {
		t.Helper()
		approvals.approve(approvals.submit(approvals.draft(id, "key-approve-"+id), changeset.StateAwaitingApproval))
	}

	planSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangesetPlan")
	desiredSchema := contracts.MustSchema("topology/v1/release.schema.json#/$defs/EngineInstanceDesiredRelease")
	releaseSchema := contracts.MustSchema("topology/v1/release.schema.json#/$defs/EngineRelease")
	outcomeSchema := contracts.MustSchema("control-plane/v1/changeset.schema.json#/$defs/ChangeOutcome")
	conforms := func(label string, schema *contracts.Schema, v any) {
		t.Helper()
		if err := contracts.ValidateValue(schema, v); err != nil {
			t.Fatalf("%s does not conform: %v", label, err)
		}
	}
	keys := 0
	draft := func(instanceKey, releaseKey string) changeset.Changeset {
		t.Helper()
		desired := changeset.DesiredChange{Kind: changeset.KindDesiredRelease, EngineInstanceID: instanceKey, ReleaseID: releaseKey}
		base, found, err := repo.TargetRevision(ctx, desired)
		if err != nil || !found {
			t.Fatalf("instance revision: %v %v", found, err)
		}
		c, err := changeset.Draft(changeset.CreateRequest{Title: "Desire release", Reason: "Roll forward.", DesiredChange: desired},
			domain.NewResourceID("cs"), requester, "API", actor.CorrelationID, base, now)
		must(err)
		keys++
		c, err = repo.CreateChangeset(ctx, c, "key-desire-"+suffix+string(rune('a'+keys)), "h", actor)
		must(err)
		return c
	}
	submit := func(c changeset.Changeset, wantState string, wantCodes ...string) changeset.Changeset {
		t.Helper()
		c, err := repo.SubmitChangeset(ctx, c.ChangesetID, c.Revision, domain.NewResourceID("plan"), now, actor)
		must(err)
		var codes []string
		for _, b := range c.BlockingReasons {
			codes = append(codes, b.Code)
		}
		if c.State != wantState || !slices.Equal(codes, wantCodes) {
			t.Fatalf("submit: state %s, blockers %v; want %s %v", c.State, codes, wantState, wantCodes)
		}
		plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
		must(err)
		conforms("plan", planSchema, plan)
		return c
	}
	// blocked submits a change that must block with exactly code, then
	// removes it so it holds no lock on the instance.
	blocked := func(instanceKey, releaseKey string, codes ...string) {
		t.Helper()
		submit(draft(instanceKey, releaseKey), changeset.StateBlocked, codes...)
		removeChangesetsFor(ctx, admin, instanceKey)
	}
	apply := func(c changeset.Changeset) changeset.Outcome {
		t.Helper()
		approvals.approve(c)
		o, err := repo.GetChangeOutcome(ctx, c.ChangesetID)
		must(err)
		conforms("outcome", outcomeSchema, o)
		return o
	}
	read := func(instanceKey string) release.DesiredRelease {
		t.Helper()
		d, err := repo.GetEngineInstanceDesiredRelease(ctx, instanceKey)
		must(err)
		conforms("desired release", desiredSchema, d)
		return d
	}

	attested, unattested, candidate := record(engine, "1.0.0", true), record(engine, "1.1.0", false), record(engine, "2.0.0-rc.1", true)
	foreign := record(other, "1.0.0", true)
	approve(attested)
	approve(unattested)
	approve(foreign)
	// The production instance arrives after approval: approving a release
	// without provenance while its engine runs in production is refused
	// (ENGINE_RELEASE_APPROVAL), and desiring it there still is.
	production := instance(engine, "production")

	// Nothing is desired yet.
	if d := read(staging); d.DesiredReleaseID != nil || d.Version != 1 {
		t.Fatalf("initial desired release: %+v", d)
	}

	// Each check that fails blocks with its own code.
	blocked(staging, candidate, "DESIRED_RELEASE_NOT_APPROVED")
	blocked(staging, foreign, "DESIRED_RELEASE_ENGINE_MISMATCH")
	blocked(production, unattested, "DESIRED_RELEASE_PROVENANCE_MISSING", "DESIRED_RELEASE_NOT_CERTIFIED")
	blocked(staging, "", "DESIRED_RELEASE_UNCHANGED")

	var canonicalProviderID string
	must(admin.QueryRow(ctx, `SELECT canonical_provider_id FROM capability.capability_provider WHERE provider_key = $1`,
		engine+".engine").Scan(&canonicalProviderID))
	qualified, replayed, err := repo.RecordProviderCapabilityCertification(ctx, certification.RecordRequest{
		ProviderID: canonicalProviderID, CapabilityKey: capabilityKey, ContractVersion: 1,
		ReleaseID: attested, QualificationProfile: "ea-09/desired-release-test-v1",
		Evidence: []certification.Evidence{{
			Type:        "INTEGRATION_TEST",
			URI:         "https://github.com/baobab-platform/baobab-cp/actions",
			Digest:      "sha256:" + strings.Repeat("a", 64),
			Description: "Production desired-release qualification.",
		}},
		Reason: "Qualified for the production desired-release test.",
	}, requester, now.Add(time.Minute), actor)
	if err != nil || replayed || qualified.ReleaseID != attested {
		t.Fatalf("qualify production desired release: %+v replay=%v err=%v", qualified, replayed, err)
	}

	// Desire an attested release for staging; the plan names the release on
	// both steps, and apply sets it at the next desired-release version.
	c := submit(draft(staging, attested), changeset.StateAwaitingApproval)
	plan, err := repo.CurrentChangesetPlan(ctx, c.ChangesetID)
	must(err)
	if plan.Steps[0].Operation != changeset.OpSetDesired || plan.Steps[0].Resources.DesiredReleaseID != attested ||
		plan.Steps[0].Resources.TargetRevision != 1 || plan.Steps[1].Resources.DesiredReleaseID != attested {
		t.Fatalf("plan steps: %+v", plan.Steps)
	}
	o := apply(c)
	if o.AffectedResources[0].ResourceType != changeset.TargetInstance || o.AffectedResources[0].Before != "" || o.AffectedResources[0].After != attested ||
		o.VerificationResult.Checks[0].Check != "DESIRED_RELEASE_MATCHES" {
		t.Fatalf("outcome: %+v", o)
	}
	d := read(staging)
	if d.DesiredReleaseID == nil || *d.DesiredReleaseID != attested || d.DesiredRelease.Status != release.StatusApproved ||
		d.Version != 2 || d.ChangesetID != c.ChangesetID || d.EngineID != engine {
		t.Fatalf("desired after apply: %+v", d)
	}
	blocked(staging, attested, "DESIRED_RELEASE_UNCHANGED")

	// Clearing it is a desired-release change too; the plan says null.
	c = submit(draft(production, attested), changeset.StateAwaitingApproval)
	apply(c)
	c = submit(draft(production, ""), changeset.StateAwaitingApproval)
	o = apply(c)
	if o.AffectedResources[0].Before != attested || o.AffectedResources[0].After != "" {
		t.Fatalf("clearing outcome: %+v", o)
	}
	if d := read(production); d.DesiredReleaseID != nil || d.Version != 3 || d.DesiredRelease != nil {
		t.Fatalf("desired after clearing: %+v", d)
	}

	// A DEPRECATED release stays desired and readable, but is never newly
	// desired; deprecating a CANDIDATE is not a transition.
	change := func(id, to string, dispositions ...release.Disposition) (release.Release, error) {
		return repo.ChangeEngineReleaseStatus(ctx, id, release.StatusChangeRequest{TargetStatus: to, Reason: "Superseded.",
			DesiredReleaseDispositions: dispositions}, "prn_operator"+suffix, now, actor)
	}
	deprecated, err := change(attested, release.StatusDeprecated)
	must(err)
	conforms("deprecated release", releaseSchema, deprecated)
	if deprecated.Status != release.StatusDeprecated || deprecated.StatusChangedBy != "prn_operator"+suffix {
		t.Fatalf("deprecated: %+v", deprecated)
	}
	if d := read(staging); d.DesiredRelease == nil || d.DesiredRelease.Status != release.StatusDeprecated {
		t.Fatalf("a deprecated desired release must stay readable: %+v", d)
	}
	blocked(production, attested, "DESIRED_RELEASE_NOT_APPROVED")
	if _, err := change(candidate, release.StatusDeprecated); !errors.Is(err, ErrEngineReleaseTransition) {
		t.Fatalf("deprecating a candidate: %v", err)
	}

	// Revocation covers every desiring instance or nothing changes.
	invalid := func(err error, code string) {
		t.Helper()
		var refused *release.ErrInvalid
		if !errors.As(err, &refused) || refused.Code != code {
			t.Fatalf("want %s, got %v", code, err)
		}
	}
	_, err = change(attested, release.StatusRevoked)
	invalid(err, release.ReasonRevocationUncovered)
	_, err = change(attested, release.StatusRevoked, release.Disposition{EngineInstanceID: production, Action: release.DispositionClear})
	invalid(err, "VALIDATION_FAILED")
	_, err = change(attested, release.StatusRevoked, release.Disposition{EngineInstanceID: staging, Action: release.DispositionReplace, ReplacementReleaseID: foreign})
	invalid(err, release.ReasonEngineMismatch)
	_, err = change(attested, release.StatusRevoked, release.Disposition{EngineInstanceID: staging, Action: release.DispositionReplace, ReplacementReleaseID: candidate})
	invalid(err, release.ReasonNotApproved)
	if d := read(staging); d.DesiredReleaseID == nil || *d.DesiredReleaseID != attested {
		t.Fatalf("a refused revocation changed the desired release: %+v", d)
	}
	revoked, err := change(attested, release.StatusRevoked,
		release.Disposition{EngineInstanceID: staging, Action: release.DispositionReplace, ReplacementReleaseID: unattested})
	must(err)
	if revoked.Status != release.StatusRevoked {
		t.Fatalf("revoked: %+v", revoked)
	}
	if d := read(staging); d.DesiredReleaseID == nil || *d.DesiredReleaseID != unattested || d.ChangesetID != "" || d.Version != 3 {
		t.Fatalf("desired after revocation: %+v", d)
	}
	// Revoking again is not a transition.
	if _, err := change(attested, release.StatusRevoked); !errors.Is(err, ErrEngineReleaseTransition) {
		t.Fatalf("revoking a revoked release: %v", err)
	}

	// The database refuses what the Control Plane never does.
	if err := exec(`UPDATE topology.engine_instance SET desired_release_id = (SELECT engine_release_id FROM topology.engine_release WHERE release_key = $1)
		WHERE engine_instance_key = $2`, candidate, production); err == nil {
		t.Fatal("the database let a CANDIDATE become desired")
	}
	if err := exec(`UPDATE topology.engine_instance SET desired_release_id = (SELECT engine_release_id FROM topology.engine_release WHERE release_key = $1)
		WHERE engine_instance_key = $2`, foreign, production); err == nil {
		t.Fatal("the database let another engine's release become desired")
	}
	if err := exec(`UPDATE topology.engine_release SET status = 'REVOKED', status_changed_by = 'x', status_changed_at = now(), status_reason = 'Withdrawn.'
		WHERE release_key = $1`, unattested); err == nil {
		t.Fatal("the database revoked a release an instance desires")
	}

	// A desired pointer to a release outside readable_as_desired is never
	// returned. Only a write around the guards can produce one.
	tx, err := admin.Begin(ctx)
	must(err)
	_, err = tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
	must(err)
	_, err = tx.Exec(ctx, `UPDATE topology.engine_instance SET desired_release_id = (SELECT engine_release_id FROM topology.engine_release WHERE release_key = $1)
		WHERE engine_instance_key = $2`, attested, production)
	must(err)
	must(tx.Commit(ctx))
	if _, err := repo.GetEngineInstanceDesiredRelease(ctx, production); !errors.Is(err, ErrDesiredReleaseUnavailable) {
		t.Fatalf("reading a revoked desired release: %v", err)
	}
	if _, err := repo.GetEngineInstanceDesiredRelease(ctx, "ei_00000000000000000000000000000000"); !errors.Is(err, ErrEngineInstanceNotFound) {
		t.Fatalf("an unknown instance: %v", err)
	}
}
