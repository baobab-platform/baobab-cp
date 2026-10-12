package api

// CX-04C: separate privilege boundaries for organisational identity,
// operating legal-actor mandates, and immutable provisioning readiness.
import (
    "context"
    "net/http"
    "strings"
    "time"

    "github.com/baobab-platform/baobab-cp/internal/administration"
    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/repository"
    "github.com/go-chi/chi/v5"
)

type governanceEvidenceReader interface {
    ReadOrganisationMandateFacts(context.Context,string,string) ([]repository.OrganisationMandateFact,error)
    LatestTenantReadinessFact(context.Context,string) (*repository.ProvisioningReadinessFact,error)
}

type organisationGovernanceHandler struct {
    overview organisationOverviewReader
    evidence governanceEvidenceReader
    grants repository.AdministrativeGrantReader
    identities repository.IdentityRepository
    environment string
    now func() time.Time
}

type governanceReadiness struct {
    TenantID string `json:"tenant_id"`
    State string `json:"state"`
    Source string `json:"source"`
    EvaluatedAt time.Time `json:"evaluated_at"`
    OverallReady bool `json:"overall_ready"`
    DesiredStateVersion int64 `json:"desired_state_version"`
    ObservedStateVersion int64 `json:"observed_state_version"`
    BlockingReasons []string `json:"blocking_reasons"`
}
type governanceTenant struct {
    TenantID string `json:"tenant_id"`
    MandatesVisible bool `json:"mandates_visible"`
    ReadinessVisible bool `json:"readiness_visible"`
    Mandates []repository.OrganisationMandateFact `json:"mandates"`
    Readiness *governanceReadiness `json:"readiness,omitempty"`
}
type governanceResponse struct {
    OrganisationID string `json:"organisation_id"`
    EvaluatedAt time.Time `json:"evaluated_at"`
    Tenants []governanceTenant `json:"tenants"`
}

func buildGovernanceReadiness(tenantID string, at time.Time, snap *repository.ProvisioningReadinessFact) governanceReadiness {
    result:=governanceReadiness{TenantID:tenantID,State:"UNKNOWN",Source:"NO_SNAPSHOT",
        EvaluatedAt:at,BlockingReasons:[]string{}}
    if snap==nil { return result }
    result.Source="IMMUTABLE_PROVISIONING_SNAPSHOT"
    result.EvaluatedAt=snap.EvaluatedAt
    result.DesiredStateVersion=snap.DesiredStateVersion
    result.ObservedStateVersion=snap.ObservedStateVersion
    result.OverallReady=snap.OverallReady
    result.BlockingReasons=snap.BlockingReasons
    if snap.EvaluatedAt.After(at.Add(time.Minute)) || snap.EvaluatedAt.Before(at.Add(-24*time.Hour)) {
        result.State="STALE"
    } else if snap.OverallReady && snap.DesiredStateVersion==snap.ObservedStateVersion && len(snap.BlockingReasons)==0 {
        result.State="READY"
    } else {
        result.State="BLOCKED"
    }
    return result
}

func (h organisationGovernanceHandler) get(w http.ResponseWriter,r *http.Request) {
    w.Header().Set("Cache-Control","private, no-store")
    id:=chi.URLParam(r,"organisationID")
    if !domain.IsUUID(id) || len(r.URL.Query())!=0 {
        problem(w,r,http.StatusBadRequest,"INVALID_ORGANISATION_REFERENCE","invalid organisation scope",false)
        return
    }
    if h.overview==nil || h.evidence==nil || h.grants==nil || h.identities==nil {
        problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_UNAVAILABLE","governance sources unavailable",true)
        return
    }
    actor,_,ok:=resolveActor(w,r,h.identities,false)
    if !ok { return }
    principal,err:=h.identities.GetPrincipal(r.Context(),actor)
    if err!=nil || principal.Status!="ACTIVE" {
        problem(w,r,http.StatusForbidden,"GOVERNANCE_DENIED","caller unavailable",false)
        return
    }
    grants,sources,err:=h.grants.AdministrativeGrantsOf(r.Context(),actor)
    if err!=nil {
        problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_AUTHORITY_UNAVAILABLE","permissions unavailable",true)
        return
    }
    at:=time.Now().UTC()
    if h.now!=nil { at=h.now() }
    scopes:=make([]administration.Scope,0,len(grants)+len(sources))
    for _,g:=range grants { scopes=append(scopes,g.Scope) }
    for _,g:=range sources { scopes=append(scopes,g.Scope) }
    rel,err:=h.grants.EffectiveRelations(r.Context(),administration.TenantsOf(scopes...),at)
    if err!=nil {
        problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_AUTHORITY_UNAVAILABLE","relations unavailable",true)
        return
    }
    authority:=administration.Effective(actor,grants,sources,at,rel)
    allowed:=false
    for _,grant:=range authority.Grants {
        scope:=grant.Scope
        if grant.Permission=="organisation.view" && scope.Level==administration.LevelOrganisation &&
            (scope.Mode=="" || scope.Mode==administration.ModeExact) &&
            strings.EqualFold(scope.OrganisationID,id) &&
            (scope.Environment=="" || scope.Environment==h.environment) { allowed=true;break }
    }
    if !allowed {
        problem(w,r,http.StatusNotFound,"ORGANISATION_NOT_FOUND","organisation unavailable",false)
        return
    }
    profile,err:=h.overview.GetOrganisation(r.Context(),id)
    if err!=nil || profile==nil || !strings.EqualFold(profile.CanonicalEntityID,id) {
        problem(w,r,http.StatusNotFound,"ORGANISATION_NOT_FOUND","organisation unavailable",false)
        return
    }
    tenantIDs,err:=h.overview.ListPrimaryTenantIDsForOrganisation(r.Context(),id,at)
    if err!=nil || len(tenantIDs)>maxOverviewRows {
        problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","tenant mapping unavailable or oversized",true)
        return
    }
    response:=governanceResponse{OrganisationID:id,EvaluatedAt:at,Tenants:[]governanceTenant{}}
    seen:=map[string]bool{}
    for _,tenantID:=range tenantIDs {
        if seen[tenantID] || !domain.ValidTenantID(tenantID) {
            problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","duplicate or malformed tenant mapping",true)
            return
        }
        seen[tenantID]=true
        if !exactTenantGrant(authority,"tenant.view",tenantID,h.environment) { continue }
        mappings,err:=h.overview.ListTenantOrganisationMappings(r.Context(),tenantID,at)
        if err!=nil {
            problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","tenant mapping source unavailable",true)
            return
        }
        primary:=false
        for _,m:=range mappings {
            if strings.EqualFold(m.OrganisationID,id) && m.MappingRole=="PRIMARY_ORGANISATION" && m.InEffect(at) {
                primary=true;break
            }
        }
        if !primary {
            problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","stale primary mapping",true)
            return
        }
        item:=governanceTenant{TenantID:tenantID,Mandates:[]repository.OrganisationMandateFact{}}
        if exactTenantGrant(authority,"legal-actor-mandate.view",tenantID,h.environment) {
            rows,err:=h.evidence.ReadOrganisationMandateFacts(r.Context(),id,tenantID)
            if err!=nil || len(rows)>maxOverviewRows {
                problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","mandate source unavailable",true)
                return
            }
            for _,row:=range rows {
                if row.TenantID!=tenantID {
                    problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","mandate tenant mismatch",true)
                    return
                }
            }
            item.Mandates=rows
            item.MandatesVisible=true
        }
        if exactTenantGrant(authority,"readiness.view",tenantID,h.environment) {
            snapshot,err:=h.evidence.LatestTenantReadinessFact(r.Context(),tenantID)
            if err!=nil {
                problem(w,r,http.StatusServiceUnavailable,"GOVERNANCE_READBACK_INCOMPLETE","readiness evidence unavailable",true)
                return
            }
            readiness:=buildGovernanceReadiness(tenantID,at,snapshot)
            item.Readiness=&readiness
            item.ReadinessVisible=true
        }
        response.Tenants=append(response.Tenants,item)
    }
    writeJSON(w,http.StatusOK,response)
}
