package erpprovisioning

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const provisioningKey = "tp_0199a1b2c3d47e8f9a0b1c2d3e4f5a6b"

type convergedFake struct {
	c   repository.ConvergedProvisioning
	d   convergence.DesiredState
	err error
}

func (f convergedFake) GetConvergedProvisioning(context.Context, string) (repository.ConvergedProvisioning, error) {
	return f.c, f.err
}
func (f convergedFake) GetDesiredState(context.Context, string, int64) (convergence.DesiredState, error) {
	return f.d, f.err
}

type financeFake struct {
	got        []string
	currencies []string
	err        error
}

func (f *financeFake) FunctionalCurrencies(_ context.Context, _ string, entities []string) ([]string, error) {
	f.got = entities
	return f.currencies, f.err
}

func approvedProvisioning() convergedFake {
	plan := &convergence.Plan{PlanID: "plan-1", PlanVersion: 2, PlanDigest: planDigest, TenantID: tenant,
		TenantProvisioningID: provisioningKey, DesiredStateDigest: "sha256:d"}
	return convergedFake{
		c: repository.ConvergedProvisioning{ID: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b", Key: provisioningKey, TenantID: tenant, DesiredStateDigest: "sha256:d",
			Plan: plan, Decision: &repository.PlanDecision{Decision: "APPROVED", PlanID: "plan-1", PlanVersion: 2, PlanDigest: planDigest}},
		d: convergence.DesiredState{Tenant: convergence.DesiredTenant{TenantID: tenant}, LegalEntities: []string{"LE-B", "LE-A"},
			MarketParticipation: []convergence.DesiredMarket{{Market: "ZA"}, {Market: "UG"}}, IsolationRequirement: "SHARED", DesiredStateDigest: "sha256:d"},
	}
}

func TestSourceAssemblesTheApprovedPlanAndFinanceCurrencies(t *testing.T) {
	fin := &financeFake{currencies: []string{"ZAR", "UGX"}}
	got, err := PlanSource{Provisionings: approvedProvisioning(), Finance: fin}.Authorised(context.Background(), provisioningKey)
	if err != nil {
		t.Fatal(err)
	}
	want := Authorised{TenantID: tenant, Authority: Authority{provisioningKey, "plan-1", 2, planDigest},
		LegalEntityIDs: []string{"LE-A", "LE-B"}, Countries: []string{"UG", "ZA"}, Currencies: []string{"UGX", "ZAR"}}
	if got.TenantID != want.TenantID || got.Authority != want.Authority || !slices.Equal(got.LegalEntityIDs, want.LegalEntityIDs) ||
		!slices.Equal(got.Countries, want.Countries) || !slices.Equal(got.Currencies, want.Currencies) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if !slices.Equal(fin.got, []string{"LE-A", "LE-B"}) {
		t.Fatalf("Finance was asked about %v", fin.got)
	}
}

func TestSourceNeverDerivesCurrenciesWithoutAFinanceBaseline(t *testing.T) {
	_, err := PlanSource{Provisionings: approvedProvisioning()}.Authorised(context.Background(), provisioningKey)
	if !errors.Is(err, ErrFinanceBaselineUnavailable) {
		t.Fatalf("got %v", err)
	}
	for name, fin := range map[string]*financeFake{"error": {err: errors.New("down")}, "empty": {}} {
		if _, err := (PlanSource{Provisionings: approvedProvisioning(), Finance: fin}).Authorised(context.Background(), provisioningKey); !errors.Is(err, ErrFinanceBaselineUnavailable) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func TestSourceRefusesWhatIsNotApprovedAndCurrent(t *testing.T) {
	fin := &financeFake{currencies: []string{"ZAR"}}
	for name, mutate := range map[string]func(*convergedFake){
		"no decision":         func(f *convergedFake) { f.c.Decision = nil },
		"rejected":            func(f *convergedFake) { f.c.Decision.Decision = "REJECTED" },
		"another digest":      func(f *convergedFake) { f.c.Decision.PlanDigest = "sha256:other" },
		"withdrawn":           func(f *convergedFake) { f.c.State = "CANCELLED" },
		"another tenant":      func(f *convergedFake) { f.d.Tenant.TenantID = "tn_other" },
		"no legal entity":     func(f *convergedFake) { f.d.LegalEntities = nil },
		"no market":           func(f *convergedFake) { f.d.MarketParticipation = nil },
		"desired state moved": func(f *convergedFake) { f.d.DesiredStateDigest = "sha256:moved" },
	} {
		f := approvedProvisioning()
		mutate(&f)
		if _, err := (PlanSource{Provisionings: f, Finance: fin}).Authorised(context.Background(), provisioningKey); !errors.Is(err, ErrNotAuthorised) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if fin.got != nil {
		t.Fatal("Finance must not be consulted for a plan that is not approved")
	}
}

func TestSourceTreatsAMissingProvisioningAsNotAuthorised(t *testing.T) {
	f := convergedFake{err: repository.ErrProvisioningNotFound}
	if _, err := (PlanSource{Provisionings: f}).Authorised(context.Background(), provisioningKey); !errors.Is(err, ErrNotAuthorised) {
		t.Fatalf("got %v", err)
	}
	if _, err := (PlanSource{Provisionings: f}).Authorised(context.Background(), "not-an-id"); !errors.Is(err, ErrNotAuthorised) {
		t.Fatalf("got %v", err)
	}
	if _, err := (PlanSource{Provisionings: convergedFake{err: errors.New("db down")}}).Authorised(context.Background(), provisioningKey); err == nil || errors.Is(err, ErrNotAuthorised) {
		t.Fatalf("an unreadable provisioning is an error, not a refusal: %v", err)
	}
}

type latestFake struct {
	sub   Submission
	found bool
	err   error
}

func (f latestFake) LatestForTenant(context.Context, string) (Submission, bool, error) {
	return f.sub, f.found, f.err
}

func TestProvisionerReadyOnlyWhenERPReportsActive(t *testing.T) {
	cases := map[string]struct {
		l  latestFake
		ok bool
	}{
		"none":         {latestFake{}, false},
		"accepted":     {latestFake{sub: Submission{OperationID: operationID, LastState: "accepted"}, found: true}, false},
		"provisioning": {latestFake{sub: Submission{OperationID: operationID, LastState: "provisioning"}, found: true}, false},
		"failed":       {latestFake{sub: Submission{OperationID: operationID, LastState: "failed"}, found: true}, false},
		"active":       {latestFake{sub: Submission{OperationID: operationID, LastState: "active"}, found: true}, true},
	}
	for name, c := range cases {
		ok, reason, err := Provisioner{Latest: c.l}.Ready(context.Background(), tenant)
		if err != nil || ok != c.ok || reason == "" {
			t.Errorf("%s: ok=%v reason=%q err=%v", name, ok, reason, err)
		}
	}
	if _, _, err := (Provisioner{Latest: latestFake{err: errors.New("db")}}).Ready(context.Background(), tenant); err == nil {
		t.Fatal("a ledger failure must not read as not-ready-without-error")
	}
}
