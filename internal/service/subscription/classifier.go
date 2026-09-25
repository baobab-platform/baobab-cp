// Package subscription implements ADR-BCP-018 gate ORG-11: ProductSubscription
// classification, its provenance, and the explanation of why a subscription
// is classified as it is (ADR-BCP-017 sections 10-13, 48; ADR-SHARED-011).
// Contract: baobab-platform/shared contracts/product/v1.
//
// The Control Plane alone classifies. A first classification comes from an
// APPROVED AdmissionDecision that belongs to the subscription's tenant; any
// later change is an audited reclassification appended to the same
// subscription. INTERNAL is recorded only when the Control Plane itself
// finds the tenant's organisation eligible from governed relationships at
// the moment of classification, and every failure to establish that fails
// closed. Classification changes charge policy only: CapabilityGrants are
// untouched.
package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nabhold/baobab-cp/internal/contracts"
	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

var (
	// ErrNotFound: no such ProductSubscription.
	ErrNotFound = errors.New("product subscription not found")
	// ErrNotClassified: the subscription has no classification yet.
	ErrNotClassified = errors.New("the product subscription has not been classified")
	// ErrAlreadyClassified: a classified subscription changes only by reclassification.
	ErrAlreadyClassified = errors.New("the product subscription is already classified; reclassify it instead")
	// ErrUnchangedType: a reclassification must change the subscription type.
	ErrUnchangedType = errors.New("a reclassification must change the subscription type")
	// ErrDecisionNotFound: the admission decision does not exist.
	ErrDecisionNotFound = errors.New("the admission decision does not exist")
	// ErrDecisionNotApproved: only an APPROVED decision classifies.
	ErrDecisionNotApproved = errors.New("the admission decision is not an approval")
	// ErrDecisionNotForTenant: the decision admitted another organisation.
	ErrDecisionNotForTenant = errors.New("the admission decision does not admit this tenant's organisation")
	// ErrNoPrimaryOrganisation: the tenant has no ACTIVE primary organisation.
	ErrNoPrimaryOrganisation = errors.New("the tenant has no ACTIVE primary organisation")
	// ErrNotInternalEligible: the Control Plane does not find the tenant's
	// organisation INTERNAL-eligible now.
	ErrNotInternalEligible = errors.New("the tenant's organisation is not eligible for an INTERNAL subscription")
	// ErrSelfClassification: nobody classifies a subscription they benefit
	// from (ADR-BCP-020 section 39).
	ErrSelfClassification = errors.New("a principal cannot classify its own organisation's subscription")
)

// InvalidError: the request does not satisfy its rules.
type InvalidError struct{ Problems []string }

func (e *InvalidError) Error() string { return fmt.Sprintf("invalid: %v", e.Problems) }

// EligibilityEvaluator establishes INTERNAL eligibility from governed
// relationships (organisation.EligibilityResolver).
type EligibilityEvaluator interface {
	InternalEligibilityBasis(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error)
	Platform() string
}

// Organisations reads the tenant's organisation and its platform relationships.
type Organisations interface {
	ListLiveTenantOrganisationMappings(ctx context.Context, tenantID string) ([]domain.TenantOrganisationMapping, error)
	ListPlatformRelationships(ctx context.Context, organisationID string, at time.Time) ([]domain.PlatformRelationship, error)
}

// Admissions reads admission decisions and their applications.
type Admissions interface {
	GetAdmissionDecisionByID(ctx context.Context, admissionDecisionID string) (*domain.AdmissionDecision, error)
	GetClientApplication(ctx context.Context, id string) (*domain.ClientApplication, error)
}

// Memberships tells whether a principal works for a tenant.
type Memberships interface {
	GetWorkforceMembership(ctx context.Context, principalID, tenantID string) (domain.WorkforceMembership, error)
}

// Actor is an authenticated platform operator: its Control Plane principal
// and the audit identity its changes are attributed to.
type Actor struct {
	PrincipalID string
	Audit       repository.AuditActor
}

// PolicyReference names the policy an explanation applies.
const PolicyReference = "ADR-BCP-017 section 11 / ADR-BCP-018 ORG-11 INTERNAL policy"

type Classifier struct {
	Repo        repository.SubscriptionClassificationRepository
	Admissions  Admissions
	Orgs        Organisations
	Memberships Memberships
	Eligibility EligibilityEvaluator
	Now         func() time.Time
}

var (
	recordSchema      = contracts.MustSchema("product/v1/subscription.schema.json#/$defs/SubscriptionClassificationRecord")
	explanationSchema = contracts.MustSchema("product/v1/subscription.schema.json#/$defs/ClassificationExplanation")
	referencePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$`)
)

func (c *Classifier) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// ClassifyRequest classifies an unclassified subscription from its
// tenant's AdmissionDecision.
type ClassifyRequest struct {
	AdmissionDecisionID string `json:"admission_decision_id"`
	Reason              string `json:"reason"`
}

// ReclassifyRequest changes a classified subscription's type. The
// reference names the governing change (a divestiture review, a contract,
// a decision record).
type ReclassifyRequest struct {
	SubscriptionType        domain.SubscriptionType `json:"subscription_type"`
	ClassificationReference string                  `json:"classification_reference"`
	Reason                  string                  `json:"reason"`
}

// decodeStrict unmarshals raw, refusing unknown fields: a caller can never
// smuggle eligibility evidence or a source into a classification.
func decodeStrict(raw []byte, into any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return &InvalidError{Problems: []string{"body: " + err.Error()}}
	}
	if dec.More() {
		return &InvalidError{Problems: []string{"body: a single JSON object is expected"}}
	}
	return nil
}

func checkReason(reason string) []string {
	if n := len(strings.TrimSpace(reason)); n == 0 || len(reason) > 2000 {
		return []string{"/reason: a reason of 1 to 2000 characters is required"}
	}
	return nil
}

// ClassifyFromAdmission records a subscription's first classification from
// an APPROVED AdmissionDecision that admitted the tenant's organisation.
// Replaying the same decision returns the existing record.
func (c *Classifier) ClassifyFromAdmission(ctx context.Context, actor Actor, subscriptionID string, raw []byte) (domain.SubscriptionClassificationRecord, bool, error) {
	var req ClassifyRequest
	if err := decodeStrict(raw, &req); err != nil {
		return domain.SubscriptionClassificationRecord{}, false, err
	}
	problems := checkReason(req.Reason)
	if _, err := domain.ParseResourceID(domain.AdmissionDecisionIDPrefix, req.AdmissionDecisionID); err != nil {
		problems = append(problems, "/admission_decision_id: an admission decision id (adm_...) is required")
	}
	if len(problems) > 0 {
		return domain.SubscriptionClassificationRecord{}, false, &InvalidError{Problems: problems}
	}
	target, err := c.target(ctx, actor, subscriptionID)
	if err != nil {
		return domain.SubscriptionClassificationRecord{}, false, err
	}
	decision, err := c.Admissions.GetAdmissionDecisionByID(ctx, req.AdmissionDecisionID)
	switch {
	case err != nil:
		return domain.SubscriptionClassificationRecord{}, false, err
	case decision == nil:
		return domain.SubscriptionClassificationRecord{}, false, ErrDecisionNotFound
	case decision.Decision != domain.DecisionApproved || !decision.ApprovedSubscriptionType.Valid():
		return domain.SubscriptionClassificationRecord{}, false, ErrDecisionNotApproved
	}
	app, err := c.Admissions.GetClientApplication(ctx, decision.ClientApplicationID)
	if err != nil {
		return domain.SubscriptionClassificationRecord{}, false, fmt.Errorf("read the decision's application: %w", err)
	}
	if app.ApplicantPrincipalID == actor.PrincipalID {
		return domain.SubscriptionClassificationRecord{}, false, ErrSelfClassification
	}
	at := c.now()
	org, err := c.primaryOrganisation(ctx, target.TenantID)
	if err != nil {
		return domain.SubscriptionClassificationRecord{}, false, err
	}
	var eligibility *domain.InternalEligibilityRecord
	if decision.ApprovedSubscriptionType == domain.SubscriptionInternal {
		// The decision found this organisation eligible when it was made;
		// the classification records eligibility as it stands now.
		if decision.InternalEligibility == nil || decision.InternalEligibility.OrganisationID != org {
			return domain.SubscriptionClassificationRecord{}, false, ErrDecisionNotForTenant
		}
		if eligibility, err = c.evaluate(ctx, org, at); err != nil {
			return domain.SubscriptionClassificationRecord{}, false, err
		}
	} else if admitted, err := c.admittedBy(ctx, org, decision.ID, at); err != nil {
		return domain.SubscriptionClassificationRecord{}, false, err
	} else if !admitted {
		return domain.SubscriptionClassificationRecord{}, false, ErrDecisionNotForTenant
	}
	return c.classify(ctx, actor, target.ID, func(t domain.ProductSubscriptionClassificationTarget, current *domain.SubscriptionClassificationRecord) (*domain.SubscriptionClassificationRecord, error) {
		if current != nil {
			if current.Source == domain.ClassificationFromAdmissionDecision && current.Reference == decision.ID {
				return nil, nil
			}
			return nil, ErrAlreadyClassified
		}
		return c.record(t, actor, domain.SubscriptionClassificationRecord{SubscriptionType: decision.ApprovedSubscriptionType,
			Source: domain.ClassificationFromAdmissionDecision, Reference: decision.ID, Reason: req.Reason,
			ClassifiedAt: at, InternalEligibility: eligibility})
	})
}

// Reclassify appends a classification of another type to a classified
// subscription. INTERNAL requires the Control Plane to find the tenant's
// organisation eligible now. Replaying the same reclassification returns
// the existing record.
func (c *Classifier) Reclassify(ctx context.Context, actor Actor, subscriptionID string, raw []byte) (domain.SubscriptionClassificationRecord, bool, error) {
	var req ReclassifyRequest
	if err := decodeStrict(raw, &req); err != nil {
		return domain.SubscriptionClassificationRecord{}, false, err
	}
	problems := checkReason(req.Reason)
	if !req.SubscriptionType.Valid() {
		problems = append(problems, "/subscription_type: one of COMMERCIAL, INTERNAL, TRIAL, PARTNER, MANUAL, MIGRATION is required")
	}
	if !referencePattern.MatchString(req.ClassificationReference) {
		problems = append(problems, "/classification_reference: an opaque reference of 3 to 128 characters is required")
	}
	if len(problems) > 0 {
		return domain.SubscriptionClassificationRecord{}, false, &InvalidError{Problems: problems}
	}
	target, err := c.target(ctx, actor, subscriptionID)
	if err != nil {
		return domain.SubscriptionClassificationRecord{}, false, err
	}
	at := c.now()
	var eligibility *domain.InternalEligibilityRecord
	if req.SubscriptionType == domain.SubscriptionInternal {
		org, err := c.primaryOrganisation(ctx, target.TenantID)
		if err != nil {
			return domain.SubscriptionClassificationRecord{}, false, err
		}
		if eligibility, err = c.evaluate(ctx, org, at); err != nil {
			return domain.SubscriptionClassificationRecord{}, false, err
		}
	}
	return c.classify(ctx, actor, target.ID, func(t domain.ProductSubscriptionClassificationTarget, current *domain.SubscriptionClassificationRecord) (*domain.SubscriptionClassificationRecord, error) {
		switch {
		case current == nil:
			return nil, ErrNotClassified
		case current.SubscriptionType == req.SubscriptionType && current.Source == domain.ClassificationFromReclassification &&
			current.Reference == req.ClassificationReference:
			return nil, nil
		case current.SubscriptionType == req.SubscriptionType:
			return nil, ErrUnchangedType
		}
		return c.record(t, actor, domain.SubscriptionClassificationRecord{SubscriptionType: req.SubscriptionType,
			PreviousSubscriptionType: current.SubscriptionType, Source: domain.ClassificationFromReclassification,
			Reference: req.ClassificationReference, Reason: req.Reason, ClassifiedAt: at, InternalEligibility: eligibility})
	})
}

// Explain answers why the subscription is classified as it is: its current
// record and history, INTERNAL eligibility re-evaluated now (NOT_ELIGIBLE
// is drift, surfaced and never acted on here) and the billing policy.
func (c *Classifier) Explain(ctx context.Context, subscriptionID string) (domain.ClassificationExplanation, error) {
	target, err := c.Repo.GetSubscriptionClassificationTarget(ctx, subscriptionID)
	if errors.Is(err, repository.ErrProductSubscriptionNotFound) {
		return domain.ClassificationExplanation{}, ErrNotFound
	}
	if err != nil {
		return domain.ClassificationExplanation{}, err
	}
	return c.explain(ctx, target)
}

// ExplainTenantProduct is Explain for the tenant's subscription to productID.
func (c *Classifier) ExplainTenantProduct(ctx context.Context, tenantID, productID string) (domain.ClassificationExplanation, error) {
	target, err := c.Repo.FindTenantProductSubscription(ctx, tenantID, productID)
	if errors.Is(err, repository.ErrProductSubscriptionNotFound) {
		return domain.ClassificationExplanation{}, ErrNotFound
	}
	if err != nil {
		return domain.ClassificationExplanation{}, err
	}
	return c.explain(ctx, target)
}

func (c *Classifier) explain(ctx context.Context, target domain.ProductSubscriptionClassificationTarget) (domain.ClassificationExplanation, error) {
	if target.ClassificationID == "" {
		return domain.ClassificationExplanation{}, ErrNotClassified
	}
	history, err := c.Repo.ListSubscriptionClassifications(ctx, target.ID)
	if err != nil {
		return domain.ClassificationExplanation{}, err
	}
	if len(history) == 0 || history[0].ID != target.ClassificationID {
		return domain.ClassificationExplanation{}, fmt.Errorf("subscription %s: its current classification is not the newest record", target.ID)
	}
	current := history[0]
	policy, err := PolicyFor(current.SubscriptionType)
	if err != nil {
		return domain.ClassificationExplanation{}, err
	}
	out := domain.ClassificationExplanation{SubscriptionID: target.ID, TenantID: target.TenantID, ProductID: target.ProductID,
		Current: current, BillingPolicy: policy, PolicyReference: PolicyReference, History: history}
	if current.SubscriptionType == domain.SubscriptionInternal {
		at := c.now()
		basis, err := c.basis(ctx, current.InternalEligibility.OrganisationID, at)
		if err != nil {
			return domain.ClassificationExplanation{}, err
		}
		out.CurrentEligibility = &domain.CurrentEligibility{EligibilityStatus: "NOT_ELIGIBLE", BasisRelationshipIDs: []string{}, EvaluatedAt: at}
		if len(basis) > 0 {
			out.CurrentEligibility.EligibilityStatus, out.CurrentEligibility.BasisRelationshipIDs = "ELIGIBLE", basis
		}
	}
	var verr *contracts.ValidationError
	if err := contracts.ValidateValue(explanationSchema, out); errors.As(err, &verr) {
		return domain.ClassificationExplanation{}, fmt.Errorf("classification explanation breaks the contract: %v", verr.Problems)
	} else if err != nil {
		return domain.ClassificationExplanation{}, err
	}
	return out, nil
}

// target returns the subscription, refusing a principal who works for its tenant.
func (c *Classifier) target(ctx context.Context, actor Actor, subscriptionID string) (domain.ProductSubscriptionClassificationTarget, error) {
	target, err := c.Repo.GetSubscriptionClassificationTarget(ctx, subscriptionID)
	if errors.Is(err, repository.ErrProductSubscriptionNotFound) {
		return target, ErrNotFound
	}
	if err != nil {
		return target, err
	}
	if c.Memberships == nil {
		return target, errors.New("classification separation of duties cannot be checked: no membership reader is configured")
	}
	m, err := c.Memberships.GetWorkforceMembership(ctx, actor.PrincipalID, target.TenantID)
	switch {
	case err == nil && m.Status == "ACTIVE":
		return target, ErrSelfClassification
	case err != nil && !errors.Is(err, repository.ErrWorkforceMembershipNotFound):
		return target, fmt.Errorf("check the classifier's tenant membership: %w", err)
	}
	return target, nil
}

// primaryOrganisation is the tenant's ACTIVE primary organisation.
func (c *Classifier) primaryOrganisation(ctx context.Context, tenantID string) (string, error) {
	mappings, err := c.Orgs.ListLiveTenantOrganisationMappings(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("read the tenant's organisation: %w", err)
	}
	for _, m := range mappings {
		if m.MappingRole == domain.TenantOrgRolePrimary && m.Status == domain.RelationshipStatusActive {
			return m.OrganisationID, nil
		}
	}
	return "", ErrNoPrimaryOrganisation
}

// admittedBy reports whether decisionID admitted org to the platform: a
// platform relationship in force at at records it.
func (c *Classifier) admittedBy(ctx context.Context, org, decisionID string, at time.Time) (bool, error) {
	rels, err := c.Orgs.ListPlatformRelationships(ctx, org, at)
	if err != nil {
		return false, fmt.Errorf("read the organisation's platform relationships: %w", err)
	}
	return slices.ContainsFunc(rels, func(r domain.PlatformRelationship) bool {
		return r.AdmissionDecisionID == decisionID && r.PlatformID == c.platform() && r.IsConsequential(at)
	}), nil
}

func (c *Classifier) platform() string {
	if c.Eligibility == nil {
		return ""
	}
	return c.Eligibility.Platform()
}

// basis returns the sorted qualifying relationship ids for org at at. An
// evaluation that cannot complete is an error, never "not eligible" and
// never "eligible".
func (c *Classifier) basis(ctx context.Context, org string, at time.Time) ([]string, error) {
	if c.Eligibility == nil {
		return nil, errors.New("INTERNAL eligibility cannot be evaluated: no evaluator is configured")
	}
	rels, err := c.Eligibility.InternalEligibilityBasis(ctx, org, at)
	if err != nil {
		return nil, fmt.Errorf("evaluate INTERNAL eligibility: %w", err)
	}
	ids := make([]string, 0, len(rels))
	for _, r := range rels {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// evaluate returns the eligibility evidence an INTERNAL record carries, or
// ErrNotInternalEligible.
func (c *Classifier) evaluate(ctx context.Context, org string, at time.Time) (*domain.InternalEligibilityRecord, error) {
	ids, err := c.basis(ctx, org, at)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrNotInternalEligible
	}
	return &domain.InternalEligibilityRecord{OrganisationID: org, PlatformID: c.Eligibility.Platform(),
		EligibilityStatus: "ELIGIBLE", BasisRelationshipIDs: ids, EvaluatedAt: at}, nil
}

// record completes r for target and checks it against the contract.
func (c *Classifier) record(target domain.ProductSubscriptionClassificationTarget, actor Actor, r domain.SubscriptionClassificationRecord) (*domain.SubscriptionClassificationRecord, error) {
	id, err := domain.FormatResourceID(domain.SubscriptionClassificationIDPrefix, domain.NewUUIDv7())
	if err != nil {
		return nil, err
	}
	r.ID, r.SubscriptionID, r.TenantID, r.ClassifiedBy = id, target.ID, target.TenantID, actor.PrincipalID
	var verr *contracts.ValidationError
	if err := contracts.ValidateValue(recordSchema, r); errors.As(err, &verr) {
		return nil, &InvalidError{Problems: verr.Problems}
	} else if err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Classifier) classify(ctx context.Context, actor Actor, subscriptionID string, decide repository.ClassificationDecision) (domain.SubscriptionClassificationRecord, bool, error) {
	rec, created, err := c.Repo.ClassifySubscription(ctx, subscriptionID, actor.Audit, decide)
	if errors.Is(err, repository.ErrProductSubscriptionNotFound) {
		return rec, false, ErrNotFound
	}
	return rec, created, err
}
