package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/baobab-platform/baobab-cp/internal/verification"
	"github.com/baobab-platform/baobab-cp/internal/verification/authority"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPEO03GPG17IndependentSARSWitnessDoesNotVerifyTaxCompliance(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {t.Skip("requires disposable PostgreSQL 17 TEST_DATABASE_URL")}
	ctx := context.Background()
	store, err := postgres.Open(ctx, dsn)
	if err != nil {t.Fatal(err)}
	defer store.Close()
	if err = store.ApplyMigrations(ctx); err != nil {t.Fatal(err)}
	repo, err := Open(ctx, dsn)
	if err != nil {t.Fatal(err)}
	defer repo.Close()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {t.Fatal(err)}
	defer db.Close()
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	caseID, claimID, evidenceID := "vcase_"+suffix, "ecl_"+suffix, "evr_"+suffix
	subject := verification.Subject{SubjectType:"ORGANISATION", SubjectID:domain.NewUUIDv7()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	vcase := verification.Case{CaseID:caseID, Subject:subject, Purpose:"COMPLIANCE_REMEDIATION",
		Status:verification.CaseVerifying, ClaimIDs:[]string{claimID}, OpenedAt:now, Version:1}
	claim := verification.Claim{ClaimID:claimID, Subject:subject, ClaimType:"TAX_COMPLIANCE_STATUS",
		Jurisdiction:"ZA", Status:verification.ClaimUnderCheck, Purpose:vcase.Purpose,
		AssertedBy:"prn_applicant", AssertedVia:"APPLICANT", AssertedAt:now,
		ClaimedValue:verification.ClaimedValue{Value:"COMPLIANT"},
		EvidenceIDs:[]string{evidenceID},Version:1}
	proof := verification.Evidence{EvidenceID:evidenceID, Subject:subject, SourceID:"esrc_applicant",
		SourceRecordReference:"synthetic/sars-witness-not-authoritative",
		ObtainedBy:"prn_applicant", Status:verification.EvidenceAvail, Purpose:vcase.Purpose,
		EvidenceType:"DOCUMENT", Classification:"CONFIDENTIAL",RetentionPolicy:"synthetic-only",
		CreatedAt:now,Version:1}
	encode:=func(v any) []byte {
		out,e:=json.Marshal(v);if e!=nil{t.Fatal(e)};return out
	}
	if _,err=db.Exec(ctx,`INSERT INTO evidence.verification_case
	 (case_id,subject_type,subject_id,status,opened_by,opened_at,document)
	 VALUES($1,'ORGANISATION',$2,'VERIFYING','prn_reviewer',$3,$4::jsonb)`,
	 caseID,subject.SubjectID,now,encode(vcase));err!=nil{t.Fatal(err)}
	if _,err=db.Exec(ctx,`INSERT INTO evidence.claim
	 (claim_id,case_id,asserted_by,status,document,created_at)
	 VALUES($1,$2,$3,'UNDER_VERIFICATION',$4::jsonb,$5)`,
	 claimID,caseID,claim.AssertedBy,encode(claim),now);err!=nil{t.Fatal(err)}
	if _,err=db.Exec(ctx,`INSERT INTO evidence.record
	 (evidence_id,source_id,status,obtained_by,document,created_at)
	 VALUES($1,'esrc_applicant','AVAILABLE','prn_applicant',$2::jsonb,$3)`,
	 evidenceID,encode(proof),now);err!=nil{t.Fatal(err)}
	witness:=AuditActor{ActorID:"prn_witness",ActorType:"human",CorrelationID:domain.NewUUIDv7()}
	checker:=AuditActor{ActorID:"prn_otherchecker",ActorType:"human",CorrelationID:domain.NewUUIDv7()}
	f:=authority.AssistedFinding{IntentID:domain.NewResourceID("asrs"),
		CaseID:caseID,ClaimID:claimID,SourceID:"esrc_sarstcs",EvidenceID:evidenceID,
		OfficialReference:"synthetic-official-query-reference",ResponseDigest:"sha256:"+strings.Repeat("a",64),
		ProvenanceRef:"evidence:"+evidenceID,ObservedAt:now.Add(-time.Minute),
		FreshnessUntil:now.Add(time.Hour),Witness:witness.ActorID,Status:"PENDING"}
	key:="synthetic-sars-human-witness-001"
	recorded,err:=repo.ProposeAssistedWitness(ctx,f,key,now,witness)
	if err!=nil||recorded.Status!="PENDING"{t.Fatalf("witness persistence: %+v %v",recorded,err)}
	if _,err=repo.DecideAssistedWitness(ctx,f.IntentID,"synthetic-self-approval", "ATTESTED",
		"OFFICIAL_PORTAL_REVIEWED",now,witness);!errors.Is(err,ErrAssistedWitnessDenied){
		t.Fatalf("original reviewer approved own SARS witness: %v",err)
	}
	decision,err:=repo.DecideAssistedWitness(ctx,f.IntentID,"synthetic-independent-approve","ATTESTED",
		"OFFICIAL_PORTAL_REVIEWED",now,checker)
	if err!=nil||decision.Status!="ATTESTED"{t.Fatalf("independent witness decision: %+v %v",decision,err)}
	var results int
	if err=db.QueryRow(ctx,"SELECT COUNT(*) FROM evidence.result WHERE claim_id=$1",claimID).Scan(&results);err!=nil{t.Fatal(err)}
	if results!=0{t.Fatal("attested portal review illegally produced VERIFIED SARS status")}
}
