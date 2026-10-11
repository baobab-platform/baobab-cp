package authority

import (
	"context"
	"errors"
	"time"
)

var ErrReverification = errors.New("source re-verification is not currently authorised")

// DueObservation is only a re-verification target. It carries no positive
// permission, cached credentials or source response body.
type DueObservation struct {
	ObservationID string
	CaseID        string
	ClaimID       string
	EvidenceID    string
	SourceID      string
	LastObserved  time.Time
	FreshnessUntil time.Time
	Suspended     bool
	Revoked       bool
}

// ReverificationPlan is non-authorising: an expired observation must block
// positive standing until a NEW authenticated source check and independent
// result have been committed. This selector never copies positive status.
type ReverificationPlan struct {
	ObservationID string
	EvidenceID string
	CaseID     string
	ClaimID    string
	SourceID   string
	Reason     string
	Execute    bool
}

func PlanReverification(row DueObservation, now time.Time, certified bool) (ReverificationPlan,error) {
	plan:=ReverificationPlan{ObservationID:row.ObservationID,EvidenceID:row.EvidenceID,CaseID:row.CaseID,ClaimID:row.ClaimID,SourceID:row.SourceID}
	if row.ObservationID=="" || row.CaseID=="" || row.ClaimID=="" || row.EvidenceID=="" ||
		row.SourceID=="" || now.IsZero() {
		return plan,ErrReverification
	}
	if row.Suspended || row.Revoked || !certified {
		plan.Reason="SOURCE_NOT_CERTIFIED"
		return plan,ErrReverification
	}
	if !row.FreshnessUntil.After(now) {
		plan.Reason="EVIDENCE_EXPIRED"
		plan.Execute=true
		return plan,nil
	}
	if row.FreshnessUntil.Sub(now)<=24*time.Hour {
		plan.Reason="EVIDENCE_NEAR_EXPIRY"
		plan.Execute=true
		return plan,nil
	}
	plan.Reason="NOT_YET_DUE"
	return plan,nil
}

// ReverificationQueue is an injected durable work-queue lease. A scheduler
// must deduplicate (case,claim,source,observation-version), retry with bounded
// backoff, and avoid attempting a suspended government source.
type ReverificationQueue interface {
	ClaimDue(context.Context,int) ([]DueObservation,error)
	RecordAttempt(context.Context,ReverificationPlan,error) error
}
