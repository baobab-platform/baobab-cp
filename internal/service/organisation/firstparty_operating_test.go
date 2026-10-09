package organisation

import (
    "fmt"
    "strings"
    "testing"

    "github.com/baobab-platform/baobab-cp/internal/domain"
)

// Annotated first-party records are approved platform-business identities,
// NOT incorporation proofs. The reconciler must not create a legal person or
// infer a tenant's PRIMARY Organisation from a corporate group relationship.
func TestAnnotatedFirstPartyOperatingIdentityNotLegalVerification(t *testing.T){
    e:=newEnv(t)
    suffix:=strings.ToUpper(token())[:10]
    zb:="FZB-"+suffix
    eq:="FEQ-"+suffix
    th:="FTH-"+suffix
    yaml:=fmt.Sprintf(`schema:
  name: "baobab-platform-legal-entity-registry"
entities:
  - id: %q
    legal_name: "ZuriBeans"
    role: subsidiary
    identity_class: OPERATING_BUSINESS
    incorporation_claim: NOT_INCORPORATED
  - id: %q
    legal_name: "Equator & Estate Co."
    role: subsidiary
    identity_class: OPERATING_BUSINESS
    incorporation_claim: NOT_INCORPORATED
  - id: %q
    legal_name: "Thamani Global"
    role: subsidiary
    identity_class: LEGAL_PERSON
    incorporation_claim: REGISTERED_CLAIMED
`,zb,eq,th)
    registry,err:=ParseFirstPartyRegistry([]byte(yaml))
    if err!=nil{t.Fatal(err)}
    service:=&FirstPartyReconciler{Orgs:e.repo}
    report,err:=service.Reconcile(e.ctx,registry,actor())
    if err!=nil{t.Fatal(err)}
    if report.Blocking{t.Fatalf("newly recognised declarations must not produce false verified conflict: %+v",report)}
    for _,entry:=range report.Entities{
        if !domain.IsUUID(entry.Outcome.OrganisationID)||!entry.Outcome.Created{
            t.Fatalf("missing operating Organisation for %s: %+v",entry.LegalEntityID,entry)
        }
        var profiles int
        if err=e.admin.QueryRow(e.ctx,`SELECT count(*) FROM registry.legal_entity_profile WHERE legal_entity_id=$1`,entry.LegalEntityID).Scan(&profiles);err!=nil||profiles!=0{
            t.Fatalf("registry fabricated legal person %s: %d %v",entry.LegalEntityID,profiles,err)
        }
        if len(entry.TenantsMapped)!=0{
            t.Fatalf("legal affiliation fabricated tenant PRIMARY for %s",entry.LegalEntityID)
        }
    }
    replay,err:=service.Reconcile(e.ctx,registry,actor())
    if err!=nil{t.Fatal(err)}
    for _,entry:=range replay.Entities{
        if entry.Outcome.Created||len(entry.Outcome.Changes)!=0||len(entry.TenantsMapped)!=0{
            t.Fatalf("first-party replay mutated %s: %+v",entry.LegalEntityID,entry)
        }
    }
}

func TestFirstPartyRejectsMixedOrFalseIncorporationClaims(t *testing.T){
    for name,claim:=range map[string]string{
        "missing class":`incorporation_claim: NOT_INCORPORATED`,
        "invalid operating company":`identity_class: OPERATING_BUSINESS
    incorporation_claim: REGISTERED_CLAIMED`,
        "unknown legal status":`identity_class: LEGAL_PERSON
    incorporation_claim: INCORPORATED_VERIFIED`,
    } {
        src:=`schema:
  name: baobab-platform-legal-entity-registry
entities:
  - id: TEST-FIRSTPARTY
    legal_name: Test Operating Name
    role: subsidiary
    `+claim+"\n"
        if _,err:=ParseFirstPartyRegistry([]byte(src));err==nil{
            t.Errorf("%s accepted",name)
        }
    }
}
