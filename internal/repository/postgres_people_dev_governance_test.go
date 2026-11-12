package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/store/postgres"
)

func TestPeopleDevGovernanceNoSyntheticCertificationPG17(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires real PostgreSQL 17 TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}

	repo, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)

	tenant := "tn_" + domain.NewUUIDv7()[:8]
	req := PeopleDevChoiceRequest{
		TenantID: tenant, LegalEmployerID: "TEST-DEV-EMPLOYER", Jurisdiction: "ZA",
		CapabilityKey: "payroll.run.read", BindingID: domain.NewUUIDv7(),
		CertificationID: domain.NewUUIDv7(),
		EvidenceRef:     "custody/independent/source-evidence",
		EffectiveFrom:   time.Now().UTC(),
	}
	// No connected release, independent EA-09 certificate, current human
	// principal and employer-specific binding exists. Under no condition may
	// this command invent one or activate the canonical DRAFT capability.
	_, err = repo.ProposePeopleDevChoice(ctx, req, domain.NewUUIDv7(), time.Now().UTC())
	if !errors.Is(err, ErrPeopleDevChoiceDenied) {
		t.Fatalf("unqualified People proposal not refused: %v", err)
	}
	_, err = repo.ApprovePeopleDevChoice(ctx, domain.NewUUIDv7(), 1, domain.NewUUIDv7(), time.Now().UTC())
	if !errors.Is(err, ErrPeopleDevChoiceDenied) {
		t.Fatalf("unknown choice approved: %v", err)
	}
	var total int
	if err = repo.pool.QueryRow(ctx, "SELECT count(*) FROM capability.people_provider_choice WHERE tenant_id=$1", tenant).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatal("denied development request persisted a provider choice")
	}
	var status string
	if err = repo.pool.QueryRow(ctx, "SELECT status FROM capability.capability WHERE code='payroll.run.read'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "DRAFT" {
		t.Fatal("proposed People selection silently promoted canonical capability")
	}
}
