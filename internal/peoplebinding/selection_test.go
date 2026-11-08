package peoplebinding

import (
    "errors"
    "testing"
    "time"
    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/repository"
)

func TestProviderSelectionScopeFences(t *testing.T) {
    now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
    const uuid = "12345678-1234-1234-1234-123456789012"
    ctx := domain.Context{ID:"ctx_test", TenantID:"tn_test", LegalEntityID:"TEST-EMPLOYER", Jurisdiction:"ZA"}
    choice := Choice{TenantID:"tn_test", LegalEmployerID:"TEST-EMPLOYER", Jurisdiction:"ZA",
        CapabilityKey:"payroll.run.read", BindingID:uuid, ProviderID:uuid, BindingRevision:1,
        SourceOfRecord:"EXTERNAL", EffectiveFrom:now.Add(-time.Hour), CertificationKey:"cert_test"}
    r := repository.CapabilityResolutionRecord{
        ResolutionID:"res_test", ContextID:"ctx_test", TenantID:"tn_test",
        CapabilityKey:"payroll.run.read", BindingID:uuid, ProviderID:uuid,
        ContractVersion:1, Decision:"RESOLVED",
    }
    valid, err := Build(ctx, choice, r, now)
    if err != nil || valid.Selection.SourceOfRecord != "EXTERNAL" || valid.Selection.LegalEmployerID != "TEST-EMPLOYER" ||
        valid.Selection.ExpiresAt.Sub(now) > 30*time.Second || !valid.PortabilityEnabled { t.Fatalf("expected qualified exact selection: %+v %v", valid,err) }
    tests := map[string]func(*domain.Context,*Choice,*repository.CapabilityResolutionRecord){
        "tenant":func(c *domain.Context, _ *Choice, _ *repository.CapabilityResolutionRecord){c.TenantID="tn_other"},
        "employer":func(c *domain.Context, _ *Choice, _ *repository.CapabilityResolutionRecord){c.LegalEntityID="OTHER-EMPLOYER"},
        "jurisdiction":func(c *domain.Context, _ *Choice, _ *repository.CapabilityResolutionRecord){c.Jurisdiction="UG"},
        "missing-country":func(c *domain.Context, _ *Choice, _ *repository.CapabilityResolutionRecord){c.Jurisdiction=""},
        "provider":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){c.ProviderID="98765432-1234-1234-1234-123456789012"},
        "binding":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){c.BindingID="98765432-1234-1234-1234-123456789012"},
        "unqualified":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){c.CertificationKey=""},
        "fallback":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){c.SourceOfRecord="AUTO_FALLBACK"},
        "grant":func(_ *domain.Context, _ *Choice, r *repository.CapabilityResolutionRecord){r.Decision="DENIED"},
        "wrong-capability":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){c.CapabilityKey="payroll.run.manage"},
        "wrong-version":func(_ *domain.Context, _ *Choice, r *repository.CapabilityResolutionRecord){r.ContractVersion=2},
        "wrong-result-binding":func(_ *domain.Context, _ *Choice, r *repository.CapabilityResolutionRecord){r.BindingID="98765432-1234-1234-1234-123456789012"},
        "expired":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){past:=now.Add(-time.Second);c.EffectiveUntil=&past},
        "revoked-cert":func(_ *domain.Context, c *Choice, _ *repository.CapabilityResolutionRecord){past:=now.Add(-time.Second);c.CertificationUntil=&past},
        "provisioning-context":func(c *domain.Context, _ *Choice, _ *repository.CapabilityResolutionRecord){c.AuthorityPurpose=domain.ContextPurposeTenantProvisioning},
    }
    for name, change := range tests {
        t.Run(name,func(t *testing.T){c,v,rec:=ctx,choice,r;change(&c,&v,&rec);if _,err:=Build(c,v,rec,now);!errors.Is(err,ErrDenied){t.Fatalf("must refuse: %v",err)}})
    }
}
