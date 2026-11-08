// Package peoplebinding applies the CP-owned, provider-neutral People v1 authority
// fence (ADR-SHARED-045). A Shared schema is not a payroll qualification.
package peoplebinding

import (
    "errors"
    "regexp"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/repository"
)

var ErrDenied = errors.New("People provider selection denied")
var country = regexp.MustCompile(`^[A-Z]{2}$`)

// Supported is the exact accepted Shared People v1 capability census.
// A later People capability must be explicitly accepted and added here.
func Supported(key string) bool {
    switch key {
    case "payroll.evidence.attest", "payroll.run.read", "workforce.employment.manage", "workforce.employment.read":
        return true
    }
    return false
}

// Choice is a CP persisted, independently reviewed tenant/employer/function
// decision. It is NEVER constructed from browser/provider-supplied fields.
type Choice struct {
    TenantID, LegalEmployerID, Jurisdiction, CapabilityKey string
    BindingID, ProviderID string // raw postgres UUIDs
    BindingRevision int64
    SourceOfRecord string
    EffectiveFrom time.Time
    EffectiveUntil *time.Time
    CertificationKey string
    CertificationUntil *time.Time
}

// Selection is the canonical contracts/people/v1/domain.schema.json readback.
type Selection struct {
    TenantID string `json:"tenant_id"`
    LegalEmployerID string `json:"legal_employer_id"`
    CapabilityKey string `json:"capability_key"`
    BindingID string `json:"binding_id"`
    ProviderID string `json:"provider_id"`
    BindingRevision int64 `json:"binding_revision"`
    Jurisdiction string `json:"jurisdiction"`
    SourceOfRecord string `json:"source_of_record"`
    EffectiveFrom time.Time `json:"effective_from"`
    EffectiveUntil *time.Time `json:"effective_until,omitempty"`
    VerifiedAt time.Time `json:"verified_at"`
    ExpiresAt time.Time `json:"expires_at"`
    QualificationReceiptRef string `json:"qualification_receipt_ref"`
}

type Decision struct {
    Selection Selection `json:"selection"`
    DecisionRef string `json:"decision_ref"`
    Revision int64 `json:"revision"`
    Status string `json:"status"`
    PortabilityEnabled bool `json:"portability_enabled"`
}

// Build authorises a current CP provider choice only if the genuine persisted
// capability resolution names exactly the same binding and provider. It does
// not invent fallback/defaults, and truncates the lease to 30 seconds.
func Build(trusted domain.Context, choice Choice, record repository.CapabilityResolutionRecord, now time.Time) (Decision, error) {
    deny := func() (Decision, error) { return Decision{}, ErrDenied }
    now = now.UTC()
    if !Supported(choice.CapabilityKey) || !country.MatchString(trusted.Jurisdiction) ||
        trusted.TenantID == "" || trusted.LegalEntityID == "" ||
        trusted.AuthorityPurpose != "" && trusted.AuthorityPurpose != domain.ContextPurposeRuntime ||
        trusted.TenantID != choice.TenantID || trusted.LegalEntityID != choice.LegalEmployerID ||
        trusted.Jurisdiction != choice.Jurisdiction || choice.SourceOfRecord != "NATIVE" && choice.SourceOfRecord != "EXTERNAL" ||
        choice.BindingRevision < 1 || choice.BindingID == "" || choice.ProviderID == "" ||
        choice.CertificationKey == "" || !now.Before(now.Add(30*time.Second)) ||
        now.Before(choice.EffectiveFrom) || choice.EffectiveUntil != nil && !now.Before(*choice.EffectiveUntil) ||
        choice.CertificationUntil != nil && !now.Before(*choice.CertificationUntil) ||
        record.Decision != "RESOLVED" || record.ContextID != trusted.ID ||
        record.TenantID != trusted.TenantID || record.CapabilityKey != choice.CapabilityKey ||
        record.ContractVersion != 1 || record.BindingID != choice.BindingID ||
        record.ProviderID != choice.ProviderID || record.ResolutionID == "" {
        return deny()
    }
    expires := now.Add(30*time.Second)
    for _, until := range []*time.Time{trusted.ExpiresAt, choice.EffectiveUntil, choice.CertificationUntil, record.ExpiresAt} {
        if until != nil && until.Before(expires) { expires = *until }
    }
    if !now.Before(expires) { return deny() }
    binding, err := domain.FormatResourceID("bind", choice.BindingID)
    if err != nil { return deny() }
    provider, err := domain.FormatResourceID("provider", choice.ProviderID)
    if err != nil { return deny() }
    return Decision{
        Selection: Selection{
            TenantID: trusted.TenantID, LegalEmployerID: trusted.LegalEntityID,
            CapabilityKey: choice.CapabilityKey, BindingID: binding, ProviderID: provider,
            BindingRevision: choice.BindingRevision, Jurisdiction: trusted.Jurisdiction,
            SourceOfRecord: choice.SourceOfRecord, EffectiveFrom: choice.EffectiveFrom,
            EffectiveUntil: choice.EffectiveUntil, VerifiedAt: now, ExpiresAt: expires,
            QualificationReceiptRef: choice.CertificationKey,
        },
        DecisionRef: record.ResolutionID, Revision: choice.BindingRevision,
        Status: "ACTIVE", PortabilityEnabled: true,
    }, nil
}
