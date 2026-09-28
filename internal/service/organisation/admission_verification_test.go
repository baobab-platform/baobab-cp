package organisation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
	"github.com/baobab-platform/baobab-cp/internal/verification"
)

// Case roles: nobody checks or decides a claim they asserted (ADR-BCP-023
// section 169).
const (
	caseAsserter = "principal:maker"
	caseChecker  = "principal:checker"
)

// verifiedCase works an ORGANISATION_ADMISSION VerificationCase about
// subject through the repository: URSB registry evidence, a
// REGISTRATION_IDENTIFIER and a LEGAL_NAME claim with the given values, a
// registry check and a VERIFIED result for each, decided by a reviewer
// other than the asserter. conclude takes the case to VERIFIED.
func (e *env) verifiedCase(t *testing.T, subject verification.Subject, legalName, regno string, conclude bool) (caseID string, evidenceID string) {
	t.Helper()
	const purpose = "ORGANISATION_ADMISSION"
	status, err := verification.Next(verification.MachineEvidence, "RECEIVED", "accept", verification.ActorPlatform)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := e.repo.RegisterEvidence(e.ctx, verification.Evidence{EvidenceID: domain.NewResourceID("evr"), EvidenceType: "REGISTRY_EXTRACT",
		Subject: subject, Purpose: purpose, SourceID: "esrc_ursb", SourceRecordReference: "URSB-" + regno, ObtainedBy: caseChecker,
		Classification: "INTERNAL", RetentionPolicy: "organisation-admission/v1", Status: status, CreatedAt: e.at, Version: 1},
		"evidence-"+token(), "hash-"+token(), actor())
	if err != nil {
		t.Fatalf("register evidence: %v", err)
	}
	c, err := e.repo.OpenVerificationCase(e.ctx, verification.Case{CaseID: domain.NewResourceID("vcase"), Subject: subject, Purpose: purpose,
		Status: "DRAFT", ClaimIDs: []string{}, OpenedAt: e.at, Version: 1}, caseAsserter, "case-"+token(), "hash-"+token(), actor())
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	var claims []string
	for claimType, value := range map[string]string{claimRegistrationID: regno, claimLegalName: legalName} {
		claim, err := e.repo.AddVerificationClaim(e.ctx, c.CaseID, verification.ClaimSubmission{Subject: subject, ClaimType: claimType,
			Jurisdiction: "UG", ClaimedValue: verification.ClaimedValue{Value: value}, Purpose: purpose, EvidenceIDs: []string{ev.EvidenceID}},
			domain.NewResourceID("ecl"), caseAsserter, e.at, actor())
		if err != nil {
			t.Fatalf("add %s claim: %v", claimType, err)
		}
		claims = append(claims, claim.ClaimID)
	}
	transition := func(command string) {
		t.Helper()
		if c, err = e.repo.TransitionVerificationCase(e.ctx, c.CaseID, c.Version, verification.Transition{Command: command,
			Reason: "every claim checked against the registry"}, e.at, actor()); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	if c, err = e.repo.GetVerificationCase(e.ctx, c.CaseID); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"start_collection", "submit", "begin_verification"} {
		transition(command)
	}
	dimensions := []verification.Dimension{{Dimension: "ISSUER_AUTHORITY", Outcome: "PASSED"}, {Dimension: "CLAIM_MATCH", Outcome: "PASSED"}}
	for _, claimID := range claims {
		check, err := e.repo.RecordVerificationCheck(e.ctx, c.CaseID, verification.CheckRecord{ClaimID: claimID, Method: "MANUAL_REGISTRY_LOOKUP",
			SourceID: "esrc_ursb", SourceRecordReference: "URSB-" + regno, Outcome: "VERIFIED", Dimensions: dimensions, ReasonCodes: []string{},
			EvidenceIDs: []string{ev.EvidenceID}}, domain.NewResourceID("vchk"), caseChecker, e.at, actor())
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if _, err := e.repo.RecordVerificationResult(e.ctx, c.CaseID, verification.ResultRecord{ClaimID: claimID, Outcome: "VERIFIED",
			CheckIDs: []string{check.CheckID}, Dimensions: dimensions, Freshness: "CURRENT", ReasonCodes: []string{}},
			domain.NewResourceID("vres"), caseChecker, e.at, actor()); err != nil {
			t.Fatalf("result: %v", err)
		}
	}
	if conclude {
		if c, err = e.repo.GetVerificationCase(e.ctx, c.CaseID); err != nil {
			t.Fatal(err)
		}
		transition("complete_verified")
		if c.Status != caseStatusVerified {
			t.Fatalf("case concluded %s", c.Status)
		}
	}
	return c.CaseID, ev.EvidenceID
}

func legalEntitySubject(le string) verification.Subject {
	return verification.Subject{SubjectType: subjectLegalEntity, SubjectID: le}
}

// TestOrganisationAdmissionVerifiesThroughItsCase: admission verifies legal
// identity only from a VERIFIED ORGANISATION_ADMISSION case about the
// admitted organisation, whose verified claims are what the applicant
// submitted, and publishes the case, results and evidence by opaque id
// (ADR-BCP-023 sections 143, 191-193). Every other case is refused before
// anything is written.
func TestOrganisationAdmissionVerifiesThroughItsCase(t *testing.T) {
	e := newEnv(t)
	dir := contracttest.SharedDir(t)
	decisions := decisionsByID{}
	onboarder := &AdmissionOnboarder{Orgs: e.repo, Cases: e.repo, Decisions: decisions}
	request := func(regno string) AdmissionRequest {
		return AdmissionRequest{AdmissionDecisionID: "adm_" + token(),
			Applicant: ApplicantOrganisation{LegalName: "Omega Traders Limited", Jurisdiction: "UG",
				RegistrationIdentifiers: []domain.OrganisationIdentifier{{Type: "COMPANY_REGISTRATION", Value: regno, IssuingJurisdiction: "UG"}}},
			PlatformAccount: AccountAssignment{Mode: AccountNone}}
	}
	refused := func(name, tenantID string, req AdmissionRequest, want error) {
		t.Helper()
		a := actor()
		if _, err := onboarder.Onboard(e.ctx, tenantID, req, a); !errors.Is(err, want) {
			t.Fatalf("%s: %v, want %v", name, err, want)
		}
		if audits, events := e.recordedUnder(t, a); audits != 0 || len(events) != 0 {
			t.Fatalf("%s wrote audits=%d events=%v", name, audits, events)
		}
	}

	tenantID, org, le := e.registeredTenant(t)
	regno := "80020" + token()
	req := request(regno)

	req.VerificationCaseID = "vcase_" + token()
	refused("an unknown case", tenantID, req, repository.ErrVerificationNotFound)
	open, _ := e.verifiedCase(t, legalEntitySubject(le), "Omega Traders Limited", regno, false)
	req.VerificationCaseID = open
	refused("a case still VERIFYING", tenantID, req, ErrVerificationCaseUnusable)
	_, _, otherLE := e.registeredTenant(t)
	elsewhere, _ := e.verifiedCase(t, legalEntitySubject(otherLE), "Omega Traders Limited", regno, true)
	req.VerificationCaseID = elsewhere
	refused("a case about another legal entity", tenantID, req, ErrVerificationCaseUnusable)
	otherName, _ := e.verifiedCase(t, legalEntitySubject(le), "Omega Holdings Limited", regno, true)
	req.VerificationCaseID = otherName
	refused("a case that verified another legal name", tenantID, req, ErrVerificationCaseUnusable)
	otherNumber, _ := e.verifiedCase(t, legalEntitySubject(le), "Omega Traders Limited", "99999"+token(), true)
	req.VerificationCaseID = otherNumber
	refused("a case that verified another registration number", tenantID, req, ErrVerificationCaseUnusable)
	if p, err := e.repo.GetLegalEntityProfile(e.ctx, le); err != nil || p.VerificationState == domain.VerificationVerified {
		t.Fatalf("a refused case verified the legal entity: %+v %v", p, err)
	}

	caseID, evidenceID := e.verifiedCase(t, legalEntitySubject(le), "  omega traders LIMITED ", regno, true)
	req.VerificationCaseID = caseID
	first := actor()
	out, err := onboarder.Onboard(e.ctx, tenantID, req, first)
	if err != nil {
		t.Fatal(err)
	}
	if out.LegalEntityVerificationState != string(domain.VerificationVerified) {
		t.Fatalf("outcome = %+v", out)
	}
	profile, err := e.repo.GetLegalEntityProfile(e.ctx, le)
	if err != nil || !slices.Contains(profile.EvidenceReferences, caseReferencePrefix+caseID) || profile.VerifiedBy != first.ActorID {
		t.Fatalf("legal entity profile = %+v %v", profile, err)
	}
	if o, err := e.repo.GetOrganisation(e.ctx, org); err != nil || o.VerificationState != domain.VerificationVerified {
		t.Fatalf("organisation = %+v %v", o, err)
	}

	// Both verified events carry the case, its two results and the evidence
	// by opaque identifier, and conform to Shared.
	for _, typ := range []string{"com.baobab-platform.control-plane.legal-entity.verified.v1", "com.baobab-platform.control-plane.organisation.verified.v1"} {
		var raw []byte
		if err := e.admin.QueryRow(e.ctx, `SELECT payload FROM messaging.outbox WHERE correlation_id=$1::uuid AND event_type=$2`,
			first.CorrelationID, typ).Scan(&raw); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		def := map[string]string{"com.baobab-platform.control-plane.legal-entity.verified.v1": "LegalEntityVerified",
			"com.baobab-platform.control-plane.organisation.verified.v1": "OrganisationVerified"}[typ]
		contracttest.ValidateJSON(t, contracttest.CompileSchema(t, dir, "organisation/v1/events.schema.json#/$defs/"+def), envelope.Data)
		results, _ := envelope.Data["verification_result_ids"].([]any)
		evidence, _ := envelope.Data["evidence_ids"].([]any)
		if envelope.Data["verification_case_id"] != caseID || len(results) != 2 || len(evidence) != 1 || evidence[0] != evidenceID {
			t.Fatalf("%s data = %v", typ, envelope.Data)
		}
	}

	// The usual case is the admitted application's own, worked before
	// approval (sections 191-192): it verifies only the application the
	// request names.
	appTenant, _, appLE := e.registeredTenant(t)
	applicationID, appRegno := "capp_"+token(), "80021"+token()
	appCase, _ := e.verifiedCase(t, verification.Subject{SubjectType: subjectApplication, SubjectID: applicationID}, "Omega Traders Limited", appRegno, true)
	byApplication := request(appRegno)
	byApplication.VerificationCaseID = appCase
	refused("an application's case without the application", appTenant, byApplication, ErrVerificationCaseUnusable)
	byApplication.ApplicationID = "capp_" + token()
	refused("an application's case for another application", appTenant, byApplication, ErrVerificationCaseUnusable)
	byApplication.ApplicationID = applicationID
	refused("an application's case with no admission decision", appTenant, byApplication, ErrVerificationCaseUnusable)
	decisions[byApplication.AdmissionDecisionID] = &domain.AdmissionDecision{ID: byApplication.AdmissionDecisionID,
		ClientApplicationID: "capp_" + token(), Decision: domain.DecisionApproved}
	refused("an application's case named against another application's decision", appTenant, byApplication, ErrVerificationCaseUnusable)
	decisions[byApplication.AdmissionDecisionID] = &domain.AdmissionDecision{ID: byApplication.AdmissionDecisionID,
		ClientApplicationID: applicationID, Decision: domain.DecisionRejected}
	refused("an application's case behind a rejected decision", appTenant, byApplication, ErrVerificationCaseUnusable)
	decisions[byApplication.AdmissionDecisionID] = &domain.AdmissionDecision{ID: byApplication.AdmissionDecisionID,
		ClientApplicationID: applicationID, Decision: domain.DecisionApproved}
	if out, err := onboarder.Onboard(e.ctx, appTenant, byApplication, actor()); err != nil || out.LegalEntityVerificationState != string(domain.VerificationVerified) {
		t.Fatalf("admission through the application's case: %+v %v", out, err)
	}
	if p, err := e.repo.GetLegalEntityProfile(e.ctx, appLE); err != nil || !slices.Contains(p.EvidenceReferences, caseReferencePrefix+appCase) {
		t.Fatalf("legal entity verified from the application's case: %+v %v", p, err)
	}

	// Replaying the decision converges and records nothing.
	replay := actor()
	if _, err := onboarder.Onboard(e.ctx, tenantID, req, replay); err != nil {
		t.Fatal(err)
	}
	if audits, events := e.recordedUnder(t, replay); audits != 0 || len(events) != 0 {
		t.Fatalf("replay recorded audits=%d events=%v", audits, events)
	}
}

// decisionsByID is admission decisions by id; an unknown id has none.
type decisionsByID map[string]*domain.AdmissionDecision

func (d decisionsByID) GetAdmissionDecisionByID(_ context.Context, id string) (*domain.AdmissionDecision, error) {
	return d[id], nil
}
