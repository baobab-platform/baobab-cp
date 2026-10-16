# INT-02 PRIMARY Organisation runtime closure

Authority: Accepted ADR-BCP-027 §7/LA-T02, ADR-BCP-018 attestation, Shared
control-plane/v1 optional caller assertion and control-plane/v2 PRIMARY model.

## Scope and compatibility

CP migration 000102 already distinguishes governed tenants with
`primary_organisation_enforced=true` from explicitly grandfathered BOOTSTRAP
rows awaiting migration. This increment reads that private durable flag into
the tenant model (never JSON) and checks PRIMARY authority on every governed
runtime context resolution. There is no new request field, consumer override,
feature switch, fabricated Organisation or inference from DEFAULT LegalEntity.

The authoritative mappings are re-read at the decision's evaluation time.
Exactly one ACTIVE/effective PRIMARY must point to an ACTIVE organisation-kind
canonical entity. Missing, future, expired, suspended, ambiguous or inaccessible
authority denies before context persistence. When a v1 caller omits its hint,
CP derives the PRIMARY and records mapping provenance. A supplied caller
Organisation retains v1's separate attestation semantics, but cannot bypass the
tenant's PRIMARY integrity. A related company's LegalEntity never substitutes.

Explicit legacy non-enforcing BOOTSTRAP rows keep pinned v1 compatibility and
remain migration-review items; they are not ordinary Organisation-first tenants
or acceptance evidence. INT-02 must test a governed mapping that naturally
expires, not relabel a permitted migration exception as an operational failure.
Existing registration constraints prevent enforced tenants from downgrading.

## Verification and remaining acceptance

Service tests cover omitted/explicit hints, stale projections, missing/expired/
future/suspended/ambiguous/wrong-tenant mappings, additional-only roles, inactive
or wrong-kind entities and unavailable authority. HTTP tests verify both the
persisted positive and denial without a context handle. The Postgres migration
regression checks that GetTenant carries the stored enforcement flag. Existing
legacy v1 context tests protect compatibility.

Source checks are not a real L4 receipt. The Infrastructure harness must pin
the merged CP revision and demonstrate current PRIMARY denial and positive
canonical Principal/Organisation/Tenant/market from one real run. Certification,
provider health, corporate/external isolation, protected staging and production
acceptance remain separate obligations.
