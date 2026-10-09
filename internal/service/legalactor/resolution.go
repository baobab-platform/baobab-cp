// Package legalactor holds the fail-closed LA-04 policy selection model.
// It does not expose a route or confer authority: callers must load candidates
// from trusted CP persistence and establish legal-actor verification themselves.
package legalactor

import (
	"strings"
	"time"
)

type Outcome string

const (
	Authorized          Outcome = "AUTHORIZED"
	NoApplicableMandate Outcome = "NO_APPLICABLE_MANDATE"
	Ambiguous           Outcome = "AMBIGUOUS"
	RevokedOrExpired    Outcome = "REVOKED_OR_EXPIRED"
	ActorNotVerified    Outcome = "ACTOR_NOT_VERIFIED"
)

const PolicyReference = "ADR-BCP-027/LA-04A/fail-closed-legal-actor-resolution"

type Request struct {
	TenantID                string
	OperatingOrganisationID string
	Role                    string
	Activity                string
	Market                  string
	Capability              string
	EffectiveAt             time.Time
}

type Candidate struct {
	MandateID                       string
	TenantID                        string
	OperatingOrganisationID         string
	ResponsibleLegalEntityID        string
	Roles                           []string
	ActivityScope                   []string
	MarketScope                     []string
	CapabilityScope                 []string
	Status                          string
	AuthorityBasisReference         string
	EvidenceReferences              []string
	LegalActorVerificationReference string
	EffectiveFrom                   time.Time
	EffectiveTo                     *time.Time
	CreatedBy                       string
	ApprovedBy                      string
	ApprovedAt                      *time.Time
	ActorVerified                   bool
}

type Resolution struct {
	Outcome                  Outcome
	EvaluatedAt              time.Time
	PolicyReference          string
	MandateID                string
	ResponsibleLegalEntityID string
	EvidenceReferences       []string
	ValidUntil               *time.Time
}

// Resolve evaluates trusted candidates at the server's present evaluation
// time. Request.EffectiveAt is an additional restriction, NEVER a way to
// redeem an expired mandate by backdating a new transaction.
//
// In LA-04A the SQL activation gate prevents ACTIVE rows. This pure function
// defines testable behavior for LA-04B; no HTTP or production consumer calls it.
// The responsibility, evidence and actor verification facts must be supplied
// only by a trusted CP repository, not by a browser or engine-supplied claim.
func Resolve(request Request, candidates []Candidate, evaluatedAt time.Time) Resolution {
	result := Resolution{
		Outcome:         NoApplicableMandate,
		EvaluatedAt:     evaluatedAt.UTC(),
		PolicyReference: PolicyReference,
	}
	if !validRequest(request, evaluatedAt) {
		return result
	}

	var matching []Candidate
	hasRevokedOrExpired := false
	for _, mandate := range candidates {
		if mandate.TenantID != request.TenantID ||
			mandate.OperatingOrganisationID != request.OperatingOrganisationID ||
			!contains(mandate.Roles, request.Role) ||
			!contains(mandate.ActivityScope, request.Activity) ||
			!contains(mandate.MarketScope, request.Market) {
			continue
		}
		if len(mandate.CapabilityScope) > 0 &&
			!contains(mandate.CapabilityScope, request.Capability) {
			continue
		}
		if mandate.Status == "REVOKED" || mandate.Status == "EXPIRED" {
			hasRevokedOrExpired = true
			continue
		}
		if mandate.Status != "ACTIVE" {
			continue
		}
		if evaluatedAt.Before(mandate.EffectiveFrom) ||
			request.EffectiveAt.Before(mandate.EffectiveFrom) {
			continue
		}
		if !effectiveAt(mandate, evaluatedAt) ||
			!effectiveAt(mandate, request.EffectiveAt) {
			hasRevokedOrExpired = true
			continue
		}
		matching = append(matching, mandate)
	}

	switch len(matching) {
	case 0:
		if hasRevokedOrExpired {
			result.Outcome = RevokedOrExpired
		}
		return result
	case 1:
		// Continue below; a single scoped mandate is not automatically
		// sufficient for legal responsibility or provider eligibility.
	default:
		result.Outcome = Ambiguous
		return result
	}

	m := matching[0]
	if m.MandateID == "" || m.ResponsibleLegalEntityID == "" ||
		strings.TrimSpace(m.AuthorityBasisReference) == "" ||
		len(m.EvidenceReferences) == 0 ||
		strings.TrimSpace(m.LegalActorVerificationReference) == "" ||
		m.ApprovedBy == "" || m.CreatedBy == "" ||
		m.ApprovedBy == m.CreatedBy || m.ApprovedAt == nil ||
		!m.ActorVerified {
		result.Outcome = ActorNotVerified
		return result
	}
	result.Outcome = Authorized
	result.MandateID = m.MandateID
	result.ResponsibleLegalEntityID = m.ResponsibleLegalEntityID
	result.EvidenceReferences = append([]string(nil), m.EvidenceReferences...)
	if m.EffectiveTo != nil {
		at := m.EffectiveTo.UTC()
		result.ValidUntil = &at
	}
	return result
}

func effectiveAt(m Candidate, at time.Time) bool {
	return !at.Before(m.EffectiveFrom) &&
		(m.EffectiveTo == nil || at.Before(*m.EffectiveTo))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validRequest(request Request, evaluatedAt time.Time) bool {
	if request.TenantID == "" || request.OperatingOrganisationID == "" ||
		request.Activity == "" || request.EffectiveAt.IsZero() ||
		evaluatedAt.IsZero() || request.EffectiveAt.After(evaluatedAt) || !knownRole(request.Role) {
		return false
	}
	if len(request.Market) != 2 {
		return false
	}
	for _, c := range request.Market {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func knownRole(role string) bool {
	switch role {
	case "CONTRACTING_PARTY", "SELLER_OF_RECORD", "INVOICE_ISSUER",
		"IMPORTER_OF_RECORD", "EXPORTER_OF_RECORD", "EMPLOYER",
		"ACCOUNTING_ENTITY", "PAYMENT_BENEFICIARY", "PROPERTY_OWNER",
		"SERVICE_PROVIDER_OF_RECORD":
		return true
	}
	return false
}
