package organisation

import (
	"context"
	"errors"
	"time"
)

var ErrFoundingAcceptance = errors.New("founding onboarding authority correlation denied")

// FoundingEvidenceAssessment is the PEO-04A read-only evidence provider.
// Production composition MUST use FoundingEvidenceReconciler backed by the
// live OEV-03 and independent signed-control proof repositories.
type FoundingEvidenceAssessment interface {
	Reconcile(context.Context, FoundingIdentityInput, FoundingIdentityInput) (FoundingCompanyReconciliation, error)
}

type FoundingAcceptanceEntry struct {
	FirstPartyID   string   `json:"first_party_id"`
	OrganisationID string   `json:"organisation_id,omitempty"`
	TenantID       string   `json:"tenant_id,omitempty"`
	Readiness      string   `json:"readiness"`
	Missing        []string `json:"missing,omitempty"`
}
type FoundingAcceptanceReport struct {
	RegistryDigest              string                    `json:"registry_digest"`
	AssessedAt                  time.Time                 `json:"assessed_at"`
	IndependentControlVerified  bool                      `json:"independent_control_verified"`
	GroupIdentityAndTenantReady bool                      `json:"group_identity_and_tenant_ready"`
	Entries                     []FoundingAcceptanceEntry `json:"entries"`
}

// FoundingAcceptanceGate joins the four FIRST-PARTY tenant qualification
// results with separate official-source registration proof. This report never
// writes tenants, incorporation, group control, IAM grants or product rights.
// The independently proven parent/child control remains a *distinct* result.
type FoundingAcceptanceGate struct {
	Snapshot FoundingSnapshotReader
	Evidence FoundingEvidenceAssessment
	Now      func() time.Time
}

var requiredFounders = map[string]struct{ class, claim string }{
	"NABHOLD":        {"LEGAL_PERSON", "REGISTERED_EVIDENCED"},
	"THAMANI-GLOBAL": {"LEGAL_PERSON", "REGISTERED_EVIDENCED"},
	"ZURIBEANS":      {"OPERATING_BUSINESS", "NOT_INCORPORATED"},
	"EQUATOR-ESTATE": {"OPERATING_BUSINESS", "NOT_INCORPORATED"},
}

func (g FoundingAcceptanceGate) Assess(ctx context.Context, reg FirstPartyRegistry,
	parent, child FoundingIdentityInput) (FoundingAcceptanceReport, error) {
	var empty FoundingAcceptanceReport
	if ctx == nil || ctx.Err() != nil || g.Snapshot == nil || g.Evidence == nil || g.Now == nil ||
		len(reg.Entities) != 4 || reg.Digest == "" {
		return empty, ErrFoundingAcceptance
	}
	seen := map[string]bool{}
	for _, entry := range reg.Entities {
		declared, ok := requiredFounders[entry.ID]
		if !ok || seen[entry.ID] || entry.IdentityClass != declared.class ||
			entry.IncorporationClaim != declared.claim {
			return empty, ErrFoundingAcceptance
		}
		seen[entry.ID] = true
	}
	if len(seen) != 4 || parent.RegistryID != "NABHOLD" || child.RegistryID != "THAMANI-GLOBAL" ||
		parent.VerificationCaseID == "" || child.VerificationCaseID == "" ||
		parent.DeclaredRegistrationNumber == "" || child.DeclaredRegistrationNumber == "" {
		return empty, ErrFoundingAcceptance
	}
	now := g.Now().UTC()
	roster, err := QualifyFoundingRoster(ctx, reg, g.Snapshot, now)
	if err != nil {
		return empty, err
	}
	mapped := map[string]FoundingQualification{}
	for _, entry := range roster {
		mapped[entry.FirstPartyID] = entry
	}
	if mapped["NABHOLD"].OrganisationID == "" || mapped["THAMANI-GLOBAL"].OrganisationID == "" ||
		mapped["NABHOLD"].OrganisationID != parent.OrganisationID ||
		mapped["THAMANI-GLOBAL"].OrganisationID != child.OrganisationID {
		return empty, ErrFoundingAcceptance
	}
	result, err := g.Evidence.Reconcile(ctx, parent, child)
	if err != nil {
		return empty, err
	}
	if result.AssessedAt.IsZero() || result.AssessedAt.After(now.Add(time.Minute)) {
		return empty, ErrFoundingAcceptance
	}
	out := FoundingAcceptanceReport{RegistryDigest: reg.Digest, AssessedAt: now,
		IndependentControlVerified: result.ControlVerified, GroupIdentityAndTenantReady: true,
		Entries: make([]FoundingAcceptanceEntry, 0, 4)}
	for _, e := range roster {
		item := FoundingAcceptanceEntry{FirstPartyID: e.FirstPartyID,
			OrganisationID: e.OrganisationID, TenantID: e.TenantID, Readiness: "PENDING",
			Missing: append([]string{}, e.Missing...)}
		if e.Readiness != "IDENTITY_AND_TENANT_GATES_SATISFIED_NOT_PRODUCTION" {
			if len(item.Missing) == 0 {
				item.Missing = append(item.Missing, "CURRENT_TENANT_ADMISSION_GATES")
			}
		}
		switch e.FirstPartyID {
		case "NABHOLD":
			if !result.NabholdRegistrationVerified || result.NabholdRegistrationEvidence == "" {
				item.Missing = append(item.Missing, "INDEPENDENT_NABHOLD_CIPC_OEV03")
			}
		case "THAMANI-GLOBAL":
			if !result.ThamaniRegistrationVerified || result.ThamaniRegistrationEvidence == "" {
				item.Missing = append(item.Missing, "INDEPENDENT_THAMANI_CIPC_OEV03")
			}
		}
		if len(item.Missing) == 0 {
			item.Readiness = "IDENTITY_TENANT_ACCEPTED_NOT_PRODUCTION"
		} else {
			out.GroupIdentityAndTenantReady = false
		}
		out.Entries = append(out.Entries, item)
	}
	return out, nil
}
