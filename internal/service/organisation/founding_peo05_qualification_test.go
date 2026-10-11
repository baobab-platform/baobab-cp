package organisation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type peo05ReaderFixture struct {
	tenants map[string]PEO05TenantEvidence
	err     error
	reads   int
}

func (f *peo05ReaderFixture) ReadPEO05Tenant(_ context.Context, tenantID, _ string, _ time.Time) (PEO05TenantEvidence, error) {
	f.reads++
	if f.err != nil {
		return PEO05TenantEvidence{}, f.err
	}
	return f.tenants[tenantID], nil
}

func peo05Fixture(t *testing.T) (PEO05Gate, FirstPartyRegistry, FoundingIdentityInput, FoundingIdentityInput, *peo05ReaderFixture) {
	t.Helper()
	founding, registry, parent, child := acceptanceFixture(t)
	now := founding.Now()
	digest := "sha256:" + strings.Repeat("a", 64)
	reader := &peo05ReaderFixture{tenants: map[string]PEO05TenantEvidence{}}
	ids := map[string]string{
		"NABHOLD":        parent.OrganisationID,
		"THAMANI-GLOBAL": child.OrganisationID,
		"ZURIBEANS":      "00000000-0000-7000-8000-000000000003",
		"EQUATOR-ESTATE": "00000000-0000-7000-8000-000000000004",
	}
	for _, reg := range registry.Entities {
		tenant := "tn_" + reg.ID
		reader.tenants[tenant] = PEO05TenantEvidence{
			TenantID: tenant, OrganisationID: ids[reg.ID],
			AdmissionDecisionID: "adm_independently_approved",
			OnboardingRequestID: "tor_governed",
			RequesterID:         "prn_maker", AuthoriserID: "prn_checker",
			ProvisioningState:  "READY",
			DesiredStateDigest: digest, PlanDesiredStateDigest: digest,
			PlanDigest: digest, ApprovedPlanDigest: digest,
			PlanApprovalID: "apd_proof", PlanApproverID: "prn_plan_approver",
			PlanExpiresAt: now.Add(time.Hour), ResidencyRequirement: "af-south-1",
			SubscriptionID: "sub_test", SubscriptionTenantID: tenant, SubscriptionStatus: "ACTIVE",
			RequiredCapabilities: []string{"commerce.order.manage"},
			Providers: []PEO05ProviderProof{{
				CapabilityKey: "commerce.order.manage", TenantID: tenant,
				EngineInstanceID: "ei_test", ProviderKey: "baobab-trade.medusa",
				ResidencyRegion: "af-south-1", SupportStatus: "CERTIFIED",
				CertificationReference: "cert_independently_approved", CertificationExpiresAt: now.Add(time.Hour),
				HealthStatus: "HEALTHY", HealthObservedAt: now.Add(-time.Minute),
				HealthExpiresAt: now.Add(time.Minute),
			}},
			IAM: PEO05IAMProof{
				TenantID: tenant, OrganisationID: ids[reg.ID],
				PrincipalID: "prn_synthetic_staff", IssuerID: "iss_test",
				Audience: "baobab-control-plane", MembershipStatus: "ACTIVE",
				EvidenceReference:   "iam_live_revision",
				RevocationCheckedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
			},
		}
	}
	return PEO05Gate{Founding: founding, Tenants: reader, Now: func() time.Time { return now }},
		registry, parent, child, reader
}

func TestPEO05AOnlySyntheticQualifiedFourTenantSetIsStagingCandidate(t *testing.T) {
	gate, reg, parent, child, reader := peo05Fixture(t)
	got, err := gate.Assess(context.Background(), reg, parent, child)
	if err != nil || !got.AllStagingCandidates || got.ProductionEligible ||
		len(got.Entries) != 4 || reader.reads != 4 || got.VerifyNonAuthorising() != nil {
		t.Fatalf("synthetic readiness was not read-only staging qualification: %+v, %v", got, err)
	}
	for _, item := range got.Entries {
		if item.Status != PEO05StagingCandidate || len(item.Missing) != 0 {
			t.Fatalf("unexpected qualification: %+v", item)
		}
	}
}

func TestPEO05AFailClosedWhenIndependentCPOrIAMAuthorityUnavailable(t *testing.T) {
	gate, reg, parent, child, reader := peo05Fixture(t)
	reader.err = errors.New("IAM offline")
	got, err := gate.Assess(context.Background(), reg, parent, child)
	if err != nil || got.AllStagingCandidates || len(got.Entries) != 4 {
		t.Fatalf("upstream outage treated as approval: %+v %v", got, err)
	}
	for _, item := range got.Entries {
		if !slices.Contains(item.Missing, "UPSTREAM_AUTHORITY_UNAVAILABLE") || item.Status != PEO05Blocked {
			t.Fatalf("upstream failure not blocked: %+v", item)
		}
	}
}

func TestPEO05ACannotSkipPackageThree(t *testing.T) {
	gate, reg, parent, child, reader := peo05Fixture(t)
	proof := gate.Founding.Evidence.(*acceptanceEvidenceFixture)
	proof.result.NabholdRegistrationVerified = false
	proof.result.NabholdRegistrationEvidence = ""
	got, err := gate.Assess(context.Background(), reg, parent, child)
	if err != nil || got.AllStagingCandidates || reader.reads != 3 {
		t.Fatalf("official CIPC failure not blocking Nabhold: %+v %v reads %d", got, err, reader.reads)
	}
	for _, e := range got.Entries {
		if e.FirstPartyID == "NABHOLD" && !slices.Contains(e.Missing, "PEO04_CURRENT_IDENTITY_TENANT_EVIDENCE") {
			t.Fatalf("registration skip: %+v", e)
		}
	}
}

func TestPEO05ARejectsOperationalBypasses(t *testing.T) {
	cases := map[string]func(*PEO05TenantEvidence){
		"sibling tenant":             func(e *PEO05TenantEvidence) { e.IAM.TenantID = "tn_sibling" },
		"sibling organisation":       func(e *PEO05TenantEvidence) { e.OrganisationID = "org_sibling" },
		"self authorised":            func(e *PEO05TenantEvidence) { e.AuthoriserID = e.RequesterID },
		"self approved plan":         func(e *PEO05TenantEvidence) { e.PlanApproverID = e.RequesterID },
		"stale plan":                 func(e *PEO05TenantEvidence) { e.PlanExpiresAt = time.Time{} },
		"modified desired intent":    func(e *PEO05TenantEvidence) { e.PlanDesiredStateDigest = "sha256:" + strings.Repeat("b", 64) },
		"plan digest mismatch":       func(e *PEO05TenantEvidence) { e.ApprovedPlanDigest = "" },
		"provisioning blocked":       func(e *PEO05TenantEvidence) { e.ProvisioningState = "BLOCKED" },
		"subscription revoked":       func(e *PEO05TenantEvidence) { e.SubscriptionStatus = "REVOKED" },
		"IAM revoked":                func(e *PEO05TenantEvidence) { e.IAM.MembershipStatus = "REVOKED" },
		"IAM unknown issuer":         func(e *PEO05TenantEvidence) { e.IAM.IssuerID = "" },
		"IAM wrong audience":         func(e *PEO05TenantEvidence) { e.IAM.Audience = "baobab-payments" },
		"IAM stale revocation proof": func(e *PEO05TenantEvidence) { e.IAM.RevocationCheckedAt = time.Time{} },
		"IAM expired proof":          func(e *PEO05TenantEvidence) { e.IAM.ExpiresAt = time.Time{} },
		"no capabilities":            func(e *PEO05TenantEvidence) { e.RequiredCapabilities = nil },
		"hidden mandatory capability": func(e *PEO05TenantEvidence) {
			e.RequiredCapabilities = append(e.RequiredCapabilities, "erp.invoice.post")
		},
		"duplicate capability": func(e *PEO05TenantEvidence) {
			e.RequiredCapabilities = append(e.RequiredCapabilities, "commerce.order.manage")
		},
		"unrequested capability":    func(e *PEO05TenantEvidence) { e.Providers[0].CapabilityKey = "fraud.missing" },
		"duplicate provider":        func(e *PEO05TenantEvidence) { e.Providers = append(e.Providers, e.Providers[0]) },
		"unsigned certification":    func(e *PEO05TenantEvidence) { e.Providers[0].CertificationReference = "" },
		"provider not certified":    func(e *PEO05TenantEvidence) { e.Providers[0].SupportStatus = "CANDIDATE" },
		"provider wrong residency":  func(e *PEO05TenantEvidence) { e.Providers[0].ResidencyRegion = "eu-west-1" },
		"provider health expired":   func(e *PEO05TenantEvidence) { e.Providers[0].HealthExpiresAt = time.Time{} },
		"provider missing instance": func(e *PEO05TenantEvidence) { e.Providers[0].EngineInstanceID = "" },
		"provider belongs sibling":  func(e *PEO05TenantEvidence) { e.Providers[0].TenantID = "tn_other" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			gate, reg, parent, child, reader := peo05Fixture(t)
			e := reader.tenants["tn_ZURIBEANS"]
			mutate(&e)
			reader.tenants["tn_ZURIBEANS"] = e
			got, err := gate.Assess(context.Background(), reg, parent, child)
			if err != nil || got.AllStagingCandidates {
				t.Fatalf("bypass accepted: %+v %v", got, err)
			}
			for _, row := range got.Entries {
				if row.FirstPartyID == "ZURIBEANS" && (row.Status != PEO05Blocked || len(row.Missing) == 0) {
					t.Fatalf("missing negative reason: %+v", row)
				}
			}
		})
	}
}

func TestPEO05ANeverReclassifiesReportAsProductionAuthority(t *testing.T) {
	gate, reg, parent, child, _ := peo05Fixture(t)
	got, err := gate.Assess(context.Background(), reg, parent, child)
	if err != nil {
		t.Fatal(err)
	}
	got.ProductionEligible = true
	if !errors.Is(got.VerifyNonAuthorising(), ErrPEO05Integrity) {
		t.Fatal("production endorsement not rejected")
	}
}
