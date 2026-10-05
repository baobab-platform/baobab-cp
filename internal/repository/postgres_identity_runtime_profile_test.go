package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestIdentityRuntimeProfilePersistence proves the MP2-C runtime evidence store
// against PostgreSQL rather than only through handler/repository stubs. A
// profile is append-only, environment/region scoped, monotonic by revision and
// idempotent only for the exact same revision content.
func TestIdentityRuntimeProfilePersistence(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Microsecond)

	// The reference namespace policy fixes these owning engine codes. Reuse the
	// canonical engine rows if another integration test has already created
	// them; the test-specific instances and provider remain isolated.
	for _, code := range []string{"baobab-iam", "baobab-cp"} {
		if _, err := admin.Exec(ctx, `
			INSERT INTO topology.engine(code, name)
			VALUES ($1, $1)
			ON CONFLICT (code) DO NOTHING`, code); err != nil {
			t.Fatal(err)
		}
	}

	var iamEngineID, cpEngineID string
	if err := admin.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code='baobab-iam'`).Scan(&iamEngineID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT engine_id::text FROM topology.engine WHERE code='baobab-cp'`).Scan(&cpEngineID); err != nil {
		t.Fatal(err)
	}

	var providerID, iamInstance, cpInstance string
	if err := admin.QueryRow(ctx, `
		INSERT INTO capability.capability_provider(provider_key, name, provider_type, engine_id, status, ownership)
		VALUES ($1, 'MP2-C runtime test', 'ADAPTER', $2::uuid, 'DRAFT', 'baobab-platform/baobab-iam')
		RETURNING canonical_provider_id`,
		"baobab-iam.mp2c-"+suffix, iamEngineID).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `
		INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE')
		RETURNING engine_instance_key`, iamEngineID).Scan(&iamInstance); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `
		INSERT INTO topology.engine_instance(engine_id, region, environment, status)
		VALUES ($1::uuid, 'af-south-1', 'staging', 'ACTIVE')
		RETURNING engine_instance_key`, cpEngineID).Scan(&cpInstance); err != nil {
		t.Fatal(err)
	}

	refs := map[string]string{
		"configuration": "ref_cfg" + suffix,
		"security":      "ref_sec" + suffix,
		"support":       "ref_sup" + suffix,
	}
	insertRef := func(id, namespace, engine, instance, entityType string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `
			INSERT INTO mapping.external_reference(
				external_reference_id, system_namespace, engine_id, engine_instance_id,
				environment, native_entity_type, native_id, source_authority,
				status, first_seen_at, last_verified_at
			)
			VALUES ($1,$2,$3,$4,'staging',$5,$6,'reconciliation','active',$7,$7)`,
			id, namespace, engine, instance, entityType, "mp2c-"+suffix+"-"+entityType, now); err != nil {
			t.Fatal(err)
		}
	}
	insertRef(refs["configuration"], "baobab_iam", "baobab-iam", iamInstance, "federation_configuration")
	insertRef(refs["security"], "baobab_iam", "baobab-iam", iamInstance, "identity_security_domain")
	insertRef(refs["support"], "baobab_cp", "baobab-cp", cpInstance, "identity_runtime_support")

	cleanup := func() {
		tx, err := admin.Begin(ctx)
		if err != nil {
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return
		}
		_, _ = tx.Exec(ctx, `
			DELETE FROM identity.identity_runtime_capability_observation
			WHERE provider_id = (SELECT provider_id FROM capability.capability_provider WHERE canonical_provider_id=$1)`, providerID)
		_, _ = tx.Exec(ctx, `
			DELETE FROM identity.identity_provider_runtime_profile
			WHERE provider_id = (SELECT provider_id FROM capability.capability_provider WHERE canonical_provider_id=$1)`, providerID)
		for _, id := range refs {
			_, _ = tx.Exec(ctx, `DELETE FROM mapping.external_reference WHERE external_reference_id=$1`, id)
		}
		_, _ = tx.Exec(ctx, `DELETE FROM capability.capability_provider WHERE canonical_provider_id=$1`, providerID)
		_, _ = tx.Exec(ctx, `DELETE FROM topology.engine_instance WHERE engine_instance_key IN ($1,$2)`, iamInstance, cpInstance)
		_ = tx.Commit(ctx)
	}
	t.Cleanup(cleanup)

	digest := "sha256:" + strings.Repeat("a", 64)
	profile := IdentityRuntimeProfile{
		ProviderID:              providerID,
		EngineInstanceID:        iamInstance,
		ConfigurationReference:  refs["configuration"],
		SecurityDomainReference: refs["security"],
		ArtifactDigest:          digest,
		Revision:                1,
		PublishedAt:             now,
		CapabilityObservations: []IdentityRuntimeCapabilityObservation{{
			Capability:         "OIDC_FEDERATION",
			VerificationStatus: "VERIFIED",
			Evidence: &IdentityRuntimeEvidence{
				EvidenceReference: refs["support"],
				ArtifactDigest:    digest,
				ObservedAt:        now.Add(-time.Minute),
				ExpiresAt:         now.Add(10 * time.Minute),
			},
		}},
	}

	replay, err := repo.RecordIdentityRuntimeProfile(
		ctx, profile, "workload:baobab-deployment-controller-staging",
		"staging", []string{"af-south-1"}, now,
	)
	if err != nil || replay {
		t.Fatalf("first publication: replay=%v err=%v", replay, err)
	}
	replay, err = repo.RecordIdentityRuntimeProfile(
		ctx, profile, "workload:baobab-deployment-controller-staging",
		"staging", []string{"af-south-1"}, now,
	)
	if err != nil || !replay {
		t.Fatalf("exact retry: replay=%v err=%v", replay, err)
	}

	conflict := profile
	conflict.PublishedAt = now.Add(time.Second)
	if _, err := repo.RecordIdentityRuntimeProfile(
		ctx, conflict, "workload:baobab-deployment-controller-staging",
		"staging", []string{"af-south-1"}, now.Add(time.Second),
	); !errors.Is(err, ErrIdentityRuntimeProfileConflict) {
		t.Fatalf("same revision with different content: %v", err)
	}

	next := profile
	next.Revision = 2
	next.PublishedAt = now.Add(time.Second)
	if _, err := repo.RecordIdentityRuntimeProfile(
		ctx, next, "workload:baobab-deployment-controller-staging",
		"staging", []string{"af-south-1"}, now.Add(time.Second),
	); err != nil {
		t.Fatalf("next revision: %v", err)
	}

	gap := next
	gap.Revision = 4
	gap.PublishedAt = now.Add(2 * time.Second)
	if _, err := repo.RecordIdentityRuntimeProfile(
		ctx, gap, "workload:baobab-deployment-controller-staging",
		"staging", []string{"af-south-1"}, now.Add(2*time.Second),
	); !errors.Is(err, ErrIdentityRuntimeProfileConflict) {
		t.Fatalf("revision gap accepted: %v", err)
	}

	if _, err := repo.RecordIdentityRuntimeProfile(
		ctx, next, "workload:baobab-deployment-controller-staging",
		"production", []string{"af-south-1"}, now.Add(time.Second),
	); !errors.Is(err, ErrIdentityRuntimeProfileOutOfScope) {
		t.Fatalf("wrong environment accepted: %v", err)
	}
	if _, err := repo.RecordIdentityRuntimeProfile(
		ctx, next, "workload:baobab-deployment-controller-staging",
		"staging", []string{"eu-west-1"}, now.Add(time.Second),
	); !errors.Is(err, ErrIdentityRuntimeProfileOutOfScope) {
		t.Fatalf("wrong region accepted: %v", err)
	}

	var profileRows, observationRows int
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM identity.identity_provider_runtime_profile p
		JOIN capability.capability_provider cp ON cp.provider_id=p.provider_id
		WHERE cp.canonical_provider_id=$1`, providerID).Scan(&profileRows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `
		SELECT count(*) FROM identity.identity_runtime_capability_observation o
		JOIN capability.capability_provider cp ON cp.provider_id=o.provider_id
		WHERE cp.canonical_provider_id=$1`, providerID).Scan(&observationRows); err != nil {
		t.Fatal(err)
	}
	if profileRows != 2 || observationRows != 2 {
		t.Fatalf("stored rows: profiles=%d observations=%d", profileRows, observationRows)
	}

	// Runtime evidence is append-only to the same standard as deployment
	// observations. UPDATE/DELETE must reach our append-only trigger. TRUNCATE
	// only needs to be refused: PostgreSQL may reject the parent profile table
	// at its FK boundary before BEFORE TRUNCATE triggers are invoked.
	for _, sql := range []string{
		`UPDATE identity.identity_provider_runtime_profile SET source='x' WHERE provider_id=(SELECT provider_id FROM capability.capability_provider WHERE canonical_provider_id='` + providerID + `')`,
		`DELETE FROM identity.identity_runtime_capability_observation WHERE provider_id=(SELECT provider_id FROM capability.capability_provider WHERE canonical_provider_id='` + providerID + `')`,
	} {
		if _, err := admin.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%q must be refused by the append-only trigger, got %v", sql, err)
		}
	}
	for _, sql := range []string{
		`TRUNCATE identity.identity_runtime_capability_observation`,
		`TRUNCATE identity.identity_provider_runtime_profile`,
	} {
		if _, err := admin.Exec(ctx, sql); err == nil {
			t.Fatalf("%q must be refused", sql)
		}
	}
}
