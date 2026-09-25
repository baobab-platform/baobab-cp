// ADR-BCP-018 gate ORG-11 — ProductSubscription classification and
// provenance (ADR-BCP-017 sections 10-13, 48; ADR-SHARED-011).
// Contract: baobab-platform/shared contracts/product/v1.

package domain

import "time"

const (
	ProductSubscriptionIDPrefix        = "sub"
	SubscriptionClassificationIDPrefix = "subcls"
)

// Valid reports whether t is one of the ADR-BCP-005 subscription types.
func (t SubscriptionType) Valid() bool {
	switch t {
	case SubscriptionCommercial, SubscriptionInternal, SubscriptionTrial, SubscriptionPartner,
		SubscriptionManual, SubscriptionMigration:
		return true
	}
	return false
}

// ClassificationSource is the governed record a classification rests on.
type ClassificationSource string

const (
	ClassificationFromAdmissionDecision ClassificationSource = "ADMISSION_DECISION"
	ClassificationFromReclassification  ClassificationSource = "RECLASSIFICATION"
	ClassificationFromMigration         ClassificationSource = "MIGRATION"
	ClassificationFromManualGovernance  ClassificationSource = "MANUAL_GOVERNANCE"
)

// SubscriptionClassificationRecord is one immutable classification or
// reclassification of a ProductSubscription. An INTERNAL record always
// carries the eligibility the Control Plane evaluated, and no other type
// carries any. Reclassification appends a record: tenant, organisation and
// subscription identity never change.
type SubscriptionClassificationRecord struct {
	ID                       string                     `json:"classification_id"`
	SubscriptionID           string                     `json:"subscription_id"`
	TenantID                 string                     `json:"tenant_id"`
	SubscriptionType         SubscriptionType           `json:"subscription_type"`
	PreviousSubscriptionType SubscriptionType           `json:"previous_subscription_type,omitempty"`
	Source                   ClassificationSource       `json:"classification_source"`
	Reference                string                     `json:"classification_reference"`
	Reason                   string                     `json:"reason"`
	ClassifiedAt             time.Time                  `json:"classified_at"`
	ClassifiedBy             string                     `json:"classified_by"`
	InternalEligibility      *InternalEligibilityRecord `json:"internal_eligibility,omitempty"`
}

// ProductSubscriptionClassificationTarget is the part of a ProductSubscription
// classification reads and writes.
type ProductSubscriptionClassificationTarget struct {
	ID               string
	TenantID         string
	ProductID        string
	Status           string
	SubscriptionType SubscriptionType
	ClassificationID string
	Version          int64
}

// BillingPolicy is what a subscription type means for billing
// (contracts/product/v1/billing-policy.yaml). Only the monetary charge and
// payment execution vary; everything else is always on.
type BillingPolicy struct {
	MonetaryCharge     string `json:"monetary_charge" yaml:"monetary_charge"`
	BillingRequired    bool   `json:"billing_required" yaml:"billing_required"`
	UsageMetering      bool   `json:"usage_metering" yaml:"usage_metering"`
	EntitlementControl bool   `json:"entitlement_control" yaml:"entitlement_control"`
	Audit              bool   `json:"audit" yaml:"audit"`
	ReadinessControl   bool   `json:"readiness_control" yaml:"readiness_control"`
	IsolationControl   bool   `json:"isolation_control" yaml:"isolation_control"`
	PaymentExecution   string `json:"payment_execution" yaml:"payment_execution"`
}

// CurrentEligibility is INTERNAL eligibility re-evaluated now. NOT_ELIGIBLE
// is drift: governed review or reclassification follows, never deletion.
type CurrentEligibility struct {
	EligibilityStatus    string    `json:"eligibility_status"`
	BasisRelationshipIDs []string  `json:"basis_relationship_ids"`
	EvaluatedAt          time.Time `json:"evaluated_at"`
}

// ClassificationExplanation answers "why is this subscription classified
// as it is?" from Control Plane records alone.
type ClassificationExplanation struct {
	SubscriptionID     string                             `json:"subscription_id"`
	TenantID           string                             `json:"tenant_id"`
	ProductID          string                             `json:"product_id"`
	Current            SubscriptionClassificationRecord   `json:"current"`
	CurrentEligibility *CurrentEligibility                `json:"current_eligibility,omitempty"`
	BillingPolicy      BillingPolicy                      `json:"billing_policy"`
	PolicyReference    string                             `json:"policy_reference"`
	History            []SubscriptionClassificationRecord `json:"history"`
}
