package organisation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// PEO-05A correlates existing, separately authoritative gates. It creates no
// tenant, IAM membership, provider binding, legal-person claim or entitlement.
var ErrPEO05Integrity = errors.New("PEO-05 founding tenant operational evidence integrity denied")

const (
	PEO05Blocked          = "BLOCKED"
	PEO05StagingCandidate = "READY_FOR_PROTECTED_STAGING_VALIDATION_NOT_PRODUCTION"
)

// PEO05TenantReader MUST be wired exclusively to trusted CP/IAM/provider
// readers; never to JSON supplied by an applicant, browser or tenant admin.
// Missing or unavailable upstream authority is a denial, not implicit READY.
type PEO05TenantReader interface {
	ReadPEO05Tenant(context.Context, string, string, time.Time) (PEO05TenantEvidence, error)
}

type PEO05IAMProof struct {
	TenantID            string
	OrganisationID      string
	PrincipalID         string
	IssuerID            string
	Audience            string
	MembershipStatus    string
	EvidenceReference   string
	RevocationCheckedAt time.Time
	ExpiresAt           time.Time
}

// PEO05ProviderProof is a current, tenant-bound certification plus observed
// health for one MANDATORY capability, not a promise that an engine exists.
type PEO05ProviderProof struct {
	CapabilityKey          string
	TenantID               string
	EngineInstanceID       string
	ProviderKey            string
	ResidencyRegion        string
	SupportStatus          string
	CertificationReference string
	CertificationExpiresAt time.Time
	HealthStatus           string
	HealthObservedAt       time.Time
	HealthExpiresAt        time.Time
}

// A CP-owned snapshot is independently joined with IAM/provider evidence.
// The reader must derive RequiredCapabilities from the *current* canonical
// product composition and must verify the latest approved plan is still current.
type PEO05TenantEvidence struct {
	TenantID               string
	OrganisationID         string
	AdmissionDecisionID    string
	OnboardingRequestID    string
	RequesterID            string
	AuthoriserID           string
	ProvisioningState      string
	DesiredStateDigest     string
	PlanDesiredStateDigest string
	PlanDigest             string
	ApprovedPlanDigest     string
	PlanApprovalID         string
	PlanApproverID         string
	PlanExpiresAt          time.Time
	ResidencyRequirement   string
	SubscriptionID         string
	SubscriptionTenantID   string
	SubscriptionStatus     string
	RequiredCapabilities   []string
	Providers              []PEO05ProviderProof
	IAM                    PEO05IAMProof
}

type PEO05Entry struct {
	FirstPartyID   string
	OrganisationID string
	TenantID       string
	Status         string
	Missing        []string
}

type PEO05Report struct {
	RegistryDigest       string
	AssessedAt           time.Time
	EvidenceClass        string
	AllStagingCandidates bool
	ProductionEligible   bool
	Entries              []PEO05Entry
}

// PEO05Gate is the PEO-04C -> PEO-05A gate. PEO-04C must execute within
// Assess, so callers cannot supply a forged "accepted" roster.
type PEO05Gate struct {
	Founding FoundingAcceptanceGate
	Tenants  PEO05TenantReader
	Now      func() time.Time
}

func (g PEO05Gate) Assess(ctx context.Context, reg FirstPartyRegistry,
	parent, child FoundingIdentityInput) (PEO05Report, error) {
	var empty PEO05Report
	if ctx == nil || ctx.Err() != nil || g.Tenants == nil || g.Now == nil {
		return empty, ErrPEO05Integrity
	}
	at := g.Now().UTC()
	if at.IsZero() {
		return empty, ErrPEO05Integrity
	}
	founding, err := g.Founding.Assess(ctx, reg, parent, child)
	if err != nil {
		return empty, err
	}
	if founding.RegistryDigest != reg.Digest || founding.AssessedAt.IsZero() ||
		founding.AssessedAt.After(at.Add(time.Second)) ||
		at.Sub(founding.AssessedAt) > 30*time.Second ||
		len(founding.Entries) != 4 {
		return empty, ErrPEO05Integrity
	}
	report := PEO05Report{RegistryDigest: reg.Digest, AssessedAt: at,
		EvidenceClass: "READ_ONLY_NON_AUTHORISING", AllStagingCandidates: true,
		ProductionEligible: false, Entries: make([]PEO05Entry, 0, len(founding.Entries))}
	seen := map[string]bool{}
	seenTenants := map[string]bool{}
	for _, entry := range founding.Entries {
		if seen[entry.FirstPartyID] || entry.FirstPartyID == "" {
			return empty, ErrPEO05Integrity
		}
		seen[entry.FirstPartyID] = true
		row := PEO05Entry{FirstPartyID: entry.FirstPartyID,
			OrganisationID: entry.OrganisationID, TenantID: entry.TenantID,
			Status: PEO05Blocked, Missing: append([]string{}, entry.Missing...)}
		if entry.Readiness != "IDENTITY_TENANT_ACCEPTED_NOT_PRODUCTION" {
			row.Missing = addPEO05Missing(row.Missing, "PEO04_CURRENT_IDENTITY_TENANT_EVIDENCE")
		} else if entry.OrganisationID == "" || entry.TenantID == "" || seenTenants[entry.TenantID] {
			return empty, ErrPEO05Integrity
		} else {
			seenTenants[entry.TenantID] = true
			snapshot, readErr := g.Tenants.ReadPEO05Tenant(ctx, entry.TenantID, entry.OrganisationID, at)
			if readErr != nil {
				row.Missing = addPEO05Missing(row.Missing, "UPSTREAM_AUTHORITY_UNAVAILABLE")
			} else {
				row.Missing = append(row.Missing, assessPEO05Tenant(snapshot, entry, at)...)
			}
		}
		if len(row.Missing) == 0 {
			row.Status = PEO05StagingCandidate
		} else {
			report.AllStagingCandidates = false
		}
		report.Entries = append(report.Entries, row)
	}
	if len(seen) != 4 {
		return empty, ErrPEO05Integrity
	}
	return report, nil
}

func assessPEO05Tenant(p PEO05TenantEvidence, entry FoundingAcceptanceEntry, at time.Time) []string {
	missing := []string{}
	add := func(condition bool, reason string) {
		if !condition {
			missing = append(missing, reason)
		}
	}
	add(p.TenantID == entry.TenantID && p.OrganisationID == entry.OrganisationID,
		"CP_TENANT_OR_ORGANISATION_MISMATCH")
	add(p.AdmissionDecisionID != "" && p.OnboardingRequestID != "" &&
		p.RequesterID != "" && p.AuthoriserID != "" && p.RequesterID != p.AuthoriserID,
		"AUTHORISED_ONBOARDING_PROVENANCE")
	add(p.ProvisioningState == "READY", "CANONICAL_PROVISIONING_NOT_READY")
	add(validPEO05Digest(p.DesiredStateDigest) &&
		p.DesiredStateDigest == p.PlanDesiredStateDigest, "FROZEN_DESIRED_STATE_NOT_CURRENT")
	add(validPEO05Digest(p.PlanDigest) && p.PlanDigest == p.ApprovedPlanDigest &&
		p.PlanApprovalID != "" && p.PlanApproverID != "" &&
		p.PlanApproverID != p.RequesterID && p.PlanApproverID != p.AuthoriserID &&
		p.PlanExpiresAt.After(at), "CURRENT_INDEPENDENT_PLAN_APPROVAL")
	add(p.ResidencyRequirement != "", "RESIDENCY_NOT_DECLARED")
	add(p.SubscriptionID != "" && p.SubscriptionTenantID == entry.TenantID &&
		p.SubscriptionStatus == "ACTIVE", "CURRENT_TENANT_SUBSCRIPTION")

	iam := p.IAM
	add(iam.TenantID == entry.TenantID && iam.OrganisationID == entry.OrganisationID &&
		iam.PrincipalID != "" && iam.IssuerID != "" &&
		iam.Audience == "baobab-control-plane" &&
		iam.MembershipStatus == "ACTIVE" && iam.EvidenceReference != "" &&
		currentPEO05Observation(iam.RevocationCheckedAt, iam.ExpiresAt, at),
		"CURRENT_IAM_TENANT_MEMBERSHIP_AND_REVOCATION")

	add(len(p.RequiredCapabilities) > 0, "CANONICAL_REQUIRED_CAPABILITIES_NOT_RESOLVED")
	expected := map[string]bool{}
	for _, key := range p.RequiredCapabilities {
		if strings.TrimSpace(key) == "" || expected[key] {
			missing = append(missing, "INVALID_REQUIRED_CAPABILITY_SET")
			continue
		}
		expected[key] = true
	}
	providers := map[string]PEO05ProviderProof{}
	for _, proof := range p.Providers {
		if !expected[proof.CapabilityKey] || proof.CapabilityKey == "" {
			missing = append(missing, "UNREQUESTED_PROVIDER_BINDING")
			continue
		}
		if _, exists := providers[proof.CapabilityKey]; exists {
			missing = append(missing, "AMBIGUOUS_PROVIDER_BINDING")
			continue
		}
		providers[proof.CapabilityKey] = proof
	}
	for key := range expected {
		proof, ok := providers[key]
		if !ok {
			missing = append(missing, "MISSING_PROVIDER_FOR_"+key)
			continue
		}
		if proof.TenantID != entry.TenantID || proof.EngineInstanceID == "" ||
			proof.ProviderKey == "" || proof.ResidencyRegion != p.ResidencyRequirement ||
			proof.SupportStatus != "CERTIFIED" || proof.CertificationReference == "" ||
			!proof.CertificationExpiresAt.After(at) ||
			proof.HealthStatus != "HEALTHY" ||
			!currentPEO05Observation(proof.HealthObservedAt, proof.HealthExpiresAt, at) {
			missing = append(missing, "CURRENT_CERTIFIED_PROVIDER_REQUIRED_FOR_"+key)
		}
	}
	slices.Sort(missing)
	return slices.Compact(missing)
}

func validPEO05Digest(v string) bool {
	if !strings.HasPrefix(v, "sha256:") || len(v) != 71 {
		return false
	}
	for _, ch := range v[7:] {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return false
		}
	}
	return true
}

func currentPEO05Observation(observed, expires, at time.Time) bool {
	return !observed.IsZero() && !expires.IsZero() &&
		!observed.After(at) && !expires.Before(at.Add(time.Second)) &&
		at.Sub(observed) <= 2*time.Minute
}

func addPEO05Missing(existing []string, reason string) []string {
	if !slices.Contains(existing, reason) {
		return append(existing, reason)
	}
	return existing
}

// Assert the gate cannot silently be used for an entitlement mutation.
func (r PEO05Report) VerifyNonAuthorising() error {
	if r.ProductionEligible || r.EvidenceClass != "READ_ONLY_NON_AUTHORISING" {
		return fmt.Errorf("%w: report cannot authorise production", ErrPEO05Integrity)
	}
	return nil
}
