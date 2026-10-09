package legalactor

import (
	"testing"
	"time"
)

var testAt = time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

func testRequest() Request {
	return Request{
		TenantID: "tn_zuribeans",
		OperatingOrganisationID: "org_zuribeans",
		Role: "SELLER_OF_RECORD",
		Activity: "B2B_COFFEE_SALE",
		Market: "ZA",
		EffectiveAt: testAt,
	}
}

func testMandate() Candidate {
	approvedAt := testAt.Add(-time.Hour)
	return Candidate{
		MandateID: "aef48e41-6cbd-4406-8d4a-8a1da4677b45",
		TenantID: "tn_zuribeans",
		OperatingOrganisationID: "org_zuribeans",
		ResponsibleLegalEntityID: "NABHOLD",
		Roles: []string{"SELLER_OF_RECORD"},
		ActivityScope: []string{"B2B_COFFEE_SALE"},
		MarketScope: []string{"ZA"},
		Status: "ACTIVE",
		AuthorityBasisReference: "governance/approved-nabhold-za-seller",
		EvidenceReferences: []string{"verification/legal-actor"},
		LegalActorVerificationReference: "registry/verified-company",
		EffectiveFrom: testAt.Add(-2 * time.Hour),
		CreatedBy: "maker",
		ApprovedBy: "independent-checker",
		ApprovedAt: &approvedAt,
		ActorVerified: true,
	}
}

func TestResolveAuthorisedScopedLegalActor(t *testing.T) {
	request, mandate := testRequest(), testMandate()
	result := Resolve(request, []Candidate{mandate}, testAt)
	if result.Outcome != Authorized ||
		result.ResponsibleLegalEntityID != "NABHOLD" ||
		result.MandateID != mandate.MandateID ||
		len(result.EvidenceReferences) != 1 {
		t.Fatalf("valid scoped legal actor was not resolved: %+v", result)
	}
	// Copies must not permit a caller to mutate the original mandate facts.
	result.EvidenceReferences[0] = "modified"
	if mandate.EvidenceReferences[0] != "verification/legal-actor" {
		t.Fatal("resolution leaked mutable evidence slice")
	}
}

func TestResolveLegalActorStrictScope(t *testing.T) {
	tests := []struct {
		name string
		change func(*Request, *Candidate)
	}{
		{"other tenant", func(r *Request, _ *Candidate) { r.TenantID = "tn_ea" }},
		{"other organisation", func(r *Request, _ *Candidate) { r.OperatingOrganisationID = "org_nabhold" }},
		{"other market", func(r *Request, _ *Candidate) { r.Market = "UG" }},
		{"other role", func(r *Request, _ *Candidate) { r.Role = "INVOICE_ISSUER" }},
		{"other activity", func(r *Request, _ *Candidate) { r.Activity = "B2B_VANILLA_EXPORT" }},
		{"capability restriction", func(_ *Request, m *Candidate) { m.CapabilityScope = []string{"trade.checkout"} }},
		{"unapproved", func(_ *Request, m *Candidate) { m.Status = "PENDING" }},
		{"not yet in effect", func(_ *Request, m *Candidate) { m.EffectiveFrom = testAt.Add(time.Hour) }},
		{"future operation timestamp", func(r *Request, _ *Candidate) { r.EffectiveAt = testAt.Add(time.Hour) }},
		{"lowercase market", func(r *Request, _ *Candidate) { r.Market = "za" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request, mandate := testRequest(), testMandate()
			tc.change(&request, &mandate)
			result := Resolve(request, []Candidate{mandate}, testAt)
			if result.Outcome != NoApplicableMandate || result.ResponsibleLegalEntityID != "" {
				t.Fatalf("unscoped mandate conferred authority: %+v", result)
			}
		})
	}
}

func TestResolveExpiredOrRevokedCannotBeBackdated(t *testing.T) {
	for _, state := range []string{"REVOKED", "EXPIRED"} {
		t.Run(state, func(t *testing.T) {
			request, m := testRequest(), testMandate()
			m.Status = state
			request.EffectiveAt = testAt.Add(-time.Hour)
			out := Resolve(request, []Candidate{m}, testAt)
			if out.Outcome != RevokedOrExpired || out.ResponsibleLegalEntityID != "" {
				t.Fatalf("%s mandate was incorrectly authorised: %+v", state, out)
			}
		})
	}
	request, m := testRequest(), testMandate()
	expiry := testAt.Add(-time.Minute)
	m.EffectiveTo = &expiry
	out := Resolve(request, []Candidate{m}, testAt)
	if out.Outcome != RevokedOrExpired {
		t.Fatalf("expired window incorrectly authorised: %+v", out)
	}
}

func TestResolveOverlappingMandatesAmbiguous(t *testing.T) {
	request, a := testRequest(), testMandate()
	b := a
	b.MandateID = "bebc4bad-f28b-4869-903d-97bdba18b7f9"
	b.ResponsibleLegalEntityID = "ANOTHER-LEGAL-ACTOR"
	for _, candidates := range [][]Candidate{{a, b}, {b, a}} {
		out := Resolve(request, candidates, testAt)
		if out.Outcome != Ambiguous || out.MandateID != "" || out.ResponsibleLegalEntityID != "" {
			t.Fatalf("overlap selected a seller by ordering: %+v", out)
		}
	}
}

func TestResolveNeverTreatsDeclarationAsActorVerification(t *testing.T) {
	tests := []struct {
		name string
		change func(*Candidate)
	}{
		{"unverified actor", func(m *Candidate) { m.ActorVerified = false }},
		{"missing actor verification reference", func(m *Candidate) { m.LegalActorVerificationReference = "" }},
		{"missing evidence", func(m *Candidate) { m.EvidenceReferences = nil }},
		{"missing approval", func(m *Candidate) { m.ApprovedAt = nil }},
		{"self approval", func(m *Candidate) { m.ApprovedBy = m.CreatedBy }},
		{"unidentified legal actor", func(m *Candidate) { m.ResponsibleLegalEntityID = "" }},
		{"missing authority basis", func(m *Candidate) { m.AuthorityBasisReference = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request, m := testRequest(), testMandate()
			tc.change(&m)
			out := Resolve(request, []Candidate{m}, testAt)
			if out.Outcome != ActorNotVerified || out.ResponsibleLegalEntityID != "" {
				t.Fatalf("unverified authority accepted: %+v", out)
			}
		})
	}
}

func TestResolveSameLegalActorDoesNotUnifyTenants(t *testing.T) {
	a := testMandate()
	b := testMandate()
	b.TenantID = "tn_equator"
	b.OperatingOrganisationID = "org_equator"
	b.MandateID = "496d56df-8d86-43b9-b59e-08a9c95345b0"
	request := testRequest()
	out := Resolve(request, []Candidate{b}, testAt)
	if out.Outcome != NoApplicableMandate {
		t.Fatalf("parent actor conferred access to sibling tenant: %+v", out)
	}
	out = Resolve(request, []Candidate{a, b}, testAt)
	if out.Outcome != Authorized || out.MandateID != a.MandateID {
		t.Fatalf("same legal actor did not retain tenant isolation: %+v", out)
	}
}

func TestResolveBoundedValidity(t *testing.T) {
	request, m := testRequest(), testMandate()
	until := testAt.Add(time.Hour)
	m.EffectiveTo = &until
	request.Capability = "trade.checkout"
	m.CapabilityScope = []string{"trade.checkout"}
	out := Resolve(request, []Candidate{m}, testAt)
	if out.Outcome != Authorized || out.ValidUntil == nil || !out.ValidUntil.Equal(until) {
		t.Fatalf("valid capability-specific mandate was not bounded: %+v", out)
	}
}
