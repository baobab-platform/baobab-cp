package organisation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

type foundingRegistrationFixture struct {
	items map[string]verification.VerifiedRegistrationBinding
}

func (r foundingRegistrationFixture) ResolveVerifiedRegistration(_ context.Context, id string, _ verification.Subject) (verification.VerifiedRegistrationBinding, error) {
	v, ok := r.items[id]
	if !ok {
		return v, errors.New("no current registry binding")
	}
	return v, nil
}

type foundingCorporateFixture struct {
	edges []domain.CorporateRelationship
	err   error
}

func (r foundingCorporateFixture) ListCorporateRelationshipsByOrganisation(_ context.Context, _ string, _ time.Time) ([]domain.CorporateRelationship, error) {
	return r.edges, r.err
}

type foundingProofFixture struct {
	proof IndependentControlProof
	err   error
}

func (r foundingProofFixture) ReadIndependentControlProof(_ context.Context, _ string) (IndependentControlProof, error) {
	return r.proof, r.err
}

func setupPEO04AFixture(t *testing.T) (FoundingEvidenceReconciler, FoundingIdentityInput, FoundingIdentityInput, ed25519.PrivateKey) {
	t.Helper()
	now := time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)
	parent := FoundingIdentityInput{RegistryID: "NABHOLD", OrganisationID: "org_nabhold_fixture",
		VerificationCaseID: "vcase_nabhold_fixture", DeclaredRegistrationNumber: "synthetic-parent-registration"}
	child := FoundingIdentityInput{RegistryID: "THAMANI-GLOBAL", OrganisationID: "org_thamani_fixture",
		VerificationCaseID: "vcase_thamani_fixture", DeclaredRegistrationNumber: "synthetic-child-registration"}
	parentBinding := verification.VerifiedRegistrationBinding{CaseID: parent.VerificationCaseID,
		Subject:            verification.Subject{SubjectType: "ORGANISATION", SubjectID: parent.OrganisationID},
		RegistrationNumber: parent.DeclaredRegistrationNumber, ClaimID: "ecl_nabhold", ResultID: "vres_nabhold",
		SourceID: "esrc_cipc", FreshnessUntil: now.Add(time.Hour)}
	childBinding := verification.VerifiedRegistrationBinding{CaseID: child.VerificationCaseID,
		Subject:            verification.Subject{SubjectType: "ORGANISATION", SubjectID: child.OrganisationID},
		RegistrationNumber: child.DeclaredRegistrationNumber, ClaimID: "ecl_thamani", ResultID: "vres_thamani",
		SourceID: "esrc_cipc", FreshnessUntil: now.Add(time.Hour)}
	majority := 60.0
	control := domain.CorporateRelationship{ID: "crel_synthetic_direct_majority",
		SourceOrganisationID: parent.OrganisationID, TargetOrganisationID: child.OrganisationID,
		RelationshipType: domain.CorpRelOwns, OwnershipPercentage: &majority,
		DirectOrDerived:   domain.CorporateFactDirect,
		VerificationState: domain.VerificationVerified, Status: domain.RelationshipStatusActive,
		EffectiveFrom: now.Add(-time.Hour), SourceAuthority: "independent-audited-register",
		EvidenceReferences: []string{"evr_control_independent_fixture"}, VerifiedBy: "prn_external_checker",
		VerifiedAt: &now}
	proof := IndependentControlProof{
		IssuerID:              "independent-audited-register",
		SourceRecordReference: "external-audit-synthetic-ref",
		EvidenceReference:     "evr_control_independent_fixture",
		EvidenceDigest:        "sha256:123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		RelationshipID:        control.ID, ParentOrganisationID: parent.OrganisationID,
		ChildOrganisationID: child.OrganisationID, ParentRegistration: parent.DeclaredRegistrationNumber,
		ChildRegistration: child.DeclaredRegistrationNumber, RelationshipType: string(control.RelationshipType),
		OwnershipPercentage: &majority, ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(20 * time.Minute),
		SignerID: "external-trust-synthetic"}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proof.Signature = ed25519.Sign(priv, proof.signingPayload())
	return FoundingEvidenceReconciler{
		Registrations: foundingRegistrationFixture{items: map[string]verification.VerifiedRegistrationBinding{
			parent.VerificationCaseID: parentBinding, child.VerificationCaseID: childBinding}},
		Corporate: foundingCorporateFixture{edges: []domain.CorporateRelationship{control}},
		Proofs:    foundingProofFixture{proof: proof},
		Trust: map[string]IndependentControlTrust{
			proof.SignerID: {IssuerID: proof.IssuerID, SignerID: proof.SignerID, PublicKey: pub,
				ProposedBy: "independent-platform-maker", ApprovedBy: "independent-platform-checker", ApprovalRecord: "synthetic-independent-trust-approval",
				ApprovedAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour)}},
		Now: func() time.Time { return now },
	}, parent, child, priv
}

func TestPEO04ARegistrationAndControlAreSeparateVerifiedTracks(t *testing.T) {
	s, parent, child, _ := setupPEO04AFixture(t)
	out, err := s.Reconcile(context.Background(), parent, child)
	if err != nil || !out.NabholdRegistrationVerified || !out.ThamaniRegistrationVerified ||
		!out.ControlVerified || out.Standing != "BOTH_TRACKS_VERIFIED" ||
		out.ControlRelationshipID == "" || out.ControlEvidenceReference == "" {
		t.Fatalf("expected independently corroborated registration and control: %+v err %v", out, err)
	}
	proof := s.Proofs.(foundingProofFixture).proof
	if ControlProofFingerprint(proof) == "" {
		t.Fatal("missing deterministic proof fingerprint")
	}
}

func TestPEO04ANoOwnershipInferenceFromRegisteredCompanies(t *testing.T) {
	s, parent, child, _ := setupPEO04AFixture(t)
	s.Proofs = foundingProofFixture{err: errors.New("ownership source inaccessible")}
	out, err := s.Reconcile(context.Background(), parent, child)
	if err != nil || !out.NabholdRegistrationVerified || !out.ThamaniRegistrationVerified ||
		out.ControlVerified || out.Standing != "PENDING_INDEPENDENT_EVIDENCE" {
		t.Fatalf("CIPC registration incorrectly became ownership: %+v err %v", out, err)
	}
	s, parent, child, _ = setupPEO04AFixture(t)
	s.Registrations = foundingRegistrationFixture{items: map[string]verification.VerifiedRegistrationBinding{}}
	out, err = s.Reconcile(context.Background(), parent, child)
	if err != nil || out.ControlVerified || out.NabholdRegistrationVerified || out.ThamaniRegistrationVerified {
		t.Fatalf("first-party declarations promoted missing CIPC results: %+v %v", out, err)
	}
}

func TestPEO04ARejectContradictoryOrApplicantControlProofs(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*FoundingEvidenceReconciler, *FoundingIdentityInput, *FoundingIdentityInput)
	}{
		{"incorrect child registration", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, c *FoundingIdentityInput) {
			c.DeclaredRegistrationNumber = "forged-number"
		}},
		{"expired official registration", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, c *FoundingIdentityInput) {
			v := s.Registrations.(foundingRegistrationFixture)
			binding := v.items[c.VerificationCaseID]
			binding.FreshnessUntil = s.Now().Add(-time.Minute)
			v.items[c.VerificationCaseID] = binding
		}},
		{"parent company missing source", func(s *FoundingEvidenceReconciler, p *FoundingIdentityInput, _ *FoundingIdentityInput) {
			v := s.Registrations.(foundingRegistrationFixture)
			binding := v.items[p.VerificationCaseID]
			binding.SourceID = "esrc_applicant"
			v.items[p.VerificationCaseID] = binding
		}},
		{"unverified parent relation", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			c.edges[0].VerificationState = domain.VerificationPendingReview
			s.Corporate = c
		}},
		{"shared governance is not ownership", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			c.edges[0].SourceAuthority = "shared-governance"
			s.Corporate = c
		}},
		{"non-controlling minority", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			p := 49.0
			c.edges[0].OwnershipPercentage = &p
			s.Corporate = c
		}},
		{"claimed majority differs from signed proof", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			p := 85.0
			c.edges[0].OwnershipPercentage = &p
			s.Corporate = c
		}},
		{"proof altered after signature", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			p := s.Proofs.(foundingProofFixture)
			p.proof.ChildRegistration = "forged"
			s.Proofs = p
		}},
		{"signer untrusted", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) { s.Trust = nil }},
		{"ambiguous verified ownership", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			c.edges = append(c.edges, c.edges[0])
			s.Corporate = c
		}},
		{"different target organisation", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			c.edges[0].TargetOrganisationID = "org_other"
			s.Corporate = c
		}},
		{"evidence not cited by relationship", func(s *FoundingEvidenceReconciler, _ *FoundingIdentityInput, _ *FoundingIdentityInput) {
			c := s.Corporate.(foundingCorporateFixture)
			c.edges[0].EvidenceReferences = []string{"evr_unrelated"}
			s.Corporate = c
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, p, c, _ := setupPEO04AFixture(t)
			tc.tamper(&s, &p, &c)
			out, err := s.Reconcile(context.Background(), p, c)
			if err == nil && out.ControlVerified {
				t.Fatalf("invalid independent control was promoted: %+v", out)
			}
		})
	}
}

func TestPEO04ARejectSelfSelectedFirstPartyIdentity(t *testing.T) {
	s, p, c, _ := setupPEO04AFixture(t)
	c.RegistryID = "ZURIBEANS"
	if _, err := s.Reconcile(context.Background(), p, c); !errors.Is(err, ErrFoundingReconciliation) {
		t.Fatalf("unincorporated entity misidentified as Thamani: %v", err)
	}
}
