package domain

import (
	"strings"
	"testing"
	"time"
)

var (
	t0  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now = time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
)

func verifiedEdge(id, source, target string, typ CorporateRelationshipType) CorporateRelationship {
	at := t0
	return CorporateRelationship{
		ID: id, SourceOrganisationID: source, TargetOrganisationID: target, RelationshipType: typ,
		DirectOrDerived: CorporateFactDirect, VerificationState: VerificationVerified,
		Status: RelationshipStatusActive, EffectiveFrom: t0, SourceAuthority: "admission-review",
		EvidenceReferences: []string{"evd_1"}, VerifiedBy: "svc:review", VerifiedAt: &at,
	}
}

func verifiedPlatformRel(org string, typ PlatformRelationshipType, basis string) PlatformRelationship {
	at := t0
	return PlatformRelationship{
		ID: "prel_" + org, PlatformID: "baobab-platform", OrganisationID: org, RelationshipType: typ,
		BasisRelationshipID: basis, VerificationState: VerificationVerified, Status: RelationshipStatusActive,
		EffectiveFrom: t0, SourceAuthority: "platform-governance",
		EvidenceReferences: []string{"evd_1"}, VerifiedBy: "svc:review", VerifiedAt: &at,
	}
}

var owners = map[string]struct{}{"owner": {}}

func eligible(org string, prs []PlatformRelationship, edges []CorporateRelationship) bool {
	return DeriveInternalEligibility(InternalEligibilityEvidence{
		OrganisationID: org, PlatformRelationships: prs, CorporateRelationships: edges, EvaluatedAt: now,
	}, owners)
}

func TestInternalEligibility(t *testing.T) {
	ownsSub := verifiedEdge("e1", "owner", "sub", CorpRelOwns)
	subOwnsGrand := verifiedEdge("e2", "sub", "grand", CorpRelOwns)
	subOwnsOwner := verifiedEdge("rev", "sub", "owner", CorpRelOwns)
	unverified := verifiedEdge("e2u", "sub", "grand", CorpRelOwns)
	unverified.VerificationState = VerificationPendingReview
	cycleA := verifiedEdge("c1", "x", "grand", CorpRelControls)
	cycleB := verifiedEdge("c2", "y", "x", CorpRelOwns)
	cycleC := verifiedEdge("c3", "x", "y", CorpRelOwns)
	affiliate := verifiedEdge("aff", "sub", "grand", CorpRelAffiliateOf)
	pending := verifiedPlatformRel("sub", PlatformRelGroupAffiliate, "e1")
	pending.Status = RelationshipStatusPending

	cases := []struct {
		name  string
		org   string
		prs   []PlatformRelationship
		edges []CorporateRelationship
		want  bool
	}{
		{"platform owner", "owner", []PlatformRelationship{verifiedPlatformRel("owner", PlatformRelOwner, "")}, nil, true},
		{"platform operator", "op", []PlatformRelationship{verifiedPlatformRel("op", PlatformRelOperator, "")}, nil, true},
		{"direct affiliate owned by owner", "sub", []PlatformRelationship{verifiedPlatformRel("sub", PlatformRelGroupAffiliate, "e1")}, []CorporateRelationship{ownsSub}, true},
		{"multi-hop affiliate", "grand", []PlatformRelationship{verifiedPlatformRel("grand", PlatformRelGroupAffiliate, "e2")}, []CorporateRelationship{ownsSub, subOwnsGrand}, true},
		{"reversed edge: affiliate owns the owner", "sub", []PlatformRelationship{verifiedPlatformRel("sub", PlatformRelGroupAffiliate, "rev")}, []CorporateRelationship{subOwnsOwner}, false},
		{"unverified hop breaks the chain", "grand", []PlatformRelationship{verifiedPlatformRel("grand", PlatformRelGroupAffiliate, "e2u")}, []CorporateRelationship{ownsSub, unverified}, false},
		{"basis edge does not target the affiliate", "grand", []PlatformRelationship{verifiedPlatformRel("grand", PlatformRelGroupAffiliate, "e1")}, []CorporateRelationship{ownsSub, subOwnsGrand}, false},
		{"basis missing from evidence", "sub", []PlatformRelationship{verifiedPlatformRel("sub", PlatformRelGroupAffiliate, "e1")}, nil, false},
		{"non-control relationship type", "grand", []PlatformRelationship{verifiedPlatformRel("grand", PlatformRelGroupAffiliate, "aff")}, []CorporateRelationship{ownsSub, affiliate}, false},
		{"cycle without owner terminates and denies", "grand", []PlatformRelationship{verifiedPlatformRel("grand", PlatformRelGroupAffiliate, "c1")}, []CorporateRelationship{cycleA, cycleB, cycleC}, false},
		{"pending affiliate relationship", "sub", []PlatformRelationship{pending}, []CorporateRelationship{ownsSub}, false},
		{"external client", "sub", []PlatformRelationship{verifiedPlatformRel("sub", PlatformRelExternalClient, "")}, []CorporateRelationship{ownsSub}, false},
		{"relationship of another organisation", "sub", []PlatformRelationship{verifiedPlatformRel("owner", PlatformRelOwner, "")}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eligible(tc.org, tc.prs, tc.edges); got != tc.want {
				t.Fatalf("eligible = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInternalEligibilityDepthIsBounded(t *testing.T) {
	var edges []CorporateRelationship
	prev := "owner"
	for i := 0; i <= MaxCorporateControlDepth; i++ {
		next := "n" + strings.Repeat("x", i+1)
		edges = append(edges, verifiedEdge("e"+next, prev, next, CorpRelOwns))
		prev = next
	}
	last := edges[len(edges)-1]
	if eligible(prev, []PlatformRelationship{verifiedPlatformRel(prev, PlatformRelGroupAffiliate, last.ID)}, edges) {
		t.Fatal("a control chain deeper than MaxCorporateControlDepth must fail closed")
	}
}

func TestConsequentialityFailsClosed(t *testing.T) {
	ended := now.Add(-time.Hour)
	e := verifiedEdge("e", "a", "b", CorpRelOwns)
	for name, mutate := range map[string]func(*CorporateRelationship){
		"conflicted": func(r *CorporateRelationship) { r.VerificationState = VerificationConflicted },
		"pending":    func(r *CorporateRelationship) { r.Status = RelationshipStatusPending },
		"ended":      func(r *CorporateRelationship) { r.EffectiveTo = &ended },
		"future":     func(r *CorporateRelationship) { r.EffectiveFrom = now.Add(time.Hour) },
		"branch":     func(r *CorporateRelationship) { r.RelationshipType = CorpRelBranchOf },
	} {
		r := e
		mutate(&r)
		if r.IsConsequential(now) {
			t.Errorf("%s edge must not be consequential", name)
		}
	}
	if !e.IsConsequential(now) {
		t.Fatal("verified active OWNS edge should be consequential")
	}
}

func TestVerifiedRequiresEvidence(t *testing.T) {
	r := verifiedEdge("e", "a", "b", CorpRelOwns)
	r.EvidenceReferences = nil
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "VERIFIED requires evidence") {
		t.Fatalf("corporate relationship: got %v", err)
	}
	p := verifiedPlatformRel("a", PlatformRelExternalClient, "")
	p.VerifiedBy = ""
	if err := p.Validate(); err == nil {
		t.Fatal("platform relationship VERIFIED without verifier must be invalid")
	}
	l := LegalEntityProfile{LegalEntityID: "LE-1", OrganisationID: "org", LegalName: "L", LegalStatus: LegalStatusUnknown,
		SourceAuthority: "control-plane-registration", VerificationState: VerificationVerified, EffectiveFrom: t0}
	if err := l.Validate(); err == nil {
		t.Fatal("legal entity profile VERIFIED without evidence must be invalid")
	}
	l.VerificationState = VerificationUnverified
	if err := l.Validate(); err != nil {
		t.Fatalf("unverified claim should be valid: %v", err)
	}
}

func TestCorporateRelationshipValidation(t *testing.T) {
	self := verifiedEdge("e", "a", "a", CorpRelOwns)
	if self.Validate() == nil {
		t.Error("self-relationship must be invalid")
	}
	derived := verifiedEdge("e", "a", "b", CorpRelOwns)
	derived.DirectOrDerived = CorporateFactDerived
	if derived.Validate() == nil {
		t.Error("DERIVED fact without lineage must be invalid")
	}
	pct := 140.0
	over := verifiedEdge("e", "a", "b", CorpRelOwns)
	over.OwnershipPercentage = &pct
	if over.Validate() == nil {
		t.Error("ownership above 100% must be invalid")
	}
	inverse := verifiedEdge("e", "a", "b", "SUBSIDIARY_OF")
	if inverse.Validate() == nil {
		t.Error("stored inverse relationship type must be invalid")
	}
	affiliate := verifiedPlatformRel("a", PlatformRelGroupAffiliate, "")
	if affiliate.Validate() == nil {
		t.Error("PLATFORM_GROUP_AFFILIATE without basis must be invalid")
	}
}

func TestResourceIDs(t *testing.T) {
	u := NewUUIDv7()
	id, err := FormatResourceID(CorporateRelationshipIDPrefix, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "crel_") || len(id) != len("crel_")+32 {
		t.Fatalf("unexpected id %q", id)
	}
	back, err := ParseResourceID(CorporateRelationshipIDPrefix, id)
	if err != nil || back != u {
		t.Fatalf("round trip: %q, %v; want %q", back, err, u)
	}
	for _, bad := range []string{
		"prel_" + id[5:],   // another resource's prefix
		"map_" + id[5:],    // generic mapping id
		"crel_" + id[5:36], // truncated
		"crel_" + strings.ToUpper(id[5:]),
		"crel_acme_foods_owns", // human-readable
	} {
		if _, err := ParseResourceID(CorporateRelationshipIDPrefix, bad); err == nil {
			t.Errorf("ParseResourceID accepted %q", bad)
		}
	}
}

func TestProjectTenantLegalEntityID(t *testing.T) {
	def := func(le string) TenantLegalEntityMapping {
		return TenantLegalEntityMapping{TenantID: "tn_a", LegalEntityID: le, MappingRole: TenantLegalEntityRoleDefault,
			Status: RelationshipStatusActive, EffectiveFrom: t0}
	}
	additional := def("LE-ADD")
	additional.MappingRole = TenantLegalEntityRoleAdditional
	if got := ProjectTenantLegalEntityID([]TenantLegalEntityMapping{additional, def("LE-DEF")}, now); got != "LE-DEF" {
		t.Fatalf("got %q, want LE-DEF", got)
	}
	if got := ProjectTenantLegalEntityID([]TenantLegalEntityMapping{def("LE-A"), def("LE-B")}, now); got != "" {
		t.Fatalf("ambiguous defaults must project nothing, got %q", got)
	}
	if got := ProjectTenantLegalEntityID([]TenantLegalEntityMapping{additional}, now); got != "" {
		t.Fatalf("no default must project nothing, got %q", got)
	}
}
