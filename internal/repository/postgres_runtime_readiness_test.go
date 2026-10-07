package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Extends the real profile persistence fixture with governed support/bindings,
// an approved release and current deployment. No provider is activated outside
// this isolated PostgreSQL construction fixture.
func verifyRuntimeReadiness(t *testing.T, repo *PostgresRepository, db *pgxpool.Pool, p IdentityRuntimeProfile, refs map[string]string, suffix, engine string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	org, estate, tenant := domain.NewUUIDv7(), "estate_"+suffix, "tn_"+suffix
	trust := "ref_trust" + suffix
	refs["trust"] = trust // parent fixture cleans references after this cleanup
	exec(`INSERT INTO mapping.external_reference(external_reference_id,system_namespace,engine_id,engine_instance_id,environment,native_entity_type,native_id,source_authority,status,first_seen_at,last_verified_at)
	 VALUES($1,'baobab_iam','baobab-iam',$2,'staging','federation_trust_material',$1,'reconciliation','active',$3,$3)`, trust, p.EngineInstanceID, now)
	exec(`UPDATE capability.capability_provider SET status='ACTIVE' WHERE canonical_provider_id=$1`, p.ProviderID)
	exec(`INSERT INTO capability.capability(code,name) VALUES('identity.authentication.perform','Authentication') ON CONFLICT(code) DO NOTHING`)
	var provider, capID, instance, scope, binding string
	for _, q := range []struct {
		sql  string
		args []any
		out  *string
	}{
		{`SELECT provider_id::text FROM capability.capability_provider WHERE canonical_provider_id=$1`, []any{p.ProviderID}, &provider},
		{`SELECT capability_id::text FROM capability.capability WHERE code='identity.authentication.perform'`, nil, &capID},
		{`SELECT engine_instance_id::text FROM topology.engine_instance WHERE engine_instance_key=$1`, []any{p.EngineInstanceID}, &instance},
		{`INSERT INTO capability.capability_scope(tenant_id,organisation_id,digital_estate_id,environment) VALUES($1,$2,$3,'staging') RETURNING scope_id::text`, []any{tenant, org, estate}, &scope},
	} {
		if err := db.QueryRow(ctx, q.sql, q.args...).Scan(q.out); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO capability.provider_capability_support(provider_id,capability_id,contract_versions,status,effective_from) VALUES($1::uuid,$2::uuid,ARRAY[1],'ACTIVE',$3)`, provider, capID, now.Add(-time.Minute))
	if err := db.QueryRow(ctx, `INSERT INTO capability.capability_binding(capability_id,engine_instance_id,scope_id,provider_id,binding_mode,status,contract_version,effective_from) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'PRIMARY','ACTIVE','1',$5) RETURNING id::text`, capID, instance, scope, provider, now.Add(-time.Minute)).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	release := domain.NewUUIDv7()
	exec(`INSERT INTO topology.engine_release(engine_release_id,engine_id,release_version,source_revision,declaration_digest,content_digest,status,recorded_by,recorded_at,reason,status_changed_by,status_changed_at,status_reason)
	 VALUES($1::uuid,$2::uuid,$3,repeat('a',40),$4,$4,'APPROVED','test-readiness',$5,'construction fixture','test-readiness',$5,'construction approval')`, release, engine, "0.0.0-readiness-"+suffix, p.ArtifactDigest, now)
	exec(`INSERT INTO topology.engine_release_artifact(engine_release_id,ordinal,artifact_type,repository,digest) VALUES($1::uuid,0,'OCI_IMAGE','ghcr.io/baobab-platform/baobab-iam',$2)`, release, p.ArtifactDigest)
	exec(`UPDATE topology.engine_instance SET desired_release_id=$1::uuid WHERE engine_instance_key=$2`, release, p.EngineInstanceID)
	observe := func(key, digest string, at, expires time.Time) {
		exec(`INSERT INTO topology.deployment_observation(observation_key,engine_instance_key,artifacts,environment,region,observed_at,expires_at,recorded_at,source)
		 VALUES($1,$2,$3::jsonb,'staging','af-south-1',$4,$5,$4,'test-readiness')`, key, p.EngineInstanceID, fmt.Sprintf(`[{"digest":%q}]`, digest), at, expires)
	}
	observe("dob_ready"+suffix, p.ArtifactDigest, now, now.Add(time.Minute))
	t.Cleanup(func() {
		tx, err := db.Begin(ctx)
		if err != nil {
			return
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SET LOCAL session_replication_role=replica`); err != nil {
			return
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM topology.deployment_observation WHERE engine_instance_key=$1`, []any{p.EngineInstanceID}},
			{`UPDATE topology.engine_instance SET desired_release_id=NULL WHERE engine_instance_key=$1`, []any{p.EngineInstanceID}},
			{`DELETE FROM topology.engine_release_artifact WHERE engine_release_id=$1::uuid`, []any{release}},
			{`DELETE FROM topology.engine_release WHERE engine_release_id=$1::uuid`, []any{release}},
			{`DELETE FROM capability.capability_binding WHERE provider_id=$1::uuid`, []any{provider}},
			{`DELETE FROM capability.capability_scope WHERE tenant_id=$1`, []any{tenant}},
			{`DELETE FROM capability.provider_capability_support WHERE provider_id=$1::uuid`, []any{provider}},
		} {
			_, _ = tx.Exec(ctx, q.sql, q.args...)
		}
		_ = tx.Commit(ctx)
	})
	read := func(at time.Time) (FederationPlatformSnapshot, error) {
		return repo.ReadFederationPlatformSnapshot(ctx, p.ProviderID, p.EngineInstanceID, org, estate, "OIDC_FEDERATION", p.ConfigurationReference, trust, "staging", at)
	}
	allow := func() {
		t.Helper()
		s, err := read(now)
		if err != nil || s.ProfileRevision != p.Revision || s.ArtifactDigest != p.ArtifactDigest {
			t.Fatalf("readiness: %+v %v", s, err)
		}
	}
	deny := func(at time.Time) {
		t.Helper()
		if _, err := read(at); !errors.Is(err, ErrFederationPlatformEvidenceNotFound) {
			t.Fatalf("unsafe readiness accepted: %v", err)
		}
	}
	allow()
	for _, tc := range []struct {
		name, change, restore string
		args                  []any
	}{
		{"support-major", `UPDATE capability.provider_capability_support SET contract_versions=ARRAY[2] WHERE provider_id=$1::uuid`, `UPDATE capability.provider_capability_support SET contract_versions=ARRAY[1] WHERE provider_id=$1::uuid`, []any{provider}},
		{"binding-major", `UPDATE capability.capability_binding SET contract_version='2' WHERE id=$1::uuid`, `UPDATE capability.capability_binding SET contract_version='1' WHERE id=$1::uuid`, []any{binding}},
		{"support-revoked", `UPDATE capability.provider_capability_support SET status='RETIRED' WHERE provider_id=$1::uuid`, `UPDATE capability.provider_capability_support SET status='ACTIVE' WHERE provider_id=$1::uuid`, []any{provider}},
		{"binding-revoked", `UPDATE capability.capability_binding SET status='RETIRED' WHERE id=$1::uuid`, `UPDATE capability.capability_binding SET status='ACTIVE' WHERE id=$1::uuid`, []any{binding}},
		{"reference-revoked", `UPDATE mapping.external_reference SET status='archived' WHERE external_reference_id=$1`, `UPDATE mapping.external_reference SET status='active' WHERE external_reference_id=$1`, []any{refs["support"]}},
	} {
		t.Run(tc.name, func(t *testing.T) { exec(tc.change, tc.args...); defer exec(tc.restore, tc.args...); deny(now) })
		allow()
	}
	// A second scope with the same requested dimensions must not be hidden by
	// the per-scope PRIMARY exclusion constraint or by the projection's LIMIT.
	var competingScope string
	if err := db.QueryRow(ctx, `INSERT INTO capability.capability_scope(tenant_id,organisation_id,digital_estate_id,environment) VALUES($1,$2,$3,'staging') RETURNING scope_id::text`, tenant, org, estate).Scan(&competingScope); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO capability.capability_binding(capability_id,engine_instance_id,scope_id,provider_id,binding_mode,status,contract_version,effective_from) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'PRIMARY','ACTIVE','1',$5)`, capID, instance, competingScope, provider, now.Add(-time.Minute))
	deny(now)
	exec(`DELETE FROM capability.capability_binding WHERE scope_id=$1::uuid`, competingScope)
	allow()
	deny(p.PublishedAt.Add(-time.Microsecond))
	deny(now.Add(time.Minute))
	observe("dob_drift"+suffix, "sha256:"+fmt.Sprintf("%064x", 1), now.Add(time.Second), now.Add(time.Minute))
	deny(now.Add(time.Second)) // never fall back to the older matching deployment
	observe("dob_restore"+suffix, p.ArtifactDigest, now.Add(2*time.Second), now.Add(time.Minute))
	if _, err := read(now.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	next := p
	next.Revision++
	next.PublishedAt = now.Add(3 * time.Second)
	next.CapabilityObservations = []IdentityRuntimeCapabilityObservation{{Capability: "OIDC_FEDERATION", VerificationStatus: "UNSUPPORTED"}}
	if _, err := repo.RecordIdentityRuntimeProfile(ctx, next, "test-readiness", "staging", []string{"af-south-1"}, next.PublishedAt); err != nil {
		t.Fatal(err)
	}
	deny(next.PublishedAt) // no fallback to an older VERIFIED profile
}
