package subscription

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestCommandsFollowTheirSharedSchema: classification and reclassification
// requests are validated against Shared product/v1
// SubscriptionClassificationCommand and SubscriptionReclassificationCommand
// before any rule of the Classifier's own.
func TestCommandsFollowTheirSharedSchema(t *testing.T) {
	body := func(v map[string]any) []byte {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	// 2000 characters, 6000 bytes: the limit counts characters, as the schema's maxLength does.
	longReason := strings.Repeat("€", 2000)
	for _, tc := range []struct {
		name  string
		raw   []byte
		valid bool
		re    bool
	}{
		{"classification", body(map[string]any{"admission_decision_id": "adm_01k4", "reason": "Approved at admission."}), true, false},
		{"2000-character reason", body(map[string]any{"admission_decision_id": "adm_01k4", "reason": longReason}), true, false},
		{"2001-character reason", body(map[string]any{"admission_decision_id": "adm_01k4", "reason": longReason + "€"}), false, false},
		{"blank reason", body(map[string]any{"admission_decision_id": "adm_01k4", "reason": "   "}), false, false},
		{"not a decision id", body(map[string]any{"admission_decision_id": "decision-1", "reason": "x"}), false, false},
		{"a caller-supplied type", body(map[string]any{"admission_decision_id": "adm_01k4", "reason": "x", "subscription_type": "INTERNAL"}), false, false},
		{"reclassification", body(map[string]any{"subscription_type": "COMMERCIAL", "classification_reference": "chg_divested", "reason": "Divested."}), true, true},
		{"unknown type", body(map[string]any{"subscription_type": "INTERNAL_GROUP", "classification_reference": "chg_divested", "reason": "x"}), false, true},
		{"short reference", body(map[string]any{"subscription_type": "COMMERCIAL", "classification_reference": "ab", "reason": "x"}), false, true},
		{"smuggled eligibility", body(map[string]any{"subscription_type": "INTERNAL", "classification_reference": "chg_x1", "reason": "x",
			"internal_eligibility": map[string]any{"organisation_id": "org_1"}}), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.re {
				var req ReclassifyRequest
				err = decodeCommand(reclassifySchema, tc.raw, &req)
				if err == nil {
					err = errorsOf(checkReason(req.Reason))
				}
			} else {
				var req ClassifyRequest
				err = decodeCommand(classifySchema, tc.raw, &req)
				if err == nil {
					err = errorsOf(checkReason(req.Reason))
				}
			}
			var invalid *InvalidError
			switch {
			case tc.valid && err != nil:
				t.Fatalf("refused a valid command: %v", err)
			case !tc.valid && !errors.As(err, &invalid):
				t.Fatalf("accepted an invalid command (err %v)", err)
			}
		})
	}
}

func errorsOf(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return &InvalidError{Problems: problems}
}
