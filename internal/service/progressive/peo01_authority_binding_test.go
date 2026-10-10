package progressive

import (
	"testing"
	"time"
)

// PEO-01: a policy assessment supplied by an applicant or stale integration
// cannot relabel a statutory/provider obligation as a deferred platform
// document, duplicate an answer or splice assessments from two contexts.
func TestPEO01RequirementAuthorityAndContextCannotBeOverridden(t *testing.T) {
	deferment, classes, now := sample()
	assessment := func(id, authority string) RequirementAssessment {
		return RequirementAssessment{
			RequirementID:    id,
			Authority:        authority,
			Applies:          Required,
			PolicyReference:  "policy/peo01/approved-v2",
			ContextReference: "trusted-tenant-context-za",
			EvaluatedAt:      now.Add(-time.Minute),
			ValidUntil:       now.Add(time.Minute),
		}
	}
	platform := assessment(classes[0].ID, "PLATFORM_DOCUMENTARY")
	provider := assessment(classes[1].ID, "PROVIDER")
	statutory := assessment(classes[2].ID, "STATUTORY")
	if _, err := EvaluateProgressiveEligibility([]RequirementAssessment{
		platform, provider, statutory}, deferment, now, classes); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func([]RequirementAssessment, []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement)
	}{
		{"provider masquerades as platform documentary", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			items[1].Authority = "PLATFORM_DOCUMENTARY"
			return items, class
		}},
		{"statutory masquerades as platform documentary", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			items[2].Authority = "PLATFORM_DOCUMENTARY"
			return items, class
		}},
		{"mixed tenant contexts", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			items[1].ContextReference = "other-tenant-foreign-context"
			return items, class
		}},
		{"duplicate per-requirement answer", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			return append(items, items[0]), class
		}},
		{"unknown requirement", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			items[2].RequirementID = "unknown.applicant.sponsored-item"
			return items, class
		}},
		{"duplicate independent classification", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			return items, append(class, class[0])
		}},
		{"missing source of approved evidence", func(items []RequirementAssessment, class []DocumentaryRequirement) ([]RequirementAssessment, []DocumentaryRequirement) {
			items[1].EvidenceState = "APPROVED_EVIDENCE"
			items[1].EvidenceReference = ""
			return items, class
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, catalogue := tc.mutate([]RequirementAssessment{platform, provider, statutory},
				append([]DocumentaryRequirement(nil), classes...))
			if _, err := EvaluateProgressiveEligibility(items, deferment, now, catalogue); err == nil {
				t.Fatal("an unbound or incorrectly classified policy assessment was accepted")
			}
		})
	}
}
