package api

import (
  "testing"
  "time"

  "github.com/baobab-platform/baobab-cp/internal/administration"
)

func TestCX05DDirectoryIndexBoundedGrantsAndNoGroupExpansion(t *testing.T) {
    now:=time.Now().UTC()
    ids:=make([]administration.Grant,0,600)
    for i:=0;i<600;i++ {
        // The exact-scope expansion helper must reject group scope and
        // never silently truncate at the former 500 fallback bound.
        ids=append(ids,administration.Grant{
          Permission:"organisation.view",
          Scope:administration.Scope{Level:administration.LevelOrganisation,
             OrganisationID:directoryOrg},
        })
    }
    _=now
    authority:=administration.EffectiveAuthority{Grants:ids}
    got,ok:=boundedDirectoryIDs(authority,"staging",20000)
    if !ok || len(got)!=1 || got[0]!=directoryOrg {t.Fatalf("expected single deduplicated grant, got %v %v",got,ok)}
    authority.Grants=append(authority.Grants,administration.Grant{
      Permission:"organisation.view",
      Scope:administration.Scope{Level:administration.LevelCorporateGroup,CorporateGroupID:"cgrp_fake"},
    })
    got,ok=boundedDirectoryIDs(authority,"staging",20000)
    if !ok || len(got)!=1 {t.Fatalf("group grant leaked into index %v %v",got,ok)}
}
