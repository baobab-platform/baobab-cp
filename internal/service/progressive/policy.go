// Package progressive implements fail-closed PEO-02/03 evidence decisions.
// It does not grant legal status, tenant access, provider readiness or entitlements.
package progressive

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const FoundingDocumentaryGraceMonths = 24

var ErrDeferralDenied = errors.New("PEO-02: founding documentary deferral denied")

// CalendarAnniversaryUTC preserves the instant of day but clamps month-end
// anniversaries (Jan 31 -> Feb 28/29) rather than overflowing to March.
// Month offsets are always calendar months, never 30-day approximations.
func CalendarAnniversaryUTC(start time.Time, months int) time.Time {
	start = start.UTC()
	yr, mn, day := start.Date()
	base := time.Date(yr, mn+time.Month(months), 1, start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), time.UTC)
	next := base.AddDate(0, 1, 0)
	maxDay := next.AddDate(0, 0, -1).Day()
	if day > maxDay {
		day = maxDay
	}
	return time.Date(base.Year(), base.Month(), day, start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), time.UTC)
}

type DocumentaryRequirement struct {
	ID string
	// Only an independently governed applicability policy classifies the
	// requirement; NEVER infer this from an unverified document label.
	Authority string // PLATFORM_DOCUMENTARY, STATUTORY or PROVIDER
}

type FoundingDeferral struct {
	OrganisationID        string
	SponsorshipID         string
	SponsorOrganisationID string
	SponsorStatus         string
	SponsorScope          string
	MakerID               string
	CheckerID             string
	PolicyReference       string
	ApprovalReference     string
	ProvisionalApprovalAt time.Time
	ApprovedAt            time.Time
	EffectiveFrom         time.Time
	ExpiresAt             time.Time
	Status                string
	RequirementIDs        []string
	MaximumDurationMonths int
}

// ValidateFoundingDeferral makes the 24-month authority boundary explicit.
// DB approval history + immutable original admission approval are additionally
// required: this policy check alone cannot issue or activate the deferral.
func ValidateFoundingDeferral(d FoundingDeferral, requirements []DocumentaryRequirement, now time.Time) error {
	deny := func(detail string) error { return fmt.Errorf("%w: %s", ErrDeferralDenied, detail) }
	if d.OrganisationID == "" || d.SponsorshipID == "" || d.SponsorOrganisationID == "" ||
		d.SponsorStatus != "ACTIVE" || d.SponsorScope != "INTERNAL_GROUP_ADMISSION" ||
		d.MakerID == "" || d.CheckerID == "" || d.MakerID == d.CheckerID ||
		strings.TrimSpace(d.PolicyReference) == "" || strings.TrimSpace(d.ApprovalReference) == "" ||
		d.MaximumDurationMonths != FoundingDocumentaryGraceMonths ||
		d.ProvisionalApprovalAt.IsZero() || d.ApprovedAt.IsZero() || d.EffectiveFrom.IsZero() || d.ExpiresAt.IsZero() {
		return deny("missing independent sponsorship, policy, maker/checker or dates")
	}
	// Starts with the FIRST effective provisional admission approval, not the
	// checker date, latest tenant creation, name change or retry.
	start := d.ProvisionalApprovalAt.UTC()
	expiry := CalendarAnniversaryUTC(start, FoundingDocumentaryGraceMonths)
	if !d.EffectiveFrom.UTC().Equal(start) || !d.ExpiresAt.UTC().Equal(expiry) ||
		d.ApprovedAt.After(now) || d.ApprovedAt.Before(start) || !now.Before(expiry) ||
		now.Before(start) || d.Status != "ACTIVE" {
		return deny("not in the approved 24-month window")
	}
	if len(d.RequirementIDs) == 0 || len(requirements) == 0 {
		return deny("no named documentary requirements")
	}
	lookup := make(map[string]string, len(requirements))
	for _, r := range requirements {
		if r.ID == "" || r.Authority == "" {
			return deny("unclassified requirement")
		}
		if old, ok := lookup[r.ID]; ok && old != r.Authority {
			return deny("contradictory classification")
		}
		lookup[r.ID] = r.Authority
	}
	seen := map[string]bool{}
	for _, id := range d.RequirementIDs {
		if id == "" || seen[id] || lookup[id] != "PLATFORM_DOCUMENTARY" {
			return deny("duplicate, unknown, statutory or provider requirement")
		}
		seen[id] = true
	}
	return nil
}

type Applicability string

const (
	Required        Applicability = "REQUIRED"
	NotApplicable   Applicability = "NOT_APPLICABLE"
	NeedsAssessment Applicability = "NEEDS_ASSESSMENT"
)

type RequirementAssessment struct {
	RequirementID     string
	PolicyReference   string
	Authority         string
	Applies           Applicability
	EvidenceReference string
	// Only APPROVED_EVIDENCE can satisfy a REQUIRED requirement. Self-declared
	// business form/employment status is not sufficient.
	EvidenceState    string
	ContextReference string
	Market           string
	Capability       string
	EvaluatedAt      time.Time
	ValidUntil       time.Time
}

type RequirementDecision struct {
	ID      string
	Blocked bool
	Reason  string
}

// EvaluateProgressiveEligibility enforces a decision issued by an approved
// applicable policy. It NEVER grants access; the caller's own capability PEP
// must separately inspect verified evidence, entitlement and readiness.
func EvaluateProgressiveEligibility(items []RequirementAssessment, deferred FoundingDeferral, now time.Time, classes []DocumentaryRequirement) ([]RequirementDecision, error) {
	if len(items) == 0 {
		return nil, errors.New("PEO-03: requirements policy assessment absent")
	}
	out := make([]RequirementDecision, 0, len(items))
	for _, v := range items {
		if strings.TrimSpace(v.RequirementID) == "" || strings.TrimSpace(v.PolicyReference) == "" ||
			strings.TrimSpace(v.ContextReference) == "" || v.EvaluatedAt.IsZero() ||
			v.ValidUntil.IsZero() || v.EvaluatedAt.After(now) || !now.Before(v.ValidUntil) ||
			(v.Applies != Required && v.Applies != NotApplicable && v.Applies != NeedsAssessment) {
			return nil, fmt.Errorf("PEO-03: missing or stale policy evidence for %q", v.RequirementID)
		}
		if v.Applies == NotApplicable {
			out = append(out, RequirementDecision{ID: v.RequirementID, Blocked: false, Reason: "POLICY_NOT_APPLICABLE"})
			continue
		}
		if v.Applies == NeedsAssessment {
			out = append(out, RequirementDecision{ID: v.RequirementID, Blocked: true, Reason: "APPLICABILITY_UNRESOLVED"})
			continue
		}
		if v.EvidenceState == "APPROVED_EVIDENCE" && v.EvidenceReference != "" {
			out = append(out, RequirementDecision{ID: v.RequirementID, Blocked: false, Reason: "REQUIREMENT_SATISFIED"})
			continue
		}
		if v.Authority == "PLATFORM_DOCUMENTARY" && deferred.Status == "ACTIVE" {
			deferrable := false
			for _, id := range deferred.RequirementIDs {
				if id == v.RequirementID {
					deferrable = true
					break
				}
			}
			if deferrable && ValidateFoundingDeferral(deferred, classes, now) == nil {
				out = append(out, RequirementDecision{ID: v.RequirementID, Blocked: false, Reason: "PLATFORM_DOCUMENT_ONLY_DEFERRED"})
				continue
			}
		}
		out = append(out, RequirementDecision{ID: v.RequirementID, Blocked: true, Reason: "REQUIREMENT_OUTSTANDING"})
	}
	return out, nil
}
