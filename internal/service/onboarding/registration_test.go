package onboarding_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/service/onboarding"
	"github.com/baobab-platform/baobab-cp/internal/store"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestTenantRegistrationRequiresAnAuthorisedRequest drives ADR-BCP-017
// sections 22-24 against real PostgreSQL: a tenant is registered only for an
// AUTHORISED request whose desired state it matches, the same transaction
// records the request FULFILLED with the tenant, a request produces one
// tenant, and every refusal leaves nothing registered.
func TestTenantRegistrationRequiresAnAuthorisedRequest(t *testing.T) {
	e := newEnv(t)
	tenants, err := postgres.Open(e.ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tenants.Close)
	requester, authoriser, registrar := e.principal(t), e.principal(t), e.principal(t)
	requested := func(t *testing.T) domain.TenantOnboardingRequest {
		t.Helper()
		a := e.decided(t, "APPROVED", "schema_per_tenant")
		req, _, err := e.svc.Request(e.ctx, requester, body(a.decisionID, ""))
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	authorised := func(t *testing.T) domain.TenantOnboardingRequest {
		t.Helper()
		req := requested(t)
		if _, err := e.svc.Authorise(e.ctx, authoriser, req.ID, reason); err != nil {
			t.Fatal(err)
		}
		return req
	}
	command := func(req domain.TenantOnboardingRequest) domain.RegisterTenant {
		return domain.RegisterTenant{Basis: domain.RegistrationOnboarding, TenantOnboardingRequestID: req.ID,
			TenantID: domain.NewTenantID(), LegalEntityID: "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]),
			DisplayName: req.DesiredState.DisplayName, IsolationStrategy: req.DesiredState.IsolationStrategy,
			ResidencyRegion: req.DesiredState.ResidencyRegion, RequestedProducts: req.DesiredState.ProductRequirements}
	}
	register := func(c domain.RegisterTenant) error {
		_, err := tenants.RegisterTenant(e.ctx, "reg-"+domain.NewUUIDv7(), store.RequestMetadata{ActorID: registrar.PrincipalID,
			ActorType: "human", CorrelationID: domain.NewUUIDv7()}, c, e.svc.RegistrationStep(registrar, c))
		return err
	}
	registered := func(tenantID string) bool {
		var n int
		if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	// The governed path: registration fulfils the request atomically.
	req := authorised(t)
	c := command(req)
	if err := register(c); err != nil {
		t.Fatalf("registration for an AUTHORISED request: %v", err)
	}
	got, err := e.repo.GetTenantOnboardingRequest(e.ctx, req.ID)
	if err != nil || got.Status != domain.OnboardingFulfilled || got.TenantID != c.TenantID {
		t.Fatalf("the request should be FULFILLED with the registered tenant: %+v %v", got, err)
	}
	var basis string
	if err := e.admin.QueryRow(e.ctx, `SELECT registration_basis FROM tenants WHERE tenant_id = $1`, c.TenantID).Scan(&basis); err != nil || basis != "ONBOARDING" {
		t.Fatalf("tenant basis = %q, %v", basis, err)
	}
	var fulfilled int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM messaging.outbox WHERE event_type = $1 AND payload->'data'->>'tenant_id' = $2`,
		events.TenantOnboardingFulfilled, c.TenantID).Scan(&fulfilled); err != nil || fulfilled != 1 {
		t.Fatalf("one tenant-onboarding.fulfilled event for the tenant, got %d (%v)", fulfilled, err)
	}

	// A request produces one tenant.
	again := command(req)
	if err := register(again); !errors.Is(err, onboarding.ErrTransition) || registered(again.TenantID) {
		t.Fatalf("a FULFILLED request registered a second tenant: %v", err)
	}

	// Refusals roll the whole registration back.
	for name, tc := range map[string]struct {
		req    func(*testing.T) domain.TenantOnboardingRequest
		mutate func(*domain.RegisterTenant)
		want   error
	}{
		"a REQUESTED request":  {requested, func(*domain.RegisterTenant) {}, onboarding.ErrTransition},
		"another display name": {authorised, func(c *domain.RegisterTenant) { c.DisplayName = "Someone Else" }, onboarding.ErrDesiredStateMismatch},
		"another residency":    {authorised, func(c *domain.RegisterTenant) { c.ResidencyRegion = "eu-west-1" }, onboarding.ErrDesiredStateMismatch},
		"another isolation":    {authorised, func(c *domain.RegisterTenant) { c.IsolationStrategy = "row_level_security" }, onboarding.ErrDesiredStateMismatch},
		"widened products":     {authorised, func(c *domain.RegisterTenant) { c.RequestedProducts = append(c.RequestedProducts, "baobab-cms") }, onboarding.ErrDesiredStateMismatch},
		"narrowed products":    {authorised, func(c *domain.RegisterTenant) { c.RequestedProducts = nil }, onboarding.ErrDesiredStateMismatch},
		"an unknown request":   {authorised, func(c *domain.RegisterTenant) { c.TenantOnboardingRequestID = "tor_0190a1b2c3d4e5f60718293a4b5c6d7e" }, onboarding.ErrNotFound},
	} {
		req := tc.req(t)
		c := command(req)
		tc.mutate(&c)
		if err := register(c); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
		if registered(c.TenantID) {
			t.Errorf("%s: a refused registration left a tenant behind", name)
		}
		if got, _ := e.repo.GetTenantOnboardingRequest(e.ctx, req.ID); got.Status == domain.OnboardingFulfilled {
			t.Errorf("%s: a refused registration fulfilled its request", name)
		}
	}

	// The store itself refuses a command without a basis, or an onboarding
	// registration without its fulfilment step.
	bare := command(authorised(t))
	bare.Basis = ""
	if _, err := tenants.RegisterTenant(e.ctx, "reg-"+domain.NewUUIDv7(), store.RequestMetadata{}, bare, nil); !errors.Is(err, store.ErrRegistrationBasis) {
		t.Fatalf("a command without a basis: %v", err)
	}
	stepless := command(authorised(t))
	if _, err := tenants.RegisterTenant(e.ctx, "reg-"+domain.NewUUIDv7(), store.RequestMetadata{}, stepless, nil); !errors.Is(err, store.ErrRegistrationBasis) {
		t.Fatalf("an onboarding registration without its step: %v", err)
	}
}

// TestTenantRegistrationBasisIsEnforcedByTheDatabase: even a direct write
// cannot register an ONBOARDING tenant without its FULFILLED request, a new
// LEGACY tenant, a BOOTSTRAP tenant without reason and evidence, or change a
// tenant's basis afterwards.
func TestTenantRegistrationBasisIsEnforcedByTheDatabase(t *testing.T) {
	e := newEnv(t)
	le := "LE-" + strings.ToUpper(strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12])
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO legal_entities(legal_entity_id) VALUES ($1) ON CONFLICT DO NOTHING`, le); err != nil {
		t.Fatal(err)
	}
	insert := func(basis, reason, evidence string) error {
		_, err := e.admin.Exec(e.ctx, `INSERT INTO tenants(tenant_id, legal_entity_id, display_name, isolation_strategy, residency_region,
			registration_basis, bootstrap_reason, bootstrap_evidence_reference)
			VALUES ($1, $2, 'T', 'row_level_security', 'af-south-1', $3, NULLIF($4, ''), NULLIF($5, ''))`,
			domain.NewTenantID(), le, basis, reason, evidence)
		return err
	}
	for name, err := range map[string]error{
		"ONBOARDING without a FULFILLED request": insert("ONBOARDING", "", ""),
		"a new LEGACY tenant":                    insert("LEGACY", "", ""),
		"BOOTSTRAP without evidence":             insert("BOOTSTRAP", "Pre-admission first-party tenant.", ""),
		"BOOTSTRAP with a short reason":          insert("BOOTSTRAP", "migrating", "evidence"),
		"an unknown basis":                       insert("DIRECT", "", ""),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	tenant := e.tenant(t, "row_level_security", "af-south-1")
	if _, err := e.admin.Exec(e.ctx, `UPDATE tenants SET registration_basis = 'ONBOARDING', bootstrap_reason = NULL,
		bootstrap_evidence_reference = NULL WHERE tenant_id = $1`, tenant); err == nil {
		t.Error("a tenant's registration basis was changed")
	}
}
