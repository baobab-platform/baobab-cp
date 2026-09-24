// ADR-BCP-018 gate ORG-05 — CorporateGroup projection.

package organisation

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/nabhold/baobab-cp/internal/domain"
	"github.com/nabhold/baobab-cp/internal/repository"
)

// CorporateGroupDeriver keeps a CorporateGroup's membership equal to what its
// grouping policy derives from the verified corporate graph (ADR-BCP-018
// sections 26-28). It records lineage for every membership, ends memberships
// that no longer qualify (history is kept), and never touches memberships
// recorded on a governed manual basis. Group membership confers no access
// (section 29); nothing in the Control Plane authorizes on it.
type CorporateGroupDeriver struct {
	Orgs repository.OrganisationRepository
	Now  func() time.Time
}

// GroupDerivationReport is the outcome of one derivation.
type GroupDerivationReport struct {
	GroupID     string                     `json:"group_id"`
	Policy      string                     `json:"policy"`
	DerivedAt   time.Time                  `json:"derived_at"`
	Added       []string                   `json:"added,omitempty"`
	Ended       []string                   `json:"ended,omitempty"`
	Unchanged   []string                   `json:"unchanged,omitempty"`
	Manual      []string                   `json:"manual,omitempty"`
	Excluded    []domain.ExcludedGroupEdge `json:"excluded,omitempty"`
	Ambiguities []string                   `json:"ambiguities,omitempty"`
	Skipped     string                     `json:"skipped,omitempty"`
}

// Derive re-derives groupID's membership. Organisation ids in the report are
// canonical entity ids.
func (d *CorporateGroupDeriver) Derive(ctx context.Context, groupID string, actor repository.AuditActor) (GroupDerivationReport, error) {
	now := time.Now().UTC()
	if d.Now != nil {
		now = d.Now()
	}
	report := GroupDerivationReport{GroupID: groupID, DerivedAt: now}
	group, err := d.Orgs.GetCorporateGroup(ctx, groupID)
	if err != nil {
		return report, err
	}
	if group == nil {
		return report, fmt.Errorf("corporate group %s not found", groupID)
	}
	report.Policy = group.GroupingPolicy
	switch {
	case group.GroupingPolicy != domain.GroupPolicyVerifiedControlMajority:
		report.Skipped = fmt.Sprintf("grouping policy %q has no derivation; membership is governed manually", group.GroupingPolicy)
		return report, nil
	case group.Status != "PENDING" && group.Status != "ACTIVE":
		report.Skipped = "group is " + group.Status + "; membership is not re-derived"
		return report, nil
	case group.RootOrganisationID == "":
		// Section 20: without a safely established root there is no
		// unique starting point, so membership cannot be derived.
		report.Skipped = "group has no root organisation; membership needs a governed manual basis"
		return report, nil
	}

	root := group.RootOrganisationID
	down, err := d.Orgs.ListCorporateControlDescendants(ctx, root, now)
	if err != nil {
		return report, err
	}
	up, err := d.Orgs.ListCorporateControlAncestry(ctx, root, now)
	if err != nil {
		return report, err
	}
	derived := domain.DeriveGroupMembership(root, append(down, up...), now)
	report.Excluded, report.Ambiguities = derived.Excluded, derived.Ambiguities

	// The root's lineage is the qualifying edges it holds over members; a
	// root with no members rests on the group definition itself.
	var rootBasis []string
	for member, basis := range derived.Members {
		if member == root {
			continue
		}
		for _, id := range basis {
			for _, e := range down {
				if e.ID == id && e.SourceOrganisationID == root && !slices.Contains(rootBasis, id) {
					rootBasis = append(rootBasis, id)
				}
			}
		}
	}
	slices.Sort(rootBasis)
	derived.Members[root] = rootBasis

	live, err := d.Orgs.ListLiveCorporateGroupMembers(ctx, groupID)
	if err != nil {
		return report, err
	}
	rootReference := rootBasisReference(groupID)
	current := map[string]domain.CorporateGroupMembership{}
	for _, m := range live {
		// A governed manual basis is a human decision derivation must not
		// override; the generated root reference is derivation's own.
		if m.ManualBasisReference != "" && m.ManualBasisReference != rootReference && len(m.BasisRelationshipIDs) == 0 {
			report.Manual = append(report.Manual, m.OrganisationID)
			continue
		}
		basis, member := derived.Members[m.OrganisationID]
		if member && slices.Equal(sorted(m.BasisRelationshipIDs), basis) && m.DerivationVersion == derived.Policy {
			current[m.OrganisationID] = m
			report.Unchanged = append(report.Unchanged, m.OrganisationID)
			continue
		}
		reason := "no longer derived under " + derived.Policy
		if member {
			reason = "lineage changed under " + derived.Policy
		}
		if err := d.Orgs.EndCorporateGroupMembership(ctx, m.ID, now, reason, actor); err != nil {
			return report, err
		}
		report.Ended = append(report.Ended, m.OrganisationID)
	}

	members := make([]string, 0, len(derived.Members))
	for member := range derived.Members {
		members = append(members, member)
	}
	slices.Sort(members)
	for _, member := range members {
		if _, ok := current[member]; ok || slices.Contains(report.Manual, member) {
			continue
		}
		m := domain.CorporateGroupMembership{
			CorporateGroupID: groupID, OrganisationID: member, GroupRole: "MEMBER",
			BasisRelationshipIDs: derived.Members[member], Status: domain.RelationshipStatusActive,
			EffectiveFrom: now, DerivedAt: now, DerivationVersion: derived.Policy,
		}
		if member == root {
			m.GroupRole = "ROOT"
			if len(m.BasisRelationshipIDs) == 0 {
				m.ManualBasisReference = rootReference
			}
		}
		if _, err := d.Orgs.EnsureCorporateGroupMembership(ctx, m, actor); err != nil {
			return report, fmt.Errorf("add %s to %s: %w", member, groupID, err)
		}
		report.Added = append(report.Added, member)
	}
	slices.Sort(report.Unchanged)
	slices.Sort(report.Ended)
	slices.Sort(report.Manual)
	return report, nil
}

// rootBasisReference is the basis recorded for a root with no members: the
// group definition that names it as root.
func rootBasisReference(groupID string) string {
	return "corporate-group/" + groupID + "#root_organisation_id"
}

func sorted(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}
