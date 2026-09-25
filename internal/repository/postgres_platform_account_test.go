package repository

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
)

// ADR-BCP-018 gate ORG-07 against real PostgreSQL: the section 83
// PlatformAccount lifecycle and the explicit tenant PlatformAccount binding
// (sections 45, 48, 119, 141, 152).

type accountScenario struct {
	f         *orgFixture
	principal string
	account   string
	other     string
	member    string
	tenant    string
}

func (f *orgFixture) principal(t *testing.T) string {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO identity.principal (actor_type) VALUES ('human') RETURNING principal_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// newAccountScenario: an ACTIVE account whose SERVICE_RECIPIENT is the
// primary organisation of a tenant, and a second ACTIVE account.
func newAccountScenario(t *testing.T) accountScenario {
	t.Helper()
	f := newOrgFixture(t)
	s := accountScenario{f: f, principal: f.principal(t), member: f.organisation(t, "Member")}
	for _, id := range []*string{&s.account, &s.other} {
		*id = domain.NewResourceID(domain.PlatformAccountIDPrefix)
		if err := f.repo.CreatePlatformAccount(f.ctx, domain.PlatformAccount{ID: *id, DisplayName: "Account", Status: domain.PlatformAccountActive,
			EffectiveFrom: f.at}, f.actor()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.EnsurePlatformAccountMembership(f.ctx, domain.PlatformAccountMembership{PlatformAccountID: *id,
			OrganisationID: s.member, AccountRole: domain.AccountRoleServiceRecipient, Status: "ACTIVE", EffectiveFrom: f.at}, f.actor()); err != nil {
			t.Fatal(err)
		}
	}
	s.tenant = f.tenant(t, uniqueLE())
	if _, err := f.repo.EnsureTenantOrganisationMapping(f.ctx, domain.TenantOrganisationMapping{TenantID: s.tenant, OrganisationID: s.member,
		MappingRole: domain.TenantOrgRolePrimary, Status: domain.RelationshipStatusActive, EffectiveFrom: f.at, Provenance: "test"}, f.actor()); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s accountScenario) bind(account string) (domain.TenantPlatformAccountBinding, bool, error) {
	return s.f.repo.BindTenantPlatformAccount(s.f.ctx, s.tenant, account, "Consumes under the MSA.", "msa_1", s.principal, s.f.at.Add(time.Hour), s.f.actor())
}

func TestPlatformAccountLifecycle(t *testing.T) {
	s := newAccountScenario(t)
	f := s.f
	change := func(status string) (domain.PlatformAccount, bool, error) {
		return f.repo.ChangePlatformAccountStatus(f.ctx, s.account, status, "governed decision", "", f.at.Add(2*time.Hour), f.actor())
	}
	if _, changed, err := change(domain.PlatformAccountActive); err != nil || changed {
		t.Fatalf("asking for the current status is a replay: %v %v", err, changed)
	}
	if _, _, err := change(domain.PlatformAccountPending); !errors.Is(err, ErrPlatformAccountTransition) {
		t.Fatalf("ACTIVE never returns to PENDING: %v", err)
	}
	if acct, changed, err := change(domain.PlatformAccountSuspended); err != nil || !changed || acct.Status != domain.PlatformAccountSuspended {
		t.Fatalf("suspend: %v %v %+v", err, changed, acct)
	}
	if _, _, err := s.bind(s.account); !errors.Is(err, ErrPlatformAccountNotActive) {
		t.Fatalf("a SUSPENDED account accepts no new binding: %v", err)
	}
	if _, _, err := change(domain.PlatformAccountActive); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, _, err := s.bind(s.account); err != nil {
		t.Fatal(err)
	}
	if _, _, err := change(domain.PlatformAccountClosed); !errors.Is(err, ErrPlatformAccountHasActiveBindings) {
		t.Fatalf("an account with ACTIVE bindings cannot close: %v", err)
	}
	if _, err := f.repo.EndTenantPlatformAccountBinding(f.ctx, s.tenant, "account closing", s.principal, f.at.Add(3*time.Hour), f.actor()); err != nil {
		t.Fatal(err)
	}
	acct, changed, err := change(domain.PlatformAccountClosed)
	if err != nil || !changed || acct.Status != domain.PlatformAccountClosed || acct.EffectiveTo == nil {
		t.Fatalf("close: %v %v %+v", err, changed, acct)
	}
	if _, _, err := change(domain.PlatformAccountActive); !errors.Is(err, ErrPlatformAccountTransition) {
		t.Fatalf("CLOSED is final: %v", err)
	}
	row, _ := domain.ParseResourceID(domain.PlatformAccountIDPrefix, s.account)
	if _, err := f.admin.Exec(f.ctx, `UPDATE registry.platform_account SET status='ACTIVE' WHERE platform_account_id=$1::uuid`, row); err == nil {
		t.Fatal("the database refuses to reopen a CLOSED account")
	}
	var tenants int
	if err := f.admin.QueryRow(f.ctx, `SELECT count(*) FROM tenants WHERE tenant_id=$1`, s.tenant).Scan(&tenants); err != nil || tenants != 1 {
		t.Fatalf("closing an account deletes no tenant: %v %d", err, tenants)
	}
	if _, _, err := f.repo.ChangePlatformAccountStatus(f.ctx, "pacct_0000000000000000000000000000000f", "SUSPENDED", "x", "", f.at, f.actor()); !errors.Is(err, ErrPlatformAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestTenantPlatformAccountBinding(t *testing.T) {
	s := newAccountScenario(t)
	f := s.f
	binding, created, err := s.bind(s.account)
	if err != nil || !created || binding.Status != domain.TenantPlatformAccountBindingActive || binding.OrganisationID != s.member ||
		binding.BoundBy != s.principal || !strings.HasPrefix(binding.ID, "tpab_") {
		t.Fatalf("bind: %v %v %+v", err, created, binding)
	}
	if err := contracts.ValidateValue(contracts.MustSchema("organisation/v1/platform.schema.json#/$defs/TenantPlatformAccountBinding"), binding); err != nil {
		t.Fatalf("the binding conforms to Shared: %v", err)
	}
	if again, created, err := s.bind(s.account); err != nil || created || again.ID != binding.ID {
		t.Fatalf("binding again to the same account converges: %v %v %s", err, created, again.ID)
	}
	if _, _, err := s.bind(s.other); !errors.Is(err, ErrTenantAlreadyBound) {
		t.Fatalf("a tenant has at most one ACTIVE binding: %v", err)
	}
	ended, err := f.repo.EndTenantPlatformAccountBinding(f.ctx, s.tenant, "moved to the regional account", s.principal, f.at.Add(2*time.Hour), f.actor())
	if err != nil || ended.Status != domain.TenantPlatformAccountBindingEnded || ended.EffectiveTo == nil || ended.EndedBy != s.principal {
		t.Fatalf("end: %v %+v", err, ended)
	}
	if _, err := f.repo.EndTenantPlatformAccountBinding(f.ctx, s.tenant, "again", s.principal, f.at, f.actor()); !errors.Is(err, ErrNoActiveBinding) {
		t.Fatalf("nothing left to end: %v", err)
	}
	moved, created, err := s.bind(s.other)
	if err != nil || !created || moved.PlatformAccountID != s.other {
		t.Fatalf("rebinding after ending: %v %v %+v", err, created, moved)
	}
	history, err := f.repo.ListTenantPlatformAccountBindings(f.ctx, s.tenant)
	if err != nil || len(history) != 2 || history[0].ID != moved.ID || history[1].ID != binding.ID || history[1].Status != domain.TenantPlatformAccountBindingEnded {
		t.Fatalf("history is kept, newest first: %v %+v", err, history)
	}

	// History is evidence: ended rows never change and nothing is deleted.
	row, _ := domain.ParseResourceID(domain.TenantPlatformAccountBindingIDPrefix, binding.ID)
	for _, stmt := range []string{
		`UPDATE registry.tenant_platform_account_binding SET reason='rewritten' WHERE binding_id=$1::uuid`,
		`UPDATE registry.tenant_platform_account_binding SET status='ACTIVE', effective_to=NULL, end_reason=NULL, ended_by=NULL WHERE binding_id=$1::uuid`,
		`DELETE FROM registry.tenant_platform_account_binding WHERE binding_id=$1::uuid`,
	} {
		if _, err := f.admin.Exec(f.ctx, stmt, row); err == nil {
			t.Fatalf("the database refused nothing: %s", stmt)
		}
	}
	activeRow, _ := domain.ParseResourceID(domain.TenantPlatformAccountBindingIDPrefix, moved.ID)
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO registry.tenant_platform_account_binding (binding_id, tenant_id, platform_account_id,
		organisation_id, status, reason, bound_by, effective_from)
		SELECT gen_random_uuid(), tenant_id, platform_account_id, organisation_id, 'ACTIVE', 'second', bound_by, effective_from
		FROM registry.tenant_platform_account_binding WHERE binding_id=$1::uuid`, activeRow); err == nil {
		t.Fatal("the database allows one ACTIVE binding per tenant")
	}
}

func TestTenantPlatformAccountBindingIsJustifiedNotInferred(t *testing.T) {
	s := newAccountScenario(t)
	f := s.f
	// A tenant whose primary organisation is not a member of the account.
	stranger := f.tenant(t, uniqueLE())
	outsider := f.organisation(t, "Outsider")
	if _, err := f.repo.EnsureTenantOrganisationMapping(f.ctx, domain.TenantOrganisationMapping{TenantID: stranger, OrganisationID: outsider,
		MappingRole: domain.TenantOrgRolePrimary, Status: domain.RelationshipStatusActive, EffectiveFrom: f.at, Provenance: "test"}, f.actor()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.BindTenantPlatformAccount(f.ctx, stranger, s.account, "x", "", s.principal, f.at, f.actor()); !errors.Is(err, ErrOrganisationNotAccountMember) {
		t.Fatalf("a binding needs the primary organisation's account membership: %v", err)
	}
	// A tenant without a primary organisation.
	bare := f.tenant(t, uniqueLE())
	if _, _, err := f.repo.BindTenantPlatformAccount(f.ctx, bare, s.account, "x", "", s.principal, f.at, f.actor()); !errors.Is(err, ErrTenantHasNoPrimaryOrganisation) {
		t.Fatalf("a binding needs a primary organisation: %v", err)
	}
	if _, _, err := f.repo.BindTenantPlatformAccount(f.ctx, "tn_doesnotexist0000", s.account, "x", "", s.principal, f.at, f.actor()); !errors.Is(err, ErrTenantNotRegistered) {
		t.Fatalf("unknown tenant: %v", err)
	}
	// Membership alone never creates a binding (section 45, 119).
	if bindings, err := f.repo.ListTenantPlatformAccountBindings(f.ctx, s.tenant); err != nil || len(bindings) != 0 {
		t.Fatalf("no binding is inferred from membership: %v %+v", err, bindings)
	}
}

// TestPlatformAccountBindingIsNeverAnAccessPath: nothing that resolves
// context or authorization reads the binding (sections 45, 141).
func TestPlatformAccountBindingIsNeverAnAccessPath(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal/resolver", "internal/auth", "internal/service", "internal/capability", "internal/product"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(raw), "tenant_platform_account_binding") || strings.Contains(string(raw), "TenantPlatformAccountBinding") {
				t.Errorf("%s reads tenant PlatformAccount bindings; they are never an access path", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlatformAccountEventsConformToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	s := newAccountScenario(t)
	f := s.f
	var correlations []string
	act := func() AuditActor {
		a := f.actor()
		correlations = append(correlations, a.CorrelationID)
		return a
	}
	if _, _, err := f.repo.BindTenantPlatformAccount(f.ctx, s.tenant, s.account, "x", "", s.principal, f.at, act()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.EndTenantPlatformAccountBinding(f.ctx, s.tenant, "y", s.principal, f.at.Add(time.Hour), act()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.repo.ChangePlatformAccountStatus(f.ctx, s.account, domain.PlatformAccountSuspended, "z", "", f.at, act()); err != nil {
		t.Fatal(err)
	}
	envelopeSchema := contracttest.CompileSchema(t, dir, "events/v1/envelope.schema.json")
	rows, err := f.admin.Query(f.ctx, `SELECT event_type, payload FROM messaging.outbox WHERE correlation_id::text = ANY($1)`, correlations)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var typ string
		var payload []byte
		if err := rows.Scan(&typ, &payload); err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		contracttest.ValidateJSON(t, envelopeSchema, envelope)
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "organisation/v1/events.schema.json#/$defs/"+events.OrganisationPayloadDef(typ)), envelope["data"])
		seen[typ] = true
	}
	for _, want := range []string{events.TenantPlatformAccountBound, events.TenantPlatformAccountBindingEnded, events.PlatformAccountStatusChanged} {
		if !seen[want] {
			t.Errorf("did not emit %s", want)
		}
	}
}
