package peoplebinding

import (
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracts"
	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestProviderSelectionScopeFences(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	const uuid = "12345678-1234-1234-1234-123456789012"
	ctx := domain.Context{ID: "ctx_test", TenantID: "tn_test", LegalEntityID: "TEST-EMPLOYER", Jurisdiction: "ZA", AuthorityPurpose: domain.ContextPurposeRuntime}
	choice := Choice{TenantID: "tn_test", LegalEmployerID: "TEST-EMPLOYER", Jurisdiction: "ZA",
		CapabilityKey: "payroll.run.read", BindingID: uuid, ProviderID: uuid, BindingRevision: 1,
		SourceOfRecord: "EXTERNAL", EffectiveFrom: now.Add(-time.Hour), CertificationKey: "cert_test"}
	r := Resolution{
		ResolutionID: "res_test", ContextID: "ctx_test", TenantID: "tn_test",
		CapabilityKey: "payroll.run.read", BindingID: uuid, ProviderID: uuid,
		ContractVersion: 1, Decision: "RESOLVED",
	}
	valid, err := Build(ctx, choice, r, now)
	if err != nil || valid.Selection.SourceOfRecord != "EXTERNAL" || valid.Selection.LegalEmployerID != "TEST-EMPLOYER" ||
		valid.Selection.ExpiresAt.Sub(now) > 30*time.Second || !valid.PortabilityEnabled {
		t.Fatalf("expected qualified exact selection: %+v %v", valid, err)
	}
	// Schema validation is against the immutable, embedded accepted Shared
	// #436 People contract, not a hand-authored mirror in this test.
	selectionSchema := contracts.MustSchema("people/v1/provider-selection.schema.json")
	if err := contracts.ValidateValue(selectionSchema, valid); err != nil {
		t.Fatalf("CP positive choice violates accepted People v1: %v", err)
	}
	tests := map[string]func(*domain.Context, *Choice, *Resolution){
		"tenant":          func(c *domain.Context, _ *Choice, _ *Resolution) { c.TenantID = "tn_other" },
		"employer":        func(c *domain.Context, _ *Choice, _ *Resolution) { c.LegalEntityID = "OTHER-EMPLOYER" },
		"jurisdiction":    func(c *domain.Context, _ *Choice, _ *Resolution) { c.Jurisdiction = "UG" },
		"missing-country": func(c *domain.Context, _ *Choice, _ *Resolution) { c.Jurisdiction = "" },
		"provider": func(_ *domain.Context, c *Choice, _ *Resolution) {
			c.ProviderID = "98765432-1234-1234-1234-123456789012"
		},
		"binding": func(_ *domain.Context, c *Choice, _ *Resolution) {
			c.BindingID = "98765432-1234-1234-1234-123456789012"
		},
		"unqualified":      func(_ *domain.Context, c *Choice, _ *Resolution) { c.CertificationKey = "" },
		"fallback":         func(_ *domain.Context, c *Choice, _ *Resolution) { c.SourceOfRecord = "AUTO_FALLBACK" },
		"grant":            func(_ *domain.Context, _ *Choice, r *Resolution) { r.Decision = "DENIED" },
		"wrong-capability": func(_ *domain.Context, c *Choice, _ *Resolution) { c.CapabilityKey = "payroll.run.manage" },
		"wrong-version":    func(_ *domain.Context, _ *Choice, r *Resolution) { r.ContractVersion = 2 },
		"wrong-result-binding": func(_ *domain.Context, _ *Choice, r *Resolution) {
			r.BindingID = "98765432-1234-1234-1234-123456789012"
		},
		"expired": func(_ *domain.Context, c *Choice, _ *Resolution) {
			past := now.Add(-time.Second)
			c.EffectiveUntil = &past
		},
		"revoked-cert": func(_ *domain.Context, c *Choice, _ *Resolution) {
			past := now.Add(-time.Second)
			c.CertificationUntil = &past
		},
		"missing-runtime-purpose": func(c *domain.Context, _ *Choice, _ *Resolution) { c.AuthorityPurpose = "" },
		"provisioning-context": func(c *domain.Context, _ *Choice, _ *Resolution) {
			c.AuthorityPurpose = domain.ContextPurposeTenantProvisioning
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			c, v, rec := ctx, choice, r
			change(&c, &v, &rec)
			if _, err := Build(c, v, rec, now); !errors.Is(err, ErrDenied) {
				t.Fatalf("must refuse: %v", err)
			}
		})
	}
}

// Regression: an old selection must not be adopted after a replacement or
// revision that happens to choose the same provider and binding tuple.
func TestSameChoiceFencesReplacementRevisionAndCertification(t *testing.T) {
	base := Choice{ChoiceID: "11111111-1111-4111-8111-111111111111", ChoiceVersion: 1,
		CertificationID: "22222222-2222-4222-8222-222222222222",
		TenantID:        "tn_example", LegalEmployerID: "TEST-EMPLOYER", Jurisdiction: "ZA",
		CapabilityKey: "payroll.run.read", BindingID: "33333333-3333-4333-8333-333333333333",
		ProviderID: "44444444-4444-4444-8444-444444444444", BindingRevision: 1,
		SourceOfRecord: "EXTERNAL", EffectiveFrom: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
		CertificationKey: "cert_example"}
	if !SameChoice(base, base) {
		t.Fatal("identical persisted choice should match")
	}
	for name, mutate := range map[string]func(*Choice){
		"replacement row":         func(c *Choice) { c.ChoiceID = "55555555-5555-4555-8555-555555555555" },
		"updated revision":        func(c *Choice) { c.ChoiceVersion++ },
		"new certification":       func(c *Choice) { c.CertificationID = "66666666-6666-4666-8666-666666666666" },
		"missing record identity": func(c *Choice) { c.ChoiceID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if SameChoice(base, changed) || SameChoice(changed, base) {
				t.Fatal("stale or replaced persisted choice was adopted")
			}
		})
	}
}
