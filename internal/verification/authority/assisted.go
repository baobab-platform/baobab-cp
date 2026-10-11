package authority

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrAssistedReview = errors.New("independently witnessed official review not satisfied")

// AssistedFinding contains only a witnessed metadata receipt, never the
// taxpayer's TCS PIN, tax reference, CAPTCHA response or portal credentials.
type AssistedFinding struct {
	IntentID          string
	CaseID            string
	ClaimID           string
	SourceID          string
	EvidenceID        string
	OfficialReference string
	ObservedAt        time.Time
	FreshnessUntil    time.Time
	ResponseDigest    string
	ProvenanceRef     string
	Witness           string
	Checker           string
	CheckedAt         time.Time
	Status            string
}

// AssistedLedger persists proposals and decisions atomically. A production
// implementation must lock the intent, require a different checker and
// independently authorise both identities.
type AssistedLedger interface {
	Propose(context.Context, AssistedFinding, string) (AssistedFinding,error)
	Decide(context.Context, string, string, string, time.Time) (AssistedFinding,error)
}

// ValidateAssistedProposal enforces safe, source-specific two-person
// submission. A proposal is PENDING and cannot create VERIFIED standing.
func ValidateAssistedProposal(f AssistedFinding, now time.Time) error {
	if f.IntentID=="" || f.CaseID=="" || f.ClaimID=="" || f.EvidenceID=="" ||
		f.SourceID!="esrc_sarstcs" && f.SourceID!="esrc_sarsvat" &&
			f.SourceID!="esrc_sarspaye" && f.SourceID!="esrc_sarsuif" &&
			f.SourceID!="esrc_sarssdl" && f.SourceID!="esrc_sarscustoms" &&
			f.SourceID!="esrc_uifdept" ||
		f.OfficialReference=="" || len(f.OfficialReference)>256 ||
		len(f.ProvenanceRef)<8 || len(f.ProvenanceRef)>512 ||
		len(f.ResponseDigest)!=71 || !strings.HasPrefix(f.ResponseDigest,"sha256:") ||
		f.Witness=="" || f.Checker!="" || f.Status!="PENDING" ||
		f.ObservedAt.After(now) || now.Sub(f.ObservedAt)>5*time.Minute ||
		!f.FreshnessUntil.After(now) || !f.FreshnessUntil.After(f.ObservedAt) {
		return ErrAssistedReview
	}
	return nil
}

// ValidateAssistedApproval is **not** a source activation or compliance
// approval. It merely certifies that a second human witnessed the evidence
// and may later support a source-governed OEV-03 VerificationCheck.
func ValidateAssistedApproval(f AssistedFinding, checker string, now time.Time) error {
	if f.Status!="PENDING" || checker=="" || checker==f.Witness ||
		f.CheckedAt!= (time.Time{}) || f.Checker!="" ||
		now.Before(f.ObservedAt) || !now.Before(f.FreshnessUntil) {
		return ErrAssistedReview
	}
	return nil
}
