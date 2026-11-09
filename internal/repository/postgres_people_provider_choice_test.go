package repository

import (
	"context"
	"errors"
	neturl "net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPeopleProviderChoiceDefaultDenialPG17 is real PostgreSQL evidence for
// the no-default policy: no chosen provider may be invented from the
// available ERP engine, and choice isolation is enforced at the database.
func TestPeopleProviderChoiceDefaultDenialPG17(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires real PostgreSQL 17 TEST_DATABASE_URL")
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

	connection, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	var count int
	err = connection.QueryRow(ctx, `
       SELECT count(*)
       FROM pg_constraint
       WHERE conname = 'people_choice_one_active_source'
         AND contype = 'x'
         AND conrelid = 'capability.people_provider_choice'::regclass
    `).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("one authoritative provider exclusion missing: count=%d error=%v", count, err)
	}
	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	result, err := repo.ReadPeopleChoice(ctx, "tn_unregisteredpeople01", "TEST-EMPLOYER",
		"ZA", "payroll.run.read", time.Now().UTC())
	if !errors.Is(err, ErrPeopleChoiceUnavailable) {
		t.Fatalf("unregistered provider must deny, got %+v %v", result, err)
	}
}

// TestPeopleProviderChoiceApprovedExternalAndRevocationPG17 validates a
// deliberately created development-only, maker/checker-approved EXTERNAL
// provider against actual PostgreSQL 17 FKs, native ACTIVE PRIMARY binding,
// scope, support, approved EngineRelease and certification. These are fixture
// facts, not an IAM-issued workload, live ERP adapter or payroll acceptance.
func TestPeopleProviderChoiceApprovedExternalAndRevocationPG17(t *testing.T) {
	sourceDB := os.Getenv("TEST_DATABASE_URL")
	if sourceDB == "" {
		t.Skip("requires real PostgreSQL 17 TEST_DATABASE_URL")
	}
	ctx := context.Background()
	// The authoritative Shared People v1 capabilities remain DRAFT. This
	// test intentionally never alters them in the shared CI database: use
	// a separate, disposable PostgreSQL 17 database to prove the exact
	// hypothetical ACTIVE lifecycle without manufacturing runtime proof.
	root, err := pgxpool.New(ctx, sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[20:]
	dbName := "baobab_people_test_" + suffix
	if _, err := root.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := root.Exec(context.Background(), "DROP DATABASE "+dbName+" WITH (FORCE)"); e != nil {
			t.Errorf("disposable PG17 database cleanup: %v", e)
		}
	})
	databaseURI, err := neturl.Parse(sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	databaseURI.Path = "/" + dbName
	databaseURI.RawPath = ""
	url := databaseURI.String()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err = store.ApplyMigrations(ctx); err != nil {
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

	engine := "baobab-people-test-" + suffix
	providerKey := engine + ".external"
	tenant := "tn_people" + suffix
	employer := "TEST-PEOPLE-" + strings.ToUpper(suffix)
	const capabilityKey = "payroll.run.read"
	// These IDs are independently read from the disposable database,
	// never asserted to be production IAM/CP/ERP qualifications.
	var engineID, instanceID, providerID, capabilityID, scopeID, bindingID, releaseID, certID string
	one := func(dest *string, sql string, args ...any) {
		t.Helper()
		if e := admin.QueryRow(ctx, sql, args...).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := admin.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	one(&engineID, "INSERT INTO topology.engine (code,name) VALUES ($1,'People PG17 fixture') RETURNING engine_id::text", engine)
	one(&instanceID, "INSERT INTO topology.engine_instance (engine_id,region,environment,status) VALUES ($1::uuid,'af-south-1','development','ACTIVE') RETURNING engine_instance_id::text", engineID)
	one(&providerID, "INSERT INTO capability.capability_provider (provider_key,name,provider_type,engine_id,status) VALUES ($1,'External People fixture','EXTERNAL_SERVICE',$2::uuid,'ACTIVE') RETURNING provider_id::text", providerKey, engineID)
	// Isolated synthetic maturity: real Shared People v1 remains DRAFT.
	exec("INSERT INTO capability.capability (code,name,status) VALUES ($1,'People payroll read','ACTIVE')", capabilityKey)
	one(&capabilityID, "SELECT capability_id::text FROM capability.capability WHERE code=$1 AND status='ACTIVE'", capabilityKey)
	one(&scopeID, "INSERT INTO capability.capability_scope (tenant_id,legal_entity_id,jurisdiction,environment) VALUES ($1,$2,'ZA','development') RETURNING scope_id::text", tenant, employer)
	exec("INSERT INTO capability.provider_capability_support (provider_id,capability_id,contract_versions,status) VALUES ($1::uuid,$2::uuid,ARRAY[1],'ACTIVE')", providerID, capabilityID)
	one(&bindingID, "INSERT INTO capability.capability_binding (capability_id,engine_instance_id,scope_id,provider_id,binding_mode,status,contract_version,effective_from,version) VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,'PRIMARY','ACTIVE','1',now()-interval '1 hour',1) RETURNING id::text", capabilityID, instanceID, scopeID, providerID)
	one(&releaseID, `INSERT INTO topology.engine_release
		(engine_release_id,engine_id,release_version,source_revision,declaration_digest,content_digest,status,recorded_by,recorded_at,reason,status_changed_by,status_changed_at,status_reason)
		VALUES (gen_random_uuid(),$1::uuid,'1.0.0',repeat('a',40),'sha256:'||repeat('b',64),'sha256:'||repeat('c',64),'APPROVED','pg17-test',now(),'Development fixture.','pg17-reviewer',now(),'Approved for isolated test.')
		RETURNING engine_release_id::text`, engineID)
	// An APPROVED release without the exact immutable provider/capability/
	// contract-major support entry must never qualify an external provider.
	exec("INSERT INTO topology.engine_release_provider_support (engine_release_id,ordinal,provider_key,capability_key,contract_versions) VALUES ($1::uuid,0,$2,$3,ARRAY[1])", releaseID, providerKey, capabilityKey)
	one(&certID, `INSERT INTO capability.provider_capability_certification
		(certification_id,provider_id,capability_id,contract_version,engine_release_id,qualification_profile,evidence,request_digest,status,certified_by,certified_at,reason)
		VALUES (gen_random_uuid(),$1::uuid,$2::uuid,1,$3::uuid,'people-pg17-fixture','[{"fixture":"pg17"}]'::jsonb,'sha256:'||repeat('d',64),'CERTIFIED','pg17-reviewer',now(),'Development source qualification.')
		RETURNING certification_id::text`, providerID, capabilityID, releaseID)
	// Maker/checker are actual current CP human Principals, not arbitrary UUIDs.
	// Separate invalid attempts prove the database refuses fabricated approval.
	var makerID, checkerID, workloadID string
	one(&makerID, "INSERT INTO identity.principal(actor_type,status) VALUES ('human','ACTIVE') RETURNING principal_id::text")
	one(&checkerID, "INSERT INTO identity.principal(actor_type,status) VALUES ('human','ACTIVE') RETURNING principal_id::text")
	one(&workloadID, "INSERT INTO identity.principal(actor_type,status) VALUES ('workload','ACTIVE') RETURNING principal_id::text")
	choiceInsert := `INSERT INTO capability.people_provider_choice
		(tenant_id,legal_employer_id,jurisdiction,capability_key,binding_id,provider_id,binding_revision,certification_id,source_of_record,status,effective_from,proposed_by,approved_by,approved_at,choice_evidence_ref)
		VALUES ($1,$2,'ZA',$3,$4::uuid,$5::uuid,1,$6::uuid,'EXTERNAL','ACTIVE',now()-interval '1 hour',$7::uuid,$8::uuid,now(),'qualified-pg17-development')`
	for label, actors := range map[string][2]string{
		"fabricated maker UUID": {domain.NewUUIDv7(), checkerID},
		"fabricated checker UUID": {makerID, domain.NewUUIDv7()},
		"workload checker": {makerID, workloadID},
		"workload maker": {workloadID, checkerID},
		"self approval": {makerID, makerID},
	} {
		t.Run(label, func(t *testing.T) {
			if _, e := admin.Exec(ctx, choiceInsert, tenant, employer, capabilityKey, bindingID, providerID, certID,
				actors[0], actors[1]); e == nil {
				t.Fatal("unqualified or forged maker/checker activated a provider")
			}
		})
	}
	exec(choiceInsert, tenant, employer, capabilityKey, bindingID, providerID, certID, makerID, checkerID)

	current, err := repo.ReadPeopleChoice(ctx, tenant, employer, "ZA", capabilityKey, time.Now().UTC())
	if err != nil || current.SourceOfRecord != "EXTERNAL" || current.BindingID != bindingID ||
		current.ProviderID != providerID || current.BindingRevision != 1 || current.CertificationKey == "" {
		t.Fatalf("real committed PG17 provider choice did not join exactly: %+v %v", current, err)
	}
	for name, tuple := range map[string][4]string{
		"foreign tenant":       {"tn_foreign", employer, "ZA", capabilityKey},
		"foreign employer":     {tenant, "OTHER-EMPLOYER", "ZA", capabilityKey},
		"foreign jurisdiction": {tenant, employer, "UG", capabilityKey},
		"foreign capability":   {tenant, employer, "ZA", "workforce.employment.read"},
	} {
		t.Run(name, func(t *testing.T) {
			_, e := repo.ReadPeopleChoice(ctx, tuple[0], tuple[1], tuple[2], tuple[3], time.Now().UTC())
			if !errors.Is(e, ErrPeopleChoiceUnavailable) {
				t.Fatalf("foreign scope must fail closed: %v", e)
			}
		})
	}
	// DB-level exclusion, not the handler, enforces one ACTIVE primary
	// provider per exact employer and overlapping effective period.
	if _, err := admin.Exec(ctx, `INSERT INTO capability.people_provider_choice
		(tenant_id,legal_employer_id,jurisdiction,capability_key,binding_id,provider_id,binding_revision,certification_id,source_of_record,status,effective_from,proposed_by,approved_by,approved_at,choice_evidence_ref)
		SELECT tenant_id,legal_employer_id,jurisdiction,capability_key,binding_id,provider_id,binding_revision,certification_id,source_of_record,status,effective_from,proposed_by,approved_by,approved_at,choice_evidence_ref
		FROM capability.people_provider_choice WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("overlapping ACTIVE employer provider was accepted")
	}
	// Canonical People DRAFT lifecycle is never eligible even with a
	// complete choice/binding/release/certification in this isolated fixture.
	exec("UPDATE capability.capability SET status='DRAFT' WHERE capability_id=$1::uuid", capabilityID)
	if _, err := repo.ReadPeopleChoice(ctx, tenant, employer, "ZA", capabilityKey, time.Now().UTC()); !errors.Is(err, ErrPeopleChoiceUnavailable) {
		t.Fatalf("DRAFT People capability must deny: %v", err)
	}
	exec("UPDATE capability.capability SET status='ACTIVE' WHERE capability_id=$1::uuid", capabilityID)
	// Provider/instance lineage is current authority: an inactive instance
	// must not be rescued by an otherwise CERTIFIED and APPROVED release.
	exec("UPDATE topology.engine_instance SET status='DRAINING' WHERE engine_instance_id=$1::uuid", instanceID)
	if _, err := repo.ReadPeopleChoice(ctx, tenant, employer, "ZA", capabilityKey, time.Now().UTC()); !errors.Is(err, ErrPeopleChoiceUnavailable) {
		t.Fatalf("inactive provider engine instance must deny: %v", err)
	}
	exec("UPDATE topology.engine_instance SET status='ACTIVE' WHERE engine_instance_id=$1::uuid", instanceID)
	// Certification revocation makes the previously valid choice instantly
	// ineligible even when the decision row and PRIMARY binding stay ACTIVE.
	exec("UPDATE capability.provider_capability_certification SET status='REVOKED',revoked_by='pg17-reviewer',revoked_at=now(),revocation_reason='Fixture revocation.' WHERE certification_id=$1::uuid", certID)
	if _, err := repo.ReadPeopleChoice(ctx, tenant, employer, "ZA", capabilityKey, time.Now().UTC()); !errors.Is(err, ErrPeopleChoiceUnavailable) {
		t.Fatalf("revoked certification must deny: %v", err)
	}
}
