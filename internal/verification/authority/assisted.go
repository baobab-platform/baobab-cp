package authority

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrAssistedReview = errors.New("independently witnessed official review not satisfied")

// AssistedFinding contains only a witnessed metadata receipt, never the
// taxpayer's TCS PIN, tax reference, CAPTCHA response or portal credentials.
type AssistedFinding struct {
	IntentID          string    `json:"intent_id"`
	CaseID            string    `json:"verification_case_id"`
	ClaimID           string    `json:"claim_id"`
	SourceID          string    `json:"source_id"`
	EvidenceID        string    `json:"evidence_id"`
	OfficialReference string    `json:"official_reference"`
	ObservedAt        time.Time `json:"observed_at"`
	FreshnessUntil    time.Time `json:"freshness_until"`
	ResponseDigest    string    `json:"response_digest"`
	ProvenanceRef     string    `json:"provenance_reference"`
	Witness           string    `json:"witness_principal_id"`
	Checker           string    `json:"checker_principal_id,omitempty"`
	CheckedAt         time.Time `json:"checked_at,omitempty"`
	Status            string    `json:"status"`
}

// AssistedLedger persists proposals and decisions atomically. A production
// implementation must lock the intent, require a different checker and
// independently authorise both identities.
type AssistedLedger interface {
	Propose(context.Context, AssistedFinding, string) (AssistedFinding, error)
	Decide(context.Context, string, string, string, time.Time) (AssistedFinding, error)
}

// ValidateAssistedProposal enforces safe, source-specific two-person
// submission. A proposal is PENDING and cannot create VERIFIED standing.
func ValidateAssistedProposal(f AssistedFinding, now time.Time) error {
	if f.IntentID == "" || f.CaseID == "" || f.ClaimID == "" || f.EvidenceID == "" ||
		f.SourceID != "esrc_sarstcs" && f.SourceID != "esrc_sarsvat" &&
			f.SourceID != "esrc_sarspaye" && f.SourceID != "esrc_sarsuif" &&
			f.SourceID != "esrc_sarssdl" && f.SourceID != "esrc_sarscustoms" &&
			f.SourceID != "esrc_uifdept" ||
		!strings.HasPrefix(f.OfficialReference, "ref-sha256:") ||
		!isLowerHexDigest("sha256:"+strings.TrimPrefix(f.OfficialReference, "ref-sha256:")) ||
		len(f.ProvenanceRef) < 8 || len(f.ProvenanceRef) > 512 ||
		len(f.ResponseDigest) != 71 || !strings.HasPrefix(f.ResponseDigest, "sha256:") ||
		!isLowerHexDigest(f.ResponseDigest) ||
		f.Witness == "" || f.Checker != "" || f.Status != "PENDING" ||
		f.ObservedAt.After(now) || now.Sub(f.ObservedAt) > 5*time.Minute ||
		!f.FreshnessUntil.After(now) || !f.FreshnessUntil.After(f.ObservedAt) {
		return ErrAssistedReview
	}
	return nil
}

// ValidateAssistedApproval is **not** a source activation or compliance
// approval. It merely certifies that a second human witnessed the evidence
// and may later support a source-governed OEV-03 VerificationCheck.
func ValidateAssistedApproval(f AssistedFinding, checker string, now time.Time) error {
	if f.Status != "PENDING" || checker == "" || checker == f.Witness ||
		f.CheckedAt != (time.Time{}) || f.Checker != "" ||
		now.Before(f.ObservedAt) || !now.Before(f.FreshnessUntil) {
		return ErrAssistedReview
	}
	return nil
}

func isLowerHexDigest(d string) bool {
	if len(d) != 71 || !strings.HasPrefix(d, "sha256:") {
		return false
	}
	raw, err := hex.DecodeString(d[7:])
	return err == nil && len(raw) == 32 && strings.ToLower(d) == d
}
