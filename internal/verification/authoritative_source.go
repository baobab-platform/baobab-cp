package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PEO-03B: source observations carry verified provenance but never constitute
// a VerificationResult. RecordVerificationResult remains a distinct,
// independent OEV-03 reviewer decision.
var ErrUntrustedObservation = errors.New("authoritative source observation is not independently trustworthy")

const MaxAuthoritativeObservationAge = 5 * time.Minute

// SourceObservation is a sanitized canonical evidence/v1 observation.
// It MUST NOT contain taxpayer PINs, document bodies or API credentials.
type SourceObservation struct {
	CaseID           string
	ClaimID          string
	Subject          Subject
	ClaimType        string
	Jurisdiction     string
	Purpose          string
	SourceID         string
	Method           string
	Outcome          string
	SourceReference  string
	ProvenanceRef    string
	ResponseDigest   string
	ObservedAt       time.Time
	FreshnessUntil   time.Time
	AdapterID        string
	WitnessPrincipal string
	CheckerPrincipal string
	Synthetic        bool
}

// CompanyRecord is the minimal allowlisted output of a credentialed CIPC
// Companies API adapter. The official API endpoint, OAuth transport, subscription
// and response mapping must be provided from CIPC's approved specification.
// No generic untrusted HTTP URL is accepted at this domain boundary.
type CompanyRecord struct {
	RegistrationNumber string
	LegalName          string
	EntityStatus       string
	SourceReference    string
	ProvenanceRef      string
	ResponseDigest     string
	ObservedAt         time.Time
	FreshnessUntil     time.Time
	Synthetic          bool
}

// CompanyRegistry is implemented by a certified CIPC connector. Test doubles
// may be used only with AllowSynthetic=true in an isolated test environment.
type CompanyRegistry interface {
	LookupCompany(context.Context, string) (CompanyRecord, error)
}

// CIPCCompaniesAdapter contains no bearer tokens or URLs. The transport is
// injected by the approved source adapter at composition root.
type CIPCCompaniesAdapter struct {
	Registry       CompanyRegistry
	Certified      bool
	AllowSynthetic bool
	AdapterID      string
}

// LookupCIPC creates a non-authorising observation for a pre-existing claim.
// A caller cannot supply the source, company reference, or observation outcome.
func (a CIPCCompaniesAdapter) LookupCIPC(ctx context.Context, caseID string, claim Claim, companyNumber string, now time.Time) (SourceObservation, error) {
	if ctx == nil || a.Registry == nil || !a.Certified || !strings.HasPrefix(caseID, "vcase_") ||
		claim.ClaimID == "" || strings.TrimSpace(companyNumber) == "" ||
		claim.Jurisdiction != "ZA" || !cipcClaimSupported(claim.ClaimType) ||
		claim.Subject.SubjectType == "" || claim.Subject.SubjectID == "" ||
		claim.Purpose == "" || a.AdapterID == "" {
		return SourceObservation{}, ErrUntrustedObservation
	}
	source, err := SourceByID("esrc_cipc")
	if err != nil || !source.Trusts(claim.ClaimType, "ZA") {
		return SourceObservation{}, ErrUntrustedObservation
	}
	record, err := a.Registry.LookupCompany(ctx, strings.TrimSpace(companyNumber))
	if err != nil {
		return SourceObservation{}, fmt.Errorf("%w: registry lookup unavailable", ErrUntrustedObservation)
	}
	if record.Synthetic && !a.AllowSynthetic {
		return SourceObservation{}, ErrUntrustedObservation
	}
	var actual string
	switch claim.ClaimType {
	case "REGISTRATION_IDENTIFIER":
		actual = record.RegistrationNumber
	case "LEGAL_NAME":
		actual = record.LegalName
	case "ENTITY_STATUS":
		actual = record.EntityStatus
	default:
		return SourceObservation{}, ErrUntrustedObservation
	}
	// A lookup for a different company cannot establish the requested claim.
	if record.RegistrationNumber != strings.TrimSpace(companyNumber) ||
		!strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(claim.ClaimedValue.Value)) {
		return SourceObservation{}, ErrUntrustedObservation
	}
	o := SourceObservation{
		CaseID: caseID, ClaimID: claim.ClaimID, Subject: claim.Subject,
		ClaimType: claim.ClaimType, Jurisdiction: "ZA", Purpose: claim.Purpose,
		SourceID: "esrc_cipc", Method: "SOURCE_API_LOOKUP", Outcome: "MATCHED",
		SourceReference: record.SourceReference, ProvenanceRef: record.ProvenanceRef,
		ResponseDigest: record.ResponseDigest, ObservedAt: record.ObservedAt,
		FreshnessUntil: record.FreshnessUntil, AdapterID: a.AdapterID,
		Synthetic: record.Synthetic,
	}
	if err := ValidateAuthoritativeObservation(claim, caseID, o, now, a.AllowSynthetic); err != nil {
		return SourceObservation{}, err
	}
	return o, nil
}

func cipcClaimSupported(t string) bool {
	// Existence and director/mandate claims need their own explicit semantic
	// mapping rather than guessing from a status label returned by an API.
	return t == "LEGAL_NAME" || t == "REGISTRATION_IDENTIFIER" || t == "ENTITY_STATUS"
}

func validSHA256(d string) bool {
	if len(d) != 71 || !strings.HasPrefix(d, "sha256:") {
		return false
	}
	v, err := hex.DecodeString(strings.TrimPrefix(d, "sha256:"))
	return err == nil && len(v) == sha256.Size && d == strings.ToLower(d)
}

// ValidateAuthoritativeObservation checks source authority and freshness;
// still NOT a positive verification result or an onboarding grant.
func ValidateAuthoritativeObservation(claim Claim, caseID string, o SourceObservation, now time.Time, allowSynthetic bool) error {
	src, err := SourceByID(o.SourceID)
	if err != nil || !src.Trusts(claim.ClaimType, claim.Jurisdiction) ||
		claim.ClaimID == "" || claim.ClaimID != o.ClaimID ||
		claim.Subject != o.Subject || claim.Purpose != o.Purpose ||
		claim.ClaimType != o.ClaimType || claim.Jurisdiction != o.Jurisdiction ||
		caseID == "" || o.CaseID != caseID || o.SourceReference == "" ||
		len(o.SourceReference) > 256 || len(o.ProvenanceRef) < 8 ||
		len(o.ProvenanceRef) > 512 || !validSHA256(o.ResponseDigest) ||
		o.Outcome != "MATCHED" || o.Synthetic && !allowSynthetic ||
		now.IsZero() || o.ObservedAt.IsZero() || o.FreshnessUntil.IsZero() ||
		o.ObservedAt.After(now.Add(time.Second)) || now.Sub(o.ObservedAt) > MaxAuthoritativeObservationAge ||
		!o.FreshnessUntil.After(now) || !o.FreshnessUntil.After(o.ObservedAt) {
		return ErrUntrustedObservation
	}
	switch o.Method {
	case "SOURCE_API_LOOKUP":
		if o.AdapterID == "" || o.WitnessPrincipal != "" || o.CheckerPrincipal != "" {
			return ErrUntrustedObservation
		}
	case "MANUAL_REGISTRY_LOOKUP", "OFFICIAL_DOCUMENT_REVIEW":
		if o.WitnessPrincipal == "" || o.CheckerPrincipal == "" ||
			o.WitnessPrincipal == o.CheckerPrincipal ||
			o.WitnessPrincipal == claim.AssertedBy ||
			o.CheckerPrincipal == claim.AssertedBy {
			return ErrUntrustedObservation
		}
	default:
		return ErrUntrustedObservation
	}
	return nil
}

// SourceCheckRecord returns an OEV-03 CHECK candidate; it cannot directly
// write any claim standing. Repository/handler must independently authenticate
// performer, bind existing claim/case, record check, then a different reviewer
// must issue the distinct VerificationResult.
func SourceCheckRecord(claim Claim, caseID, performer string, o SourceObservation, now time.Time, allowSynthetic bool) (CheckRecord, error) {
	if performer == "" || performer == claim.AssertedBy ||
		(o.Method != "SOURCE_API_LOOKUP" && performer != o.CheckerPrincipal) {
		return CheckRecord{}, ErrUntrustedObservation
	}
	if err := ValidateAuthoritativeObservation(claim, caseID, o, now, allowSynthetic); err != nil {
		return CheckRecord{}, err
	}
	return CheckRecord{
		ClaimID: claim.ClaimID, Method: o.Method, SourceID: o.SourceID,
		SourceObservedAt: &o.ObservedAt, SourceRecordReference: o.SourceReference,
		Outcome: "MATCHED", FreshnessUntil: &o.FreshnessUntil,
		Dimensions: []Dimension{{Dimension: "CLAIM_MATCH", Outcome: "PASSED"},
			{Dimension: "ISSUER_AUTHORITY", Outcome: "PASSED"}},
		ReasonCodes: []string{}, EvidenceIDs: []string{},
	}, nil
}

// AssistedOfficialReview is a recorded, two-person official-portal observation;
// an applicant-provided TCS PIN must NEVER be placed in this structure.
// The source registry must be separately ACTIVE before it can support a check.
func AssistedOfficialReview(caseID string, claim Claim, sourceID, sourceRef, proofDigest, proofRef,
	witness, checker string, observed, expires time.Time) SourceObservation {
	return SourceObservation{
		CaseID: caseID, ClaimID: claim.ClaimID, Subject: claim.Subject,
		ClaimType: claim.ClaimType, Jurisdiction: claim.Jurisdiction,
		Purpose: claim.Purpose, SourceID: sourceID,
		Method: "MANUAL_REGISTRY_LOOKUP", Outcome: "MATCHED",
		SourceReference: sourceRef, ResponseDigest: proofDigest,
		ProvenanceRef: proofRef, WitnessPrincipal: witness,
		CheckerPrincipal: checker, ObservedAt: observed, FreshnessUntil: expires,
	}
}
