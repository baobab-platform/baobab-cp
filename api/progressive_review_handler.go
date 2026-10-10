// PEO-03 staff review visibility. No decision, admission, tenant authority,
// legal verification or subscription classification is created by these reads.
package api

import (
    "context"
    "net/http"
    "strconv"

    "github.com/baobab-platform/baobab-cp/internal/store/postgres"
    "github.com/go-chi/chi/v5"
)

type progressiveReviewReader interface {
    GetProgressiveApplicationForReview(context.Context,string,string) (postgres.ProgressiveApplicantDraft,error)
    ListProgressiveApplicationsForReview(context.Context,string,int) ([]postgres.ProgressiveApplicantDraft,error)
}

func (h progressiveApplicantHandler) reviewApplicant(w http.ResponseWriter,r *http.Request) (string,bool) {
    // Unlike applicant creation, privileged access never JIT-creates a staff
    // principal. Router also enforces human, admission:review and CP admin.
    actor,_,ok:=resolveActor(w,r,h.api.identities,false)
    return actor,ok
}

func (h progressiveApplicantHandler) reviewGet(w http.ResponseWriter,r *http.Request) {
    actor,ok:=h.reviewApplicant(w,r)
    if !ok{return}
    reader,ok:=h.repo.(progressiveReviewReader)
    if !ok{
        problem(w,r,http.StatusServiceUnavailable,"REVIEW_UNAVAILABLE",
            "progressive application review is not configured",true)
        return
    }
    app,err:=reader.GetProgressiveApplicationForReview(r.Context(),actor,chi.URLParam(r,"applicationID"))
    if err!=nil{h.failed(w,r,err);return}
    w.Header().Set("Cache-Control","no-store")
    writeJSON(w,http.StatusOK,app)
}

func (h progressiveApplicantHandler) reviewList(w http.ResponseWriter,r *http.Request) {
    actor,ok:=h.reviewApplicant(w,r)
    if !ok{return}
    reader,ok:=h.repo.(progressiveReviewReader)
    if !ok{
        problem(w,r,http.StatusServiceUnavailable,"REVIEW_UNAVAILABLE",
            "progressive application review is not configured",true)
        return
    }
    limit:=25
    if raw:=r.URL.Query().Get("limit");raw!=""{
        n,err:=strconv.Atoi(raw)
        if err!=nil || n<1 || n>100 {
            problem(w,r,http.StatusBadRequest,"INVALID_PAGE_LIMIT","limit must be between 1 and 100",false)
            return
        }
        limit=n
    }
    apps,err:=reader.ListProgressiveApplicationsForReview(r.Context(),actor,limit)
    if err!=nil{h.failed(w,r,err);return}
    w.Header().Set("Cache-Control","no-store")
    writeJSON(w,http.StatusOK,apps)
}
