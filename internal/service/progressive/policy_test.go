package progressive

import (
	"errors"
	"testing"
	"time"
)

func when(value string) time.Time { return time.Date(2026, 1, 31, 10, 15, 30, 0, time.UTC) }

func TestCalendarAnniversaryClampsMonthEndInUTC(t *testing.T) {
	cases := []struct {
		start  string
		months int
		want   string
	}{
		{"2024-02-29T15:04:05Z", 24, "2026-02-28T15:04:05Z"},
		{"2026-01-31T10:15:30Z", 1, "2026-02-28T10:15:30Z"},
		{"2023-01-31T10:15:30Z", 13, "2024-02-29T10:15:30Z"},
		{"2026-01-31T10:15:30Z", 24, "2028-01-31T10:15:30Z"},
	}
	for _, v := range cases {
		start, _ := time.Parse(time.RFC3339, v.start)
		got := CalendarAnniversaryUTC(start, v.months).Format(time.RFC3339)
		if got != v.want {
			t.Errorf("%s+%d: got %s, want %s", v.start, v.months, got, v.want)
		}
	}
}
func sample() (FoundingDeferral, []DocumentaryRequirement, time.Time) {
	first := time.Date(2026, 2, 28, 10, 0, 0, 0, time.UTC)
	req := []DocumentaryRequirement{
		{ID: "platform.founding.trading-name-proof", Authority: "PLATFORM_DOCUMENTARY"},
		{ID: "provider.merchant.kyc", Authority: "PROVIDER"},
		{ID: "statutory.market-licence", Authority: "STATUTORY"},
	}
	d := FoundingDeferral{
		OrganisationID: "org-genuine", SponsorshipID: "independent-sponsorship", SponsorOrganisationID: "sponsor",
		SponsorStatus: "ACTIVE", SponsorScope: "INTERNAL_GROUP_ADMISSION",
		MakerID: "human-maker", CheckerID: "human-checker",
		PolicyReference: "policy/peo02/v2", ApprovalReference: "review/independent/1",
		ProvisionalApprovalAt: first, ApprovedAt: first.Add(time.Hour),
		EffectiveFrom: first, ExpiresAt: CalendarAnniversaryUTC(first, 24),
		Status: "ACTIVE", RequirementIDs: []string{req[0].ID},
		MaximumDurationMonths: 24,
	}
	return d, req, first.Add(400 * 24 * time.Hour)
}
func TestPEO02OneTimeBoundedDocumentaryGrace(t *testing.T) {
	d, requirements, now := sample()
	if err := ValidateFoundingDeferral(d, requirements, now); err != nil {
		t.Fatal(err)
	}
	copies := []struct {
		name   string
		mutate func(*FoundingDeferral)
	}{
		{"different applicant checker", func(x *FoundingDeferral) { x.CheckerID = x.MakerID }},
		{"revoked sponsor", func(x *FoundingDeferral) { x.SponsorStatus = "REVOKED" }},
		{"unapproved scope", func(x *FoundingDeferral) { x.SponsorScope = "INTERNAL" }},
		{"wrong expiry", func(x *FoundingDeferral) { x.ExpiresAt = x.ExpiresAt.Add(24 * time.Hour) }},
		{"incorrect start reapply reset", func(x *FoundingDeferral) { x.EffectiveFrom = x.EffectiveFrom.Add(24 * time.Hour) }},
		{"wider than 24 months", func(x *FoundingDeferral) { x.MaximumDurationMonths = 36 }},
		{"provider evidence waived", func(x *FoundingDeferral) { x.RequirementIDs = []string{"provider.merchant.kyc"} }},
		{"statutory evidence waived", func(x *FoundingDeferral) { x.RequirementIDs = []string{"statutory.market-licence"} }},
		{"duplicate evidence", func(x *FoundingDeferral) { x.RequirementIDs = []string{requirements[0].ID, requirements[0].ID} }},
	}
	for _, v := range copies {
		t.Run(v.name, func(t *testing.T) {
			x := d
			v.mutate(&x)
			if !errors.Is(ValidateFoundingDeferral(x, requirements, now), ErrDeferralDenied) {
				t.Fatal("expected denial")
			}
		})
	}
	for _, at := range []time.Time{d.ExpiresAt, d.ExpiresAt.Add(time.Nanosecond)} {
		if !errors.Is(ValidateFoundingDeferral(d, requirements, at), ErrDeferralDenied) {
			t.Fatal("grace never survives month 24")
		}
	}
	month12 := d.EffectiveFrom.AddDate(1, 0, 0)
	if err := ValidateFoundingDeferral(d, requirements, month12); err != nil {
		t.Fatalf("month 12 is not expiry: %v", err)
	}
}

func TestPEO03ApplicabilityIsNotUniversalTaxOrLegalStatus(t *testing.T) {
	d, classes, now := sample()
	examples := []RequirementAssessment{
		{RequirementID: "platform.founding.trading-name-proof", Authority: "PLATFORM_DOCUMENTARY", Applies: Required},
		{RequirementID: "provider.merchant.kyc", Authority: "PROVIDER", Applies: Required},
		{RequirementID: "statutory.market-licence", Authority: "STATUTORY", Applies: NotApplicable},
	}
	for i := range examples {
		examples[i].PolicyReference = "regulations/approved-policy/reference"
		examples[i].ContextReference = "canonical-current-context"
		examples[i].EvaluatedAt = now.Add(-time.Minute)
		examples[i].ValidUntil = now.Add(time.Minute)
	}
	result, err := EvaluateProgressiveEligibility(examples, d, now, classes)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 || result[0].Reason != "PLATFORM_DOCUMENT_ONLY_DEFERRED" ||
		!result[1].Blocked || result[1].Reason != "REQUIREMENT_OUTSTANDING" ||
		result[2].Blocked || result[2].Reason != "POLICY_NOT_APPLICABLE" {
		t.Fatalf("%+v", result)
	}
	examples[0].Applies = NeedsAssessment
	result, err = EvaluateProgressiveEligibility(examples, d, now, classes)
	if err != nil || !result[0].Blocked {
		t.Fatalf("unassessed applicability should deny: %+v, %v", result, err)
	}
	examples[0].ValidUntil = now
	if _, err := EvaluateProgressiveEligibility(examples, d, now, classes); err == nil {
		t.Fatal("stale policy decision should deny")
	}
}
