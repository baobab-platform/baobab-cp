package postgres

import (
    "context"
    "errors"
    "os"
    "testing"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

func TestPEO03ReviewerSeesSubmittedOnlyAndNeverOwnApplicantData(t *testing.T) {
    url:=os.Getenv("TEST_DATABASE_URL")
    if url=="" { t.Skip("PostgreSQL 17 acceptance database not configured") }
    ctx:=context.Background()
    db,err:=Open(ctx,url)
    if err!=nil{t.Fatal(err)}
    defer db.Close()
    if err=db.ApplyMigrations(ctx);err!=nil{t.Fatal(err)}

    var applicant,reviewer string
    for _,p:=range []*string{&applicant,&reviewer}{
        if err=db.pool.QueryRow(ctx,`INSERT INTO identity.principal(actor_type)
           VALUES('human') RETURNING principal_id::text`).Scan(p);err!=nil{t.Fatal(err)}
    }
    meta:=basestore.RequestMetadata{
        ActorID:applicant,ActorType:"human",CorrelationID:domain.NewUUIDv7(),
    }
    raw:=[]byte(`{"business_identity":{
        "operating_name":"Synthetic applicant only",
        "organisation_form":"UNINCORPORATED_ORGANISATION",
        "operating_country":"ZA","incorporation_claim":"NOT_INCORPORATED",
        "authorised_representative":{"full_name":"Synthetic Applicant","role":"owner"}
    },"requested_markets":[{"country_code":"ZA"}],"requirements":{"operates_b2b":true}}`)
    app,err:=db.CreateProgressiveApplication(ctx,meta,applicant,"peo03-review-"+domain.NewUUIDv7(),raw)
    if err!=nil{t.Fatal(err)}
    if _,err=db.GetProgressiveApplicationForReview(ctx,reviewer,app.ID);
        !errors.Is(err,ErrProgressiveApplicationNotFound){
        t.Fatalf("staff can inspect private draft: %v",err)
    }
    if _,err=db.ChangeProgressiveApplication(ctx,meta,applicant,app.ID,nil,true);err!=nil{t.Fatal(err)}
    if _,err=db.GetProgressiveApplicationForReview(ctx,applicant,app.ID);
        !errors.Is(err,ErrProgressiveApplicationNotFound){
        t.Fatalf("applicant attempted to act as own reviewer: %v",err)
    }
    lookedUp,err:=db.GetProgressiveApplicationForReview(ctx,reviewer,app.ID)
    if err!=nil || lookedUp.Status!="SUBMITTED" || lookedUp.ID!=app.ID {
        t.Fatalf("review must see submitted claims only: %+v %v",lookedUp,err)
    }
    queue,err:=db.ListProgressiveApplicationsForReview(ctx,reviewer,100)
    if err!=nil{t.Fatal(err)}
    found:=false
    for _,item:=range queue { if item.ID==app.ID { found=true } }
    if !found {t.Fatal("submitted application not present in reviewer queue")}
    if _,err=db.ListProgressiveApplicationsForReview(ctx,reviewer,101);
        !errors.Is(err,ErrProgressiveApplicationConflict){
        t.Fatal("unbounded review enumeration accepted")
    }
    // A review read may not mutate the application's state or mint authority.
    after,err:=db.GetProgressiveApplication(ctx,applicant,app.ID)
    if err!=nil || after.Status!="SUBMITTED" {
        t.Fatalf("review read mutated applicant lifecycle: %+v %v",after,err)
    }
}
