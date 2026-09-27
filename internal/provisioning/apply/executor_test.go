package apply

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/operations"
	"github.com/baobab-platform/baobab-cp/internal/provisioning"
	"github.com/baobab-platform/baobab-cp/internal/provisioning/convergence"
	provisioningdomain "github.com/baobab-platform/baobab-cp/internal/provisioning/domain"
)

type registry struct{ candidates []convergence.Candidate }

func (registry) PlanningProduct(context.Context, string) (convergence.Product, bool, error) {
	return convergence.Product{ProductID: "baobab-xbt", CompositionKey: "solution.baobab-xbt"}, true, nil
}
func (registry) PlanningComposition(context.Context, string) ([]convergence.Member, bool, error) {
	return []convergence.Member{{CapabilityKey: "commerce.order.manage", Criticality: convergence.CriticalityMandatory}}, true, nil
}
func (registry) PlanningMarket(context.Context, string) (bool, error)     { return true, nil }
func (registry) PlanningCapability(context.Context, string) (bool, error) { return true, nil }
func (r *registry) PlanningCandidates(context.Context, string) ([]convergence.Candidate, error) {
	return r.candidates, nil
}

type store struct {
	op        operations.Operation
	executed  convergence.ExecutedProvisioning
	completed *operations.Outcome
	// failed is the recovery state recorded for a failed execution.
	failed string
}

func (s *store) ClaimExecution(context.Context, time.Duration) (operations.Operation, bool, error) {
	return s.op, s.completed == nil, nil
}
func (s *store) FinishAbandonedCancellations(context.Context) (int, error) { return 0, nil }
func (s *store) LoadExecuted(context.Context, string) (convergence.ExecutedProvisioning, error) {
	return s.executed, nil
}
func (s *store) SaveExecutionManifest(context.Context, provisioningdomain.TenantManifestRecord) error {
	return nil
}
func (s *store) MarkProvisioningBlocked(context.Context, string, string) error { return nil }
func (s *store) MarkExecutionFailed(_ context.Context, _ string, code string, retryable bool) error {
	s.failed = "BLOCKED " + code
	if retryable {
		s.failed = "FAILED"
	}
	return nil
}
func (s *store) CompleteOperation(_ context.Context, _ string, _ int, o operations.Outcome) (operations.Status, error) {
	s.completed = &o
	return o.Status, nil
}
func (s *store) LegalEntityOf(context.Context, string) (string, error) { return "LE-1", nil }

type topology struct{}

func (topology) GetMarketByCode(context.Context, string) (domain.Market, error) {
	return domain.Market{ID: domain.NewUUIDv7(), IsActive: true}, nil
}
func (topology) EngineRowIDByCode(context.Context, string) (string, error) {
	return domain.NewUUIDv7(), nil
}

var errPipelineRan = errors.New("the pipeline ran")

// fixture is an approved plan queued for execution, planned against r.
func fixture(t *testing.T, r *registry, generated time.Time) *store {
	t.Helper()
	desired, err := convergence.FreezeDesiredState(domain.TenantOnboardingRequest{ID: "tor_1", AdmissionDecisionID: "adm_1",
		Status: domain.OnboardingFulfilled, AuthorisedBy: "prn_authoriser", TenantID: "tn_acme",
		DesiredState: domain.OnboardingDesiredState{DisplayName: "Acme", ResidencyRegion: "af-south-1", IsolationStrategy: "row_level_security",
			MarketScope: []string{"UG"}, ProductRequirements: []string{"baobab-xbt"},
			MarketParticipation: []domain.OnboardingMarketParticipation{{Market: "UG", Activities: []string{"SELLING"}}}}}, "tn_acme", 1, generated)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.NewResourceID("tp")
	plan, err := convergence.Planner{Registry: r}.Plan(context.Background(), convergence.Input{Desired: desired,
		TenantProvisioningID: key, PlanID: "plan_1", PlanVersion: 1, BaseRevision: 1, Now: generated})
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatalf("plan: %v %v", err, plan.Blockers)
	}
	return &store{
		op: operations.Operation{ID: "op_1", SubjectID: key, PlanID: plan.PlanID, PlanDigest: plan.PlanDigest,
			ApprovalID: "apd_1", ExecutionAttempt: 1},
		executed: convergence.ExecutedProvisioning{ID: domain.NewUUIDv7(), Key: key, TenantID: "tn_acme", Plan: &plan,
			Desired: &desired, ApprovalID: "apd_1", ApprovedDigest: plan.PlanDigest, Approved: true},
	}
}

func executor(r *registry, s *store, now time.Time) Executor {
	return Executor{Store: s, Registry: topology{}, Planner: convergence.Planner{Registry: r}, Now: func() time.Time { return now },
		Pipeline: func(context.Context, string, provisioning.ResolvedManifest) (*provisioning.Orchestrator, error) {
			return nil, errPipelineRan
		}}
}

func problemCode(t *testing.T, o *operations.Outcome) string {
	t.Helper()
	if o == nil {
		t.Fatal("no outcome was recorded")
	}
	var p struct{ Code string }
	if len(o.Problem) > 0 {
		if err := json.Unmarshal(o.Problem, &p); err != nil {
			t.Fatal(err)
		}
	}
	return p.Code
}

// TestAnApprovedPlanIsRevalidatedBeforeExecution: authoritative state that
// changed after approval, while the operation was queued, stops execution
// as PLAN_STALE before anything is written; unchanged state proceeds.
func TestAnApprovedPlanIsRevalidatedBeforeExecution(t *testing.T) {
	generated := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	candidate := convergence.Candidate{ProviderKey: "baobab-trade.medusa", EngineID: "baobab-trade",
		EngineInstanceID: "ei_0000000000000000000000000000000a", Region: "af-south-1", Environment: "production", ProductionPermitted: true}

	unchanged := &registry{candidates: []convergence.Candidate{candidate}}
	s := fixture(t, unchanged, generated)
	if ran, err := executor(unchanged, s, generated.Add(time.Hour)).RunOnce(context.Background()); !ran || err != nil {
		t.Fatalf("run: %v %v", ran, err)
	}
	// It reached the pipeline, which this fixture refuses to build.
	if code := problemCode(t, s.completed); code != "PROVISIONING_UNAVAILABLE" {
		t.Fatalf("unchanged state did not proceed to execution: %s", code)
	}
	if s.failed != "FAILED" {
		t.Fatalf("a retryable failure left the provisioning %q, not FAILED", s.failed)
	}

	for label, change := range map[string]func(r *registry){
		"the provider lost production permission": func(r *registry) { r.candidates[0].ProductionPermitted = false },
		"the chosen instance was replaced": func(r *registry) {
			r.candidates[0].EngineInstanceID = "ei_0000000000000000000000000000000b"
		},
	} {
		r := &registry{candidates: []convergence.Candidate{candidate}}
		s := fixture(t, r, generated)
		change(r)
		if _, err := executor(r, s, generated.Add(time.Hour)).RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if code := problemCode(t, s.completed); code != "PLAN_STALE" || s.completed.Retryable {
			t.Errorf("%s: %s (retryable %v)", label, code, s.completed.Retryable)
		}
		// Not left APPLYING: blocked, for a replan.
		if s.failed != "BLOCKED PLAN_STALE" {
			t.Errorf("%s: the provisioning was left %q", label, s.failed)
		}
	}

	// An expired plan is not started; an execution already under way is
	// resumed, not abandoned, when its validity window closes.
	expired := &registry{candidates: []convergence.Candidate{candidate}}
	s = fixture(t, expired, generated)
	if _, err := executor(expired, s, generated.Add(30*24*time.Hour)).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code := problemCode(t, s.completed); code != "PLAN_STALE" {
		t.Fatalf("an expired plan was started: %s", code)
	}
	s = fixture(t, expired, generated)
	s.op.ExecutionAttempt = 2
	if _, err := executor(expired, s, generated.Add(30*24*time.Hour)).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code := problemCode(t, s.completed); code != "PROVISIONING_UNAVAILABLE" {
		t.Fatalf("a resumed execution was abandoned for expiry: %s", code)
	}
}
