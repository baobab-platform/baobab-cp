package api

import (
    "testing"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/repository"
)
func TestCX04CGovernanceReadinessNeverInfersCertification(t *testing.T) {
    now:=time.Date(2026,10,12,10,0,0,0,time.UTC)
    tenant:="tn_cx04c"
    none:=buildGovernanceReadiness(tenant,now,nil)
    if none.State!="UNKNOWN" || none.Source!="NO_SNAPSHOT" || none.OverallReady {
        t.Fatalf("no evidence may not be ready: %+v",none)
    }
    snap:=&repository.ProvisioningReadinessFact{
        EvaluatedAt:now.Add(-25*time.Hour),OverallReady:true,
        DesiredStateVersion:7,ObservedStateVersion:7,BlockingReasons:[]string{},
    }
    stale:=buildGovernanceReadiness(tenant,now,snap)
    if stale.State!="STALE" || stale.Source!="IMMUTABLE_PROVISIONING_SNAPSHOT" {
        t.Fatalf("old success must be stale: %+v",stale)
    }
    snap.EvaluatedAt=now.Add(-time.Hour)
    snap.BlockingReasons=[]string{"provider missing attested response"}
    blocked:=buildGovernanceReadiness(tenant,now,snap)
    if blocked.State!="BLOCKED" { t.Fatalf("blocking evidence ignored: %+v",blocked) }
    snap.BlockingReasons=[]string{}
    snap.ObservedStateVersion=6
    if got:=buildGovernanceReadiness(tenant,now,snap);got.State!="BLOCKED" {
        t.Fatalf("revision mismatch allowed readiness: %+v",got)
    }
    snap.ObservedStateVersion=7
    if got:=buildGovernanceReadiness(tenant,now,snap);got.State!="READY" {
        t.Fatalf("fresh converged success rejected: %+v",got)
    }
    snap.EvaluatedAt=now.Add(2*time.Minute)
    if got:=buildGovernanceReadiness(tenant,now,snap);got.State!="STALE" {
        t.Fatalf("future-dated evidence is not current: %+v",got)
    }
}
