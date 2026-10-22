package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

// TestINT03CGovernedTwoHumanPrincipalDeactivation uses DB fixtures ONLY as
// a test of SQL trigger/transaction semantics, NEVER as evidence of actual
// IAM issuance or CP-governed acceptance. Its audit rows are immutable, so
// the entire database must be disposable. No persistent test DB is mutated.
func TestINT03CGovernedTwoHumanPrincipalDeactivation(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || os.Getenv("BAOBAB_INT03C_DISPOSABLE_DB") != "1" {
		t.Skip("explicit disposable PostgreSQL 17 fixture database not configured")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	maker := domain.NewPrincipalID()
	checker := domain.NewPrincipalID()
	target := domain.NewPrincipalID()
	for _, user := range []struct{ id, actor string }{
		{maker, "human"}, {checker, "human"}, {target, "workload"},
	} {
		if err := r.CreateIdentity(ctx, domain.Principal{
			ID: user.id, ActorType: user.actor, Status: "ACTIVE",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.LinkExternalIdentity(ctx, domain.ExternalIdentity{
		ID: domain.NewExternalIdentityID(), PrincipalID: target,
		Issuer: int03CIssuer, Subject: "baobab-int03c-issuer-owner",
		ProviderType: "ory-hydra", Status: "ACTIVE",
	}); err != nil {
		t.Fatal(err)
	}
	run := strings.Repeat("b", 32)
	sha := strings.Repeat("a", 40)
	evidence := "sha256:" + strings.Repeat("c", 64)
	key := domain.NewPrincipalID()
	if _, err := r.ProposeInt03C(ctx, target, int03CIssuer,
		"baobab-int03c-issuer-owner", run, sha, maker, key); !errors.Is(err, ErrInt03CNotPermitted) {
		t.Fatalf("source-only nonce bypassed provenance: %v", err)
	}
	p, err := r.ProposeInt03CProvenance(ctx, target, int03CIssuer,
		"baobab-int03c-issuer-owner", run, sha, evidence, maker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApproveInt03CProvenance(ctx, p.ProvenanceID, maker, evidence); !errors.Is(err, ErrInt03CNotPermitted) {
		t.Fatalf("self-checker approved provenance: %v", err)
	}
	if _, err := r.ApproveInt03CProvenance(ctx, p.ProvenanceID, checker,
		"sha256:"+strings.Repeat("d", 64)); !errors.Is(err, ErrInt03CNotPermitted) {
		t.Fatalf("incorrect original proof accepted: %v", err)
	}
	approved, err := r.ApproveInt03CProvenance(ctx, p.ProvenanceID, checker, evidence)
	if err != nil || approved.Status != "INDEPENDENTLY_APPROVED_UNATTESTED" {
		t.Fatalf("independent CP provenance approval: %v %+v", err, approved)
	}
	plan, err := r.ProposeInt03C(ctx, target, int03CIssuer,
		"baobab-int03c-issuer-owner", run, sha, maker, key)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := r.ProposeInt03C(ctx, target, int03CIssuer,
		"baobab-int03c-issuer-owner", run, sha, maker, key)
	if err != nil || replayed.RequestID != plan.RequestID || replayed.PlanDigest != plan.PlanDigest {
		t.Fatalf("immutable replay changed request: %v", err)
	}
	if _, err := r.ApproveInt03C(ctx, plan.RequestID, plan.PlanDigest, maker); !errors.Is(err, ErrInt03CNotPermitted) {
		t.Fatalf("self-checker approved deactivation: %v", err)
	}
	done, err := r.ApproveInt03C(ctx, plan.RequestID, plan.PlanDigest, checker)
	if err != nil || done.Decision != "COMPLETED" {
		t.Fatalf("atomic canonical disable: %v %+v", err, done)
	}
	status, err := r.ReadInt03CPrincipal(ctx, target)
	if err != nil || status != "DISABLED" {
		t.Fatalf("canonical Principal remains active: %v %s", err, status)
	}
	if _, err := r.ApproveInt03C(ctx, plan.RequestID, plan.PlanDigest, checker); !errors.Is(err, ErrInt03CNotPermitted) {
		t.Fatalf("terminal deactivation could be replayed: %v", err)
	}
}
