package organisation

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"testing"
	"time"
	"strings"

	"github.com/baobab-platform/baobab-cp/internal/domain"

	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The test is synthetic. It demonstrates the schema, signed proof and
// restricted history; it establishes NO factual ownership of any company.
func TestPEO04APostgresIndependentControlEvidenceIsImmutable(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn==""{t.Skip("disposable PostgreSQL 17 TEST_DATABASE_URL required")}
	ctx:=context.Background()
	db,err:=postgres.Open(ctx,dsn)
	if err!=nil{t.Fatal(err)}
	defer db.Close()
	if err=db.ApplyMigrations(ctx);err!=nil{t.Fatal(err)}
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil{t.Fatal(err)}
	defer pool.Close()
	reconciler,_,_,private:=setupPEO04AFixture(t)
	proof:=reconciler.Proofs.(foundingProofFixture).proof
	// Every test run owns an isolated synthetic proof chain.
	proof.RelationshipID="crel_"+strings.ReplaceAll(domain.NewUUIDv7(),"-","")
	proof.Signature=ed25519.Sign(private,proof.signingPayload())
	trust:=reconciler.Trust
	store:=PostgresControlProofStore{Pool:pool,Trust:trust,Now:reconciler.Now}
	approval:=IndependentProofApproval{MakerID:"prn_synthetic_independent_maker",
		CheckerID:"prn_synthetic_independent_checker",
		ApprovalReference:"synthetic-control-approval-01",ApprovedAt:reconciler.Now().Add(-time.Minute)}
	if err=store.Record(ctx,proof,approval);err!=nil{t.Fatal(err)}
	if err=store.Record(ctx,proof,approval);err!=nil{t.Fatalf("idempotent record rejected: %v",err)}
	got,err:=store.ReadIndependentControlProof(ctx,proof.RelationshipID)
	if err!=nil||ControlProofFingerprint(got)!=ControlProofFingerprint(proof){
		t.Fatalf("signed snapshot unrecoverable: %v",err)
	}
	wrong:=approval
	wrong.CheckerID=wrong.MakerID
	if err=store.Record(ctx,proof,wrong);!errors.Is(err,ErrUntrustedCorporateProof){
		t.Fatalf("self-approved control accepted: %v",err)
	}
	wrong=approval
	wrong.CheckerID="prn_another_checker"
	if err=store.Record(ctx,proof,wrong);!errors.Is(err,ErrUntrustedCorporateProof){
		t.Fatalf("approval on replay rewritten: %v",err)
	}
	spoof:=proof
	spoof.ChildRegistration="synthetic_forged_company"
	if err=store.Record(ctx,spoof,approval);!errors.Is(err,ErrUntrustedCorporateProof){
		t.Fatalf("forged proof signature accepted: %v",err)
	}
	var stored int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM evidence.founding_control_attestation
		WHERE relationship_id=$1`,proof.RelationshipID).Scan(&stored);err!=nil||stored!=1{
		t.Fatalf("unexpected immutable attestation count: %d %v",stored,err)
	}
	if _,err=pool.Exec(ctx,`UPDATE evidence.founding_control_attestation
		SET maker_principal='forged' WHERE relationship_id=$1`,proof.RelationshipID);err==nil{
		t.Fatal("immutability bypassed by SQL UPDATE")
	}
	if _,err=pool.Exec(ctx,`DELETE FROM evidence.founding_control_attestation WHERE relationship_id=$1`,proof.RelationshipID);err==nil{
		t.Fatal("signed control evidence could be deleted by runtime")
	}
	// Independently signed changed ownership, while old attestation remains
	// current, must BLOCK as ambiguous until a governed supersession.
	other:=proof
	other.ObservedAt=other.ObservedAt.Add(time.Second)
	other.ExpiresAt=other.ExpiresAt.Add(time.Hour)
	other.Signature=ed25519.Sign(private,other.signingPayload())
	if err=store.Record(ctx,other,approval);err!=nil{t.Fatal(err)}
	if _,err=store.ReadIndependentControlProof(ctx,proof.RelationshipID);!errors.Is(err,ErrUntrustedCorporateProof){
		t.Fatalf("conflicting active attestation silently selected: %v",err)
	}
	// The newest approved proof may be read after the earlier signed proof
	// expires. The old immutable row remains for audit and recovery.
	atExpiry := proof.ExpiresAt.Add(time.Minute)
	latest := other
	latest.ExpiresAt = atExpiry.Add(30 * time.Minute)
	latest.ObservedAt = proof.ExpiresAt
	latest.Signature = ed25519.Sign(private, latest.signingPayload())
	// Use a freshly accepted trust interval for the synthetic renewal.
	approvedTrust := trust[latest.SignerID]
	approvedTrust.ExpiresAt = atExpiry.Add(2 * time.Hour)
	approvedTrust.ApprovedAt = atExpiry.Add(-time.Minute)
	renewer := PostgresControlProofStore{Pool: pool,
		Trust: map[string]IndependentControlTrust{latest.SignerID: approvedTrust},
		Now: func() time.Time { return atExpiry }}
	if current, readErr := renewer.ReadIndependentControlProof(ctx, proof.RelationshipID); readErr != nil ||
		ControlProofFingerprint(current) != ControlProofFingerprint(other) {
		t.Fatalf("expired historical proof should not block live renewal: %v", readErr)
	}
	// Previous overlapping proof is expired but 'other' remains active;
	// this test also ensures the newest live proof does not silently win.
	if err = renewer.Record(ctx, latest, IndependentProofApproval{
		MakerID: approval.MakerID, CheckerID: approval.CheckerID,
		ApprovalReference: "synthetic-control-renewal-approval",
		ApprovedAt: atExpiry.Add(-time.Minute),
	}); err != nil { t.Fatal(err) }
	if _, err = renewer.ReadIndependentControlProof(ctx, proof.RelationshipID); !errors.Is(err, ErrUntrustedCorporateProof) {
		t.Fatal("two live signed control versions must still be reconciled")
	}
}
