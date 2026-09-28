// Package verification is the ADR-BCP-023 verification workflow (gate
// OEV-03): verification cases, the claims they examine, the checks
// performed on them, the results that establish a claim's standing, and
// discrepancies between sources. Contract: baobab-platform/shared
// contracts/evidence/v1 (evidence.schema.json, verification.schema.json,
// lifecycle.yaml and source-registry.yaml), loaded from the embedded
// contracts rather than restated here.
//
// Its rules are the ones the Shared validator holds the contract's example
// to: nobody checks or decides a claim they asserted (section 169); a
// source verifies only what it is registered as authoritative for, in the
// claim's jurisdiction (sections 21-22); a positive check cites only
// evidence reviewers may open (section 238); and a claim's standing is its
// current result's outcome.
package verification

import "time"

// Subject is subjectReference.
type Subject struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
}

// Case is VerificationCase.
type Case struct {
	CaseID           string     `json:"case_id"`
	Subject          Subject    `json:"subject"`
	ApplicationID    string     `json:"application_id,omitempty"`
	OrganisationID   string     `json:"organisation_id,omitempty"`
	Purpose          string     `json:"purpose"`
	PolicyProfile    string     `json:"policy_profile,omitempty"`
	PolicyVersion    int        `json:"policy_version,omitempty"`
	Status           string     `json:"status"`
	ClaimIDs         []string   `json:"claim_ids"`
	AssignedReviewer string     `json:"assigned_reviewer,omitempty"`
	OpenedAt         time.Time  `json:"opened_at"`
	SubmittedAt      *time.Time `json:"submitted_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Supersedes       string     `json:"supersedes,omitempty"`
	CorrelationID    string     `json:"correlation_id,omitempty"`
	Version          int64      `json:"version"`
}

// ClaimedValue is claimedValue.
type ClaimedValue struct {
	Value           string `json:"value"`
	NormalisedValue string `json:"normalised_value,omitempty"`
}

// Claim is EvidenceClaim.
type Claim struct {
	ClaimID         string       `json:"claim_id"`
	Subject         Subject      `json:"subject"`
	ClaimType       string       `json:"claim_type"`
	Jurisdiction    string       `json:"jurisdiction,omitempty"`
	ClaimedValue    ClaimedValue `json:"claimed_value"`
	AssertedBy      string       `json:"asserted_by"`
	AssertedVia     string       `json:"asserted_via"`
	AssertedAt      time.Time    `json:"asserted_at"`
	SourceAppID     string       `json:"source_application_id,omitempty"`
	Purpose         string       `json:"purpose"`
	Status          string       `json:"status"`
	Freshness       string       `json:"freshness,omitempty"`
	EvidenceIDs     []string     `json:"evidence_ids"`
	CurrentResultID string       `json:"current_result_id,omitempty"`
	Version         int64        `json:"version"`
}

// ClaimSubmission is EvidenceClaimSubmission.
type ClaimSubmission struct {
	Subject      Subject      `json:"subject"`
	ClaimType    string       `json:"claim_type"`
	Jurisdiction string       `json:"jurisdiction,omitempty"`
	ClaimedValue ClaimedValue `json:"claimed_value"`
	Purpose      string       `json:"purpose"`
	EvidenceIDs  []string     `json:"evidence_ids,omitempty"`
}

// ApplicantClaim is ApplicantClaimSubmission: what an applicant asserts
// about their own application. Its subject, purpose, asserter, origin and
// status are derived, never supplied (sections 9, 191).
type ApplicantClaim struct {
	ClaimType    string       `json:"claim_type"`
	Jurisdiction string       `json:"jurisdiction,omitempty"`
	ClaimedValue ClaimedValue `json:"claimed_value"`
}

// Dimension is dimensionResult.
type Dimension struct {
	Dimension string `json:"dimension"`
	Outcome   string `json:"outcome"`
	Detail    string `json:"detail,omitempty"`
}

// Check is VerificationCheck.
type Check struct {
	CheckID               string      `json:"check_id"`
	CaseID                string      `json:"case_id"`
	ClaimID               string      `json:"claim_id"`
	Method                string      `json:"method"`
	SourceID              string      `json:"source_id"`
	Provider              string      `json:"provider,omitempty"`
	RequestedAt           time.Time   `json:"requested_at"`
	PerformedAt           *time.Time  `json:"performed_at,omitempty"`
	PerformedBy           string      `json:"performed_by,omitempty"`
	SourceObservedAt      *time.Time  `json:"source_observed_at,omitempty"`
	SourceRecordReference string      `json:"source_record_reference,omitempty"`
	Outcome               string      `json:"outcome"`
	Dimensions            []Dimension `json:"dimensions"`
	ReasonCodes           []string    `json:"reason_codes"`
	FreshnessUntil        *time.Time  `json:"freshness_until,omitempty"`
	EvidenceIDs           []string    `json:"evidence_ids"`
}

// CheckRecord is VerificationCheckRecordRequest.
type CheckRecord struct {
	ClaimID               string      `json:"claim_id"`
	Method                string      `json:"method"`
	SourceID              string      `json:"source_id"`
	Provider              string      `json:"provider,omitempty"`
	SourceObservedAt      *time.Time  `json:"source_observed_at,omitempty"`
	SourceRecordReference string      `json:"source_record_reference,omitempty"`
	Outcome               string      `json:"outcome"`
	Dimensions            []Dimension `json:"dimensions"`
	ReasonCodes           []string    `json:"reason_codes"`
	FreshnessUntil        *time.Time  `json:"freshness_until,omitempty"`
	EvidenceIDs           []string    `json:"evidence_ids"`
}

// Result is VerificationResult.
type Result struct {
	ResultID       string      `json:"result_id"`
	ClaimID        string      `json:"claim_id"`
	CaseID         string      `json:"case_id"`
	Outcome        string      `json:"outcome"`
	CheckIDs       []string    `json:"check_ids"`
	Dimensions     []Dimension `json:"dimensions"`
	Freshness      string      `json:"freshness"`
	FreshnessUntil *time.Time  `json:"freshness_until,omitempty"`
	DecidedBy      string      `json:"decided_by,omitempty"`
	DecidedAt      time.Time   `json:"decided_at"`
	DiscrepancyIDs []string    `json:"discrepancy_ids,omitempty"`
	Supersedes     string      `json:"supersedes,omitempty"`
	ReasonCodes    []string    `json:"reason_codes"`
}

// ResultRecord is VerificationResultRecordRequest.
type ResultRecord struct {
	ClaimID        string      `json:"claim_id"`
	Outcome        string      `json:"outcome"`
	CheckIDs       []string    `json:"check_ids"`
	Dimensions     []Dimension `json:"dimensions"`
	Freshness      string      `json:"freshness"`
	FreshnessUntil *time.Time  `json:"freshness_until,omitempty"`
	DiscrepancyIDs []string    `json:"discrepancy_ids,omitempty"`
	ReasonCodes    []string    `json:"reason_codes"`
}

// SourceValue is sourceValue.
type SourceValue struct {
	SourceID        string     `json:"source_id"`
	Value           string     `json:"value"`
	NormalisedValue string     `json:"normalised_value,omitempty"`
	ObservedAt      *time.Time `json:"observed_at,omitempty"`
	EvidenceID      string     `json:"evidence_id,omitempty"`
	CheckID         string     `json:"check_id,omitempty"`
}

// Discrepancy is EvidenceDiscrepancy.
type Discrepancy struct {
	DiscrepancyID     string        `json:"discrepancy_id"`
	Subject           Subject       `json:"subject"`
	ClaimType         string        `json:"claim_type"`
	CaseID            string        `json:"case_id,omitempty"`
	ConflictingValues []SourceValue `json:"conflicting_values"`
	Severity          string        `json:"severity"`
	Status            string        `json:"status"`
	DetectedAt        time.Time     `json:"detected_at"`
	Resolution        string        `json:"resolution,omitempty"`
	ResolvedBy        string        `json:"resolved_by,omitempty"`
	ResolvedAt        *time.Time    `json:"resolved_at,omitempty"`
	Reason            string        `json:"reason,omitempty"`
	Version           int64         `json:"version"`
}

// DiscrepancyRecord is EvidenceDiscrepancyRecordRequest.
type DiscrepancyRecord struct {
	Subject           Subject       `json:"subject"`
	ClaimType         string        `json:"claim_type"`
	ConflictingValues []SourceValue `json:"conflicting_values"`
	Severity          string        `json:"severity"`
}

// Transition is VerificationCaseTransitionRequest and
// EvidenceDiscrepancyTransitionRequest.
type Transition struct {
	Command    string `json:"command"`
	Resolution string `json:"resolution,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Evidence is EvidenceRecord.
type Evidence struct {
	EvidenceID            string     `json:"evidence_id"`
	EvidenceType          string     `json:"evidence_type"`
	Subject               Subject    `json:"subject"`
	Purpose               string     `json:"purpose"`
	SourceID              string     `json:"source_id"`
	SourceRecordReference string     `json:"source_record_reference,omitempty"`
	ArtifactID            string     `json:"artifact_id,omitempty"`
	CredentialReference   string     `json:"credential_reference,omitempty"`
	SubmittedBy           string     `json:"submitted_by,omitempty"`
	ObtainedBy            string     `json:"obtained_by,omitempty"`
	IssuedAt              *time.Time `json:"issued_at,omitempty"`
	ObservedAt            *time.Time `json:"observed_at,omitempty"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	Classification        string     `json:"classification"`
	RetentionPolicy       string     `json:"retention_policy"`
	Status                string     `json:"status"`
	Supersedes            string     `json:"supersedes,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	Version               int64      `json:"version"`
}

// EvidenceRegistration is EvidenceRegistrationRequest.
type EvidenceRegistration struct {
	EvidenceType          string     `json:"evidence_type"`
	Subject               Subject    `json:"subject"`
	Purpose               string     `json:"purpose"`
	SourceID              string     `json:"source_id"`
	SourceRecordReference string     `json:"source_record_reference,omitempty"`
	CredentialReference   string     `json:"credential_reference,omitempty"`
	IssuedAt              *time.Time `json:"issued_at,omitempty"`
	ObservedAt            *time.Time `json:"observed_at,omitempty"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
	Classification        string     `json:"classification"`
	RetentionPolicy       string     `json:"retention_policy"`
	Supersedes            string     `json:"supersedes,omitempty"`
}

// Reference is EvidenceReference: an EvidenceRecord's metadata only.
type Reference struct {
	EvidenceID     string     `json:"evidence_id"`
	EvidenceType   string     `json:"evidence_type"`
	Status         string     `json:"status"`
	Classification string     `json:"classification"`
	ObservedAt     *time.Time `json:"observed_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

// Reference returns the record's metadata-only view.
func (e Evidence) Reference() Reference {
	return Reference{EvidenceID: e.EvidenceID, EvidenceType: e.EvidenceType, Status: e.Status,
		Classification: e.Classification, ObservedAt: e.ObservedAt, ExpiresAt: e.ExpiresAt}
}

// TrustedClaim is trustedClaim.
type TrustedClaim struct {
	ClaimType    string `json:"claim_type" yaml:"claim_type"`
	Jurisdiction string `json:"jurisdiction" yaml:"jurisdiction"`
}

// Source is EvidenceSource.
type Source struct {
	SourceID             string         `json:"source_id" yaml:"source_id"`
	SourceClass          string         `json:"source_class" yaml:"source_class"`
	Name                 string         `json:"name" yaml:"name"`
	Jurisdiction         string         `json:"jurisdiction,omitempty" yaml:"jurisdiction"`
	AuthorityReference   string         `json:"authority_reference,omitempty" yaml:"authority_reference"`
	TrustedFor           []TrustedClaim `json:"trusted_for" yaml:"trusted_for"`
	AccessClassification string         `json:"access_classification" yaml:"access_classification"`
	EffectiveFrom        string         `json:"effective_from" yaml:"effective_from"`
	EffectiveTo          string         `json:"effective_to,omitempty" yaml:"effective_to"`
	Status               string         `json:"status" yaml:"status"`
}

// Trusts reports whether an active source is authoritative for a claim type
// in a jurisdiction; a claim without a jurisdiction is matched on type.
func (s Source) Trusts(claimType, jurisdiction string) bool {
	if s.Status != "ACTIVE" {
		return false
	}
	for _, t := range s.TrustedFor {
		if t.ClaimType == claimType && (jurisdiction == "" || t.Jurisdiction == jurisdiction) {
			return true
		}
	}
	return false
}
