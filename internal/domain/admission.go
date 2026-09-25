// ADR-BCP-017 — ClientApplication and AdmissionDecision.
// Contract: baobab-platform/shared contracts/admission/v1.

package domain

import (
	"encoding/json"
	"time"
)

const (
	ClientApplicationIDPrefix = "capp"
	AdmissionDecisionIDPrefix = "adm"
)

type ApplicationStatus string

const (
	ApplicationDraft               ApplicationStatus = "DRAFT"
	ApplicationSubmitted           ApplicationStatus = "SUBMITTED"
	ApplicationValidating          ApplicationStatus = "VALIDATING"
	ApplicationInformationRequired ApplicationStatus = "INFORMATION_REQUIRED"
	ApplicationUnderReview         ApplicationStatus = "UNDER_REVIEW"
	ApplicationApproved            ApplicationStatus = "APPROVED"
	ApplicationRejected            ApplicationStatus = "REJECTED"
	ApplicationWithdrawn           ApplicationStatus = "WITHDRAWN"
	ApplicationExpired             ApplicationStatus = "EXPIRED"
	ApplicationCancelled           ApplicationStatus = "CANCELLED"
)

// Valid reports whether s is one of the contract's statuses.
func (s ApplicationStatus) Valid() bool {
	switch s {
	case ApplicationDraft, ApplicationSubmitted, ApplicationValidating, ApplicationInformationRequired,
		ApplicationUnderReview, ApplicationApproved, ApplicationRejected, ApplicationWithdrawn,
		ApplicationExpired, ApplicationCancelled:
		return true
	}
	return false
}

// Terminal reports whether no transition leaves s.
func (s ApplicationStatus) Terminal() bool {
	switch s {
	case ApplicationApproved, ApplicationRejected, ApplicationWithdrawn, ApplicationExpired, ApplicationCancelled:
		return true
	}
	return false
}

// EditableByApplicant reports whether the applicant may change the
// application's content in status s (lifecycle.yaml editable_by_applicant).
func (s ApplicationStatus) EditableByApplicant() bool {
	return s == ApplicationDraft || s == ApplicationInformationRequired
}

type ApplicationChannel string

const (
	ChannelSelfService        ApplicationChannel = "SELF_SERVICE"
	ChannelAssistedEnterprise ApplicationChannel = "ASSISTED_ENTERPRISE"
	ChannelInternalGroup      ApplicationChannel = "INTERNAL_GROUP"
)

// SubscriptionType is the ADR-BCP-005 vocabulary (ADR-BCP-017 section 10).
type SubscriptionType string

const (
	SubscriptionCommercial SubscriptionType = "COMMERCIAL"
	SubscriptionInternal   SubscriptionType = "INTERNAL"
	SubscriptionTrial      SubscriptionType = "TRIAL"
	SubscriptionPartner    SubscriptionType = "PARTNER"
	SubscriptionManual     SubscriptionType = "MANUAL"
	SubscriptionMigration  SubscriptionType = "MIGRATION"
)

type AdmissionDecisionValue string

const (
	DecisionApproved AdmissionDecisionValue = "APPROVED"
	DecisionRejected AdmissionDecisionValue = "REJECTED"
)

// AdmissionActor is who may issue an application command.
type AdmissionActor string

const (
	ActorApplicant AdmissionActor = "APPLICANT"
	ActorReviewer  AdmissionActor = "REVIEWER"
	ActorDecider   AdmissionActor = "DECIDER"
	ActorPlatform  AdmissionActor = "PLATFORM"
)

type ApplicationCommand string

const (
	CommandSubmit             ApplicationCommand = "submit"
	CommandBeginValidation    ApplicationCommand = "begin_validation"
	CommandRequestInformation ApplicationCommand = "request_information"
	CommandRespond            ApplicationCommand = "respond"
	CommandBeginReview        ApplicationCommand = "begin_review"
	CommandApprove            ApplicationCommand = "approve"
	CommandReject             ApplicationCommand = "reject"
	CommandWithdraw           ApplicationCommand = "withdraw"
	CommandExpire             ApplicationCommand = "expire"
	CommandCancel             ApplicationCommand = "cancel"
)

// ApplicationTransition is one permitted lifecycle step.
type ApplicationTransition struct {
	Command ApplicationCommand
	From    ApplicationStatus
	To      ApplicationStatus
	Actor   AdmissionActor
}

// ApplicationTransitions is ADR-BCP-017 section 8 exactly as Shared
// publishes it in contracts/admission/v1/lifecycle.yaml; a contract test
// compares the two. Anything not listed is refused.
var ApplicationTransitions = []ApplicationTransition{
	{CommandSubmit, ApplicationDraft, ApplicationSubmitted, ActorApplicant},
	{CommandBeginValidation, ApplicationSubmitted, ApplicationValidating, ActorReviewer},
	{CommandRequestInformation, ApplicationValidating, ApplicationInformationRequired, ActorReviewer},
	{CommandRequestInformation, ApplicationUnderReview, ApplicationInformationRequired, ActorReviewer},
	{CommandRespond, ApplicationInformationRequired, ApplicationValidating, ActorApplicant},
	{CommandBeginReview, ApplicationValidating, ApplicationUnderReview, ActorReviewer},
	{CommandApprove, ApplicationUnderReview, ApplicationApproved, ActorDecider},
	{CommandReject, ApplicationUnderReview, ApplicationRejected, ActorDecider},
	{CommandWithdraw, ApplicationDraft, ApplicationWithdrawn, ActorApplicant},
	{CommandWithdraw, ApplicationSubmitted, ApplicationWithdrawn, ActorApplicant},
	{CommandWithdraw, ApplicationValidating, ApplicationWithdrawn, ActorApplicant},
	{CommandWithdraw, ApplicationInformationRequired, ApplicationWithdrawn, ActorApplicant},
	{CommandWithdraw, ApplicationUnderReview, ApplicationWithdrawn, ActorApplicant},
	{CommandExpire, ApplicationDraft, ApplicationExpired, ActorPlatform},
	{CommandExpire, ApplicationInformationRequired, ApplicationExpired, ActorPlatform},
	{CommandCancel, ApplicationSubmitted, ApplicationCancelled, ActorPlatform},
	{CommandCancel, ApplicationValidating, ApplicationCancelled, ActorPlatform},
	{CommandCancel, ApplicationInformationRequired, ApplicationCancelled, ActorPlatform},
	{CommandCancel, ApplicationUnderReview, ApplicationCancelled, ActorPlatform},
}

// NextApplicationStatus returns where command leads from status, and who
// may issue it; ok is false when the transition is not permitted.
func NextApplicationStatus(command ApplicationCommand, from ApplicationStatus) (ApplicationStatus, AdmissionActor, bool) {
	for _, t := range ApplicationTransitions {
		if t.Command == command && t.From == from {
			return t.To, t.Actor, true
		}
	}
	return "", "", false
}

type RequestedMarket struct {
	CountryCode string `json:"country_code"`
	Note        string `json:"note,omitempty"`
}

type ApplicationEvidence struct {
	EvidenceType      string    `json:"evidence_type"`
	EvidenceReference string    `json:"evidence_reference"`
	Description       string    `json:"description,omitempty"`
	RecordedAt        time.Time `json:"recorded_at"`
}

type InformationRequest struct {
	RequestedAt time.Time  `json:"requested_at"`
	Message     string     `json:"message"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
	Response    string     `json:"response,omitempty"`
}

type DecisionSummary struct {
	AdmissionDecisionID string                 `json:"admission_decision_id"`
	Decision            AdmissionDecisionValue `json:"decision"`
	DecidedAt           time.Time              `json:"decided_at"`
	Reason              string                 `json:"reason"`
}

// ClientApplication is ADR-BCP-017 section 6. The organisation profile and
// business requirements are applicant evidence, kept as the contract's
// JSON documents: the Control Plane validates them against Shared and
// never interprets them as canonical state (section 7).
type ClientApplication struct {
	ID                   string                `json:"client_application_id"`
	Reference            string                `json:"reference"`
	Status               ApplicationStatus     `json:"status"`
	Channel              ApplicationChannel    `json:"application_channel"`
	Version              int64                 `json:"version"`
	ApplicantPrincipalID string                `json:"applicant_principal_id"`
	AssignedReviewer     string                `json:"assigned_reviewer,omitempty"`
	OrganisationProfile  json.RawMessage       `json:"organisation_profile"`
	Requirements         json.RawMessage       `json:"requirements"`
	RequestedMarkets     []RequestedMarket     `json:"requested_markets"`
	Evidence             []ApplicationEvidence `json:"evidence"`
	InformationRequests  []InformationRequest  `json:"information_requests"`
	Decision             *DecisionSummary      `json:"decision,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
	SubmittedAt          *time.Time            `json:"submitted_at,omitempty"`
	ClosedAt             *time.Time            `json:"closed_at,omitempty"`
}

// ApplicantView is what the applicant sees: internal assignment is omitted.
func (a ClientApplication) ApplicantView() ClientApplication {
	a.AssignedReviewer = ""
	return a
}

// RequestedCountries is the set of countries the application requests.
func (a ClientApplication) RequestedCountries() map[string]bool {
	out := make(map[string]bool, len(a.RequestedMarkets))
	for _, m := range a.RequestedMarkets {
		out[m.CountryCode] = true
	}
	return out
}

// InternalEligibilityRecord is why an INTERNAL classification was granted
// (ADR-BCP-017 section 13; Shared InternalEligibilityEvidence), recorded by
// the Control Plane from its own evaluation of governed relationships.
type InternalEligibilityRecord struct {
	OrganisationID       string    `json:"organisation_id"`
	PlatformID           string    `json:"platform_id"`
	EligibilityStatus    string    `json:"eligibility_status"`
	BasisRelationshipIDs []string  `json:"basis_relationship_ids"`
	EvaluatedAt          time.Time `json:"evaluated_at"`
}

// AdmissionDecision is ADR-BCP-017 section 21. Immutable once recorded.
type AdmissionDecision struct {
	ID                            string                     `json:"admission_decision_id"`
	ClientApplicationID           string                     `json:"client_application_id"`
	Decision                      AdmissionDecisionValue     `json:"decision"`
	Reason                        string                     `json:"reason"`
	DecidedBy                     string                     `json:"decided_by"`
	DecidedAt                     time.Time                  `json:"decided_at"`
	ApprovedSubscriptionType      SubscriptionType           `json:"approved_subscription_type,omitempty"`
	InternalEligibility           *InternalEligibilityRecord `json:"internal_eligibility,omitempty"`
	ApprovedMarketScope           []string                   `json:"approved_market_scope,omitempty"`
	ApprovedProductRequirements   []string                   `json:"approved_product_requirements,omitempty"`
	ApprovedIsolationRequirements string                     `json:"approved_isolation_requirements,omitempty"`
	Conditions                    []string                   `json:"conditions,omitempty"`
	EvidenceReferences            []string                   `json:"evidence_references,omitempty"`
}

// Summary is the applicant-visible part of the decision.
func (d AdmissionDecision) Summary() DecisionSummary {
	return DecisionSummary{AdmissionDecisionID: d.ID, Decision: d.Decision, DecidedAt: d.DecidedAt, Reason: d.Reason}
}
