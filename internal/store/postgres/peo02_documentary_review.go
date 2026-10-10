// PEO-02R: UTC-calendar documentary review scheduling at months 6/12/18/24.
// A due checkpoint is evidence work, NOT a positive applicability decision,
// legal waiver, grace extension, automatic provider restriction or entitlement.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
)

type foundingReviewDue struct {
	DeferralID      string    `json:"deferral_id"`
	OrganisationID  string    `json:"organisation_id"`
	MilestoneMonths int       `json:"milestone_month"`
	ScheduledAt     time.Time `json:"scheduled_at"`
}

// SweepFoundingDocumentaryReviews materialises only already-due checkpoints
// from immutable original approvals. A retry, competing worker, reapplication
// or tenant rename cannot produce a duplicate or postpone the deadline.
// Review conclusions must be made later by independently authorised humans
// or an approved versioned policy through a separate consuming PEP.
func (s *Store) SweepFoundingDocumentaryReviews(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// The durable source is admission.founding_documentary_deferral; it must
	// never be rebuilt from the most recent tenant, group affiliation, or
	// legal-actor registration. EXPIRED deferrals still need remediation
	// evidence, but REVOKED/FULFILLED ones have no continuing grace.
	rows, err := tx.Query(ctx, `WITH due AS (
	   SELECT d.deferral_id, d.organisation_id, d.policy_reference,
	          d.requirement_ids, milestone.m,
	          admission.founding_review_milestone_utc(d.effective_from,milestone.m) AS due_at
	   FROM admission.founding_documentary_deferral d
	   CROSS JOIN (VALUES(6),(12),(18),(24)) milestone(m)
	   WHERE d.status IN ('ACTIVE','EXPIRED')
	     AND admission.founding_review_milestone_utc(d.effective_from,milestone.m)
	         <= clock_timestamp()
	     AND NOT EXISTS (
	         SELECT 1 FROM admission.founding_documentary_review_checkpoint c
	         WHERE c.deferral_id=d.deferral_id
	           AND c.milestone_month=milestone.m
	     )
	   ORDER BY due_at,d.deferral_id,milestone.m
	   LIMIT $1
	 )
	 INSERT INTO admission.founding_documentary_review_checkpoint
	   (deferral_id,milestone_month,organisation_id,scheduled_at,
	    policy_reference,requirement_ids)
	 SELECT deferral_id,m,organisation_id,due_at,policy_reference,requirement_ids
	 FROM due
	 ON CONFLICT (deferral_id,milestone_month) DO NOTHING
	 RETURNING deferral_id::text,organisation_id::text,milestone_month,scheduled_at`, limit)
	if err != nil {
		return 0, err
	}
	var due []foundingReviewDue
	for rows.Next() {
		var item foundingReviewDue
		if err = rows.Scan(&item.DeferralID, &item.OrganisationID,
			&item.MilestoneMonths, &item.ScheduledAt); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, item := range due {
		payload, e := json.Marshal(map[string]any{
			"deferral_id": item.DeferralID, "organisation_id": item.OrganisationID,
			"milestone_month": item.MilestoneMonths,
			"scheduled_at": item.ScheduledAt, "result": "REASSESSMENT_REQUIRED",
		})
		if e != nil {
			return 0, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO audit_events
		   (actor_id,actor_type,correlation_id,action,target,result,payload)
		   VALUES('workload:baobab-cp:founding-review','workload',
		     $1::uuid,'founding_documentary_review.due',$2,
		     'accepted',$3::jsonb)`,
			domain.NewUUIDv7(), fmt.Sprintf("founding-documentary-review/%s/%d",
				item.DeferralID, item.MilestoneMonths), payload)
		if e != nil {
			return 0, e
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(due), nil
}

// FoundingDocumentaryReviewDue gives the controlled review service its own
// non-positive ledger, independent from any mutable capability/provider flags.
func (s *Store) FoundingDocumentaryReviewDue(ctx context.Context, limit int) ([]foundingReviewDue, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT deferral_id::text,organisation_id::text,
	    milestone_month,scheduled_at
	  FROM admission.founding_documentary_review_checkpoint
	  WHERE status='REASSESSMENT_REQUIRED'
	  ORDER BY scheduled_at,deferral_id,milestone_month
	  LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]foundingReviewDue, 0, limit)
	for rows.Next() {
		var item foundingReviewDue
		if err = rows.Scan(&item.DeferralID, &item.OrganisationID,
			&item.MilestoneMonths, &item.ScheduledAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
