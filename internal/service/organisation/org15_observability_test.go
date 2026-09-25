package organisation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/auth"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/metrics"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/service"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// ADR-BCP-018 gate ORG-15 against real PostgreSQL. Assertions are scoped to
// each test's own records: other tests share the database.

func (e *env) drift(t *testing.T, at time.Time) domain.RelationshipDriftReport {
	t.Helper()
	report, err := e.repo.DetectRelationshipDrift(e.ctx, at, 1000)
	if err != nil {
		t.Fatalf("detect drift: %v", err)
	}
	return report
}

func findingFor(report domain.RelationshipDriftReport, resourceID string) (domain.RelationshipDriftFinding, bool) {
	for _, f := range report.Findings {
		if f.ResourceID == resourceID {
			return f, true
		}
	}
	return domain.RelationshipDriftFinding{}, false
}

func (e *env) version(t *testing.T, id string) int64 {
	t.Helper()
	var v int64
	if err := e.admin.QueryRow(e.ctx, `SELECT version FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestRelationshipDriftIsObservedNotCascaded is ADR-BCP-018 sections
// 127-129: a divestiture that ends an affiliate's ownership basis, and a
// suspended organisation still mapped to its tenant and holding a role, are
// reported as actionable drift of the right severity, and nothing changes.
func TestRelationshipDriftIsObservedNotCascaded(t *testing.T) {
	e := newEnv(t)
	sub, provisioned, req := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	f := newLifecycleFixture(t, e, sub)
	role, _, err := e.repo.EnsureCounterpartyRole(e.ctx, domain.CounterpartyRole{OrganisationID: sub, TenantID: req.TenantID,
		Role: domain.CounterpartyRoleBuyer, EffectiveFrom: e.at, SourceAuthority: "test"}, actor())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findingFor(e.drift(t, e.at.Add(time.Hour)), f.affiliate); ok {
		t.Fatal("an affiliate on an in-force verified basis is not drift")
	}

	reviewer := &CorporateChangeReviewer{Orgs: e.repo, Eligibility: f.resolver}
	if _, err := reviewer.EndCorporateRelationship(e.ctx, f.ownsSub, e.at.Add(2*time.Hour), "shares sold", actor()); err != nil {
		t.Fatal(err)
	}
	report := e.drift(t, e.at.Add(3*time.Hour))
	got, ok := findingFor(report, f.affiliate)
	if !ok || got.Rule != domain.DriftAffiliateBasisNotInForce || got.Severity != "CRITICAL" || got.Remediation != "REVIEW" ||
		got.AutoRepairable || !slices.Equal(got.RelatedResourceIDs, []string{f.ownsSub}) || !slices.Equal(got.OrganisationIDs, []string{sub}) {
		t.Fatalf("affiliate drift = %+v %v", got, ok)
	}
	if report.BySeverity["CRITICAL"] < 1 {
		t.Fatalf("by_severity %v must count the critical finding", report.BySeverity)
	}
	// Observing drift changes nothing (section 129).
	if pr, err := e.repo.GetPlatformRelationship(e.ctx, f.affiliate); err != nil || pr.Status != domain.RelationshipStatusActive {
		t.Fatalf("affiliate after drift detection: %+v %v", pr, err)
	}

	if err := e.repo.SuspendOrganisationEntity(e.ctx, sub, e.version(t, sub), e.at.Add(4*time.Hour), "sanctions review", actor()); err != nil {
		t.Fatal(err)
	}
	report = e.drift(t, e.at.Add(5*time.Hour))
	mapping, ok := findingFor(report, provisioned.TenantOrganisationMapping)
	if !ok || mapping.Rule != domain.DriftTenantMappingToInactiveOrg || mapping.Severity != "DEGRADED" || mapping.TenantID != req.TenantID ||
		!strings.Contains(mapping.ObservedState, "SUSPENDED") {
		t.Fatalf("tenant mapping drift = %+v %v", mapping, ok)
	}
	if r, ok := findingFor(report, role); !ok || r.Rule != domain.DriftCounterpartyRoleOnInactiveOrg || r.Severity != "INFO" {
		t.Fatalf("counterparty role drift = %+v %v", r, ok)
	}
	for i := 1; i < len(report.Findings); i++ {
		if slices.Index(domain.DriftSeverities, report.Findings[i-1].Severity) > slices.Index(domain.DriftSeverities, report.Findings[i].Severity) {
			t.Fatal("findings must be ordered most severe first")
		}
	}
	if small, err := e.repo.DetectRelationshipDrift(e.ctx, e.at.Add(5*time.Hour), 1); err != nil || len(small.Findings) != 1 || !small.Truncated {
		t.Fatalf("a page smaller than the findings is truncated: %+v %v", small, err)
	}
}

// TestOrganisationSuspensionIsAtomic is ADR-BCP-018 section 124's
// "Organisation suspended": the canonical entity, its profile and the event
// change together, only from ACTIVE at the expected version, and a
// suspended organisation can no longer be attested in context.
func TestOrganisationSuspensionIsAtomic(t *testing.T) {
	e := newEnv(t)
	org, _, req := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	if _, err := e.admin.Exec(e.ctx, `UPDATE tenants SET observed_state='active' WHERE tenant_id=$1`, req.TenantID); err != nil {
		t.Fatal(err)
	}
	v := e.version(t, org)
	if err := e.repo.SuspendOrganisationEntity(e.ctx, org, v+1, e.at, "review", actor()); err == nil {
		t.Fatal("a stale version must be refused")
	}
	canonical := service.CanonicalEntityService{Repository: e.repo, Organisations: e.repo}
	suspending := actor()
	entity, err := canonical.SuspendAs(e.ctx, org, v, suspending)
	if err != nil || entity.Status != "SUSPENDED" || entity.Version != v+1 {
		t.Fatalf("suspend: %+v %v", entity, err)
	}
	if got := e.outboxTypes(t, suspending.CorrelationID); !slices.Equal(got, []string{events.OrganisationSuspended}) {
		t.Fatalf("suspension published %v", got)
	}
	var entityStatus, profileStatus string
	if err := e.admin.QueryRow(e.ctx, `SELECT ce.status, p.status FROM registry.canonical_entity ce
		JOIN registry.organisation_profile p ON p.canonical_entity_id = ce.canonical_entity_id
		WHERE ce.canonical_entity_id=$1::uuid`, org).Scan(&entityStatus, &profileStatus); err != nil || entityStatus != "suspended" || profileStatus != "SUSPENDED" {
		t.Fatalf("after suspension: entity %q profile %q %v", entityStatus, profileStatus, err)
	}
	if _, err := canonical.SuspendAs(e.ctx, org, v+1, actor()); err == nil {
		t.Fatal("a suspended organisation cannot be suspended again")
	}
	var product string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ('PRODUCT','active') RETURNING canonical_entity_id::text`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.SuspendOrganisationEntity(e.ctx, product, 1, e.at, "review", actor()); !errors.Is(err, repository.ErrNotAnOrganisation) {
		t.Fatalf("a non-organisation must be refused: %v", err)
	}

	tenants, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tenants.Close)
	resolver := service.ContextResolutionService{
		Identity: service.IdentityService{Repository: repository.NewInMemoryRepository(), Provision: service.WorkloadOnlyProvisioningPolicy},
		Tenants:  tenants, Canonical: e.repo, Mappings: e.repo,
	}
	before := metrics.RelationshipResolutionFailures.Value(metrics.OutcomeInactive)
	principal := auth.Principal{Subject: "baobab-trade", Issuer: "https://iam.baobab-platform.test/realms/baobab", ActorType: "workload",
		TenantID: req.TenantID, ClientID: "baobab-trade", TokenID: "token-" + token()}
	if _, _, err := resolver.Resolve(e.ctx, principal, req.TenantID, org, domain.NewUUIDv7(), time.Now().UTC()); err == nil {
		t.Fatal("a suspended organisation must not be attested")
	}
	if after := metrics.RelationshipResolutionFailures.Value(metrics.OutcomeInactive); after != before+1 {
		t.Fatalf("relationship_resolution_failure_total{outcome=inactive} went %d -> %d", before, after)
	}
}

// TestOrganisationAuditLineage is ADR-BCP-018 section 131: an
// organisation's lineage includes who recorded and verified its ownership,
// its platform relationships and tenant mapping, newest first, paged
// without gaps or overlaps, and excludes other organisations' changes.
func TestOrganisationAuditLineage(t *testing.T) {
	e := newEnv(t)
	sub, _, _ := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	f := newLifecycleFixture(t, e, sub)
	other, _, _ := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")

	lineage, err := e.repo.ListOrganisationAudit(e.ctx, sub, 500, "")
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for i, entry := range lineage {
		actions[entry.Action] = true
		if entry.Actor.ActorID == "" || entry.Actor.ActorType == "" {
			t.Fatalf("entry %+v has no actor", entry)
		}
		if i > 0 && entry.OccurredAt.After(lineage[i-1].OccurredAt) {
			t.Fatal("lineage must be newest first")
		}
		var payload map[string]any
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(entry.Payload), other) {
			t.Fatalf("lineage of %s includes %s's change: %s", sub, other, entry.Payload)
		}
	}
	for _, want := range []string{"corporate_relationship.recorded", "corporate_relationship.verified", "platform_relationship.recorded",
		"platform_relationship.verified", "tenant_organisation_mapping.activated", "organisation.created"} {
		if !actions[want] {
			t.Errorf("lineage lacks %s; has %v", want, actions)
		}
	}
	// The owner's lineage includes the same ownership fact from its side.
	if owner, err := e.repo.ListOrganisationAudit(e.ctx, f.owner, 500, ""); err != nil || len(owner) == 0 {
		t.Fatalf("owner lineage: %d %v", len(owner), err)
	}

	first, err := e.repo.ListOrganisationAudit(e.ctx, sub, 2, "")
	if err != nil || len(first) != 2 {
		t.Fatalf("first page: %d %v", len(first), err)
	}
	second, err := e.repo.ListOrganisationAudit(e.ctx, sub, 2, first[1].AuditID)
	if err != nil || len(second) == 0 || second[0].AuditID != lineage[2].AuditID {
		t.Fatalf("second page must continue exactly after the first: %+v %v", second, err)
	}

	// The lineage query can use migration 000048's partial index, which it
	// only can while both repeat the same action predicate.
	tx, err := e.admin.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(e.ctx)
	if _, err := tx.Exec(e.ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	rows, err := tx.Query(e.ctx, `EXPLAIN SELECT audit_id FROM audit_events a WHERE a.`+repository.OrganisationAuditActionPredicate+` AND a.payload @> $1::jsonb`,
		`{"organisation_id":"`+sub+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if !strings.Contains(plan.String(), "audit_organisation_payload_idx") {
		t.Fatalf("lineage query does not use the partial index:\n%s", plan.String())
	}
}

// TestOrganisationMetricsFollowTheSharedCatalogue checks the metrics the
// Control Plane exposes against ADR-BCP-018 section 130 as published in
// Shared, and that state gauges read real counts.
func TestOrganisationMetricsFollowTheSharedCatalogue(t *testing.T) {
	e := newEnv(t)
	e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	registry := metrics.NewRegistry()
	registry.Register(repository.OrganisationMetricsCollector{Repo: e.repo, Now: func() time.Time { return e.at.Add(time.Hour) }})
	families, err := registry.Gather(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := metrics.Default.Gather(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range append(families, defaults...) {
		names[f.Name] = true
		if f.Name == "organisation_total" {
			active := 0.0
			for _, s := range f.Samples {
				if s.Labels["status"] == "ACTIVE" {
					active = s.Value
				}
			}
			if active < 1 {
				t.Fatalf("organisation_total{status=ACTIVE} = %v; want at least the provisioned organisation", active)
			}
		}
		for _, s := range f.Samples {
			for label := range s.Labels {
				if !slices.Contains([]string{"status", "verification_state", "relationship_type", "rule", "severity", "outcome"}, label) {
					t.Fatalf("%s carries label %q outside the bounded catalogue", f.Name, label)
				}
			}
		}
	}

	dir := contracttest.SharedDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "contracts", "organisation", "v1", "observability.schema.json"))
	if err != nil {
		t.Skip("pinned baobab-platform/shared revision has no observability.schema.json yet")
	}
	var schema struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	catalogue := schema.Defs["organisationMetric"].Enum
	for _, name := range catalogue {
		if !names[name] {
			t.Errorf("section 130 metric %s is not exposed", name)
		}
	}
	for name := range names {
		if !slices.Contains(catalogue, name) {
			t.Errorf("exposed metric %s is not in the Shared catalogue", name)
		}
	}
}

// TestOrganisationObservabilityConformsToSharedContract validates drift
// reports and audit lineage against the pinned Shared schema.
func TestOrganisationObservabilityConformsToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	if _, err := os.Stat(filepath.Join(dir, "contracts", "organisation", "v1", "observability.schema.json")); err != nil {
		t.Skip("pinned baobab-platform/shared revision has no observability.schema.json yet")
	}
	e := newEnv(t)
	reportSchema := contracttest.CompileSchema(t, dir, "organisation/v1/observability.schema.json#/$defs/RelationshipDriftReport")
	auditSchema := contracttest.CompileSchema(t, dir, "organisation/v1/observability.schema.json#/$defs/OrganisationAuditEntry")
	sub, _, _ := e.tenantWithOrganisation(t, domain.PlatformRelExternalClient, "")
	f := newLifecycleFixture(t, e, sub)
	reviewer := &CorporateChangeReviewer{Orgs: e.repo, Eligibility: f.resolver}
	if _, err := reviewer.EndCorporateRelationship(e.ctx, f.ownsSub, e.at.Add(time.Hour), "sold", actor()); err != nil {
		t.Fatal(err)
	}
	contracttest.ValidateJSON(t, reportSchema, e.drift(t, e.at.Add(2*time.Hour)))
	lineage, err := e.repo.ListOrganisationAudit(e.ctx, sub, 50, "")
	if err != nil || len(lineage) == 0 {
		t.Fatalf("lineage %d %v", len(lineage), err)
	}
	for _, entry := range lineage {
		contracttest.ValidateJSON(t, auditSchema, entry)
	}
}
