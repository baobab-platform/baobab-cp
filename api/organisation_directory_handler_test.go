package api

import (
    "slices"
    "testing"

    "github.com/baobab-platform/baobab-cp/internal/administration"
)

const directoryOrg = "0199d000-1111-7777-8000-111111111111"
const directoryOther = "0199d000-1111-7777-8000-222222222222"

func TestDirectoryOnlyEnumeratesCurrentExactOrganisationGrants(t *testing.T) {
    makeGrant := func(permission string, scope administration.Scope) administration.EffectiveGrant {
        return administration.EffectiveGrant{Permission:permission, Scope:scope}
    }
    v := administration.EffectiveAuthority{Grants:[]administration.EffectiveGrant{
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelOrganisation, OrganisationID:directoryOrg}),
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelOrganisation, OrganisationID:directoryOrg}),
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelOrganisation, OrganisationID:directoryOther, Environment:"staging"}),
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelOrganisation, OrganisationID:directoryOther, Environment:"production"}),
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelPlatform}),
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelCorporateGroup, CorporateGroupID:"cgrp_foreign"}),
        makeGrant("organisation.manage", administration.Scope{Level:administration.LevelOrganisation, OrganisationID:directoryOther}),
        makeGrant("organisation.view", administration.Scope{Level:administration.LevelOrganisation, OrganisationID:"../foreign"}),
    }}
    ids, ok := exactDirectoryIDs(v, "staging")
    if !ok || !slices.Equal(ids, []string{directoryOrg,directoryOther}) {
        t.Fatalf("invalid scoped directory: ok=%v ids=%v",ok,ids)
    }
    ids, ok = exactDirectoryIDs(v, "integration")
    if !ok || !slices.Equal(ids, []string{directoryOrg}) {
        t.Fatalf("environment-restricted grant leaked: ok=%v ids=%v",ok,ids)
    }
    large := administration.EffectiveAuthority{Grants:make([]administration.EffectiveGrant, 0)}
    // 101 distinct UUIDs must never be truncated to 100 and misreported as complete.
    for i:=0; i<maxOverviewRows+1; i++ {
        id:=directoryOrg
        id = id[:25]+string('0'+rune(i/100))+string('0'+rune((i/10)%10))+string('0'+rune(i%10))+id[28:]
        large.Grants=append(large.Grants,makeGrant("organisation.view",
            administration.Scope{Level:administration.LevelOrganisation, OrganisationID:id}))
    }
    if _, ok := exactDirectoryIDs(large,"staging"); ok {
        t.Fatal("oversized directory must fail closed")
    }
}

func TestTenantSectionsNeedIndependentExactPermissions(t *testing.T) {
    tenantID := "tn_01k4zuribeans"
    auth := administration.EffectiveAuthority{Grants: []administration.EffectiveGrant{
        {Permission:"tenant.view",Scope:administration.Scope{Level:administration.LevelTenant,TenantID:tenantID}},
        {Permission:"market.view",Scope:administration.Scope{Level:administration.LevelPlatform}},
        {Permission:"subscription.view",Scope:administration.Scope{Level:administration.LevelCorporateGroup,CorporateGroupID:"corp_owner"}},
        {Permission:"digital-estate.view",Scope:administration.Scope{Level:administration.LevelTenant,TenantID:"tn_sibling"}},
    }}
    if !exactTenantGrant(auth,"tenant.view",tenantID,"staging") { t.Fatal("tenant.view should cover its exact tenant") }
    for _, key := range []string{"market.view","subscription.view","digital-estate.view"} {
        if exactTenantGrant(auth,key,tenantID,"staging") {
            t.Fatalf("foreign/broad %s must not grant tenant section read", key)
        }
    }
    auth.Grants=append(auth.Grants,administration.EffectiveGrant{Permission:"market.view",
        Scope:administration.Scope{Level:administration.LevelTenant,TenantID:tenantID,Environment:"staging"}})
    if !exactTenantGrant(auth,"market.view",tenantID,"staging") ||
        exactTenantGrant(auth,"market.view",tenantID,"production") {
        t.Fatal("environment-scoped tenant permission mismatch")
    }
}
