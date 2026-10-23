package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

// ADR-SHARED-042 Decision 9 (ADR-BCP-031): an assignment is proposed by one
// ACTIVE human and approved by another, its scope never changes, every change
// is one governed transition by an ACTIVE human recorded in the same
// transaction, REVOKED is terminal, two ACTIVE assignments never cover the same
// subject, operation and Market, nothing is deleted, and every resolution
// answer is immutable evidence naming an assignment only when ATTESTED.
// Synthetic identifiers only.
func TestEstateResourceAssignmentGovernanceAndEvidence(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := strings.ReplaceAll(domain.NewUUIDv7(), "-", "")[:12]
	estate, server := "estate_"+suffix, "synthetic-server-"+suffix
	human := func() string {
		var id string
		if err := db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type) VALUES('human')
			RETURNING principal_id::text`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	maker, checker, other := human(), human(), human()
	meta := func(actor string) basestore.RequestMetadata {
		return basestore.RequestMetadata{ActorID: actor, ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	key := func() string { return "synthetic-" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "") }
	rejects := func(name, sql string, args ...any) {
		t.Helper()
		if _, err := db.pool.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	proposal := EstateResourceAssignmentProposal{Scope: "WORKLOAD", WorkloadClientID: server, DigitalEstateID: estate,
		TradeEngineInstance: "ei_synthetic01", MarketScope: []string{"market_ug"},
		PermittedOperations: []string{"trade.order.commit"}, EffectiveFrom: time.Now().Add(-time.Hour).UTC(),
		ProposalReference: "changeset:synthetic-assign", EvidenceReference: "changeset:synthetic-assign"}

	if _, err := db.ProposeEstateResourceAssignment(ctx, key(), meta(maker), maker, proposal); !errors.Is(err, ErrAssignmentEstateNotGoverned) {
		t.Fatalf("an estate without a commerce profile was assigned: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO registry.estate_commerce_profile (digital_estate_id, tenant_id,
		trade_engine_instance_id, commerce_model, market_scope, status, revision, effective_from, approval_reference,
		evidence_reference) VALUES ($1, 'tn_synthetic01', 'ei_synthetic01', 'B2C', ARRAY['market_ug'], 'ACTIVE', 1,
		now() - interval '1 hour', 'approval:synthetic', 'changeset:synthetic')`, estate); err != nil {
		t.Fatal(err)
	}

	proposeKey := key()
	a, err := db.ProposeEstateResourceAssignment(ctx, proposeKey, meta(maker), maker, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "PROPOSED" || a.Revision != 1 || a.TenantID != "tn_synthetic01" || a.ProposedBy != maker || a.ApprovedBy != "" {
		t.Fatalf("proposal = %+v", a)
	}
	if replay, err := db.ProposeEstateResourceAssignment(ctx, proposeKey, meta(maker), maker, proposal); err != nil ||
		replay.AssignmentID != a.AssignmentID {
		t.Fatalf("an idempotent replay = %+v, %v", replay, err)
	}
	changed := proposal
	changed.MarketScope = []string{"market_ke"}
	if _, err := db.ProposeEstateResourceAssignment(ctx, proposeKey, meta(maker), maker, changed); err == nil {
		t.Fatal("a different proposal reused an idempotency key")
	}
	both := proposal
	both.NativeResource = &NativeRef{Reference: "trade/native/synthetic", Digest: strings.Repeat("a", 64)}
	if _, err := db.ProposeEstateResourceAssignment(ctx, key(), meta(maker), maker, both); !errors.Is(err, ErrAssignmentInvalid) {
		t.Fatalf("a proposal naming two subjects = %v", err)
	}

	transition := func(actor, action string, revision int) (EstateResourceAssignment, error) {
		return db.TransitionEstateResourceAssignment(ctx, key(), meta(actor), actor, a.AssignmentID,
			EstateResourceAssignmentTransition{Action: action, ExpectedRevision: revision, Reason: "synthetic",
				EvidenceReference: "changeset:synthetic-assign"})
	}
	if _, err := transition(maker, "APPROVE", 1); !errors.Is(err, ErrAssignmentSelfApproval) {
		t.Fatalf("a self-approval = %v", err)
	}
	if _, err := transition(checker, "APPROVE", 7); !errors.Is(err, ErrAssignmentRevisionConflict) {
		t.Fatalf("a stale approval = %v", err)
	}
	if _, err := transition(checker, "REINSTATE", 1); !errors.Is(err, ErrAssignmentInvalidTransition) {
		t.Fatalf("reinstating a proposal = %v", err)
	}

	// Direct writes cannot skip the governed path.
	rejects("an ACTIVE insert", `INSERT INTO registry.estate_resource_assignment (scope, workload_client_id,
		digital_estate_id, tenant_id, trade_engine_instance_id, market_scope, permitted_operations, status, revision,
		effective_from, proposed_by, approved_by, proposal_reference, approval_reference, evidence_reference)
		VALUES ('WORKLOAD', $1, $2, 'tn_synthetic01', 'ei_synthetic01', ARRAY['market_ug'], ARRAY['trade.order.commit'],
		'ACTIVE', 1, now(), $3::uuid, $4::uuid, 'changeset:x', 'approval:x', 'changeset:x')`, server+"-x", estate, maker, checker)
	rejects("an untransitioned approval", `UPDATE registry.estate_resource_assignment SET status = 'ACTIVE',
		revision = 2, approved_by = $2::uuid, approval_reference = 'approval:x' WHERE assignment_id = $1::uuid`,
		a.AssignmentID, checker)
	rejects("a delete", `DELETE FROM registry.estate_resource_assignment WHERE assignment_id = $1::uuid`, a.AssignmentID)

	if a, err = transition(checker, "APPROVE", 1); err != nil {
		t.Fatal(err)
	}
	if a.Status != "ACTIVE" || a.Revision != 2 || a.ApprovedBy != checker {
		t.Fatalf("approval = %+v", a)
	}
	rejects("a scope change", `UPDATE registry.estate_resource_assignment SET tenant_id = 'tn_other01', revision = 3
		WHERE assignment_id = $1::uuid`, a.AssignmentID)

	overlap := proposal
	overlap.ProposalReference = "changeset:synthetic-overlap"
	second, err := db.ProposeEstateResourceAssignment(ctx, key(), meta(other), other, overlap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.TransitionEstateResourceAssignment(ctx, key(), meta(checker), checker, second.AssignmentID,
		EstateResourceAssignmentTransition{Action: "APPROVE", ExpectedRevision: 1, Reason: "synthetic",
			EvidenceReference: "changeset:synthetic-overlap"}); !errors.Is(err, ErrAssignmentOverlap) {
		t.Fatalf("an overlapping approval = %v", err)
	}

	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	attestation := domain.NewUUIDv7()
	record := EstateResourceAssignmentResolution{AttestationID: attestation, CallerClientID: "synthetic-trade",
		Scope: "WORKLOAD", SubjectReference: server, DigitalEstateID: estate, EngineInstanceID: "ei_synthetic01",
		MarketID: "market_ug", Operation: "trade.order.commit", Outcome: "ATTESTED", AssignmentID: a.AssignmentID,
		AssignmentRevision: a.Revision, TenantID: a.TenantID, CandidateCount: 1, ResolvedAt: now, ExpiresAt: &expires,
		CorrelationID: domain.NewUUIDv7()}
	if err := db.RecordEstateResourceAssignmentResolution(ctx, record); err != nil {
		t.Fatal(err)
	}
	long := now.Add(5 * time.Minute)
	for name, bad := range map[string]func(*EstateResourceAssignmentResolution){
		"an attestation naming no assignment": func(r *EstateResourceAssignmentResolution) { r.AssignmentID = "" },
		"an attestation outliving 60 seconds": func(r *EstateResourceAssignmentResolution) { r.ExpiresAt = &long },
		"a denial naming an assignment":       func(r *EstateResourceAssignmentResolution) { r.Outcome = "ASSIGNMENT_AMBIGUOUS" },
	} {
		r := record
		r.AttestationID = domain.NewUUIDv7()
		bad(&r)
		if err := db.RecordEstateResourceAssignmentResolution(ctx, r); err == nil {
			t.Fatalf("%s was recorded", name)
		}
	}
	rejects("an evidence update", `UPDATE registry.estate_resource_assignment_resolution SET outcome = 'ASSIGNMENT_NOT_FOUND'
		WHERE attestation_id = $1::uuid`, attestation)
	rejects("an evidence delete", `DELETE FROM registry.estate_resource_assignment_resolution WHERE attestation_id = $1::uuid`, attestation)

	provenance := func(caller, market string, at time.Time) bool {
		t.Helper()
		ok, err := db.EstateAssignmentProvenance(ctx, caller, attestation, estate, "ei_synthetic01", market, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !provenance("synthetic-trade", "market_ug", now) {
		t.Fatal("a current attestation was not accepted as provenance")
	}
	if provenance("synthetic-other", "market_ug", now) || provenance("synthetic-trade", "market_ke", now) ||
		provenance("synthetic-trade", "market_ug", expires.Add(time.Second)) {
		t.Fatal("provenance was accepted for another caller, Market or after expiry")
	}

	if a, err = transition(other, "SUSPEND", 2); err != nil || a.Status != "SUSPENDED" {
		t.Fatalf("suspension = %+v, %v", a, err)
	}
	if provenance("synthetic-trade", "market_ug", now) {
		t.Fatal("a suspended assignment still provided provenance")
	}
	if _, err := transition(maker, "REINSTATE", 3); !errors.Is(err, ErrAssignmentSelfApproval) {
		t.Fatalf("the proposer reinstated their own assignment: %v", err)
	}
	if a, err = transition(checker, "REINSTATE", 3); err != nil || a.Status != "ACTIVE" || a.Revision != 4 {
		t.Fatalf("reinstatement = %+v, %v", a, err)
	}
	if provenance("synthetic-trade", "market_ug", now) {
		t.Fatal("an attestation of an earlier revision survived the reinstatement")
	}
	if a, err = transition(maker, "REVOKE", 4); err != nil || a.Status != "REVOKED" {
		t.Fatalf("revocation = %+v, %v", a, err)
	}
	if _, err := transition(checker, "REINSTATE", 5); !errors.Is(err, ErrAssignmentTerminal) {
		t.Fatalf("a revoked assignment was reinstated: %v", err)
	}
	var transitions int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM registry.estate_resource_assignment_transition
		WHERE assignment_id = $1::uuid`, a.AssignmentID).Scan(&transitions); err != nil || transitions != 4 {
		t.Fatalf("transitions = %d, %v", transitions, err)
	}
	rejects("a transition rewrite", `UPDATE registry.estate_resource_assignment_transition SET reason = 'x'
		WHERE assignment_id = $1::uuid`, a.AssignmentID)

	candidates, err := db.EstateResourceAssignmentCandidates(ctx, "WORKLOAD", server, estate)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates = %+v, %v", candidates, err)
	}
	if ok, err := db.NativeConfigurationAttested(ctx, "synthetic-trade", "ei_synthetic01", "trade/native/unknown",
		strings.Repeat("a", 64)); err != nil || ok {
		t.Fatalf("an unattested native configuration verified: %v, %v", ok, err)
	}
}
