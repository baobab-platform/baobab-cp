package organisation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nabhold/baobab-cp/internal/contracttest"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// ADR-BCP-018 gate ORG-13 against real PostgreSQL. The database is shared
// with other tests that run concurrently, so every assertion is scoped to
// the records a test creates, never to global counts.

// legacyEntity inserts an ADR-BCP-016 bounded record as registration did.
func (e *env) legacyEntity(t *testing.T, kind, status, tenantID string) (id, key string) {
	t.Helper()
	key = strings.ToLower(strings.TrimSuffix(kind, "_ORGANISATION")) + ":estate:" + token()
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO registry.canonical_entity (entity_type, status, tenant_id, external_key)
		VALUES ($1, $2, NULLIF($3,''), $4) RETURNING canonical_entity_id::text`, kind, status, tenantID, key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, key
}

type profileRow struct{ display, state, source, status string }

func (e *env) profile(t *testing.T, org string) (profileRow, bool) {
	t.Helper()
	var p profileRow
	err := e.admin.QueryRow(e.ctx, `SELECT display_name, verification_state, source_authority, status
		FROM registry.organisation_profile WHERE canonical_entity_id=$1::uuid`, org).Scan(&p.display, &p.state, &p.source, &p.status)
	if err != nil {
		return p, false
	}
	return p, true
}

func (e *env) rolesOf(t *testing.T, org string) []domain.CounterpartyRole {
	t.Helper()
	var tenants []string
	rows, err := e.admin.Query(e.ctx, `SELECT DISTINCT tenant_id FROM registry.counterparty_role WHERE organisation_id=$1::uuid`, org)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tn string
		if err := rows.Scan(&tn); err != nil {
			t.Fatal(err)
		}
		tenants = append(tenants, tn)
	}
	rows.Close()
	var out []domain.CounterpartyRole
	for _, tn := range tenants {
		roles, err := e.repo.ListCounterpartyRoles(e.ctx, tn, org)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, roles...)
	}
	return out
}

func (e *env) reconcile(t *testing.T, a repository.AuditActor) ReconciliationReport {
	t.Helper()
	// A tiny batch exercises the chunked backfill loop.
	report, err := (&CounterpartyReconciler{Repo: e.repo, BatchSize: 2, Now: func() time.Time { return e.at.Add(48 * time.Hour) }}).Run(e.ctx, a)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return report
}

// TestReconciliationMigratesLegacyBuyerAndSupplierRecords is ADR-BCP-018
// section 112 phases 1-2: every legacy record keeps its id and entity type
// and gains an UNVERIFIED profile; records in use gain a role in their
// owning tenant; reruns change nothing and an ended role is never recreated.
func TestReconciliationMigratesLegacyBuyerAndSupplierRecords(t *testing.T) {
	e := newEnv(t)
	zuri := e.tenantFor(t, "LE-"+strings.ToUpper(token()))
	thamani := e.tenantFor(t, "LE-"+strings.ToUpper(token()))
	buyer, buyerKey := e.legacyEntity(t, domain.EntityTypeBuyerOrganisation, "active", zuri)
	supplier, _ := e.legacyEntity(t, domain.EntityTypeSupplierOrganisation, "suspended", thamani)
	draft, _ := e.legacyEntity(t, domain.EntityTypeBuyerOrganisation, "draft", zuri)
	retired, _ := e.legacyEntity(t, domain.EntityTypeSupplierOrganisation, "retired", thamani)
	orphan, _ := e.legacyEntity(t, domain.EntityTypeBuyerOrganisation, "active", "")
	a := actor()
	e.reconcile(t, a)

	for _, org := range []string{buyer, supplier, draft, retired, orphan} {
		p, ok := e.profile(t, org)
		if !ok || p.state != "UNVERIFIED" || p.source != repository.LegacySourceAuthority {
			t.Fatalf("%s: profile %+v present=%v; want an UNVERIFIED legacy profile", org, p, ok)
		}
	}
	if p, _ := e.profile(t, buyer); p.display != buyerKey || p.status != "ACTIVE" {
		t.Fatalf("buyer profile %+v: want the canonical key as display name and ACTIVE", p)
	}
	var kind string
	if err := e.admin.QueryRow(e.ctx, `SELECT entity_type FROM registry.canonical_entity WHERE canonical_entity_id=$1::uuid`, buyer).Scan(&kind); err != nil || kind != domain.EntityTypeBuyerOrganisation {
		t.Fatalf("legacy entity type must be preserved, got %q %v", kind, err)
	}
	want := map[string]struct{ tenant, role, status, legacy string }{
		buyer:    {zuri, "BUYER", "ACTIVE", domain.EntityTypeBuyerOrganisation},
		supplier: {thamani, "SUPPLIER", "SUSPENDED", domain.EntityTypeSupplierOrganisation},
		draft:    {zuri, "BUYER", "PENDING", domain.EntityTypeBuyerOrganisation},
	}
	for org, w := range want {
		roles := e.rolesOf(t, org)
		if len(roles) != 1 || roles[0].TenantID != w.tenant || roles[0].Role != w.role || roles[0].Status != w.status ||
			roles[0].LegacyEntityType != w.legacy || roles[0].SourceAuthority != repository.LegacySourceAuthority {
			t.Fatalf("%s roles %+v; want %+v", org, roles, w)
		}
	}
	for _, org := range []string{retired, orphan} {
		if roles := e.rolesOf(t, org); len(roles) != 0 {
			t.Fatalf("%s: a retired or tenant-less record must gain no role, got %+v", org, roles)
		}
	}
	if audits, _ := e.recordedUnder(t, a); audits == 0 {
		t.Fatal("the migration must be audited")
	}

	// Rerunning changes nothing for these records; an ended role stays ended.
	role := e.rolesOf(t, buyer)[0]
	if err := e.repo.EndCounterpartyRole(e.ctx, role.ID, e.at.Add(72*time.Hour), "relationship closed", actor()); err != nil {
		t.Fatal(err)
	}
	e.reconcile(t, actor())
	for org := range want {
		if roles := e.rolesOf(t, org); len(roles) != 1 {
			t.Fatalf("%s: rerun must not add roles, got %+v", org, roles)
		}
	}
	if roles := e.rolesOf(t, buyer); roles[0].Status != domain.RelationshipStatusEnded {
		t.Fatalf("an ended role must not be recreated: %+v", roles)
	}
	backlog, err := e.repo.LegacyBackfillBacklog(e.ctx)
	if err != nil || backlog["no owning tenant"] < 1 || backlog["not in use (no role)"] < 1 {
		t.Fatalf("backlog %v %v: want the orphan and the retired record reported", backlog, err)
	}
}

// candidateFor finds the candidate of a pair directly, independent of other
// tests' candidates.
func (e *env) candidateFor(t *testing.T, a, b string) (domain.OrganisationResolutionCandidate, bool) {
	t.Helper()
	var row string
	err := e.admin.QueryRow(e.ctx, `SELECT candidate_id::text FROM registry.organisation_resolution_candidate
		WHERE organisation_a=LEAST($1::uuid,$2::uuid) AND organisation_b=GREATEST($1::uuid,$2::uuid)`, a, b).Scan(&row)
	if err != nil {
		return domain.OrganisationResolutionCandidate{}, false
	}
	id, err := domain.FormatResourceID(domain.ResolutionCandidateIDPrefix, row)
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.repo.GetResolutionCandidate(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return c, true
}

func (e *env) claim(t *testing.T, le, name string, ids ...domain.OrganisationIdentifier) {
	t.Helper()
	if _, err := e.repo.RecordLegalEntityClaims(e.ctx, le, repository.LegalEntityClaims{LegalName: name, Identifiers: ids}, actor()); err != nil {
		t.Fatal(err)
	}
}

// TestReconciliationQuarantinesSharedGovernedIdentifiersOnly is ADR-BCP-018
// sections 99-101 and ADR-BCP-023 section 52: organisations sharing a
// governed identifier (after normalisation) are quarantined as a pair; a
// shared name, a conflicting jurisdiction or an ungoverned identifier
// never pairs; a DISTINCT decision holds until a new match appears; and no
// decision merges anything.
func TestReconciliationQuarantinesSharedGovernedIdentifiersOnly(t *testing.T) {
	e := newEnv(t)
	reg := "PVT-" + strings.ToUpper(token())
	spaced := strings.ToLower(strings.ReplaceAll(reg, "-", " "))
	_, alpha, alphaLE := e.registeredTenant(t)
	_, beta, betaLE := e.registeredTenant(t)
	_, sameName, sameNameLE := e.registeredTenant(t)
	_, uganda, ugandaLE := e.registeredTenant(t)
	_, customs, customsLE := e.registeredTenant(t)
	e.claim(t, alphaLE, "Alpha Coffee Ltd", domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: reg, IssuingJurisdiction: "KE"})
	e.claim(t, betaLE, "Alpha Coffee Limited", domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: spaced})
	e.claim(t, sameNameLE, "Alpha Coffee Ltd", domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: "OTHER-" + token(), IssuingJurisdiction: "KE"})
	e.claim(t, ugandaLE, "Alpha Coffee Uganda", domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: reg, IssuingJurisdiction: "UG"})
	e.claim(t, customsLE, "Alpha Customs", domain.OrganisationIdentifier{Type: "CUSTOMS_IDENTIFIER", Value: reg})

	report := e.reconcile(t, actor())
	c, ok := e.candidateFor(t, alpha, beta)
	if !ok || c.Status != domain.ResolutionCandidateOpen {
		t.Fatalf("alpha/beta share a registration after normalisation: %+v %v", c, ok)
	}
	norm := domain.NormaliseIdentifierValue(reg)
	if len(c.MatchedIdentifiers) != 1 || c.MatchedIdentifiers[0] != (domain.MatchedIdentifier{Type: "COMPANY_REGISTRATION", NormalisedValue: norm, IssuingJurisdiction: "KE"}) {
		t.Fatalf("matched identifiers %+v", c.MatchedIdentifiers)
	}
	if !contains(report.Candidates.Opened, c.ID) {
		t.Fatalf("report %+v does not list %s as opened", report.Candidates, c.ID)
	}
	for name, pair := range map[string][2]string{
		"same name, different registration": {alpha, sameName},
		"conflicting jurisdictions":         {alpha, uganda},
		"ungoverned identifier type":        {alpha, customs},
	} {
		if c, ok := e.candidateFor(t, pair[0], pair[1]); ok {
			t.Fatalf("%s must never pair: %+v", name, c)
		}
	}
	// beta states no jurisdiction, so it may be either registration: both
	// pairs are quarantined for a human to judge.
	if _, ok := e.candidateFor(t, beta, uganda); !ok {
		t.Fatal("an unstated jurisdiction must not hide a possible duplicate")
	}

	// Replay: still one OPEN candidate, unchanged.
	e.reconcile(t, actor())
	if again, _ := e.candidateFor(t, alpha, beta); again.ID != c.ID || again.Status != domain.ResolutionCandidateOpen {
		t.Fatalf("replay changed the candidate: %+v", again)
	}

	reviewer := actor()
	distinct := domain.ResolutionCandidateDecision{Decision: domain.ResolutionCandidateDistinct, Reason: "different registries", EvidenceReferences: []string{"evd_" + token()}}
	decided, changed, err := e.repo.DecideResolutionCandidate(e.ctx, c.ID, distinct, e.at.Add(49*time.Hour), reviewer)
	if err != nil || !changed || decided.Status != domain.ResolutionCandidateDistinct || decided.DecidedBy != reviewer.ActorID || decided.DecidedAt == nil {
		t.Fatalf("decide: %+v changed=%v %v", decided, changed, err)
	}
	if _, changed, err := e.repo.DecideResolutionCandidate(e.ctx, c.ID, distinct, e.at.Add(50*time.Hour), actor()); err != nil || changed {
		t.Fatalf("replaying a decision must change nothing: changed=%v %v", changed, err)
	}
	duplicate := domain.ResolutionCandidateDecision{Decision: domain.ResolutionCandidateDuplicateConfirmed, Reason: "same", SurvivingOrganisationID: alpha}
	if _, _, err := e.repo.DecideResolutionCandidate(e.ctx, c.ID, duplicate, e.at.Add(50*time.Hour), actor()); !errors.Is(err, repository.ErrResolutionCandidateDecided) {
		t.Fatalf("a different decision must not overwrite one: %v", err)
	}
	e.reconcile(t, actor())
	if held, _ := e.candidateFor(t, alpha, beta); held.Status != domain.ResolutionCandidateDistinct {
		t.Fatalf("a DISTINCT decision must hold while no new match appears: %+v", held)
	}

	// A new shared governed identifier reopens the pair for review.
	tax := domain.OrganisationIdentifier{Type: "TAX_IDENTIFIER", Value: "P" + strings.ToUpper(token()), IssuingJurisdiction: "KE"}
	e.claim(t, alphaLE, "Alpha Coffee Ltd", domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: reg, IssuingJurisdiction: "KE"}, tax)
	e.claim(t, betaLE, "Alpha Coffee Limited", domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: spaced}, tax)
	report = e.reconcile(t, actor())
	reopened, _ := e.candidateFor(t, alpha, beta)
	if reopened.Status != domain.ResolutionCandidateOpen || reopened.Decision != nil || len(reopened.MatchedIdentifiers) != 2 || !contains(report.Candidates.Reopened, c.ID) {
		t.Fatalf("a new match must reopen the pair: %+v report=%+v", reopened, report.Candidates)
	}

	wrong := duplicate
	wrong.SurvivingOrganisationID = sameName
	if _, _, err := e.repo.DecideResolutionCandidate(e.ctx, c.ID, wrong, e.at.Add(51*time.Hour), actor()); !errors.Is(err, repository.ErrResolutionDecisionInvalid) {
		t.Fatalf("the surviving organisation must be one of the pair: %v", err)
	}
	confirmed, _, err := e.repo.DecideResolutionCandidate(e.ctx, c.ID, duplicate, e.at.Add(51*time.Hour), actor())
	if err != nil || confirmed.Status != domain.ResolutionCandidateDuplicateConfirmed || confirmed.Decision.SurvivingOrganisationID != alpha {
		t.Fatalf("confirm duplicate: %+v %v", confirmed, err)
	}
	// Confirming a duplicate merges nothing (ADR-BCP-023 section 52).
	for _, org := range []string{alpha, beta} {
		if p, ok := e.profile(t, org); !ok || p.status != "ACTIVE" {
			t.Fatalf("%s must be untouched by a decision: %+v %v", org, p, ok)
		}
	}
}

// TestCounterpartyRoleLifecycle covers assignment, idempotency, the
// organisation-kind and tenant guards, and which roles can be relied on.
func TestCounterpartyRoleLifecycle(t *testing.T) {
	e := newEnv(t)
	tenantID, org, _ := e.registeredTenant(t)
	role := domain.CounterpartyRole{OrganisationID: org, TenantID: tenantID, Role: domain.CounterpartyRoleBuyer,
		EffectiveFrom: e.at, SourceAuthority: "test"}
	id, created, err := e.repo.EnsureCounterpartyRole(e.ctx, role, actor())
	if err != nil || !created {
		t.Fatalf("assign: %v created=%v", err, created)
	}
	if again, created, err := e.repo.EnsureCounterpartyRole(e.ctx, role, actor()); err != nil || created || again != id {
		t.Fatalf("replay must return the live role: %s created=%v %v", again, created, err)
	}
	if held, err := e.repo.HoldsCounterpartyRole(e.ctx, org, tenantID, domain.CounterpartyRoleBuyer, e.at.Add(time.Hour)); err != nil || !held {
		t.Fatalf("an ACTIVE role in effect is held: %v %v", held, err)
	}
	if held, _ := e.repo.HoldsCounterpartyRole(e.ctx, org, tenantID, domain.CounterpartyRoleBuyer, e.at.Add(-time.Hour)); held {
		t.Fatal("a role is not held before it takes effect")
	}
	otherTenant, _, _ := e.registeredTenant(t)
	if held, _ := e.repo.HoldsCounterpartyRole(e.ctx, org, otherTenant, domain.CounterpartyRoleBuyer, e.at.Add(time.Hour)); held {
		t.Fatal("a role is held for its own tenant only")
	}
	pending := role
	pending.Role, pending.Status = domain.CounterpartyRoleSupplier, domain.RelationshipStatusPending
	if _, _, err := e.repo.EnsureCounterpartyRole(e.ctx, pending, actor()); err != nil {
		t.Fatal(err)
	}
	if held, _ := e.repo.HoldsCounterpartyRole(e.ctx, org, tenantID, domain.CounterpartyRoleSupplier, e.at.Add(time.Hour)); held {
		t.Fatal("a PENDING role is recorded but never relied on")
	}
	if err := e.repo.EndCounterpartyRole(e.ctx, id, e.at.Add(2*time.Hour), "closed", actor()); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.EndCounterpartyRole(e.ctx, id, e.at.Add(3*time.Hour), "closed again", actor()); err != nil {
		t.Fatalf("ending an ended role changes nothing: %v", err)
	}
	if held, _ := e.repo.HoldsCounterpartyRole(e.ctx, org, tenantID, domain.CounterpartyRoleBuyer, e.at.Add(3*time.Hour)); held {
		t.Fatal("an ended role is not held")
	}
	if ended, err := e.repo.GetCounterpartyRole(e.ctx, id); err != nil || ended.EffectiveTo == nil || !ended.EffectiveTo.Equal(e.at.Add(2*time.Hour)) {
		t.Fatalf("the first end stands: %+v %v", ended, err)
	}

	var product string
	if err := e.admin.QueryRow(e.ctx, `INSERT INTO registry.canonical_entity (entity_type, status) VALUES ('PRODUCT','active') RETURNING canonical_entity_id::text`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		mutate func(*domain.CounterpartyRole)
		want   error
	}{
		"non-organisation":     {func(r *domain.CounterpartyRole) { r.OrganisationID = product }, repository.ErrNotAnOrganisation},
		"unregistered tenant":  {func(r *domain.CounterpartyRole) { r.TenantID = "tn_" + token() }, repository.ErrTenantNotRegistered},
		"unknown organisation": {func(r *domain.CounterpartyRole) { r.OrganisationID = domain.NewUUIDv7() }, repository.ErrCanonicalEntityNotFound},
	} {
		r := role
		r.Role = "CARRIER"
		tc.mutate(&r)
		if _, _, err := e.repo.EnsureCounterpartyRole(e.ctx, r, actor()); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}
	for name, r := range map[string]domain.CounterpartyRole{
		"legacy kind as role": {OrganisationID: org, TenantID: tenantID, Role: "BUYER_ORGANISATION", EffectiveFrom: e.at, SourceAuthority: "test"},
		"ENDED on creation":   {OrganisationID: org, TenantID: tenantID, Role: "CARRIER", Status: "ENDED", EffectiveFrom: e.at, SourceAuthority: "test"},
		"no source authority": {OrganisationID: org, TenantID: tenantID, Role: "CARRIER", EffectiveFrom: e.at},
	} {
		if _, _, err := e.repo.EnsureCounterpartyRole(e.ctx, r, actor()); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// TestCounterpartyRecordsConformToSharedContract validates what the Control
// Plane stores and returns against the pinned Shared counterparty schema.
func TestCounterpartyRecordsConformToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	if _, err := os.Stat(filepath.Join(dir, "contracts", "organisation", "v1", "counterparty.schema.json")); err != nil {
		t.Skip("pinned baobab-platform/shared revision has no contracts/organisation/v1/counterparty.schema.json yet")
	}
	e := newEnv(t)
	roleSchema := contracttest.CompileSchema(t, dir, "organisation/v1/counterparty.schema.json#/$defs/CounterpartyRole")
	candidateSchema := contracttest.CompileSchema(t, dir, "organisation/v1/counterparty.schema.json#/$defs/OrganisationResolutionCandidate")
	decisionSchema := contracttest.CompileSchema(t, dir, "organisation/v1/counterparty.schema.json#/$defs/ResolutionCandidateDecision")

	zuri := e.tenantFor(t, "LE-"+strings.ToUpper(token()))
	legacy, _ := e.legacyEntity(t, domain.EntityTypeBuyerOrganisation, "active", zuri)
	e.reconcile(t, actor())
	roles := e.rolesOf(t, legacy)
	if len(roles) != 1 {
		t.Fatalf("roles %+v", roles)
	}
	contracttest.ValidateJSON(t, roleSchema, roles[0])
	if err := e.repo.EndCounterpartyRole(e.ctx, roles[0].ID, e.at.Add(72*time.Hour), "closed", actor()); err != nil {
		t.Fatal(err)
	}
	ended, err := e.repo.GetCounterpartyRole(e.ctx, roles[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ValidateJSON(t, roleSchema, ended)

	reg := "REG-" + strings.ToUpper(token())
	_, a, aLE := e.registeredTenant(t)
	_, b, bLE := e.registeredTenant(t)
	e.claim(t, aLE, "A", domain.OrganisationIdentifier{Type: "LEI", Value: reg})
	e.claim(t, bLE, "B", domain.OrganisationIdentifier{Type: "LEI", Value: reg})
	e.reconcile(t, actor())
	open, ok := e.candidateFor(t, a, b)
	if !ok {
		t.Fatal("no candidate")
	}
	contracttest.ValidateJSON(t, candidateSchema, open)
	decision := domain.ResolutionCandidateDecision{Decision: domain.ResolutionCandidateDuplicateConfirmed, Reason: "same LEI", SurvivingOrganisationID: b}
	contracttest.ValidateJSON(t, decisionSchema, decision)
	decided, _, err := e.repo.DecideResolutionCandidate(e.ctx, open.ID, decision, e.at.Add(49*time.Hour), actor())
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ValidateJSON(t, candidateSchema, decided)
}
