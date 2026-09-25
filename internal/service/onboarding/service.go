// Package onboarding hands APPROVED admission decisions to tenant
// provisioning through a governed TenantOnboardingRequest (ADR-BCP-017
// sections 22-24, 39, 41, 44, 46). Contract: baobab-platform/shared
// contracts/admission/v1/onboarding.schema.json and onboarding-lifecycle.yaml.
//
// Approval activates nothing. A request exists only when an onboarding
// requester, who is neither the applicant nor the decider, makes one; its
// desired state takes classification, markets, products and isolation from
// the decision; a different principal must authorise it; and provisioning
// fulfils it by naming the tenant it produced, which must match the desired
// state. The admission decision is never rewritten.
package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/events"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

const onboardingContract = "admission/v1/onboarding.schema.json#/$defs/"

var (
	requestSchema      = contracts.MustSchema(onboardingContract + "TenantOnboardingRequestCommand")
	authoriseSchema    = contracts.MustSchema(onboardingContract + "OnboardingAuthorisationCommand")
	cancelSchema       = contracts.MustSchema(onboardingContract + "OnboardingCancellationCommand")
	fulfilSchema       = contracts.MustSchema(onboardingContract + "OnboardingFulfilmentCommand")
	onboardingSchema   = contracts.MustSchema(onboardingContract + "TenantOnboardingRequest")
	ErrNotFound        = repository.ErrOnboardingRequestNotFound
	ErrTenantNotFound  = repository.ErrTenantNotRegistered
	ErrDecisionMissing = errors.New("admission decision not found")
	// ErrDecisionNotApproved: only an APPROVED decision can be onboarded.
	ErrDecisionNotApproved = errors.New("admission decision is not APPROVED")
	// ErrSeparationOfDuties: section 39 forbids this principal the step.
	ErrSeparationOfDuties = errors.New("separation of duties forbids this principal")
	// ErrIsolationDecided: the decision set the isolation; the request cannot change it.
	ErrIsolationDecided = errors.New("the admission decision already set the isolation strategy")
	// ErrIsolationRequired: neither the decision nor the request set an isolation strategy.
	ErrIsolationRequired = errors.New("an isolation strategy is required")
	// ErrTransition: the lifecycle does not permit the step from the current status.
	ErrTransition = errors.New("tenant onboarding request transition not allowed")
	// ErrTenantAlreadyOnboarded: another request already produced the tenant.
	ErrTenantAlreadyOnboarded = errors.New("the tenant was produced by another onboarding request")
	// ErrDesiredStateMismatch: the tenant named at fulfilment does not match the desired state.
	ErrDesiredStateMismatch = errors.New("the tenant does not match the onboarding request's desired state")
)

// InvalidError lists why a command does not conform to the Shared contract.
type InvalidError struct{ Problems []string }

func (e *InvalidError) Error() string { return strings.Join(e.Problems, "; ") }

// Actor is the authenticated platform principal making the call.
type Actor struct {
	PrincipalID string
	Audit       repository.AuditActor
}

// Admissions reads the application and decision a request is made from.
type Admissions interface {
	GetAdmissionDecisionByID(ctx context.Context, id string) (*domain.AdmissionDecision, error)
	GetClientApplication(ctx context.Context, id string) (*domain.ClientApplication, error)
}

// Service is the onboarding handoff.
type Service struct {
	Repo       repository.TenantOnboardingRepository
	Admissions Admissions
	Now        func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func decode(schema *contracts.Schema, raw []byte, into any) error {
	if err := contracts.Validate(schema, raw); err != nil {
		var invalid *contracts.ValidationError
		if errors.As(err, &invalid) {
			return &InvalidError{Problems: invalid.Problems}
		}
		return &InvalidError{Problems: []string{err.Error()}}
	}
	return json.Unmarshal(raw, into)
}

// Request hands an APPROVED decision to onboarding. Repeating it for a
// decision with a live request returns that request, created=false.
func (s *Service) Request(ctx context.Context, actor Actor, raw []byte) (domain.TenantOnboardingRequest, bool, error) {
	var cmd struct {
		AdmissionDecisionID string `json:"admission_decision_id"`
		DisplayName         string `json:"display_name"`
		ResidencyRegion     string `json:"residency_region"`
		IsolationStrategy   string `json:"isolation_strategy"`
		Reason              string `json:"reason"`
	}
	if err := decode(requestSchema, raw, &cmd); err != nil {
		return domain.TenantOnboardingRequest{}, false, err
	}
	decision, err := s.Admissions.GetAdmissionDecisionByID(ctx, cmd.AdmissionDecisionID)
	if err != nil {
		return domain.TenantOnboardingRequest{}, false, err
	}
	if decision == nil {
		return domain.TenantOnboardingRequest{}, false, ErrDecisionMissing
	}
	if decision.Decision != domain.DecisionApproved {
		return domain.TenantOnboardingRequest{}, false, ErrDecisionNotApproved
	}
	app, err := s.Admissions.GetClientApplication(ctx, decision.ClientApplicationID)
	if err != nil {
		return domain.TenantOnboardingRequest{}, false, err
	}
	if actor.PrincipalID == app.ApplicantPrincipalID || actor.PrincipalID == decision.DecidedBy {
		return domain.TenantOnboardingRequest{}, false, fmt.Errorf("%w: the requester is the applicant or the decider", ErrSeparationOfDuties)
	}
	isolation := decision.ApprovedIsolationRequirements
	switch {
	case isolation != "" && cmd.IsolationStrategy != "" && cmd.IsolationStrategy != isolation:
		return domain.TenantOnboardingRequest{}, false, ErrIsolationDecided
	case isolation == "" && cmd.IsolationStrategy == "":
		return domain.TenantOnboardingRequest{}, false, ErrIsolationRequired
	case isolation == "":
		isolation = cmd.IsolationStrategy
	}
	req := domain.TenantOnboardingRequest{
		ID: domain.NewResourceID(domain.TenantOnboardingRequestIDPrefix), ClientApplicationID: decision.ClientApplicationID,
		AdmissionDecisionID: decision.ID, Status: domain.OnboardingRequested, Reason: cmd.Reason,
		DesiredState: domain.OnboardingDesiredState{DisplayName: cmd.DisplayName, ResidencyRegion: cmd.ResidencyRegion,
			IsolationStrategy: isolation, SubscriptionType: decision.ApprovedSubscriptionType,
			MarketScope: slices.Clone(decision.ApprovedMarketScope), ProductRequirements: slices.Clone(decision.ApprovedProductRequirements)},
		CorrelationID: domain.NewUUIDv7(), RequestedBy: actor.PrincipalID, RequestedAt: s.now(),
	}
	if req.DesiredState.ProductRequirements == nil {
		req.DesiredState.ProductRequirements = []string{}
	}
	if err := contracts.ValidateValue(onboardingSchema, req); err != nil {
		return domain.TenantOnboardingRequest{}, false, fmt.Errorf("the request built from the decision does not conform: %w", err)
	}
	return s.Repo.CreateTenantOnboardingRequest(ctx, req, actor.Audit)
}

// Get returns a request.
func (s *Service) Get(ctx context.Context, id string) (domain.TenantOnboardingRequest, error) {
	return s.Repo.GetTenantOnboardingRequest(ctx, id)
}

// List returns requests, newest first, optionally in one status.
func (s *Service) List(ctx context.Context, status string, limit int) ([]domain.TenantOnboardingRequest, error) {
	return s.Repo.ListTenantOnboardingRequests(ctx, status, limit)
}

// Authorise lets provisioning fulfil a REQUESTED request. The authoriser is
// neither the requester nor the applicant.
func (s *Service) Authorise(ctx context.Context, actor Actor, id string, raw []byte) (domain.TenantOnboardingRequest, error) {
	var cmd struct {
		Reason string `json:"reason"`
	}
	if err := decode(authoriseSchema, raw, &cmd); err != nil {
		return domain.TenantOnboardingRequest{}, err
	}
	current, err := s.Repo.GetTenantOnboardingRequest(ctx, id)
	if err != nil {
		return current, err
	}
	app, err := s.Admissions.GetClientApplication(ctx, current.ClientApplicationID)
	if err != nil {
		return current, err
	}
	at := s.now()
	return s.Repo.TransitionTenantOnboardingRequest(ctx, id, "", func(cur domain.TenantOnboardingRequest, _ *repository.OnboardingTenantFacts) (domain.TenantOnboardingRequest, repository.OnboardingChange, error) {
		if cur.Status != domain.OnboardingRequested {
			return cur, repository.OnboardingChange{}, fmt.Errorf("%w: authorise from %s", ErrTransition, cur.Status)
		}
		if actor.PrincipalID == cur.RequestedBy || actor.PrincipalID == app.ApplicantPrincipalID {
			return cur, repository.OnboardingChange{}, fmt.Errorf("%w: the authoriser is the requester or the applicant", ErrSeparationOfDuties)
		}
		cur.Status, cur.AuthorisedBy, cur.AuthorisedAt = domain.OnboardingAuthorised, actor.PrincipalID, &at
		return cur, repository.OnboardingChange{AuditAction: "tenant_onboarding.authorised", EventType: events.TenantOnboardingAuthorised,
			EventData: map[string]any{"authorised_at": events.Timestamp(at)}, AuditPayload: map[string]any{"reason": cmd.Reason}}, nil
	}, actor.Audit)
}

// Cancel withdraws a REQUESTED or AUTHORISED request. The decision stands.
func (s *Service) Cancel(ctx context.Context, actor Actor, id string, raw []byte) (domain.TenantOnboardingRequest, error) {
	var cmd struct {
		Reason string `json:"reason"`
	}
	if err := decode(cancelSchema, raw, &cmd); err != nil {
		return domain.TenantOnboardingRequest{}, err
	}
	at := s.now()
	return s.Repo.TransitionTenantOnboardingRequest(ctx, id, "", func(cur domain.TenantOnboardingRequest, _ *repository.OnboardingTenantFacts) (domain.TenantOnboardingRequest, repository.OnboardingChange, error) {
		if cur.Status != domain.OnboardingRequested && cur.Status != domain.OnboardingAuthorised {
			return cur, repository.OnboardingChange{}, fmt.Errorf("%w: cancel from %s", ErrTransition, cur.Status)
		}
		cur.Status, cur.CancelledBy, cur.CancelledAt, cur.CancellationReason = domain.OnboardingCancelled, actor.PrincipalID, &at, cmd.Reason
		return cur, repository.OnboardingChange{AuditAction: "tenant_onboarding.cancelled", EventType: events.TenantOnboardingCancelled,
			EventData: map[string]any{"cancelled_at": events.Timestamp(at)}, AuditPayload: map[string]any{"reason": cmd.Reason}}, nil
	}, actor.Audit)
}

// Fulfil records the tenant provisioning produced from an AUTHORISED
// request. The tenant must match the desired state; fulfilment activates
// nothing (section 22).
func (s *Service) Fulfil(ctx context.Context, actor Actor, id string, raw []byte) (domain.TenantOnboardingRequest, error) {
	var cmd struct {
		TenantID string `json:"tenant_id"`
	}
	if err := decode(fulfilSchema, raw, &cmd); err != nil {
		return domain.TenantOnboardingRequest{}, err
	}
	at := s.now()
	return s.Repo.TransitionTenantOnboardingRequest(ctx, id, cmd.TenantID, func(cur domain.TenantOnboardingRequest, tenant *repository.OnboardingTenantFacts) (domain.TenantOnboardingRequest, repository.OnboardingChange, error) {
		if cur.Status != domain.OnboardingAuthorised {
			return cur, repository.OnboardingChange{}, fmt.Errorf("%w: fulfil from %s (only an AUTHORISED request is provisioned)", ErrTransition, cur.Status)
		}
		if tenant.OnboardedBy != "" {
			return cur, repository.OnboardingChange{}, ErrTenantAlreadyOnboarded
		}
		if tenant.IsolationStrategy != cur.DesiredState.IsolationStrategy || tenant.ResidencyRegion != cur.DesiredState.ResidencyRegion {
			return cur, repository.OnboardingChange{}, ErrDesiredStateMismatch
		}
		cur.Status, cur.TenantID, cur.FulfilledAt = domain.OnboardingFulfilled, tenant.TenantID, &at
		return cur, repository.OnboardingChange{AuditAction: "tenant_onboarding.fulfilled", EventType: events.TenantOnboardingFulfilled,
			EventData: map[string]any{"tenant_id": tenant.TenantID, "fulfilled_at": events.Timestamp(at)}}, nil
	}, actor.Audit)
}
