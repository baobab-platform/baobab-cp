// ADR-BCP-018 sections 26-29 and 81-82 — CorporateGroup membership derivation.
//
// A CorporateGroup is a projection of the verified corporate graph. Its
// membership is derived under an explicit, versioned policy and always keeps
// its lineage. It is never an authorization boundary (section 29): nothing
// in the Control Plane grants access because two organisations share a group.

package domain

import (
	"fmt"
	"sort"
	"time"
)

// GroupPolicyVerifiedControlMajority is the initial grouping policy. An edge
// qualifies for membership only if it is consequential (VERIFIED, ACTIVE,
// in its effective window) and is either CONTROLS, or OWNS with a recorded
// ownership percentage strictly above 50. Ownership is not control (section
// 21): OWNS without a recorded percentage, or at 50% or below, is excluded
// and reported rather than assumed.
const GroupPolicyVerifiedControlMajority = "verified-control-majority/v1"

// ExcludedGroupEdge is an edge the policy did not use, with the reason.
type ExcludedGroupEdge struct {
	RelationshipID string `json:"relationship_id"`
	Reason         string `json:"reason"`
}

// GroupDerivation is the outcome of deriving a group's membership.
type GroupDerivation struct {
	Policy string `json:"policy"`
	// Members maps each derived member organisation to the ids of the
	// qualifying edges that bring it into the group (its lineage). The root
	// maps to no edges.
	Members  map[string][]string `json:"members"`
	Excluded []ExcludedGroupEdge `json:"excluded,omitempty"`
	// Ambiguities are graph conditions a reviewer must see (section 82):
	// cycles back into the group, or a root that is itself controlled.
	Ambiguities []string `json:"ambiguities,omitempty"`
}

// QualifiesForGroup applies GroupPolicyVerifiedControlMajority to one edge.
// It returns "" when the edge qualifies, else the reason it does not.
func QualifiesForGroup(r CorporateRelationship, at time.Time) string {
	if !r.IsConsequential(at) {
		return "not consequential (requires VERIFIED, ACTIVE, OWNS/CONTROLS, in effect)"
	}
	if r.RelationshipType == CorpRelControls {
		return ""
	}
	if r.OwnershipPercentage == nil {
		return "OWNS without a recorded ownership percentage does not establish control"
	}
	if *r.OwnershipPercentage <= 50 {
		return fmt.Sprintf("OWNS %.2f%% is not a controlling majority", *r.OwnershipPercentage)
	}
	return ""
}

// DeriveGroupMembership derives membership of the group rooted at root from
// edges, following qualifying edges downwards (source -> target). edges
// should hold every edge reachable from root; edges that do not touch the
// reachable set are ignored. Traversal is bounded by MaxCorporateControlDepth
// and never revisits a member, so cross-holdings and cycles terminate.
func DeriveGroupMembership(root string, edges []CorporateRelationship, at time.Time) GroupDerivation {
	d := GroupDerivation{Policy: GroupPolicyVerifiedControlMajority, Members: map[string][]string{root: nil}}
	outbound := map[string][]CorporateRelationship{}
	for _, e := range edges {
		outbound[e.SourceOrganisationID] = append(outbound[e.SourceOrganisationID], e)
		if e.TargetOrganisationID == root && QualifiesForGroup(e, at) == "" {
			d.Ambiguities = append(d.Ambiguities, fmt.Sprintf(
				"root %s is itself controlled via %s by %s; the group root may not be the ultimate parent",
				root, e.ID, e.SourceOrganisationID))
		}
	}
	excluded := map[string]string{}
	frontier := []string{root}
	for depth := 0; depth < MaxCorporateControlDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, node := range frontier {
			for _, e := range outbound[node] {
				if reason := QualifiesForGroup(e, at); reason != "" {
					excluded[e.ID] = reason
					continue
				}
				target := e.TargetOrganisationID
				if target == root {
					d.Ambiguities = append(d.Ambiguities, fmt.Sprintf("cycle: %s returns control to the root", e.ID))
					continue
				}
				if _, seen := d.Members[target]; seen {
					if !contains(d.Members[target], e.ID) {
						d.Members[target] = append(d.Members[target], e.ID)
					}
					continue
				}
				d.Members[target] = []string{e.ID}
				next = append(next, target)
			}
		}
		frontier = next
	}
	if len(frontier) > 0 {
		d.Ambiguities = append(d.Ambiguities, fmt.Sprintf("control chain deeper than %d levels was not followed", MaxCorporateControlDepth))
	}
	for id, reason := range excluded {
		d.Excluded = append(d.Excluded, ExcludedGroupEdge{RelationshipID: id, Reason: reason})
	}
	sort.Slice(d.Excluded, func(i, j int) bool { return d.Excluded[i].RelationshipID < d.Excluded[j].RelationshipID })
	for _, basis := range d.Members {
		sort.Strings(basis)
	}
	sort.Strings(d.Ambiguities)
	return d
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
