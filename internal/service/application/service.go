// Package application implements the ADR-BCP-017 client application
// workflow (gates OA-01, OA-03, OA-04, OA-05/06 classification, OA-09):
// an applicant's ClientApplication from draft to an explicit
// AdmissionDecision. Contract: baobab-platform/shared contracts/admission/v1.
//
// Every request body is validated against the Shared schema, and every
// application the service writes is validated as a whole ClientApplication
// before it is stored, so nothing stored or returned can drift from the
// contract. The service owns the rules the schema cannot express:
// ownership, the lifecycle, optimistic concurrency, separation of duties
// and server-authoritative INTERNAL eligibility.
package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/nabhold/baobab-cp/internal/contracts"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/events"
	"github.com/nabhold/baobab-cp/internal/repository"
)

var (
	// ErrNotFound also answers for an application the caller may not see,
	// so its existence is not disclosed.
	ErrNotFound = errors.New("client application not found")
	// ErrVersionConflict: the application changed since the caller read it.
	ErrVersionConflict = errors.New("the application has changed; reload it and retry")
	// ErrSelfDecision: nobody decides their own application (ADR-BCP-020 section 39).
	ErrSelfDecision = errors.New("an applicant cannot decide their own application")
	// ErrMarketScope: an approval may only cover markets the application requested.
	ErrMarketScope = errors.New("the approved market scope must lie within the requested markets")
	// ErrNotInternalEligible: the named organisation is not INTERNAL-eligible now.
	ErrNotInternalEligible = errors.New("the organisation is not eligible for an INTERNAL subscription")
	// ErrIdempotencyConflict: the Idempotency-Key was used for another request.
	ErrIdempotencyConflict = errors.New("the idempotency key was used for a different request")
)

// TransitionError: the command is not permitted from the current status.
type TransitionError struct {
	Command domain.ApplicationCommand
	Status  domain.ApplicationStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s is not permitted while the application is %s", e.Command, e.Status)
}

// InvalidError: the request (Application false), or the application it
// would produce (Application true, e.g. submitting an incomplete draft),
// does not satisfy the contract.
type InvalidError struct {
	Problems    []string
	Application bool
}

func (e *InvalidError) Error() string { return fmt.Sprintf("invalid: %v", e.Problems) }

// EligibilityEvaluator establishes INTERNAL eligibility from governed
// relationships (organisation.EligibilityResolver).
type EligibilityEvaluator interface {
	InternalEligibilityBasis(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error)
	Platform() string
}

// Actor is an authenticated caller: its Control Plane principal and the
// audit identity its changes are attributed to.
type Actor struct {
	PrincipalID string
	Audit       repository.AuditActor
}

type Service struct {
	Repo        repository.AdmissionRepository
	Eligibility EligibilityEvaluator
	Now         func() time.Time
}

var (
	draftSchema       = contracts.MustSchema("admission/v1/application.schema.json#/$defs/ClientApplicationDraft")
	applicationSchema = contracts.MustSchema("admission/v1/application.schema.json#/$defs/ClientApplication")
	responseSchema    = contracts.MustSchema("admission/v1/application.schema.json#/$defs/ApplicantResponseCommand")
	infoRequestSchema = contracts.MustSchema("admission/v1/application.schema.json#/$defs/InformationRequestCommand")
	closureSchema     = contracts.MustSchema("admission/v1/application.schema.json#/$defs/ClosureCommand")
	decisionReqSchema = contracts.MustSchema("admission/v1/decision.schema.json#/$defs/AdmissionDecisionRequest")
	decisionSchema    = contracts.MustSchema("admission/v1/decision.schema.json#/$defs/AdmissionDecision")
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// decode validates raw against schema and unmarshals it.
func decode(schema *contracts.Schema, raw []byte, into any) error {
	var verr *contracts.ValidationError
	if err := contracts.Validate(schema, raw); errors.As(err, &verr) {
		return &InvalidError{Problems: verr.Problems}
	} else if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

// checkApplication validates the whole application the service is about
// to store against the contract.
func checkApplication(app domain.ClientApplication) error {
	var verr *contracts.ValidationError
	if err := contracts.ValidateValue(applicationSchema, app); errors.As(err, &verr) {
		return &InvalidError{Problems: verr.Problems, Application: true}
	} else if err != nil {
		return err
	}
	return nil
}

type draft struct {
	Version             *int64                    `json:"version"`
	OrganisationProfile json.RawMessage           `json:"organisation_profile"`
	Requirements        json.RawMessage           `json:"requirements"`
	RequestedMarkets    *[]domain.RequestedMarket `json:"requested_markets"`
	Evidence            *[]draftEvidence          `json:"evidence"`
}

type draftEvidence struct {
	EvidenceType      string `json:"evidence_type"`
	EvidenceReference string `json:"evidence_reference"`
	Description       string `json:"description,omitempty"`
}

// apply replaces each section the draft supplies. Evidence already on the
// application keeps its original recorded_at.
func (d draft) apply(app *domain.ClientApplication, at time.Time) {
	if d.OrganisationProfile != nil {
		app.OrganisationProfile = d.OrganisationProfile
	}
	if d.Requirements != nil {
		app.Requirements = d.Requirements
	}
	if d.RequestedMarkets != nil {
		app.RequestedMarkets = *d.RequestedMarkets
	}
	if d.Evidence != nil {
		recorded := map[[2]string]time.Time{}
		for _, e := range app.Evidence {
			recorded[[2]string{e.EvidenceType, e.EvidenceReference}] = e.RecordedAt
		}
		evidence := make([]domain.ApplicationEvidence, 0, len(*d.Evidence))
		for _, e := range *d.Evidence {
			when, ok := recorded[[2]string{e.EvidenceType, e.EvidenceReference}]
			if !ok {
				when = at
			}
			evidence = append(evidence, domain.ApplicationEvidence{EvidenceType: e.EvidenceType,
				EvidenceReference: e.EvidenceReference, Description: e.Description, RecordedAt: when})
		}
		app.Evidence = evidence
	}
}

func (d draft) sections() []string {
	var out []string
	if d.OrganisationProfile != nil {
		out = append(out, "organisation_profile")
	}
	if d.Requirements != nil {
		out = append(out, "requirements")
	}
	if d.RequestedMarkets != nil {
		out = append(out, "requested_markets")
	}
	if d.Evidence != nil {
		out = append(out, "evidence")
	}
	return out
}

// requestHash identifies a create request for Idempotency-Key replay.
func requestHash(raw []byte) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	sum := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// Create opens a SELF_SERVICE application for the applicant (section 5).
// Nothing but the application is created.
func (s *Service) Create(ctx context.Context, applicant Actor, raw []byte, idempotencyKey string) (domain.ClientApplication, bool, error) {
	var d draft
	if err := decode(draftSchema, raw, &d); err != nil {
		return domain.ClientApplication{}, false, err
	}
	if d.Version != nil {
		return domain.ClientApplication{}, false, &InvalidError{Problems: []string{"/version: a new application has no version"}}
	}
	hash := ""
	if idempotencyKey != "" {
		var err error
		if hash, err = requestHash(raw); err != nil {
			return domain.ClientApplication{}, false, err
		}
	}
	app, replayed, err := s.Repo.CreateClientApplication(ctx, applicant.PrincipalID, idempotencyKey, hash, applicant.Audit,
		func(id, reference string) (domain.ClientApplication, repository.ApplicationChange, error) {
			at := s.now()
			app := domain.ClientApplication{ID: id, Reference: reference, Status: domain.ApplicationDraft,
				Channel: domain.ChannelSelfService, Version: 1, ApplicantPrincipalID: applicant.PrincipalID,
				OrganisationProfile: json.RawMessage(`{}`), Requirements: json.RawMessage(`{}`),
				RequestedMarkets: []domain.RequestedMarket{}, Evidence: []domain.ApplicationEvidence{},
				InformationRequests: []domain.InformationRequest{}, CreatedAt: at, UpdatedAt: at}
			d.apply(&app, at)
			if err := checkApplication(app); err != nil {
				return app, repository.ApplicationChange{}, err
			}
			return app, repository.ApplicationChange{
				AuditAction: "client_application.created", AuditPayload: map[string]any{"application_channel": app.Channel, "reference": reference},
				EventType: events.ClientApplicationCreated,
				EventData: map[string]any{"client_application_id": id, "application_channel": app.Channel, "created_at": events.Timestamp(at)},
			}, nil
		})
	if errors.Is(err, repository.ErrApplicationIdempotencyConflict) {
		return app, false, ErrIdempotencyConflict
	}
	return app.ApplicantView(), replayed, err
}

// GetForApplicant returns the caller's own application.
func (s *Service) GetForApplicant(ctx context.Context, applicant Actor, id string) (domain.ClientApplication, error) {
	app, err := s.get(ctx, id)
	if err != nil {
		return app, err
	}
	if app.ApplicantPrincipalID != applicant.PrincipalID {
		return domain.ClientApplication{}, ErrNotFound
	}
	return app.ApplicantView(), nil
}

// ListForApplicant pages the caller's own applications, newest first.
func (s *Service) ListForApplicant(ctx context.Context, applicant Actor, limit int, before string) ([]domain.ClientApplication, error) {
	apps, err := s.Repo.ListClientApplications(ctx, repository.ClientApplicationQuery{ApplicantPrincipalID: applicant.PrincipalID, Limit: limit, Before: before})
	for i := range apps {
		apps[i] = apps[i].ApplicantView()
	}
	return apps, err
}

// Get returns any application: the platform view.
func (s *Service) Get(ctx context.Context, id string) (domain.ClientApplication, error) {
	return s.get(ctx, id)
}

// List pages applications in the given statuses, newest first: the platform view.
func (s *Service) List(ctx context.Context, statuses []domain.ApplicationStatus, limit int, before string) ([]domain.ClientApplication, error) {
	return s.Repo.ListClientApplications(ctx, repository.ClientApplicationQuery{Statuses: statuses, Limit: limit, Before: before})
}

// GetDecision returns an application's AdmissionDecision, or ErrNotFound.
func (s *Service) GetDecision(ctx context.Context, id string) (domain.AdmissionDecision, error) {
	d, err := s.Repo.GetAdmissionDecision(ctx, id)
	switch {
	case errors.Is(err, repository.ErrClientApplicationNotFound) || (err == nil && d == nil):
		return domain.AdmissionDecision{}, ErrNotFound
	case err != nil:
		return domain.AdmissionDecision{}, err
	}
	return *d, nil
}

func (s *Service) get(ctx context.Context, id string) (domain.ClientApplication, error) {
	app, err := s.Repo.GetClientApplication(ctx, id)
	if errors.Is(err, repository.ErrClientApplicationNotFound) {
		return domain.ClientApplication{}, ErrNotFound
	}
	if err != nil {
		return domain.ClientApplication{}, err
	}
	return *app, nil
}

// step is one command's effect on a locked application.
type step struct {
	command domain.ApplicationCommand
	actor   domain.AdmissionActor
	// owner, when set, must be the application's applicant.
	owner string
	// version, when set, must be the application's current version.
	version *int64
	// edit changes the application and returns the change to record.
	edit func(app *domain.ClientApplication, at time.Time) (repository.ApplicationChange, error)
}

func (s *Service) run(ctx context.Context, id string, actor Actor, st step) (domain.ClientApplication, error) {
	app, err := s.Repo.UpdateClientApplication(ctx, id, actor.Audit, func(app *domain.ClientApplication) (*repository.ApplicationChange, error) {
		if st.owner != "" && app.ApplicantPrincipalID != st.owner {
			return nil, ErrNotFound
		}
		if st.version != nil && *st.version != app.Version {
			return nil, ErrVersionConflict
		}
		at := s.now()
		if st.command != "" {
			to, who, ok := domain.NextApplicationStatus(st.command, app.Status)
			if !ok || who != st.actor {
				return nil, &TransitionError{Command: st.command, Status: app.Status}
			}
			app.Status = to
			if to.Terminal() {
				app.ClosedAt = &at
			}
		}
		change, err := st.edit(app, at)
		if err != nil {
			return nil, err
		}
		app.Version++
		app.UpdatedAt = at
		if err := checkApplication(*app); err != nil {
			return nil, err
		}
		return &change, nil
	})
	if errors.Is(err, repository.ErrClientApplicationNotFound) {
		return app, ErrNotFound
	}
	return app, err
}

// Update edits the applicant's DRAFT or INFORMATION_REQUIRED application.
func (s *Service) Update(ctx context.Context, applicant Actor, id string, raw []byte) (domain.ClientApplication, error) {
	var d draft
	if err := decode(draftSchema, raw, &d); err != nil {
		return domain.ClientApplication{}, err
	}
	if d.Version == nil {
		return domain.ClientApplication{}, &InvalidError{Problems: []string{"/version: an update names the version it was based on"}}
	}
	app, err := s.run(ctx, id, applicant, step{owner: applicant.PrincipalID, version: d.Version,
		edit: func(app *domain.ClientApplication, at time.Time) (repository.ApplicationChange, error) {
			if !app.Status.EditableByApplicant() {
				return repository.ApplicationChange{}, &TransitionError{Command: "edit", Status: app.Status}
			}
			d.apply(app, at)
			return repository.ApplicationChange{AuditAction: "client_application.updated",
				AuditPayload: map[string]any{"sections": d.sections(), "status": app.Status}}, nil
		}})
	return app.ApplicantView(), err
}

// Submit sends a complete DRAFT for validation.
func (s *Service) Submit(ctx context.Context, applicant Actor, id string) (domain.ClientApplication, error) {
	app, err := s.run(ctx, id, applicant, step{command: domain.CommandSubmit, actor: domain.ActorApplicant, owner: applicant.PrincipalID,
		edit: func(app *domain.ClientApplication, at time.Time) (repository.ApplicationChange, error) {
			app.SubmittedAt = &at
			return repository.ApplicationChange{AuditAction: "client_application.submitted", EventType: events.ClientApplicationSubmitted,
				EventData: map[string]any{"client_application_id": app.ID, "submitted_at": events.Timestamp(at)}}, nil
		}})
	return app.ApplicantView(), err
}

// Respond answers the open information request and returns the application
// to validation.
func (s *Service) Respond(ctx context.Context, applicant Actor, id string, raw []byte) (domain.ClientApplication, error) {
	var cmd struct {
		Version  int64  `json:"version"`
		Response string `json:"response"`
	}
	if err := decode(responseSchema, raw, &cmd); err != nil {
		return domain.ClientApplication{}, err
	}
	app, err := s.run(ctx, id, applicant, step{command: domain.CommandRespond, actor: domain.ActorApplicant, owner: applicant.PrincipalID,
		version: &cmd.Version,
		edit: func(app *domain.ClientApplication, at time.Time) (repository.ApplicationChange, error) {
			last := len(app.InformationRequests) - 1
			if last < 0 || app.InformationRequests[last].RespondedAt != nil {
				return repository.ApplicationChange{}, &TransitionError{Command: domain.CommandRespond, Status: domain.ApplicationInformationRequired}
			}
			app.InformationRequests[last].RespondedAt = &at
			app.InformationRequests[last].Response = cmd.Response
			return repository.ApplicationChange{AuditAction: "client_application.information_provided",
				AuditPayload: map[string]any{"response": cmd.Response}}, nil
		}})
	return app.ApplicantView(), err
}

// Withdraw closes the applicant's open application.
func (s *Service) Withdraw(ctx context.Context, applicant Actor, id string, raw []byte) (domain.ClientApplication, error) {
	reason, err := closureReason(raw)
	if err != nil {
		return domain.ClientApplication{}, err
	}
	app, err := s.run(ctx, id, applicant, step{command: domain.CommandWithdraw, actor: domain.ActorApplicant, owner: applicant.PrincipalID,
		edit: func(app *domain.ClientApplication, at time.Time) (repository.ApplicationChange, error) {
			return repository.ApplicationChange{AuditAction: "client_application.withdrawn", AuditPayload: map[string]any{"reason": reason},
				EventType: events.ClientApplicationWithdrawn, ClosureReason: reason,
				EventData: map[string]any{"client_application_id": app.ID, "withdrawn_at": events.Timestamp(at)}}, nil
		}})
	return app.ApplicantView(), err
}

func closureReason(raw []byte) (string, error) {
	var cmd struct {
		Reason string `json:"reason"`
	}
	err := decode(closureSchema, raw, &cmd)
	return cmd.Reason, err
}

// BeginValidation picks a SUBMITTED application up; the reviewer is assigned.
func (s *Service) BeginValidation(ctx context.Context, reviewer Actor, id string) (domain.ClientApplication, error) {
	return s.run(ctx, id, reviewer, step{command: domain.CommandBeginValidation, actor: domain.ActorReviewer,
		edit: func(app *domain.ClientApplication, _ time.Time) (repository.ApplicationChange, error) {
			app.AssignedReviewer = reviewer.PrincipalID
			return repository.ApplicationChange{AuditAction: "client_application.validation_started"}, nil
		}})
}

// RequestInformation returns the application to its applicant.
func (s *Service) RequestInformation(ctx context.Context, reviewer Actor, id string, raw []byte) (domain.ClientApplication, error) {
	var cmd struct {
		Message string `json:"message"`
	}
	if err := decode(infoRequestSchema, raw, &cmd); err != nil {
		return domain.ClientApplication{}, err
	}
	return s.run(ctx, id, reviewer, step{command: domain.CommandRequestInformation, actor: domain.ActorReviewer,
		edit: func(app *domain.ClientApplication, at time.Time) (repository.ApplicationChange, error) {
			app.InformationRequests = append(app.InformationRequests, domain.InformationRequest{RequestedAt: at, Message: cmd.Message})
			return repository.ApplicationChange{AuditAction: "client_application.information_requested",
				AuditPayload: map[string]any{"message": cmd.Message}, EventType: events.ClientApplicationInformationRequested,
				EventData: map[string]any{"client_application_id": app.ID, "requested_at": events.Timestamp(at)}}, nil
		}})
}

// BeginReview moves a validated application to review.
func (s *Service) BeginReview(ctx context.Context, reviewer Actor, id string) (domain.ClientApplication, error) {
	return s.run(ctx, id, reviewer, step{command: domain.CommandBeginReview, actor: domain.ActorReviewer,
		edit: func(app *domain.ClientApplication, _ time.Time) (repository.ApplicationChange, error) {
			if app.AssignedReviewer == "" {
				app.AssignedReviewer = reviewer.PrincipalID
			}
			return repository.ApplicationChange{AuditAction: "client_application.review_started"}, nil
		}})
}

// Cancel closes an open, submitted application on the platform's side.
func (s *Service) Cancel(ctx context.Context, operator Actor, id string, raw []byte) (domain.ClientApplication, error) {
	reason, err := closureReason(raw)
	if err != nil {
		return domain.ClientApplication{}, err
	}
	return s.run(ctx, id, operator, step{command: domain.CommandCancel, actor: domain.ActorPlatform,
		edit: func(*domain.ClientApplication, time.Time) (repository.ApplicationChange, error) {
			return repository.ApplicationChange{AuditAction: "client_application.cancelled",
				AuditPayload: map[string]any{"reason": reason}, ClosureReason: reason}, nil
		}})
}

type decisionRequest struct {
	Decision                      domain.AdmissionDecisionValue `json:"decision"`
	Reason                        string                        `json:"reason"`
	ApprovedSubscriptionType      domain.SubscriptionType       `json:"approved_subscription_type"`
	InternalEligibilityOrgID      string                        `json:"internal_eligibility_organisation_id"`
	ApprovedMarketScope           []string                      `json:"approved_market_scope"`
	ApprovedProductRequirements   []string                      `json:"approved_product_requirements"`
	ApprovedIsolationRequirements string                        `json:"approved_isolation_requirements"`
	Conditions                    []string                      `json:"conditions"`
	EvidenceReferences            []string                      `json:"evidence_references"`
}

// Decide records the explicit AdmissionDecision on an application under
// review (section 21). The decider is never the applicant. INTERNAL is
// accepted only when the Control Plane itself finds the named organisation
// eligible now, and the decision records the relationships it rests on.
// Approval activates nothing (section 22).
func (s *Service) Decide(ctx context.Context, decider Actor, id string, raw []byte) (domain.AdmissionDecision, error) {
	var req decisionRequest
	if err := decode(decisionReqSchema, raw, &req); err != nil {
		return domain.AdmissionDecision{}, err
	}
	current, err := s.get(ctx, id)
	if err != nil {
		return domain.AdmissionDecision{}, err
	}
	if current.ApplicantPrincipalID == decider.PrincipalID {
		return domain.AdmissionDecision{}, ErrSelfDecision
	}
	requested := current.RequestedCountries()
	for _, country := range req.ApprovedMarketScope {
		if !requested[country] {
			return domain.AdmissionDecision{}, ErrMarketScope
		}
	}
	decidedAt := s.now()
	var eligibility *domain.InternalEligibilityRecord
	if req.ApprovedSubscriptionType == domain.SubscriptionInternal {
		if s.Eligibility == nil {
			return domain.AdmissionDecision{}, errors.New("INTERNAL eligibility cannot be evaluated: no evaluator is configured")
		}
		basis, err := s.Eligibility.InternalEligibilityBasis(ctx, req.InternalEligibilityOrgID, decidedAt)
		if err != nil {
			return domain.AdmissionDecision{}, fmt.Errorf("evaluate INTERNAL eligibility: %w", err)
		}
		if len(basis) == 0 {
			return domain.AdmissionDecision{}, ErrNotInternalEligible
		}
		ids := make([]string, 0, len(basis))
		for _, pr := range basis {
			ids = append(ids, pr.ID)
		}
		slices.Sort(ids)
		eligibility = &domain.InternalEligibilityRecord{OrganisationID: req.InternalEligibilityOrgID, PlatformID: s.Eligibility.Platform(),
			EligibilityStatus: "ELIGIBLE", BasisRelationshipIDs: slices.Compact(ids), EvaluatedAt: decidedAt}
	}
	decisionID, err := domain.FormatResourceID(domain.AdmissionDecisionIDPrefix, domain.NewUUIDv7())
	if err != nil {
		return domain.AdmissionDecision{}, err
	}
	decision := domain.AdmissionDecision{ID: decisionID, ClientApplicationID: id, Decision: req.Decision, Reason: req.Reason,
		DecidedBy: decider.PrincipalID, DecidedAt: decidedAt, ApprovedSubscriptionType: req.ApprovedSubscriptionType,
		InternalEligibility: eligibility, ApprovedMarketScope: req.ApprovedMarketScope,
		ApprovedProductRequirements: req.ApprovedProductRequirements, ApprovedIsolationRequirements: req.ApprovedIsolationRequirements,
		Conditions: req.Conditions, EvidenceReferences: req.EvidenceReferences}
	var verr *contracts.ValidationError
	if err := contracts.ValidateValue(decisionSchema, decision); errors.As(err, &verr) {
		return domain.AdmissionDecision{}, &InvalidError{Problems: verr.Problems}
	} else if err != nil {
		return domain.AdmissionDecision{}, err
	}

	command, eventType := domain.CommandApprove, events.ClientApplicationApproved
	if req.Decision == domain.DecisionRejected {
		command, eventType = domain.CommandReject, events.ClientApplicationRejected
	}
	_, err = s.run(ctx, id, decider, step{command: command, actor: domain.ActorDecider,
		edit: func(app *domain.ClientApplication, _ time.Time) (repository.ApplicationChange, error) {
			// Re-checked under the row lock: the applicant cannot change, but
			// the check must not depend on the earlier read.
			if app.ApplicantPrincipalID == decider.PrincipalID {
				return repository.ApplicationChange{}, ErrSelfDecision
			}
			summary := decision.Summary()
			app.Decision = &summary
			data := map[string]any{"client_application_id": app.ID, "admission_decision_id": decisionID, "decided_at": events.Timestamp(decidedAt)}
			if req.Decision == domain.DecisionApproved {
				data["approved_subscription_type"] = req.ApprovedSubscriptionType
			}
			return repository.ApplicationChange{AuditAction: "client_application." + map[domain.AdmissionDecisionValue]string{
				domain.DecisionApproved: "approved", domain.DecisionRejected: "rejected"}[req.Decision],
				AuditPayload: map[string]any{"admission_decision_id": decisionID, "approved_subscription_type": req.ApprovedSubscriptionType,
					"internal_eligibility": eligibility, "evidence_references": req.EvidenceReferences},
				EventType: eventType, EventData: data, Decision: &decision}, nil
		}})
	if err != nil {
		return domain.AdmissionDecision{}, err
	}
	return decision, nil
}
