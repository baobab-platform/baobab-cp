package api

import (
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/administration"
)

func TestCX05DDirectoryIndexBoundedGrantsAndNoGroupExpansion(t *testing.T) {
	grants := make([]administration.EffectiveGrant, 0, 600)
	for i := 0; i < 600; i++ {
		grants = append(grants, administration.EffectiveGrant{
			Permission: "organisation.view",
			Scope: administration.Scope{Level: administration.LevelOrganisation,
				OrganisationID: directoryOrg},
		})
	}
	authority := administration.EffectiveAuthority{Grants: grants}
	got, ok := boundedDirectoryIDs(authority, "staging", 20000)
	if !ok || len(got) != 1 || got[0] != directoryOrg {
		t.Fatalf("expected a single deduplicated grant, got %v %v", got, ok)
	}
	authority.Grants = append(authority.Grants, administration.EffectiveGrant{
		Permission: "organisation.view",
		Scope: administration.Scope{Level: administration.LevelCorporateGroup, CorporateGroupID: "cgrp_fake"},
	})
	got, ok = boundedDirectoryIDs(authority, "staging", 20000)
	if !ok || len(got) != 1 {
		t.Fatalf("group grant leaked into index %v %v", got, ok)
	}
}
