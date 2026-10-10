package postgres

import (
	"testing"
)

func TestFoundingLifecycleRejectsReinstatementAndUnreviewedTransitions(t *testing.T) {
	valid := FoundingLifecycleInput{
		Reason:            "Independent human reviewer withdrew the sponsorship authority",
		EvidenceReference: "review/peo02/revocation-001",
		ExpectedStatus:    "ACTIVE",
	}
	for _, tc := range []struct {
		kind, action string
		input        FoundingLifecycleInput
		allowed      bool
	}{
		{"SPONSORSHIP", "SUSPEND", valid, true},
		{"SPONSORSHIP", "REVOKE", valid, true},
		{"DOCUMENTARY_DEFERRAL", "REVOKE", valid, true},
		{"DOCUMENTARY_DEFERRAL", "SUSPEND", valid, false},
		{"SPONSORSHIP", "ACTIVATE", valid, false},
		{"SPONSORSHIP", "RENEW", valid, false},
		{"DOCUMENTARY_DEFERRAL", "FULFIL", valid, false},
		{"UNKNOWN", "REVOKE", valid, false},
		{"SPONSORSHIP", "REVOKE", FoundingLifecycleInput{
			Reason: "", EvidenceReference: valid.EvidenceReference, ExpectedStatus: "ACTIVE",
		}, false},
		{"SPONSORSHIP", "REVOKE", FoundingLifecycleInput{
			Reason: valid.Reason, EvidenceReference: "", ExpectedStatus: "ACTIVE",
		}, false},
		{"SPONSORSHIP", "REVOKE", FoundingLifecycleInput{
			Reason: valid.Reason, EvidenceReference: valid.EvidenceReference, ExpectedStatus: "REVOKED",
		}, false},
		{"SPONSORSHIP", "SUSPEND", FoundingLifecycleInput{
			Reason: valid.Reason, EvidenceReference: valid.EvidenceReference, ExpectedStatus: "SUSPENDED",
		}, false},
		{"SPONSORSHIP", "REVOKE", FoundingLifecycleInput{
			Reason: valid.Reason, EvidenceReference: valid.EvidenceReference, ExpectedStatus: "SUSPENDED",
		}, true},
	} {
		got := validateFoundingLifecycle(tc.kind, tc.action, tc.input) == nil
		if got != tc.allowed {
			t.Errorf("%s/%s expected=%s allowed=%v got=%v", tc.kind, tc.action, tc.input.ExpectedStatus, tc.allowed, got)
		}
	}
}
