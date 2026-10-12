package api

// CX-05: server-authoritative search and cursor paging. Candidate IDs come
// only from current EXACT effective grants, never client-supplied identity
// filters or corporate parenthood. No persisted page cursor can grant access.

import (
    "net/http"
    "net/url"
    "slices"
    "sort"
    "strconv"
    "strings"
    "time"
    "unicode/utf8"

    "github.com/baobab-platform/baobab-cp/internal/administration"
    "github.com/baobab-platform/baobab-cp/internal/domain"
)

const maxPagedDirectoryRows = 500

type directoryPageFilter struct {
    Query, Sort, Verification, Status, After string
    Limit int
}
type directoryPageResponse struct {
    EvaluatedAt time.Time `json:"evaluated_at"`
    PageSize int `json:"page_size"`
    Sort string `json:"sort"`
    TotalVisible int `json:"total_visible"`
    Items []organisationDirectoryEntry `json:"items"`
    NextAfter string `json:"next_after,omitempty"`
}

func parseDirectoryPageQuery(values url.Values) (directoryPageFilter, bool) {
    filter := directoryPageFilter{Sort:"name-asc", Limit:25}
    allowed := map[string]bool{"q":true,"sort":true,"verification":true,"status":true,"after":true,"limit":true}
    for k,v := range values {
        if !allowed[k] || len(v)!=1 { return directoryPageFilter{}, false }
    }
    filter.Query = strings.TrimSpace(values.Get("q"))
    if !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query)>80 { return directoryPageFilter{}, false }
    if v:=values.Get("sort"); v!="" { filter.Sort=v }
    if filter.Sort!="name-asc" && filter.Sort!="name-desc" { return directoryPageFilter{},false }
    filter.Verification=values.Get("verification")
    if filter.Verification!="" && !slices.Contains([]string{
        "UNVERIFIED","PENDING_REVIEW","VERIFIED","CONFLICTED","REJECTED","EXPIRED",
    },filter.Verification) { return directoryPageFilter{},false }
    filter.Status=values.Get("status")
    if len(filter.Status)>40 { return directoryPageFilter{},false }
    for _,ch:=range filter.Status { if ch<'A' || ch>'Z' { if ch!='_' { return directoryPageFilter{},false } } }
    filter.After=values.Get("after")
    if filter.After!="" && !domain.IsUUID(filter.After) { return directoryPageFilter{},false }
    if v:=values.Get("limit");v!="" {
        n,err:=strconv.Atoi(v)
        if err!=nil || n<1 || n>50 { return directoryPageFilter{},false }
        filter.Limit=n
    }
    return filter,true
}

func pageDirectoryEntries(input []organisationDirectoryEntry, f directoryPageFilter) (directoryPageResponse, bool) {
    matched:=make([]organisationDirectoryEntry,0)
    for _, item:=range input {
        if f.Query!="" && !strings.Contains(strings.ToLower(item.DisplayName), strings.ToLower(f.Query)) { continue }
        if f.Verification!="" && string(item.VerificationState)!=f.Verification { continue }
        if f.Status!="" && item.Status!=f.Status { continue }
        matched=append(matched,item)
    }
    sort.Slice(matched,func(i,j int) bool {
        left,right:=strings.ToLower(matched[i].DisplayName),strings.ToLower(matched[j].DisplayName)
        if left==right { return matched[i].OrganisationID<matched[j].OrganisationID }
        if f.Sort=="name-desc" { return left>right }
        return left<right
    })
    start:=0
    if f.After!="" {
        at:=slices.IndexFunc(matched,func(x organisationDirectoryEntry) bool {
            return strings.EqualFold(x.OrganisationID,f.After)
        })
        if at<0 { return directoryPageResponse{},false }
        start=at+1
    }
    end:=start+f.Limit
    if end>len(matched) { end=len(matched) }
    out:=directoryPageResponse{PageSize:f.Limit,Sort:f.Sort,TotalVisible:len(matched),
        Items:append([]organisationDirectoryEntry{},matched[start:end]...)}
    if end<len(matched) && end>start { out.NextAfter=matched[end-1].OrganisationID }
    return out,true
}

func (h organisationOverviewHandler) directoryPage(w http.ResponseWriter,r *http.Request) {
    w.Header().Set("Cache-Control","private, no-store")
    f,ok:=parseDirectoryPageQuery(r.URL.Query())
    if !ok {
        problem(w,r,http.StatusBadRequest,"INVALID_DIRECTORY_PAGE","invalid directory filters or cursor",false)
        return
    }
    if h.reader==nil || h.grants==nil || h.identities==nil {
        problem(w,r,http.StatusServiceUnavailable,"DIRECTORY_UNAVAILABLE","authoritative sources unavailable",true)
        return
    }
    actor,_,ok:=resolveActor(w,r,h.identities,false)
    if !ok { return }
    principal,err:=h.identities.GetPrincipal(r.Context(),actor)
    if err!=nil || principal.Status!="ACTIVE" {
        problem(w,r,http.StatusForbidden,"DIRECTORY_DENIED","caller unavailable",false)
        return
    }
    grants,sources,err:=h.grants.AdministrativeGrantsOf(r.Context(),actor)
    if err!=nil {
        problem(w,r,http.StatusServiceUnavailable,"DIRECTORY_AUTHORITY_UNAVAILABLE","grants unavailable",true)
        return
    }
    at:=time.Now().UTC()
    if h.now!=nil { at=h.now() }
    scopes:=make([]administration.Scope,0,len(grants)+len(sources))
    for _,g:=range grants { scopes=append(scopes,g.Scope) }
    for _,g:=range sources { scopes=append(scopes,g.Scope) }
    rel,err:=h.grants.EffectiveRelations(r.Context(),administration.TenantsOf(scopes...),at)
    if err!=nil {
        problem(w,r,http.StatusServiceUnavailable,"DIRECTORY_AUTHORITY_UNAVAILABLE","relations unavailable",true)
        return
    }
    ids,valid:=boundedDirectoryIDs(administration.Effective(actor,grants,sources,at,rel),h.environment,maxPagedDirectoryRows)
    if !valid {
        problem(w,r,http.StatusServiceUnavailable,"DIRECTORY_TOO_LARGE","bounded search corpus exceeded",true)
        return
    }
    candidates:=make([]organisationDirectoryEntry,0,len(ids))
    for _,id:=range ids {
        profile,err:=h.reader.GetOrganisation(r.Context(),id)
        if err!=nil {
            problem(w,r,http.StatusServiceUnavailable,"DIRECTORY_UNAVAILABLE","registry unavailable",true)
            return
        }
        if profile==nil { continue }
        if !strings.EqualFold(profile.CanonicalEntityID,id) {
            problem(w,r,http.StatusServiceUnavailable,"DIRECTORY_INCONSISTENT","registry mismatch",true)
            return
        }
        candidates=append(candidates,organisationDirectoryEntry{
            OrganisationID:id,DisplayName:profile.DisplayName,Status:profile.Status,
            VerificationState:profile.VerificationState,SourceAuthority:profile.SourceAuthority,
            Jurisdiction:profile.Jurisdiction,
        })
    }
    response,valid:=pageDirectoryEntries(candidates,f)
    if !valid {
        problem(w,r,http.StatusConflict,"DIRECTORY_CURSOR_STALE","cursor is not part of the currently authorised result",false)
        return
    }
    response.EvaluatedAt=at
    writeJSON(w,http.StatusOK,response)
}
