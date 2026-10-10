package api

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

type inertProgressiveReviewReader struct { inertProgressiveWriter }
func (inertProgressiveReviewReader) GetProgressiveApplicationForReview(
    context.Context,string,string,
) (postgres.ProgressiveApplicantDraft,error) {
    return postgres.ProgressiveApplicantDraft{},errors.New("unauthorised direct review invocation")
}
func (inertProgressiveReviewReader) ListProgressiveApplicationsForReview(
    context.Context,string,int,
) ([]postgres.ProgressiveApplicantDraft,error) {
    return nil,errors.New("unauthorised direct review list")
}

func TestPEO03ReviewReadRoutesFailClosed(t *testing.T) {
    paths:=[]string{
        "/v2/admin/client-applications",
        "/v2/admin/client-applications/capp_0190a1b2c3d4e5f60718293a4b5c6d7e",
    }
    cases:=[]struct{
        name string
        deps Dependencies
        want int
    }{
        {"disabled",Dependencies{Store:&fakeStore{}},http.StatusNotFound},
        {"production denied",Dependencies{
            Store:&fakeStore{},Environment:"production",
            ProgressiveApplicationsEnabled:true,ProgressiveApplications:inertProgressiveReviewReader{},
        },http.StatusNotFound},
        {"unknown environment denied",Dependencies{
            Store:&fakeStore{},Environment:"PRODUCTION",
            ProgressiveApplicationsEnabled:true,ProgressiveApplications:inertProgressiveReviewReader{},
        },http.StatusNotFound},
        {"not mounted if store cannot review",Dependencies{
            Store:&fakeStore{},Environment:"staging",
            ProgressiveApplicationsEnabled:true,ProgressiveApplications:inertProgressiveWriter{},
        },http.StatusNotFound},
        {"staging rejects invalid bearer",Dependencies{
            Store:&fakeStore{},Environment:"staging",
            AdminVerifier:fakeVerifier{err:errors.New("invalid bearer")},
            ProgressiveApplicationsEnabled:true,ProgressiveApplications:inertProgressiveReviewReader{},
        },http.StatusUnauthorized},
    }
    for _,tc:=range cases{
        t.Run(tc.name,func(t *testing.T){
            api:=New(tc.deps)
            for _,path:=range paths{
                req:=httptest.NewRequest(http.MethodGet,path,nil)
                req.Header.Set("Authorization","Bearer invalid")
                w:=httptest.NewRecorder()
                api.ServeHTTP(w,req)
                if w.Code!=tc.want { t.Errorf("%s returned %d want %d: %s",path,w.Code,tc.want,w.Body.String()) }
            }
        })
    }
}
