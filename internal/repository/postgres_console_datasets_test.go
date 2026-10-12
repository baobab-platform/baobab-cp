package repository

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

func TestCX05GDurableExportAndRevocation(t *testing.T) {
	f := newOrgFixture(t)
	if err := f.repo.SetConsoleDatasetKey(bytes.Repeat([]byte{0x42}, 32)); err != nil { t.Fatal(err) }
	id := f.organisation(t, "CX05G export fixture")
	now := time.Now().UTC().Truncate(time.Microsecond)
	maker := AuditActor{ActorID: "prn_cx05g_maker", ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	checker := AuditActor{ActorID: "prn_cx05g_checker", ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	rows, err := f.repo.SnapshotConsoleDataset(f.ctx, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := domain.DatasetSourceFingerprint(rows)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ConsoleDatasetIntent{Kind: domain.ConsoleExport, MakerPrincipalID: maker.ActorID,
		Purpose: "audited staging disclosure", RecipientReference: "review-case-cx05g",
		IdempotencyKey: "cx05g-export-" + domain.NewUUIDv7(), SourceFingerprint: hash,
		TargetIDs: []string{id}, ValidUntil: now.Add(time.Hour)}
	grants := func(principal, org, perm string) bool {
		return org == id && (principal == maker.ActorID || principal == checker.ActorID) &&
			(perm == "organisation.view" || perm == "organisation.export")
	}
	workloadMaker := AuditActor{ActorID: maker.ActorID, ActorType: "workload", CorrelationID: domain.NewUUIDv7()}
	if _, err := f.repo.ProposeConsoleDataset(f.ctx, intent, nil, workloadMaker, grants, now); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("workload maker was not rejected: %v", err)
	}
	proposed, err := f.repo.ProposeConsoleDataset(f.ctx, intent, nil, maker, grants, now)
	if err != nil || proposed.State != "PENDING_APPROVAL" {
		t.Fatalf("proposal %v %v", proposed, err)
	}
	replayed, err := f.repo.ProposeConsoleDataset(f.ctx, intent, nil, maker, grants, now)
	if err != nil || replayed.Digest != proposed.Digest {
		t.Fatalf("idempotent replay %v %v", replayed, err)
	}
	workloadChecker := AuditActor{ActorID: checker.ActorID, ActorType: "workload", CorrelationID: domain.NewUUIDv7()}
	if _, err := f.repo.ApproveConsoleDataset(f.ctx, proposed.Digest, "case-cx05g",
		workloadChecker, grants, now); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("workload checker was not rejected: %v", err)
	}
	if _, err := f.repo.ApproveConsoleDataset(f.ctx, proposed.Digest, "case-cx05g",
		maker, grants, now); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("self approval was not denied: %v", err)
	}
	if _, err := f.repo.ExecuteConsoleDataset(f.ctx, proposed.Digest,
		checker, grants, now); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("unapproved execution was not denied: %v", err)
	}
	if _, err := f.repo.ApproveConsoleDataset(f.ctx, proposed.Digest, "case-cx05g",
		checker, grants, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ExecuteConsoleDataset(f.ctx, proposed.Digest,
		checker, grants, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ExecuteConsoleDataset(f.ctx, proposed.Digest,
		checker, grants, now); err != nil {
		t.Fatal("completed plan should be idempotent:", err)
	}
	data, err := f.repo.ReadConsoleDatasetExport(f.ctx, proposed.Digest, maker, grants, now)
	if err != nil || !bytes.Contains(data, []byte("CX05G export fixture")) {
		t.Fatalf("read redacted CSV %q: %v", data, err)
	}
	revoked := func(principal, org, perm string) bool {
		return principal != maker.ActorID && grants(principal, org, perm)
	}
	if _, err := f.repo.ReadConsoleDatasetExport(f.ctx, proposed.Digest,
		maker, revoked, now); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("revoked disclosure not denied: %v", err)
	}
	if _, err := f.repo.ReadConsoleDatasetExport(f.ctx, proposed.Digest,
		maker, grants, now.Add(2*time.Hour)); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("expired disclosure not denied: %v", err)
	}
}

func TestCX05GBulkAtomicRevisionAndIndependentApproval(t *testing.T) {
	f := newOrgFixture(t)
	first := f.organisation(t, "CX05G bulk first")
	second := f.organisation(t, "CX05G bulk second")
	ids := []string{first, second}
	now := time.Now().UTC().Truncate(time.Microsecond)
	maker := AuditActor{ActorID: "prn_cx05g_bulk_maker", ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	checker := AuditActor{ActorID: "prn_cx05g_bulk_checker", ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	rows, err := f.repo.SnapshotConsoleDataset(f.ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := domain.DatasetSourceFingerprint(rows)
	if err != nil {
		t.Fatal(err)
	}
	changes := []domain.ConsoleMetadataChange{
		{OrganisationID: first, InternalLabel: "Review A", ExpectedVersion: 1},
		{OrganisationID: second, InternalLabel: "Review B", ExpectedVersion: 1},
	}
	changeHash, err := domain.BulkChangeFingerprint(changes)
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.ConsoleDatasetIntent{Kind: domain.ConsoleBulk, MakerPrincipalID: maker.ActorID,
		Action: "organisation.metadata.update", Purpose: "approved internal labels",
		RecipientReference: "case-cx05g-bulk", IdempotencyKey: "cx05g-bulk-" + domain.NewUUIDv7(),
		SourceFingerprint: hash, ChangeFingerprint: changeHash,
		TargetIDs: ids, ValidUntil: now.Add(time.Hour)}
	grants := func(principal, org, perm string) bool {
		if org != first && org != second {
			return false
		}
		if principal == maker.ActorID {
			return perm == "organisation.view" || perm == "organisation.bulk.propose" ||
				perm == "organisation.manage"
		}
		return principal == checker.ActorID &&
			(perm == "organisation.view" || perm == "organisation.bulk.approve")
	}
	proposal, err := f.repo.ProposeConsoleDataset(f.ctx, intent, changes, maker, grants, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ApproveConsoleDataset(f.ctx, proposal.Digest,
		"case-cx05g-bulk", checker, grants, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ExecuteConsoleDataset(f.ctx, proposal.Digest, checker,
		func(principal, org, perm string) bool { return org != second && grants(principal, org, perm) }, now); !errors.Is(err, ErrConsoleDatasetDenied) {
		t.Fatalf("foreign target grant not denied: %v", err)
	}
	var initial int64
	if err := f.admin.QueryRow(f.ctx, `SELECT console_metadata_version FROM registry.organisation_profile
		WHERE canonical_entity_id=$1::uuid`, first).Scan(&initial); err != nil {
		t.Fatal(err)
	}
	if initial != 1 {
		t.Fatalf("partial bulk update applied: %d", initial)
	}
	if _, err := f.repo.ExecuteConsoleDataset(f.ctx, proposal.Digest,
		checker, grants, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		var revision int64
		var label string
		if err := f.admin.QueryRow(context.Background(), `SELECT console_metadata_version,
			metadata->>'console_internal_label' FROM registry.organisation_profile
			WHERE canonical_entity_id=$1::uuid`, id).Scan(&revision, &label); err != nil {
			t.Fatal(err)
		}
		if revision != 2 || label == "" {
			t.Fatalf("missing atomic update %s revision=%d label=%s", id, revision, label)
		}
	}
}
