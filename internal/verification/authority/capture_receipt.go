package authority

import "time"

// CaptureReceipt is the only wire result of an official-source intake.
// It carries no raw response, tax details, credential or positive grant.
type CaptureReceipt struct {
	ObservationID    string    `json:"observation_id"`
	EvidenceID       string    `json:"evidence_id"`
	CaseID           string    `json:"verification_case_id"`
	ClaimID          string    `json:"claim_id"`
	SourceID         string    `json:"source_id"`
	ObservedAt       time.Time `json:"observed_at"`
	FreshnessUntil   time.Time `json:"freshness_until"`
	AuthorityGranted bool      `json:"authority_granted"`
}
