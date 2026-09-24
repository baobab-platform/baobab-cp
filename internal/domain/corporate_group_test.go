package domain

import (
	"reflect"
	"strings"
	"testing"
)

func pct(v float64) *float64 { return &v }

func owns(id, source, target string, p *float64) CorporateRelationship {
	e := verifiedEdge(id, source, target, CorpRelOwns)
	e.OwnershipPercentage = p
	return e
}

func TestDeriveGroupMembershipPolicy(t *testing.T) {
	controls := verifiedEdge("c1", "root", "ctrl", CorpRelControls)
	unverified := owns("u1", "root", "pending", pct(100))
	unverified.VerificationState = VerificationPendingReview
	branch := verifiedEdge("b1", "root", "branch", CorpRelBranchOf)

	d := DeriveGroupMembership("root", []CorporateRelationship{
		owns("m1", "root", "majority", pct(80)),
		owns("m2", "majority", "grandchild", pct(100)),
		owns("x1", "root", "half", pct(50)),
		owns("x2", "root", "minority", pct(30)),
		owns("x3", "root", "unknown-stake", nil),
		controls, unverified, branch,
	}, now)

	want := map[string][]string{
		"root": nil, "majority": {"m1"}, "grandchild": {"m2"}, "ctrl": {"c1"},
	}
	if !reflect.DeepEqual(d.Members, want) {
		t.Fatalf("members = %v, want %v", d.Members, want)
	}
	reasons := map[string]string{}
	for _, x := range d.Excluded {
		reasons[x.RelationshipID] = x.Reason
	}
	for id, fragment := range map[string]string{
		"x1": "not a controlling majority", // exactly 50% is not control
		"x2": "not a controlling majority",
		"x3": "without a recorded ownership percentage",
		"u1": "not consequential",
		"b1": "not consequential", // BRANCH_OF never establishes group control
	} {
		if !strings.Contains(reasons[id], fragment) {
			t.Errorf("edge %s: reason %q, want it to mention %q", id, reasons[id], fragment)
		}
	}
	if d.Policy != GroupPolicyVerifiedControlMajority || len(d.Ambiguities) != 0 {
		t.Fatalf("policy=%s ambiguities=%v", d.Policy, d.Ambiguities)
	}
}

func TestDeriveGroupMembershipGraphShapes(t *testing.T) {
	// Two qualifying parents inside the group: both edges are lineage.
	d := DeriveGroupMembership("root", []CorporateRelationship{
		owns("a", "root", "left", pct(100)),
		owns("b", "root", "right", pct(100)),
		verifiedEdge("c", "left", "jv", CorpRelControls),
		verifiedEdge("d", "right", "jv", CorpRelControls),
	}, now)
	if !reflect.DeepEqual(d.Members["jv"], []string{"c", "d"}) {
		t.Fatalf("jointly controlled member lineage = %v", d.Members["jv"])
	}

	// A cycle back to the root terminates and is reported, not resolved.
	d = DeriveGroupMembership("root", []CorporateRelationship{
		owns("a", "root", "sub", pct(100)),
		owns("back", "sub", "root", pct(60)),
	}, now)
	if len(d.Members) != 2 || len(d.Ambiguities) == 0 || !strings.Contains(strings.Join(d.Ambiguities, ";"), "cycle") {
		t.Fatalf("cycle: members=%v ambiguities=%v", d.Members, d.Ambiguities)
	}

	// A root that is itself controlled from outside is flagged.
	d = DeriveGroupMembership("root", []CorporateRelationship{
		verifiedEdge("up", "parent", "root", CorpRelControls),
		owns("a", "root", "sub", pct(100)),
	}, now)
	if _, in := d.Members["parent"]; in || !strings.Contains(strings.Join(d.Ambiguities, ";"), "may not be the ultimate parent") {
		t.Fatalf("controlled root: members=%v ambiguities=%v", d.Members, d.Ambiguities)
	}

	// A lone root is still a group of one.
	d = DeriveGroupMembership("root", nil, now)
	if len(d.Members) != 1 || d.Members["root"] != nil {
		t.Fatalf("lone root: %v", d.Members)
	}
}

func TestDeriveGroupMembershipDepthIsBounded(t *testing.T) {
	var edges []CorporateRelationship
	prev := "root"
	for i := 0; i <= MaxCorporateControlDepth+1; i++ {
		next := "n" + strings.Repeat("x", i+1)
		edges = append(edges, owns("e"+next, prev, next, pct(100)))
		prev = next
	}
	d := DeriveGroupMembership("root", edges, now)
	if len(d.Members) > MaxCorporateControlDepth+1 || !strings.Contains(strings.Join(d.Ambiguities, ";"), "deeper than") {
		t.Fatalf("depth bound: %d members, ambiguities=%v", len(d.Members), d.Ambiguities)
	}
}
