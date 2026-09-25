package repository

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nabhold/baobab-cp/internal/domain"
)

// ADR-BCP-018 gate ORG-16 — production readiness (sections 171-176)
// against real PostgreSQL. Set TEST_DATABASE_URL to run; skipped otherwise.

// organisationTables are the tables a section 173 path must never scan
// sequentially.
var organisationTables = []string{
	"canonical_entity", "organisation_profile", "legal_entity_profile", "corporate_relationship",
	"corporate_group_membership", "platform_relationship", "platform_account_membership",
	"tenant_organisation_mapping", "tenant_legal_entity_mapping", "iam_organisation_reference",
	"counterparty_role", "organisation_resolution_candidate",
}

// explain returns the plan of sql inside tx.
func explain(t *testing.T, f *orgFixture, tx pgx.Tx, sql string, args ...any) string {
	t.Helper()
	rows, err := tx.Query(f.ctx, "EXPLAIN "+sql, args...)
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, sql)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}

func seqScannedOrganisationTable(plan string) (string, bool) {
	for _, table := range organisationTables {
		if strings.Contains(plan, "Seq Scan on "+table+" ") || strings.Contains(plan, "Seq Scan on "+table+"\n") {
			return table, true
		}
	}
	return "", false
}

// TestOrganisationQueryPathsUseIndexes is section 173: every listed query
// path is served by an index. With sequential scans disabled the planner
// still falls back to one when no index applies, so a Seq Scan in the plan
// is a missing index. Recursive walks and lineage run the repository's own
// SQL; the single-table paths repeat the repository predicates.
func TestOrganisationQueryPathsUseIndexes(t *testing.T) {
	f := newOrgFixture(t)
	org, at := f.organisation(t, "Plan"), f.at
	anyUUID := domain.NewUUIDv7()
	paths := []struct {
		name string
		sql  string
		args []any
	}{
		{"direct and current relationships (ListCorporateRelationshipsByOrganisation)", `SELECT corporate_relationship_id
			FROM registry.corporate_relationship
			WHERE (source_organisation_id=$1::uuid OR target_organisation_id=$1::uuid)
			  AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{org, at}},
		{"control ancestry as of a date", corporateControlAncestrySQL, []any{org, at, domain.MaxCorporateControlDepth}},
		{"control descendants as of a date", corporateControlDescendantsSQL, []any{org, at, domain.MaxCorporateControlDepth}},
		{"organisation -> group", `SELECT corporate_group_membership_id FROM registry.corporate_group_membership
			WHERE organisation_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{org, at}},
		{"group -> members as of a date", `SELECT corporate_group_membership_id FROM registry.corporate_group_membership
			WHERE corporate_group_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{anyUUID, at}},
		{"organisation -> platform relationships", `SELECT platform_relationship_id FROM registry.platform_relationship
			WHERE organisation_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{org, at}},
		{"live affiliates on a corporate basis (divestiture review)", `SELECT platform_relationship_id FROM registry.platform_relationship
			WHERE basis_relationship_id=$1::uuid AND status IN ` + liveStatuses, []any{anyUUID}},
		{"platform owners of a platform (INTERNAL eligibility)", `SELECT DISTINCT organisation_id FROM registry.platform_relationship
			WHERE platform_id=$1 AND relationship_type='PLATFORM_OWNER'
			  AND verification_state='VERIFIED' AND (status='ACTIVE' OR (status='ENDED' AND effective_to IS NOT NULL))
			  AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{"baobab-platform", at}},
		{"platform account -> members as of a date", `SELECT platform_account_membership_id FROM registry.platform_account_membership
			WHERE platform_account_id=$1::uuid AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{anyUUID, at}},
		{"tenant -> organisation", `SELECT tenant_organisation_mapping_id FROM registry.tenant_organisation_mapping
			WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{"tn_plan", at}},
		{"tenant -> legal entity", `SELECT tenant_legal_entity_mapping_id FROM registry.tenant_legal_entity_mapping
			WHERE tenant_id=$1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, []any{"tn_plan", at}},
		{"IAM organisation -> organisation", `SELECT organisation_id FROM registry.iam_organisation_reference
			WHERE provider=$1 AND issuer=$2 AND provider_organisation_id=$3 AND status='ACTIVE'`, []any{"keycloak", "https://iam.example", "org"}},
		{"counterparty role held (context attestation)", `SELECT 1 FROM registry.counterparty_role
			WHERE organisation_id=$1::uuid AND tenant_id=$2 AND role=$3 AND status='ACTIVE'`, []any{org, "tn_plan", "BUYER"}},
		{"organisation audit lineage targets", organisationAuditTargetsSQL, []any{org}},
	}

	tx, err := f.admin.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err := tx.Exec(f.ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		plan := explain(t, f, tx, p.sql, p.args...)
		if table, ok := seqScannedOrganisationTable(plan); ok {
			t.Errorf("%s scans %s sequentially:\n%s", p.name, table, plan)
		}
	}
}

// TestCorporateGraphWalksScale is section 173 at volume: a corporate graph
// of ORG_SCALE_TEST organisations (default 5000) in a four-way ownership
// tree, planned on real statistics. Walks from a leaf and from a mid-tree
// organisation must use indexes and return exactly their own chain. It
// runs in one transaction that is rolled back, so the shared test
// database is untouched.
func TestCorporateGraphWalksScale(t *testing.T) {
	f := newOrgFixture(t)
	n := 5000
	if v := os.Getenv("ORG_SCALE_TEST"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 64 {
			t.Fatalf("ORG_SCALE_TEST must be an integer >= 64, got %q", v)
		}
	}
	tx, err := f.admin.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	start := time.Now()
	for _, seed := range []struct {
		sql  string
		args []any
	}{
		{`CREATE TEMP TABLE scale_org ON COMMIT DROP AS SELECT n, gen_random_uuid() AS id FROM generate_series(1, $1::int) n`, []any{n}},
		{`INSERT INTO registry.canonical_entity (canonical_entity_id, entity_type, status) SELECT id, 'ORGANISATION', 'active' FROM scale_org`, nil},
		// Organisation n is owned by organisation n/4 (for n >= 4).
		{`INSERT INTO registry.corporate_relationship (source_organisation_id, target_organisation_id, relationship_type,
			verification_state, status, effective_from, source_authority, evidence_references, verified_by, verified_at)
		 SELECT p.id, c.id, 'OWNS', 'VERIFIED', 'ACTIVE', $1::timestamptz, 'scale-test', '["evd_scale"]', 'principal:scale', $1::timestamptz
		 FROM scale_org c JOIN scale_org p ON p.n = c.n / 4 WHERE c.n >= 4`, []any{f.at}},
		{`INSERT INTO registry.platform_relationship (platform_id, organisation_id, relationship_type, status, effective_from, source_authority)
		 SELECT 'scale-platform', id, 'EXTERNAL_CLIENT', 'PENDING', $1::timestamptz, 'scale-test' FROM scale_org`, []any{f.at}},
		{`ANALYZE registry.canonical_entity, registry.corporate_relationship, registry.platform_relationship`, nil},
	} {
		if _, err := tx.Exec(f.ctx, seed.sql, seed.args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, seed.sql)
		}
	}
	seeded := time.Since(start)

	idOf := func(k int) string {
		var id string
		if err := tx.QueryRow(f.ctx, `SELECT id::text FROM scale_org WHERE n=$1`, k).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	depthOf := func(k int) (d int) {
		for ; k >= 4; k /= 4 {
			d++
		}
		return d
	}
	leaf, mid := n, n/16
	at := f.at.Add(time.Hour)
	for _, walk := range []struct {
		name, sql string
		from      int
		want      int
	}{
		{"ancestry of a leaf", corporateControlAncestrySQL, leaf, depthOf(leaf)},
		// The descendants of a mid-tree organisation: every organisation
		// below it, i.e. all k with k/4^j == mid for some j >= 1.
		{"descendants of a mid-tree organisation", corporateControlDescendantsSQL, mid, func() (c int) {
			for lo, hi := mid*4, mid*4+3; lo <= n; lo, hi = lo*4, hi*4+3 {
				c += min(hi, n) - lo + 1
			}
			return c
		}()},
	} {
		id := idOf(walk.from)
		if plan := explain(t, f, tx, walk.sql, id, at, domain.MaxCorporateControlDepth); strings.Contains(plan, "Seq Scan on corporate_relationship") {
			t.Errorf("%s scans corporate_relationship at %d organisations:\n%s", walk.name, n, plan)
		}
		began := time.Now()
		rows, err := tx.Query(f.ctx, walk.sql, id, at, domain.MaxCorporateControlDepth)
		if err != nil {
			t.Fatal(err)
		}
		got := 0
		for rows.Next() {
			got++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if got != walk.want {
			t.Errorf("%s returned %d relationships, want %d", walk.name, got, walk.want)
		}
		t.Logf("%d organisations (seeded in %s): %s -> %d relationships in %s", n, seeded.Round(time.Millisecond), walk.name, got, time.Since(began).Round(time.Microsecond))
	}
}

// TestAuditEventsAreAppendOnly is ADR-BCP-008 section 37 with migration
// 000049: no role, including the table owner, can rewrite or remove audit
// history.
func TestAuditEventsAreAppendOnly(t *testing.T) {
	f := newOrgFixture(t)
	org := f.organisation(t, "Audited")
	var auditID string
	if err := f.admin.QueryRow(f.ctx, `SELECT audit_id::text FROM audit_events WHERE target=$1 LIMIT 1`, "organisation/"+org).Scan(&auditID); err != nil {
		t.Fatalf("the fixture's audit row: %v", err)
	}
	for name, stmt := range map[string]string{
		"UPDATE":   `UPDATE audit_events SET result='tampered' WHERE audit_id='` + auditID + `'::uuid`,
		"DELETE":   `DELETE FROM audit_events WHERE audit_id='` + auditID + `'::uuid`,
		"TRUNCATE": `TRUNCATE audit_events`,
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := f.admin.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			// Fail fast rather than queue behind other tests' writers.
			if _, err := tx.Exec(f.ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(f.ctx, stmt)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" || !strings.Contains(pgErr.Message, "append-only") {
				t.Fatalf("%s must be refused as append-only, got %v", name, err)
			}
		})
	}
	var result *string
	if err := f.admin.QueryRow(f.ctx, `SELECT result FROM audit_events WHERE audit_id=$1::uuid`, auditID).Scan(&result); err != nil || (result != nil && *result == "tampered") {
		t.Fatalf("audit row changed: %v %v", result, err)
	}
}

func violationsOf(r IntegrityReport, check, resourceID string) bool {
	for _, v := range r.Violations {
		if v.Check == check && v.ResourceID == resourceID {
			return true
		}
	}
	return false
}

func mentions(r IntegrityReport, ids ...string) []IntegrityViolation {
	var out []IntegrityViolation
	for _, v := range r.Violations {
		for _, id := range ids {
			if v.ResourceID == id {
				out = append(out, v)
			}
		}
	}
	return out
}

// TestOrganisationIntegrityVerification is section 176: after a restore,
// the cross-table invariants no constraint enforces are checked. Records
// written through the repository pass; each kind of corruption a partial
// or mismatched restore can produce is reported, with a count.
func TestOrganisationIntegrityVerification(t *testing.T) {
	f := newOrgFixture(t)
	const limit = 1_000_000 // every violation: the database is shared

	// A consistent fixture written through the repository.
	parent, child := f.organisation(t, "Parent"), f.organisation(t, "Child")
	edge := f.edge(t, parent, child)
	if err := f.repo.VerifyCorporateRelationship(f.ctx, edge, f.evidence(), f.actor()); err != nil {
		t.Fatal(err)
	}
	le := uniqueLE()
	tenant := f.tenant(t, le)
	if _, err := f.repo.EnsureDefaultTenantLegalEntityMapping(f.ctx, tenant, le, "test", f.at, f.actor()); err != nil {
		t.Fatal(err)
	}
	edgeRow, _ := domain.ParseResourceID(domain.CorporateRelationshipIDPrefix, edge)
	clean, err := f.repo.VerifyOrganisationIntegrity(f.ctx, limit)
	if err != nil {
		t.Fatal(err)
	}
	if got := mentions(clean, parent, child, edge, tenant); len(got) != 0 {
		t.Fatalf("repository-written records must verify cleanly: %+v", got)
	}

	// Corruption as a partial restore would leave it.
	var (
		orphan, derived, badAffiliate, group, membership, unaudited, notAnOrg string
		other                                                                 = f.organisation(t, "Other")
	)
	mustScan := func(sql string, dst *string, args ...any) {
		t.Helper()
		if err := f.admin.QueryRow(f.ctx, sql, args...).Scan(dst); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	mustScan(`SELECT gen_random_uuid()::text`, &orphan)
	mustScan(`INSERT INTO registry.corporate_relationship (source_organisation_id, target_organisation_id, relationship_type,
		direct_or_derived, basis_relationship_ids, derived_at, derivation_version, effective_from, source_authority)
		VALUES ($1::uuid, $2::uuid, 'CONTROLS', 'DERIVED', ARRAY[$3::uuid], now(), 'v1', now(), 'test')
		RETURNING corporate_relationship_id::text`, &derived, parent, other, orphan)
	mustScan(`INSERT INTO registry.corporate_group (display_name, grouping_policy, effective_from)
		VALUES ('Restored group', 'CONTROL', now()) RETURNING corporate_group_id::text`, &group)
	mustScan(`INSERT INTO registry.corporate_group_membership (corporate_group_id, organisation_id, basis_relationship_ids,
		effective_from, derived_at, derivation_version) VALUES ($1::uuid, $2::uuid, ARRAY[$3::uuid], now(), now(), 'v1')
		RETURNING corporate_group_membership_id::text`, &membership, group, child, orphan)
	// An affiliate whose basis is ownership of a different organisation.
	mustScan(`INSERT INTO registry.platform_relationship (platform_id, organisation_id, relationship_type, basis_relationship_id,
		effective_from, source_authority) VALUES ('integrity-platform', $1::uuid, 'PLATFORM_GROUP_AFFILIATE', $2::uuid, now(), 'test')
		RETURNING platform_relationship_id::text`, &badAffiliate, other, edgeRow)
	// Verified with evidence, but its audit trail was not restored.
	mustScan(`INSERT INTO registry.corporate_relationship (source_organisation_id, target_organisation_id, relationship_type,
		verification_state, status, effective_from, source_authority, evidence_references, verified_by, verified_at)
		VALUES ($1::uuid, $2::uuid, 'OWNS', 'VERIFIED', 'ACTIVE', now(), 'test', '["evd_x"]', 'principal:x', now())
		RETURNING corporate_relationship_id::text`, &unaudited, other, child)
	mustScan(`INSERT INTO registry.canonical_entity (entity_type, status) VALUES ('PRODUCT', 'active') RETURNING canonical_entity_id::text`, &notAnOrg)
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO registry.organisation_profile (canonical_entity_id, display_name, source_authority, status, effective_from)
		VALUES ($1::uuid, 'Not an organisation', 'test', 'ACTIVE', now())`, notAnOrg); err != nil {
		t.Fatal(err)
	}
	// A tenant row restored from a different point in time than its mapping.
	divergent := uniqueLE()
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1)`, divergent); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `UPDATE tenants SET legal_entity_id=$2 WHERE tenant_id=$1`, tenant, divergent); err != nil {
		t.Fatal(err)
	}
	unmapped := f.tenant(t, uniqueLE()) // written around the repository: no DEFAULT mapping

	report, err := f.repo.VerifyOrganisationIntegrity(f.ctx, limit)
	if err != nil {
		t.Fatal(err)
	}
	contractID := func(prefix, row string) string {
		id, err := domain.FormatResourceID(prefix, row)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	unauditedID := contractID(domain.CorporateRelationshipIDPrefix, unaudited)
	derivedID := contractID(domain.CorporateRelationshipIDPrefix, derived)
	membershipID := contractID(domain.CorporateGroupMembershipIDPrefix, membership)
	affiliateID := contractID(domain.PlatformRelationshipIDPrefix, badAffiliate)
	for _, want := range []struct{ check, id string }{
		{"DANGLING_DERIVED_BASIS", derivedID},
		{"DANGLING_GROUP_MEMBERSHIP_BASIS", membershipID},
		{"AFFILIATE_BASIS_SHAPE", affiliateID},
		{"VERIFIED_WITHOUT_AUDIT_PROVENANCE", unauditedID},
		{"NON_ORGANISATION_REFERENCED", notAnOrg},
		{"TENANT_LEGAL_ENTITY_PROJECTION", tenant},
		{"TENANT_LEGAL_ENTITY_PROJECTION", unmapped},
	} {
		if !violationsOf(report, want.check, want.id) {
			t.Errorf("%s not reported for %s", want.check, want.id)
		}
	}
	if report.OK() || report.ByCheck["AFFILIATE_BASIS_SHAPE/PLATFORM_RELATIONSHIP"] < 1 {
		t.Fatalf("counts must include the violations: %+v", report.ByCheck)
	}
	if got := mentions(report, parent, edge); len(got) != 0 {
		t.Fatalf("consistent records must not be reported: %+v", got)
	}

	// The per-check limit bounds the report without hiding the count.
	small, err := f.repo.VerifyOrganisationIntegrity(f.ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !small.Truncated || small.ByCheck["TENANT_LEGAL_ENTITY_PROJECTION/TENANT"] < 2 || len(small.Violations) > len(integrityChecks) {
		t.Fatalf("a limit of 1 must truncate and keep full counts: truncated=%v counts=%v n=%d", small.Truncated, small.ByCheck, len(small.Violations))
	}
	if _, err := f.repo.VerifyOrganisationIntegrity(f.ctx, 0); err == nil {
		t.Fatal("a non-positive limit must be refused")
	}
}
