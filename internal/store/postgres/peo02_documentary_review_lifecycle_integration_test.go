package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"os"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	basestore "github.com/baobab-platform/baobab-cp/internal/store"
)

// Integration fixture: no real business identities, legal permissions, or
// verified documents. Authorised source decisions are not forged by a client;
// only synthetic DB rows with the normal schema triggers are created.
func TestPEO02SFullDocumentaryReviewAppealAndScopedHoldLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PostgreSQL 17 CI integration database unavailable")
	}
	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}

	actors := make([]string, 12)
	for i := range actors {
		if err = db.pool.QueryRow(ctx, `INSERT INTO identity.principal(actor_type)
            VALUES('human') RETURNING principal_id::text`).Scan(&actors[i]); err != nil {
			t.Fatal(err)
		}
	}
	operating, sponsor := domain.NewUUIDv7(), domain.NewUUIDv7()
	for _, org := range []string{operating, sponsor} {
		if _, err = db.pool.Exec(ctx, `INSERT INTO registry.canonical_entity
            (canonical_entity_id,entity_type,status)
            VALUES($1::uuid,'ORGANISATION','active')`, org); err != nil {
			t.Fatal(err)
		}
		if _, err = db.pool.Exec(ctx, `INSERT INTO registry.organisation_profile
            (canonical_entity_id,display_name,source_authority,status,effective_from)
            VALUES($1::uuid,'Synthetic PEO review lifecycle','synthetic-test','ACTIVE',
                   clock_timestamp()-interval '1 year')`, org); err != nil {
			t.Fatal(err)
		}
	}
	original := time.Now().UTC().AddDate(0, -7, 0).Truncate(time.Microsecond)
	now := time.Now().UTC()
	grant, application, admission, deferral := domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7(), domain.NewUUIDv7()
	_, err = db.pool.Exec(ctx, `INSERT INTO admission.founding_group_sponsorship
      (sponsorship_id,sponsor_organisation_id,operating_organisation_id,platform_id,
       status,scope,authority_basis_reference,evidence_references,
       proposed_by,approved_by,approved_at,effective_from,effective_to,provenance)
      VALUES($1::uuid,$2::uuid,$3::uuid,'baobab','ACTIVE','INTERNAL_GROUP_ADMISSION',
             'synthetic/peo02s/sponsorship',ARRAY['synthetic/peo02s/evidence'],
             $4::uuid,$5::uuid,$6,$7,$8,'synthetic-only-ci-fixture')`,
		grant, sponsor, operating, actors[2], actors[3], now, original.Add(-24*time.Hour), now.AddDate(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.pool.Exec(ctx, `INSERT INTO admission.client_application
       (client_application_id,reference,status,application_channel,
        applicant_principal_id,created_at,updated_at,submitted_at,closed_at)
       VALUES($1::uuid,'APP-2026-'||lpad(nextval('admission.client_application_reference_seq')::text,6,'0'),
         'APPROVED','INTERNAL_GROUP',$2::uuid,$3,$3,$3,$3)`, application, actors[0], original)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO admission.admission_decision
       (admission_decision_id,client_application_id,decision,reason,decided_by,decided_at,
        approved_subscription_type,internal_eligibility,approved_market_scope,evidence_references)
       VALUES($1::uuid,$2::uuid,'APPROVED','independent synthetic admission decision',$3::uuid,$4,
         'INTERNAL','{"source":"synthetic-test"}'::jsonb,ARRAY['ZA'],ARRAY['synthetic/approval'])`,
		admission, application, actors[1], original)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO admission.founding_documentary_deferral
       (deferral_id,organisation_id,sponsorship_id,admission_decision_id,
        requirement_ids,policy_reference,approval_reference,proposed_by,approved_by,
        approved_at,effective_from,expires_at,maximum_duration_months,status)
       VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,
         ARRAY['platform.founding.trading-name-proof'],
         'policy/peo02/approved-synthetic-only','synthetic/peo02s/approval',
         $5::uuid,$6::uuid,$7,$8,admission.founding_grace_expiry($8),24,'ACTIVE')`,
		deferral, operating, grant, admission, actors[4], actors[5], now, original)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = db.SweepFoundingDocumentaryReviews(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var checkpoint int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.founding_documentary_review_checkpoint
       WHERE deferral_id=$1::uuid AND milestone_month=6`, deferral).Scan(&checkpoint); err != nil || checkpoint != 1 {
		t.Fatalf("missing original month-6 synthetic due checkpoint: count=%d err=%v", checkpoint, err)
	}

	meta := func(i int) basestore.RequestMetadata {
		return basestore.RequestMetadata{ActorID: actors[i], ActorType: "human", CorrelationID: domain.NewUUIDv7()}
	}
	key := func(tag string) string { return fmt.Sprintf("peo02s-%s-%s", tag, domain.NewUUIDv7()) }
	body := syntheticAssessment(actors[6], deferral, operating, 6, time.Now().UTC())
	// The original deferral/checkpoint cannot manufacture a current source
	// policy classification. Until an independent approval exists the
	// documentary review proposal must be refused.
	if _, err = db.ProposeDocumentaryReview(ctx, key("no-source-policy"), meta(6),
		actors[6], deferral, 6, body); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("missing independently approved policy was accepted: %v", err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO admission.founding_documentary_policy_requirement
	    (policy_reference,requirement_id,requirement_authority,
	     authority_basis_reference,approved_by,approved_at,effective_from,effective_to)
	    VALUES('policy/peo02/approved-synthetic-only',
	      'platform.founding.trading-name-proof','PLATFORM_DOCUMENTARY',
	      'synthetic/governed-classification', $1::uuid, $2, $3, $4)`,
		actors[2], now.Add(-48*time.Hour), now.Add(-24*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Applicant/maker assessment cannot relabel an independently published
	// platform-document requirement as statutory or provider authority.
	for _, wrongAuthority := range []string{"STATUTORY", "PROVIDER"} {
		var changed map[string]any
		if err = json.Unmarshal(body, &changed); err != nil {
			t.Fatal(err)
		}
		items := changed["applicability_assessments"].([]any)
		items[0].(map[string]any)["authority"] = wrongAuthority
		mutated, marshalErr := json.Marshal(changed)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = db.ProposeDocumentaryReview(ctx, key("relabel"), meta(6),
			actors[6], deferral, 6, mutated); !errors.Is(err, ErrDocumentaryReviewDenied) {
			t.Fatalf("%s relabelling was accepted: %v", wrongAuthority, err)
		}
	}
	firstKey := key("propose")
	proposal, err := db.ProposeDocumentaryReview(ctx, firstKey, meta(6), actors[6], deferral, 6, body)
	if err != nil || proposal.Status != "PENDING" {
		t.Fatalf("review proposal denied: %+v %v", proposal, err)
	}
	replay, err := db.ProposeDocumentaryReview(ctx, firstKey, meta(6), actors[6], deferral, 6, body)
	if err != nil || replay.ID != proposal.ID {
		t.Fatalf("review idempotency broken: %+v %v", replay, err)
	}
	if _, err = db.ProposeDocumentaryReview(ctx, key("second"), meta(9), actors[9], deferral, 6,
		syntheticAssessment(actors[9], deferral, operating, 6, time.Now().UTC())); err == nil {
		t.Fatal("second proposal without successful appeal was accepted")
	}
	restriction := ReviewRestriction{RequirementID: "platform.founding.trading-name-proof",
		CapabilityKey: "trade.checkout", MarketCode: "ZA"}
	decisionInput := ReviewDecisionInput{
		Outcome:           "REMEDIATION_REQUIRED",
		DecisionReference: "synthetic/peo02s/decision",
		Reason:            "Synthetic assessment incomplete; restrict only ZA trade checkout",
		Restrictions:      []ReviewRestriction{restriction},
	}
	if _, err = db.DecideDocumentaryReview(ctx, key("self"), meta(6), actors[6], proposal.ID, decisionInput); err == nil {
		t.Fatal("review maker approved own negative restriction")
	}
	decisionKey := key("decision")
	// Concurrent same-key retries must converge on one decision and one
	// audit fact. They must not contend on the unique index or return 500.
	type concurrentDecision struct {
		receipt ReviewReceipt
		err     error
	}
	startDecisions := make(chan struct{})
	decisionResults := make(chan concurrentDecision, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-startDecisions
			receipt, callErr := db.DecideDocumentaryReview(ctx, decisionKey, meta(7),
				actors[7], proposal.ID, decisionInput)
			decisionResults <- concurrentDecision{receipt: receipt, err: callErr}
		}()
	}
	close(startDecisions)
	firstDecision, secondDecision := <-decisionResults, <-decisionResults
	if firstDecision.err != nil || secondDecision.err != nil ||
		firstDecision.receipt.Status != "DECIDED" ||
		firstDecision.receipt.ID != secondDecision.receipt.ID {
		t.Fatalf("same-key concurrent review decisions did not converge: first=%+v second=%+v",
			firstDecision, secondDecision)
	}
	decision := firstDecision.receipt
	same, err := db.DecideDocumentaryReview(ctx, decisionKey, meta(7), actors[7], proposal.ID, decisionInput)
	if err != nil || same.ID != decision.ID {
		t.Fatalf("decision replay changed authority: %+v %v", same, err)
	}
	changed := decisionInput
	changed.Reason = "Synthetic changed assessment should be denied on replay"
	if _, err = db.DecideDocumentaryReview(ctx, decisionKey, meta(7), actors[7], proposal.ID, changed); err == nil {
		t.Fatal("mutated decision replay accepted")
	}
	for _, tc := range []struct {
		capability, market string
		want               bool
	}{
		{"trade.checkout", "ZA", true}, {"trade.checkout", "UG", false},
		{"erp.invoice", "ZA", false}, {"payments.capture", "ZA", false},
	} {
		blocked, e := db.DocumentaryScopeBlocked(ctx, operating, tc.capability, tc.market)
		if e != nil || blocked != tc.want {
			t.Errorf("%s/%s blocked=%v, want=%v, err=%v",
				tc.capability, tc.market, blocked, tc.want, e)
		}
	}
	if _, err = db.FileDocumentaryReviewAppeal(ctx, key("self-appeal"), meta(7), actors[7], decision.ID,
		ReviewAppealInput{Reason: "Synthetic appeal should not self-adjudicate", EvidenceReference: "synthetic/appeal"}); err == nil {
		t.Fatal("review checker appealed own decision")
	}
	appealKey := key("appeal")
	appealInput := ReviewAppealInput{Reason: "Synthetic appeal requests independent reassessment",
		EvidenceReference: "synthetic/peo02s/new-evidence"}
	appeal, err := db.FileDocumentaryReviewAppeal(ctx, appealKey, meta(8), actors[8], decision.ID, appealInput)
	if err != nil || appeal.Status != "FILED" {
		t.Fatalf("appeal not filed: %+v %v", appeal, err)
	}
	sameAppeal, err := db.FileDocumentaryReviewAppeal(ctx, appealKey, meta(8), actors[8], decision.ID, appealInput)
	if err != nil || sameAppeal.ID != appeal.ID {
		t.Fatalf("appeal replay changed: %+v %v", sameAppeal, err)
	}
	appealDecision := ReviewAppealDecisionInput{Outcome: "RETURN_FOR_REASSESSMENT",
		Reason:            "Synthetic record requires a new independent review",
		EvidenceReference: "synthetic/peo02s/appeal-decision"}
	if _, err = db.DecideDocumentaryReviewAppeal(ctx, key("appeal-maker"), meta(6), actors[6], appeal.ID, appealDecision); err == nil {
		t.Fatal("original review maker adjudicated the appeal")
	}
	appealDecKey := key("appeal-adjudication")
	adjudication, err := db.DecideDocumentaryReviewAppeal(ctx, appealDecKey, meta(9), actors[9], appeal.ID, appealDecision)
	if err != nil || adjudication.Status != "DECIDED" {
		t.Fatalf("independent appeal decision denied: %+v %v", adjudication, err)
	}
	newProposal, err := db.ProposeDocumentaryReview(ctx, key("reassess"), meta(10), actors[10],
		deferral, 6, syntheticAssessment(actors[10], deferral, operating, 6, time.Now().UTC()))
	if err != nil || newProposal.Status != "PENDING" {
		t.Fatalf("authorised reassessment was not reopened: %+v %v", newProposal, err)
	}
	var rev int
	var linked string
	if err = db.pool.QueryRow(ctx, `SELECT revision,reopened_from_appeal_id::text
       FROM admission.documentary_review_intent WHERE review_id=$1::uuid`,
		newProposal.ID).Scan(&rev, &linked); err != nil || rev != 2 || linked != appeal.ID {
		t.Fatalf("reassessment lost appeal lineage: revision=%d appeal=%s err=%v", rev, linked, err)
	}
	// This review is still PENDING but the same checker/key already belongs
	// to revision 1. The conflict must be deterministic and fail closed.
	if _, err = db.DecideDocumentaryReview(ctx, decisionKey, meta(7), actors[7],
		newProposal.ID, decisionInput); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("cross-review idempotency key did not return a stable conflict: %v", err)
	}
	var crossReviewDecisions int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.documentary_review_decision
        WHERE review_id=$1::uuid`, newProposal.ID).Scan(&crossReviewDecisions); err != nil || crossReviewDecisions != 0 {
		t.Fatalf("reused key persisted a second review decision: %d %v", crossReviewDecisions, err)
	}
	revisedDecision, err := db.DecideDocumentaryReview(ctx, key("reassess-decide"), meta(11), actors[11],
		newProposal.ID, ReviewDecisionInput{Outcome: "ASSESSMENT_INCOMPLETE",
			DecisionReference: "synthetic/peo02s/reassessment",
			Reason:            "Further independent verification required; do not clear any previous hold"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FileDocumentaryReviewAppeal(ctx, appealKey, meta(8), actors[8],
		revisedDecision.ID, appealInput); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("cross-decision appeal idempotency key did not return a stable conflict: %v", err)
	}
	var crossDecisionAppeals int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.documentary_review_appeal
        WHERE decision_id=$1::uuid`, revisedDecision.ID).Scan(&crossDecisionAppeals); err != nil || crossDecisionAppeals != 0 {
		t.Fatalf("reused key persisted an unrelated appeal: %d %v", crossDecisionAppeals, err)
	}
	blocked, err := db.DocumentaryScopeBlocked(ctx, operating, "trade.checkout", "ZA")
	if err != nil || !blocked {
		t.Fatalf("successful appeal silently cleared history: blocked=%v err=%v", blocked, err)
	}
	var n int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.documentary_remediation_restriction
        WHERE organisation_id=$1::uuid`, operating).Scan(&n); err != nil || n != 1 {
		t.Fatalf("restriction unexpectedly duplicated or cleared: %d %v", n, err)
	}
	var changedOriginal int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.founding_documentary_deferral
      WHERE deferral_id=$1::uuid AND status='ACTIVE'
        AND expires_at=admission.founding_grace_expiry(effective_from)`,
		deferral).Scan(&changedOriginal); err != nil || changedOriginal != 1 {
		t.Fatalf("appeal or reassessment mutated original 24-month grace: %d %v", changedOriginal, err)
	}
	var audit int
	targets := []string{
		"founding-governance/" + proposal.ID,
		"founding-governance/" + decision.ID,
		"founding-governance/" + appeal.ID,
		"founding-governance/" + adjudication.ID,
		"founding-governance/" + newProposal.ID,
		"founding-governance/" + revisedDecision.ID,
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
       WHERE target=ANY($1::text[]) AND action LIKE 'founding_governance.documentary_%'`,
		targets).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if audit != 6 {
		t.Fatalf("expected exactly six committed, nonreplayed review/appeal audit facts, got %d", audit)
	}
	// Exercise the third independent actor/key uniqueness constraint on a
	// different, newly filed appeal. Only a fresh key may adjudicate it.
	secondAppeal, err := db.FileDocumentaryReviewAppeal(ctx, key("second-appeal"),
		meta(8), actors[8], revisedDecision.ID, appealInput)
	if err != nil || secondAppeal.Status != "FILED" {
		t.Fatalf("new appeal with fresh key rejected: %+v %v", secondAppeal, err)
	}
	if _, err = db.DecideDocumentaryReviewAppeal(ctx, appealDecKey, meta(9),
		actors[9], secondAppeal.ID, appealDecision); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("cross-appeal adjudication idempotency key did not return a stable conflict: %v", err)
	}
	var crossAppealDecisions int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM admission.documentary_appeal_decision
        WHERE appeal_id=$1::uuid`, secondAppeal.ID).Scan(&crossAppealDecisions); err != nil || crossAppealDecisions != 0 {
		t.Fatalf("reused adjudication key persisted an unrelated decision: %d %v",
			crossAppealDecisions, err)
	}
	// The same adjudicator may handle the separate appeal with a fresh key.
	secondAdjudication, err := db.DecideDocumentaryReviewAppeal(ctx, key("second-adjudication"),
		meta(9), actors[9], secondAppeal.ID, ReviewAppealDecisionInput{
			Outcome: "UPHOLD", Reason: "Synthetic prior remediation restriction must remain",
			EvidenceReference: "synthetic/peo02s/second-adjudication"})
	if err != nil || secondAdjudication.Status != "DECIDED" {
		t.Fatalf("fresh-key independent appeal adjudication failed: %+v %v", secondAdjudication, err)
	}
	// PEO-02U: the existing negative hold cannot be released by appeal,
	// a self-declared document reference or an absent verification result.
	// Synthetically seed a normal OEV-03 verified source chain to exercise
	// the full maker/checker release, expiry and re-blocking projection.
	var restrictionID string
	if err = db.pool.QueryRow(ctx, `SELECT restriction_id::text FROM
	 admission.documentary_remediation_restriction
	 WHERE organisation_id=$1::uuid AND capability_key='trade.checkout'`, operating).Scan(&restrictionID); err != nil {
		t.Fatal(err)
	}
	release := DocumentaryReleaseProposal{
		VerificationResultID: "vres_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", ""),
		EvidenceReference: "synthetic/cipc-lookup-provenance",
		Reason: "Synthetic test release requires an independently verified source",
	}
	if _, err = db.ProposeDocumentaryRelease(ctx, key("no-evidence"), meta(4),
		actors[4], restrictionID, release); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("invented evidence result released a hold: %v", err)
	}
	caseID := "vcase_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	claimID := "ecl_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	checkID := "vchk_" + strings.ReplaceAll(domain.NewUUIDv7(), "-", "")
	resultID := release.VerificationResultID
	verifiedUntil := time.Now().UTC().Add(time.Hour)
	claimDoc := fmt.Sprintf(`{"claim_type":"LEGAL_NAME","jurisdiction":"ZA",
	  "purpose":"COMPLIANCE_REMEDIATION"}`)
	caseDoc := `{"purpose":"COMPLIANCE_REMEDIATION"}`
	resultDoc, _ := json.Marshal(map[string]any{
		"outcome": "VERIFIED", "check_ids": []string{checkID},
		"freshness_until": verifiedUntil,
	})
	_, err = db.pool.Exec(ctx, `INSERT INTO evidence.verification_case
	 (case_id,subject_type,subject_id,status,opened_by,opened_at,completed_at,document)
	 VALUES($1,'ORGANISATION',$2,'VERIFIED',$3,clock_timestamp(),
	     clock_timestamp(),$4::jsonb)`,
		caseID, operating, actors[0], caseDoc)
	if err != nil { t.Fatal(err) }
	_, err = db.pool.Exec(ctx, `INSERT INTO evidence.claim
	 (claim_id,case_id,asserted_by,status,document,created_at)
	 VALUES($1,$2,$3,'UNDER_VERIFICATION',$4::jsonb,clock_timestamp())`,
		claimID, caseID, actors[9], claimDoc)
	if err != nil { t.Fatal(err) }
	_, err = db.pool.Exec(ctx, `INSERT INTO evidence.check
	 (check_id,case_id,claim_id,source_id,outcome,performed_by,performed_at,document)
	 VALUES($1,$2,$3,'esrc_cipc','VERIFIED',$4,clock_timestamp(),'{}'::jsonb)`,
		checkID, caseID, claimID, actors[10])
	if err != nil { t.Fatal(err) }
	_, err = db.pool.Exec(ctx, `INSERT INTO evidence.result
	 (result_id,case_id,claim_id,outcome,decided_by,decided_at,document)
	 VALUES($1,$2,$3,'VERIFIED',$4,clock_timestamp(),$5::jsonb)`,
		resultID, caseID, claimID, actors[1], resultDoc)
	if err != nil { t.Fatal(err) }
	_, err = db.pool.Exec(ctx, `UPDATE evidence.claim
	 SET status='VERIFIED',current_result_id=$2 WHERE claim_id=$1`, claimID, resultID)
	if err != nil { t.Fatal(err) }
	// Even a synthetically VERIFIED ledger result is not enough without
	// a separately governed, requirement-specific trusted source binding.
	if _, err = db.ProposeDocumentaryRelease(ctx, key("no-binding"), meta(4),
		actors[4], restrictionID, release); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("verification without source/requirement policy released a hold: %v", err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO admission.documentary_release_source_policy
	 (policy_reference,requirement_id,claim_type,source_id,market_code,
	  authority_basis_reference,approved_by,approved_at,effective_from,effective_to)
	 VALUES('policy/peo02/approved-synthetic-only','platform.founding.trading-name-proof',
	   'LEGAL_NAME','esrc_cipc','ZA','synthetic/reviewed-source-policy',
	   $1::uuid,$2,$3,$4)`,
		actors[3], now.Add(-time.Hour), now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil { t.Fatal(err) }
	made, err := db.ProposeDocumentaryRelease(ctx, key("release"), meta(4),
		actors[4], restrictionID, release)
	if err != nil || made.Status != "PENDING" { t.Fatalf("release maker denied: %+v %v", made, err) }
	if _, err = db.DecideDocumentaryRelease(ctx, key("self-release"), meta(4),
		actors[4], made.ID, DocumentaryReleaseDecision{Outcome: "APPROVE",
			Reason: "Synthetic self-approval must not release any restriction"}); !errors.Is(err, ErrDocumentaryReviewDenied) {
		t.Fatalf("same actor approved release: %v", err)
	}
	checked, err := db.DecideDocumentaryRelease(ctx, key("checker"), meta(5),
		actors[5], made.ID, DocumentaryReleaseDecision{Outcome: "APPROVE",
			Reason: "Synthetic source-verified remediation now independently approved"})
	if err != nil || checked.Status != "APPROVE" { t.Fatalf("independent release rejected: %+v %v", checked, err) }
	cleared, err := db.DocumentaryScopeBlocked(ctx, operating, "trade.checkout", "ZA")
	if err != nil || cleared { t.Fatalf("verified synthetic release did not clear exact scope: blocked=%v err=%v", cleared, err) }
	// A result that becomes REVOKED must immediately re-impose the old
	// negative restriction. Historical review/release rows remain untouched.
	_, err = db.pool.Exec(ctx, `UPDATE evidence.claim SET status='REVOKED'
	 WHERE claim_id=$1`, claimID)
	if err != nil { t.Fatal(err) }
	reblocked, err := db.DocumentaryScopeBlocked(ctx, operating, "trade.checkout", "ZA")
	if err != nil || !reblocked { t.Fatalf("revoked evidence did not restore hold: blocked=%v err=%v", reblocked, err) }
	// Source event/evidence claims are synthetic-only. These tests DO NOT
	// establish external statutory applicability or live PEP acceptance.
}
