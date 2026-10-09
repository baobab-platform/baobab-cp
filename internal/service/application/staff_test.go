package application_test

import (
    "encoding/json"
    "errors"
    "fmt"
    "testing"

    "github.com/baobab-platform/baobab-cp/internal/domain"
    "github.com/baobab-platform/baobab-cp/internal/service/application"
)

// PostgreSQL-backed NBO-01 acceptance: the same audited domain lifecycle is
// used by assisted and internal-group applications. No privilege is minted.
func TestNBOStaffCreatedApplicationIsGoverned(t *testing.T) {
    e := newEnv(t)
    e.svc.Principals = e.repo
    applicant, maker, independent := e.principal(t), e.principal(t), e.principal(t)
    raw := []byte(fmt.Sprintf(`{"application_channel":"INTERNAL_GROUP","applicant_principal_id":%q,
      "reason":"Staff-assisted founding enterprise intake for independent review",
      "draft":%s}`, applicant.PrincipalID, completeDraft))
    const key = "nbo-staff-admission-retry-0001"

    created, replay, err := e.svc.CreateForStaff(e.ctx, maker, raw, key)
    if err != nil || replay {
        t.Fatalf("create staff application: replay=%v err=%v", replay, err)
    }
    if created.Channel != domain.ChannelInternalGroup || created.Status != domain.ApplicationDraft ||
       created.OpenedByStaffPrincipalID != maker.PrincipalID || created.ApplicantPrincipalID != applicant.PrincipalID ||
       created.Decision != nil {
        t.Fatalf("staff intake must remain applicant-owned, auditable and DRAFT: %+v", created)
    }
    stored, err := e.svc.Get(e.ctx, created.ID)
    if err != nil || stored.OpenedByStaffPrincipalID != maker.PrincipalID {
        t.Fatalf("maker provenance must survive PostgreSQL read: %+v, %v", stored, err)
    }
    repeated, replay, err := e.svc.CreateForStaff(e.ctx, maker, raw, key)
    if err != nil || !replay || repeated.ID != created.ID {
        t.Fatalf("same request/key must converge: %+v, replay=%v, err=%v", repeated, replay, err)
    }
    changed := []byte(fmt.Sprintf(`{"application_channel":"ASSISTED_ENTERPRISE","applicant_principal_id":%q,
      "reason":"Staff-assisted founding enterprise intake for independent review",
      "draft":%s}`, applicant.PrincipalID, completeDraft))
    if _, _, err := e.svc.CreateForStaff(e.ctx, maker, changed, key); !errors.Is(err, application.ErrIdempotencyConflict) {
        t.Fatalf("reused key with changed channel must conflict: %v", err)
    }
    // The applicant retains the normal submit rights. The maker cannot
    // approve the subsequent admission, even if they hold decide scope.
    if _, err := e.svc.Submit(e.ctx, applicant, created.ID); err != nil { t.Fatal(err) }
    if _, err := e.svc.BeginValidation(e.ctx, maker, created.ID); err != nil { t.Fatal(err) }
    if _, err := e.svc.BeginReview(e.ctx, maker, created.ID); err != nil { t.Fatal(err) }
    approval := []byte(`{"decision":"APPROVED","reason":"Approved for scoped admission only.",
      "approved_subscription_type":"COMMERCIAL","approved_market_scope":["UG"],
      "approved_product_requirements":["b2b-trade"],"evidence_references":["evd_kilima_certificate"]}`)
    for _, rejected := range []application.Actor{applicant, maker} {
        if _, err := e.svc.Decide(e.ctx, rejected, created.ID, approval); !errors.Is(err, application.ErrSelfDecision) {
            t.Fatalf("applicant and staff maker must never self-decide: %v", err)
        }
    }
    decision, err := e.svc.Decide(e.ctx, independent, created.ID, approval)
    if err != nil || decision.DecidedBy != independent.PrincipalID ||
       decision.ApprovedSubscriptionType != domain.SubscriptionCommercial {
        t.Fatalf("independent admission decision: %+v, %v", decision, err)
    }
    if a := e.audits(t, created.ID); len(a) == 0 || a[0] != "client_application.staff_created" {
        t.Fatalf("staff-created application must preserve a distinct audit action: %v", a)
    }
}

func TestNBOStaffCreationRejectsAbsentApplicantAndFabricatedAuthority(t *testing.T) {
    e := newEnv(t)
    e.svc.Principals = e.repo
    maker := e.principal(t)
    invalidPrincipal := domain.NewPrincipalID()
    request := func(channel, applicantID string, draft string) []byte {
        value := map[string]any{"application_channel":channel,"applicant_principal_id":applicantID,
           "reason":"Governed assisted intake and independent authorisation", "draft":json.RawMessage(draft)}
        raw, _ := json.Marshal(value)
        return raw
    }
    if _, _, err := e.svc.CreateForStaff(e.ctx, maker, request("INTERNAL_GROUP", invalidPrincipal, `{}`), ""); !errors.Is(err, application.ErrApplicantNotRegistered) {
        t.Fatalf("unregistered applicant must be refused: %v", err)
    }
    if _, _, err := e.svc.CreateForStaff(e.ctx, maker, request("INTERNAL_GROUP", maker.PrincipalID, `{}`), ""); !errors.Is(err, application.ErrApplicantNotRegistered) {
        t.Fatalf("maker cannot impersonate an applicant: %v", err)
    }
    applicant := e.principal(t)
    for _, badChannel := range []string{"INTERNAL", "COMMERCIAL", "SELF_SERVICE"} {
        if _, _, err := e.svc.CreateForStaff(e.ctx, maker, request(badChannel, applicant.PrincipalID, `{}`), ""); err == nil {
            t.Fatalf("staff creation must refuse subscription type or applicant channel %s", badChannel)
        }
    }
    if _, _, err := e.svc.CreateForStaff(e.ctx, maker,
        request("INTERNAL_GROUP", applicant.PrincipalID, `{"status":"APPROVED"}`), ""); err == nil {
        t.Fatal("staff create cannot set admission or verification authority")
    }
}
