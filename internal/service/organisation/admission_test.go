package organisation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baobab-platform/baobab-cp/internal/contracttest"
	"github.com/baobab-platform/baobab-cp/internal/domain"
	"github.com/baobab-platform/baobab-cp/internal/repository"
)

// registeredTenant is a tenant as registration leaves it: a PRIMARY
// organisation and DEFAULT legal entity recorded as unverified claims and no
// platform relationship (admission assigns that).
func (e *env) registeredTenant(t *testing.T) (tenantID, org, le string) {
	t.Helper()
	le = "LE-" + strings.ToUpper(token())
	tenantID = e.tenantFor(t, le)
	org = e.canonicalOrganisation(t)
	if _, err := (&Provisioner{Orgs: e.repo}).ProvisionTenantOrganisation(e.ctx, ProvisionRequest{TenantID: tenantID, CanonicalEntityID: org,
		LegalEntityID: le, DisplayName: "Registered " + org[:8], SourceAuthority: "control-plane-registration", EffectiveFrom: e.at,
		SkipPlatformRel: true, Actor: actor()}); err != nil {
		t.Fatal(err)
	}
	return tenantID, org, le
}

// verifiedOrganisation is an existing organisation whose legal entity
// carries the given governed identifier.
func (e *env) verifiedOrganisation(t *testing.T, id domain.OrganisationIdentifier) (org, le string) {
	t.Helper()
	_, org, le = e.registeredTenant(t)
	if _, err := e.repo.RecordLegalEntityClaims(e.ctx, le, repository.LegalEntityClaims{LegalName: "Existing " + org[:8], Jurisdiction: "KE",
		Identifiers: []domain.OrganisationIdentifier{id}}, actor()); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.VerifyLegalEntityProfile(e.ctx, le, e.evidence(), actor()); err != nil {
		t.Fatal(err)
	}
	return org, le
}

func registration(value string) domain.OrganisationIdentifier {
	return domain.OrganisationIdentifier{Type: "COMPANY_REGISTRATION", Value: value, IssuingJurisdiction: "KE"}
}

func (e *env) recordedUnder(t *testing.T, a repository.AuditActor) (audits int, events []string) {
	t.Helper()
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1::uuid`, a.CorrelationID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	return audits, e.outboxTypes(t, a.CorrelationID)
}

// TestOrganisationAdmissionOnboardsNewOrganisation is ADR-BCP-018's external
// customer flow (sections 68, 108) for an organisation Baobab has not seen:
// applicant data becomes claims, the reviewer's evidence verifies legal
// identity, declared ownership is recorded for review only, the platform
// relationship is EXTERNAL_CLIENT, the organisation joins a new account, and
// replaying the decision changes nothing.
func TestOrganisationAdmissionOnboardsNewOrganisation(t *testing.T) {
	e := newEnv(t)
	onboarder := &AdmissionOnboarder{Orgs: e.repo, Now: func() time.Time { return e.at.Add(time.Hour) }}
	tenantID, org, le := e.registeredTenant(t)
	parentID := registration("PARENT-" + token())
	parent, _ := e.verifiedOrganisation(t, parentID)
	decision := "adm_" + token()
	req := AdmissionRequest{
		AdmissionDecisionID: decision,
		Applicant: ApplicantOrganisation{LegalName: "Beta Logistics Limited", Jurisdiction: "KE",
			RegistrationIdentifiers: []domain.OrganisationIdentifier{{Type: "COMPANY_REGISTRATION", Value: "BETA-" + token(), IssuingJurisdiction: "KE", Verified: true}}},
		LegalVerification: &LegalVerification{EvidenceReferences: []string{"evd_certificate"}, Reason: "registry extract checked"},
		CorporateClaims: []CorporateClaim{
			{RelationshipType: domain.CorpRelOwns, ApplicantRole: "TARGET", CounterpartyOrganisationID: parent, OwnershipPercentage: ptr(100.0)},
			{RelationshipType: domain.CorpRelControls, ApplicantRole: "TARGET", CounterpartyIdentifiers: []domain.OrganisationIdentifier{parentID}},
			{RelationshipType: domain.CorpRelOwns, ApplicantRole: "SOURCE", CounterpartyIdentifiers: []domain.OrganisationIdentifier{registration("NOBODY-" + token())}},
		},
		PlatformAccount: AccountAssignment{Mode: AccountNew, DisplayName: "Beta Group"},
	}

	first := actor()
	out, err := onboarder.Onboard(e.ctx, tenantID, req, first)
	if err != nil {
		t.Fatal(err)
	}
	if out.IdentityResolution != IdentityNewOrganisation || out.OrganisationID != org || out.LegalEntityID != le ||
		out.LegalEntityVerificationState != string(domain.VerificationVerified) {
		t.Fatalf("outcome = %+v", out)
	}
	statuses := []string{}
	for _, c := range out.CorporateClaims {
		statuses = append(statuses, c.Status)
	}
	if !slices.Equal(statuses, []string{ClaimRecorded, ClaimRecorded, ClaimCounterpartyUnknown}) {
		t.Fatalf("claims = %+v", out.CorporateClaims)
	}

	// Applicant evidence is recorded unverified even though it said otherwise.
	profile, err := e.repo.GetLegalEntityProfile(e.ctx, le)
	if err != nil || profile.LegalName != "Beta Logistics Limited" || len(profile.RegistrationIdentifiers) != 1 || profile.RegistrationIdentifiers[0].Verified {
		t.Fatalf("legal entity profile = %+v %v", profile, err)
	}
	if o, err := e.repo.GetOrganisation(e.ctx, org); err != nil || o.VerificationState != domain.VerificationVerified {
		t.Fatalf("organisation = %+v %v", o, err)
	}
	// Declared ownership is a claim for review, never a verified fact.
	for _, c := range out.CorporateClaims[:2] {
		var state, status, authority, source, target string
		if err := e.admin.QueryRow(e.ctx, `SELECT verification_state, status, source_authority, source_organisation_id::text, target_organisation_id::text
			FROM registry.corporate_relationship WHERE corporate_relationship_id=$1::uuid`, mustRow(t, c.CorporateRelationshipID)).Scan(&state, &status, &authority, &source, &target); err != nil {
			t.Fatal(err)
		}
		if state != "PENDING_REVIEW" || status != "PENDING" || authority != "applicant-submission" || source != parent || target != org {
			t.Fatalf("claim %s: %s %s %s %s->%s", c.CorporateRelationshipID, state, status, authority, source, target)
		}
	}
	// Server-authoritative EXTERNAL_CLIENT, verified on the decision.
	pr, err := e.repo.GetPlatformRelationship(e.ctx, out.PlatformRelationshipID)
	if err != nil || pr.RelationshipType != domain.PlatformRelExternalClient || pr.VerificationState != domain.VerificationVerified ||
		pr.AdmissionDecisionID != decision || !slices.Equal(pr.EvidenceReferences, []string{"admission-decision:" + decision}) {
		t.Fatalf("platform relationship = %+v %v", pr, err)
	}
	if eligible, err := (&EligibilityResolver{Orgs: e.repo}).ResolveInternalEligibility(e.ctx, org, e.at.Add(2*time.Hour)); err != nil || eligible {
		t.Fatalf("an external client must never be INTERNAL-eligible: %v %v", eligible, err)
	}
	members, err := e.repo.ListPlatformAccountMemberships(e.ctx, out.PlatformAccountID, e.at.Add(2*time.Hour))
	if err != nil || len(members) != 1 || members[0].OrganisationID != org || members[0].AccountRole != domain.AccountRolePrimaryOrganisation {
		t.Fatalf("account memberships = %+v %v", members, err)
	}
	if _, events := e.recordedUnder(t, first); !slices.Contains(events, "com.baobab-platform.control-plane.platform-relationship.activated.v1") {
		t.Fatalf("onboarding events = %v", events)
	}

	// Replaying the decision converges and records nothing.
	replay := actor()
	again, err := onboarder.Onboard(e.ctx, tenantID, req, replay)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatalf("replay outcome differs:\n%s\n%s", a, b)
	}
	if audits, events := e.recordedUnder(t, replay); audits != 0 || len(events) != 0 {
		t.Fatalf("replay recorded audits=%d events=%v", audits, events)
	}
}

// TestOrganisationAdmissionQuarantinesPossibleDuplicates proves sections
// 99-100: a governed identifier held by another organisation quarantines the
// admission (formatting differences do not hide it, a shared name alone
// does not trigger it), and only a reviewer's explicit resolution proceeds:
// confirming a new organisation, or re-pointing the tenant at the existing
// one without merging or deleting anything.
func TestOrganisationAdmissionQuarantinesPossibleDuplicates(t *testing.T) {
	e := newEnv(t)
	onboarder := &AdmissionOnboarder{Orgs: e.repo, Now: func() time.Time { return e.at.Add(time.Hour) }}
	value := "PVT-2021-" + token()
	existing, existingLE := e.verifiedOrganisation(t, registration(value))

	tenantID, registered, registeredLE := e.registeredTenant(t)
	req := AdmissionRequest{AdmissionDecisionID: "adm_" + token(),
		// Same registration, formatted differently.
		Applicant: ApplicantOrganisation{LegalName: "Acme Foods Kenya", RegistrationIdentifiers: []domain.OrganisationIdentifier{
			{Type: "COMPANY_REGISTRATION", Value: strings.ToLower(strings.ReplaceAll(value, "-", " ")), IssuingJurisdiction: "ke"}}},
		PlatformAccount: AccountAssignment{Mode: AccountNone}}
	quarantined := actor()
	out, err := onboarder.Onboard(e.ctx, tenantID, req, quarantined)
	if err != nil || out.IdentityResolution != IdentityQuarantined || !slices.Equal(out.CandidateOrganisationIDs, []string{existing}) || out.OrganisationID != "" {
		t.Fatalf("quarantine: %+v %v", out, err)
	}
	if audits, events := e.recordedUnder(t, quarantined); audits != 0 || len(events) != 0 {
		t.Fatalf("a quarantined admission wrote audits=%d events=%v", audits, events)
	}

	// A shared name alone never matches.
	nameOnly := req
	nameOnly.AdmissionDecisionID = "adm_" + token()
	nameOnly.Applicant = ApplicantOrganisation{LegalName: "Existing " + existing[:8], RegistrationIdentifiers: []domain.OrganisationIdentifier{registration("OTHER-" + token())}}
	otherTenant, _, _ := e.registeredTenant(t)
	if out, err := onboarder.Onboard(e.ctx, otherTenant, nameOnly, actor()); err != nil || out.IdentityResolution != IdentityNewOrganisation {
		t.Fatalf("name-only match: %+v %v", out, err)
	}

	// Reviewer: it is the existing organisation. The tenant is re-pointed;
	// the registered organisation is kept, not merged.
	resolved := req
	resolved.IdentityResolution = &IdentityResolution{Decision: UseExistingOrganisation, OrganisationID: existing, Reason: "same registration number"}
	out, err = onboarder.Onboard(e.ctx, tenantID, resolved, actor())
	if err != nil || out.IdentityResolution != IdentityExistingOrganisation || out.OrganisationID != existing || out.LegalEntityID != existingLE {
		t.Fatalf("reviewer-resolved existing: %+v %v", out, err)
	}
	mappings, err := e.repo.ListTenantOrganisationMappings(e.ctx, tenantID, e.at.Add(2*time.Hour))
	if err != nil || len(mappings) != 1 || mappings[0].OrganisationID != existing || mappings[0].ID != out.TenantOrganisationMappingID {
		t.Fatalf("tenant mappings after re-point: %+v %v", mappings, err)
	}
	var tenantLE string
	if err := e.admin.QueryRow(e.ctx, `SELECT legal_entity_id FROM tenants WHERE tenant_id=$1`, tenantID).Scan(&tenantLE); err != nil || tenantLE != existingLE {
		t.Fatalf("tenant legal entity projection = %q %v; want %s", tenantLE, err, existingLE)
	}
	var endedMappings int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM registry.tenant_organisation_mapping WHERE tenant_id=$1 AND organisation_id=$2::uuid AND status='ENDED'`,
		tenantID, registered).Scan(&endedMappings); err != nil || endedMappings != 1 {
		t.Fatalf("the registered mapping must be ended, not deleted: %d %v", endedMappings, err)
	}
	if o, err := e.repo.GetOrganisation(e.ctx, registered); err != nil || o == nil {
		t.Fatalf("the registered organisation must survive the re-point: %+v %v", o, err)
	}
	if p, err := e.repo.GetLegalEntityProfile(e.ctx, registeredLE); err != nil || p == nil || p.OrganisationID != registered {
		t.Fatalf("the registered legal entity must be untouched: %+v %v", p, err)
	}
	if p, err := e.repo.GetLegalEntityProfile(e.ctx, existingLE); err != nil || p.VerificationState != domain.VerificationVerified || p.LegalName == "Acme Foods Kenya" {
		t.Fatalf("applicant claims must never overwrite a verified legal entity: %+v %v", p, err)
	}

	// Reviewer: genuinely a different organisation despite the collision.
	confirmTenant, confirmOrg, _ := e.registeredTenant(t)
	confirmed := req
	confirmed.AdmissionDecisionID = "adm_" + token()
	confirmed.IdentityResolution = &IdentityResolution{Decision: ConfirmNewOrganisation, Reason: "registration number reissued by the registry"}
	if out, err := onboarder.Onboard(e.ctx, confirmTenant, confirmed, actor()); err != nil || out.IdentityResolution != IdentityNewOrganisation || out.OrganisationID != confirmOrg {
		t.Fatalf("reviewer-confirmed new: %+v %v", out, err)
	}
}

// TestOrganisationAdmissionRefusals covers the fail-closed rules: first-party
// organisations are never admitted as external clients (section 70), unknown
// accounts and malformed requests are refused.
func TestOrganisationAdmissionRefusals(t *testing.T) {
	e := newEnv(t)
	onboarder := &AdmissionOnboarder{Orgs: e.repo, Now: func() time.Time { return e.at.Add(time.Hour) }}
	base := func() AdmissionRequest {
		return AdmissionRequest{AdmissionDecisionID: "adm_" + token(),
			Applicant:       ApplicantOrganisation{LegalName: "Gamma Ltd", RegistrationIdentifiers: []domain.OrganisationIdentifier{registration("GAMMA-" + token())}},
			PlatformAccount: AccountAssignment{Mode: AccountNone}}
	}

	tenantID, org, _ := e.registeredTenant(t)
	if _, err := e.repo.EnsurePlatformRelationship(e.ctx, domain.PlatformRelationship{PlatformID: DefaultPlatformID, OrganisationID: org,
		RelationshipType: domain.PlatformRelOwner, VerificationState: domain.VerificationPendingReview, Status: domain.RelationshipStatusPending,
		EffectiveFrom: e.at, SourceAuthority: "platform-governance"}, actor()); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarder.Onboard(e.ctx, tenantID, base(), actor()); !errors.Is(err, ErrFirstPartyOrganisation) {
		t.Fatalf("first-party organisation: %v", err)
	}

	tenant2, _, _ := e.registeredTenant(t)
	missing := base()
	missing.PlatformAccount = AccountAssignment{Mode: AccountExisting, PlatformAccountID: domain.NewResourceID(domain.PlatformAccountIDPrefix)}
	if _, err := onboarder.Onboard(e.ctx, tenant2, missing, actor()); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown account: %v", err)
	}
	if _, err := onboarder.Onboard(e.ctx, "tn_"+token(), base(), actor()); err == nil || !strings.Contains(err.Error(), "no registered primary organisation") {
		t.Fatalf("unregistered tenant: %v", err)
	}
}

func TestAdmissionRequestValidation(t *testing.T) {
	valid := AdmissionRequest{AdmissionDecisionID: "adm_1",
		Applicant:       ApplicantOrganisation{LegalName: "X", RegistrationIdentifiers: []domain.OrganisationIdentifier{registration("1")}},
		PlatformAccount: AccountAssignment{Mode: AccountNone}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AdmissionRequest){
		"no decision":    func(r *AdmissionRequest) { r.AdmissionDecisionID = "" },
		"no identifiers": func(r *AdmissionRequest) { r.Applicant.RegistrationIdentifiers = nil },
		"existing without id": func(r *AdmissionRequest) {
			r.IdentityResolution = &IdentityResolution{Decision: UseExistingOrganisation, Reason: "x"}
		},
		"resolution without reason": func(r *AdmissionRequest) {
			r.IdentityResolution = &IdentityResolution{Decision: ConfirmNewOrganisation}
		},
		"verification without proof": func(r *AdmissionRequest) { r.LegalVerification = &LegalVerification{Reason: "x"} },
		"claim with two counterparties": func(r *AdmissionRequest) {
			r.CorporateClaims = []CorporateClaim{{RelationshipType: domain.CorpRelOwns, ApplicantRole: "SOURCE", CounterpartyOrganisationID: "o",
				CounterpartyIdentifiers: []domain.OrganisationIdentifier{registration("2")}}}
		},
		"new account without name":     func(r *AdmissionRequest) { r.PlatformAccount = AccountAssignment{Mode: AccountNew} },
		"permission-like account role": func(r *AdmissionRequest) { r.PlatformAccount.AccountRole = "ADMIN" },
	} {
		r := valid
		mutate(&r)
		if r.Validate() == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func ptr(v float64) *float64 { return &v }

// TestOrganisationAdmissionConformsToSharedContract validates a real request
// and the outcomes onboarding produces against baobab-platform/shared
// contracts/organisation/v1/admission.schema.json. It skips while the pinned
// Shared revision predates that file.
func TestOrganisationAdmissionConformsToSharedContract(t *testing.T) {
	dir := contracttest.SharedDir(t)
	if _, err := os.Stat(filepath.Join(dir, "contracts", "organisation", "v1", "admission.schema.json")); err != nil {
		t.Skip("pinned baobab-platform/shared revision has no contracts/organisation/v1/admission.schema.json yet")
	}
	e := newEnv(t)
	onboarder := &AdmissionOnboarder{Orgs: e.repo, Now: func() time.Time { return e.at.Add(time.Hour) }}
	requestSchema := contracttest.CompileSchema(t, dir, "organisation/v1/admission.schema.json#/$defs/OrganisationAdmissionRequest")
	outcomeSchema := contracttest.CompileSchema(t, dir, "organisation/v1/admission.schema.json#/$defs/OrganisationAdmissionOutcome")

	value := "DELTA-" + token()
	parent, _ := e.verifiedOrganisation(t, registration("PARENT-"+token()))
	tenantID, _, _ := e.registeredTenant(t)
	req := AdmissionRequest{AdmissionDecisionID: "adm_" + token(), ApplicationID: "app_" + token(),
		Applicant:         ApplicantOrganisation{LegalName: "Delta Ltd", Jurisdiction: "KE", RegistrationIdentifiers: []domain.OrganisationIdentifier{registration(value)}},
		LegalVerification: &LegalVerification{EvidenceReferences: []string{"evd_certificate"}, Reason: "checked"},
		CorporateClaims:   []CorporateClaim{{RelationshipType: domain.CorpRelOwns, ApplicantRole: "TARGET", CounterpartyOrganisationID: parent, OwnershipPercentage: ptr(60)}},
		PlatformAccount:   AccountAssignment{Mode: AccountNew, DisplayName: "Delta"},
	}
	contracttest.ValidateJSON(t, requestSchema, req)
	out, err := onboarder.Onboard(e.ctx, tenantID, req, actor())
	if err != nil {
		t.Fatal(err)
	}
	contracttest.ValidateJSON(t, outcomeSchema, out)

	duplicateTenant, _, _ := e.registeredTenant(t)
	dup := req
	dup.AdmissionDecisionID, dup.CorporateClaims, dup.LegalVerification = "adm_"+token(), nil, nil
	dup.PlatformAccount = AccountAssignment{Mode: AccountNone}
	quarantined, err := onboarder.Onboard(e.ctx, duplicateTenant, dup, actor())
	if err != nil || quarantined.IdentityResolution != IdentityQuarantined {
		t.Fatalf("duplicate: %+v %v", quarantined, err)
	}
	contracttest.ValidateJSON(t, outcomeSchema, quarantined)
}
