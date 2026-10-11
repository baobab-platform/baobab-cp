package organisation

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

var ErrFoundingReconciliation = errors.New("independent founding-company evidence unavailable")

// FoundingIdentityInput is an exact, governed binding to a first-party
// Organisation. It MUST come from Shared/CP governance, never a browser
// request. Corporate-group labels are not proof of incorporation or control.
type FoundingIdentityInput struct {
	RegistryID                  string
	OrganisationID              string
	VerificationCaseID          string
	DeclaredRegistrationNumber  string
}

// IndependentControlProof is a narrowly scoped, externally attested snapshot
// of a CURRENT DIRECT corporate-control claim. CIPC registration data alone
// does not supply ownership evidence. The producer's private key is never
// held by Baobab CP or an affiliated business.
type IndependentControlProof struct {
	IssuerID             string    `json:"issuer_id"`
	SourceRecordReference string   `json:"source_record_reference"`
	EvidenceReference    string    `json:"evidence_reference"`
	EvidenceDigest       string    `json:"evidence_digest"`
	RelationshipID       string    `json:"relationship_id"`
	ParentOrganisationID string    `json:"parent_organisation_id"`
	ChildOrganisationID  string    `json:"child_organisation_id"`
	ParentRegistration   string    `json:"parent_registration"`
	ChildRegistration    string    `json:"child_registration"`
	RelationshipType     string    `json:"relationship_type"`
	OwnershipPercentage  *float64  `json:"ownership_percentage,omitempty"`
	ObservedAt           time.Time `json:"observed_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	SignerID             string    `json:"signer_id"`
	Signature            []byte    `json:"signature"`
}

func (p IndependentControlProof) signingPayload() []byte {
	// The signature covers every consequential field, with deterministic
	// struct JSON field order and a domain separator against cross-protocol use.
	wire := p
	wire.Signature = nil
	data, _ := json.Marshal(wire)
	return append([]byte("baobab/corporate-control-proof/v1\x00"), data...)
}

// IndependentControlTrust is operator-reviewed external authority,
// separately configured from first-party Shared registry evidence and the
// issuer's submitted certificate. No browser controls trusted public keys.
type IndependentControlTrust struct {
	SignerID string
	PublicKey ed25519.PublicKey
	ApprovedBy string
	ApprovedAt time.Time
	ExpiresAt time.Time
}

func (t IndependentControlTrust) verify(proof IndependentControlProof, now time.Time) bool {
	if t.SignerID == "" || t.SignerID != proof.SignerID ||
		len(t.PublicKey) != ed25519.PublicKeySize ||
		len(proof.Signature) != ed25519.SignatureSize ||
		t.ApprovedBy == "" || t.ApprovedAt.IsZero() ||
		t.ApprovedAt.After(now) || !t.ExpiresAt.After(now) ||
		proof.IssuerID == "" || proof.SourceRecordReference == "" ||
		proof.EvidenceReference == "" || proof.RelationshipID == "" ||
		proof.ParentOrganisationID == "" || proof.ChildOrganisationID == "" ||
		proof.ParentOrganisationID == proof.ChildOrganisationID ||
		proof.ObservedAt.IsZero() || proof.ObservedAt.After(now) ||
		!proof.ExpiresAt.After(now) || proof.ExpiresAt.After(t.ExpiresAt) ||
		len(proof.EvidenceDigest) != 71 ||
		!strings.HasPrefix(proof.EvidenceDigest, "sha256:") {
		return false
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(proof.EvidenceDigest, "sha256:")); err != nil {
		return false
	}
	return ed25519.Verify(t.PublicKey, proof.signingPayload(), proof.Signature)
}

type FoundingRegistrationSource interface {
	ResolveVerifiedRegistration(context.Context, string, verification.Subject) (verification.VerifiedRegistrationBinding, error)
}
type FoundingCorporateSource interface {
	ListCorporateRelationshipsByOrganisation(context.Context, string, time.Time) ([]domain.CorporateRelationship, error)
}
type IndependentCorporateControlSource interface {
	ReadIndependentControlProof(context.Context, string) (IndependentControlProof, error)
}

// FoundingCompanyReconciliation is a non-authorising evidence report for the
// PEO-T20 and LA-T27 gates. It NEVER creates an incorporation, VERIFIED
// LegalEntityProfile, CorporateRelationship, Tenant, IAM grant, or mandate.
type FoundingCompanyReconciliation struct {
	NabholdRegistrationVerified bool
	ThamaniRegistrationVerified bool
	ControlVerified bool
	NabholdRegistrationEvidence string
	ThamaniRegistrationEvidence string
	ControlEvidenceReference string
	ControlRelationshipID string
	AssessedAt time.Time
	Standing string // PENDING_INDEPENDENT_EVIDENCE or BOTH_TRACKS_VERIFIED
	Reasons []string
}

// FoundingEvidenceReconciler requires OEV-03 verified CIPC registrations for
// EACH company and a separate, independently attested direct control edge.
// It cannot infer ownership from corporate-group membership or Shared roles.
type FoundingEvidenceReconciler struct {
	Registrations FoundingRegistrationSource
	Corporate FoundingCorporateSource
	Proofs IndependentCorporateControlSource
	Trust map[string]IndependentControlTrust // exact external signer ID
	Now func() time.Time
}

func (r FoundingEvidenceReconciler) Reconcile(ctx context.Context, parent, child FoundingIdentityInput) (FoundingCompanyReconciliation, error) {
	out := FoundingCompanyReconciliation{Standing:"PENDING_INDEPENDENT_EVIDENCE"}
	if ctx == nil || ctx.Err() != nil || r.Registrations == nil || r.Corporate == nil ||
		r.Proofs == nil || r.Now == nil || parent.RegistryID != "NABHOLD" ||
		child.RegistryID != "THAMANI-GLOBAL" ||
		parent.OrganisationID == "" || child.OrganisationID == "" ||
		parent.OrganisationID == child.OrganisationID ||
		parent.VerificationCaseID == "" || child.VerificationCaseID == "" ||
		parent.DeclaredRegistrationNumber == "" || child.DeclaredRegistrationNumber == "" {
		return out, ErrFoundingReconciliation
	}
	now := r.Now().UTC()
	out.AssessedAt = now
	parentBinding, err1 := r.Registrations.ResolveVerifiedRegistration(ctx,parent.VerificationCaseID,
		verification.Subject{SubjectType:"ORGANISATION",SubjectID:parent.OrganisationID})
	childBinding, err2 := r.Registrations.ResolveVerifiedRegistration(ctx,child.VerificationCaseID,
		verification.Subject{SubjectType:"ORGANISATION",SubjectID:child.OrganisationID})
	pGood := err1 == nil && foundingRegistrationMatches(parent,parentBinding,now)
	cGood := err2 == nil && foundingRegistrationMatches(child,childBinding,now)
	out.NabholdRegistrationVerified, out.ThamaniRegistrationVerified = pGood,cGood
	if pGood {
		out.NabholdRegistrationEvidence = parentBinding.ResultID
	} else {
		out.Reasons=append(out.Reasons,"NABHOLD_CIPC_OEV03_PENDING")
	}
	if cGood {
		out.ThamaniRegistrationEvidence = childBinding.ResultID
	} else {
		out.Reasons=append(out.Reasons,"THAMANI_CIPC_OEV03_PENDING")
	}
	// Do not even consult corporate ownership evidence until both legal
	// identities are source-verified independently. Applicant certificates
	// and Shared registration entries cannot open this gate.
	if !pGood || !cGood {
		out.Reasons=append(out.Reasons,"CONTROL_PROOF_NOT_APPLICABLE_YET")
		return out,nil
	}
	edges, err := r.Corporate.ListCorporateRelationshipsByOrganisation(ctx,parent.OrganisationID,now)
	if err != nil {return out,ErrFoundingReconciliation}
	matches := make([]domain.CorporateRelationship,0,1)
	for _,edge:=range edges {
		if edge.SourceOrganisationID==parent.OrganisationID &&
			edge.TargetOrganisationID==child.OrganisationID &&
			edge.DirectOrDerived==domain.CorporateFactDirect &&
			edge.VerificationState==domain.VerificationVerified &&
			edge.IsConsequential(now) && domain.QualifiesForGroup(edge,now)=="" &&
			edge.SourceAuthority!="" && edge.SourceAuthority!="shared-governance" {
			matches=append(matches,edge)
		}
	}
	if len(matches)!=1 {
		out.Reasons=append(out.Reasons,"UNIQUE_DIRECT_VERIFIED_CONTROL_EDGE_REQUIRED")
		return out,nil
	}
	edge:=matches[0]
	proof,err:=r.Proofs.ReadIndependentControlProof(ctx,edge.ID)
	if err!=nil {
		out.Reasons=append(out.Reasons,"INDEPENDENT_CONTROL_PROOF_MISSING")
		return out,nil
	}
	trust,ok:=r.Trust[proof.SignerID]
	if !ok || !trust.verify(proof,now) ||
		proof.RelationshipID!=edge.ID ||
		proof.ParentOrganisationID!=parent.OrganisationID ||
		proof.ChildOrganisationID!=child.OrganisationID ||
		proof.ParentRegistration!=parentBinding.RegistrationNumber ||
		proof.ChildRegistration!=childBinding.RegistrationNumber ||
		proof.RelationshipType!=string(edge.RelationshipType) ||
		!sameControlPercentage(proof.OwnershipPercentage,edge.OwnershipPercentage,edge.RelationshipType) ||
		!evidenceRefPresent(edge.EvidenceReferences,proof.EvidenceReference) {
		out.Reasons=append(out.Reasons,"INDEPENDENT_CONTROL_PROOF_UNTRUSTED_OR_MISMATCHED")
		return out,nil
	}
	out.ControlVerified=true
	out.ControlEvidenceReference=proof.EvidenceReference
	out.ControlRelationshipID=edge.ID
	out.Standing="BOTH_TRACKS_VERIFIED"
	return out,nil
}

func foundingRegistrationMatches(input FoundingIdentityInput, b verification.VerifiedRegistrationBinding, now time.Time) bool {
	return b.CaseID == input.VerificationCaseID && b.Subject == (verification.Subject{SubjectType:"ORGANISATION",SubjectID:input.OrganisationID}) &&
		b.SourceID == "esrc_cipc" && b.RegistrationNumber == input.DeclaredRegistrationNumber &&
		b.ClaimID!="" && b.ResultID!="" && b.FreshnessUntil.After(now)
}

func sameControlPercentage(a,b *float64,t domain.CorporateRelationshipType)bool {
	if t==domain.CorpRelOwns {return a!=nil&&b!=nil&&*a==*b&&*a>50&&*a<=100}
	if t==domain.CorpRelControls {return a==nil&&b==nil}
	return false
}
func evidenceRefPresent(refs []string,want string)bool {
	for _,v:=range refs{if v==want{return true}}
	return false
}

// A compiled fingerprint is available for audit output without publishing
// a raw control document, secret, company director or beneficiary identity.
func ControlProofFingerprint(proof IndependentControlProof) string {
	sum:=sha256.Sum256(proof.signingPayload())
	return "sha256:"+hex.EncodeToString(sum[:])
}
