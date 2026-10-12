# CP Console next design increments — design programme v1

**Based on:** `baobab-platform/baobab-cp` main after PR #352/#353 (12 October 2026).
**Authority:** ADR-BCP-019, ADR-BCP-020, ADR-BCP-026/027, Shared contracts,
IAM ADR-IAM-0033. **Status:** design and staged engineering programme, not
production acceptance or a declaration of ready endpoints.

## Product outcome

Build a Console that combines Azure-class multi-scope navigation, Stripe-class
workflow clarity, Vercel-class polish and Baobab's unique organisation-first,
multi-market, independent-tenant governance. Use the owned Tailwind v4,
shadcn/ui, Lucide and Baobab semantic token foundation. Public marketing,
applicant, organisation-admin and platform-operator experiences are distinct;
the Console is a secured platform administrative surface, not a Digital Estate.

## Ten increments, evidence and sequencing

| Increment | Experience | Authority / source | Acceptance and critical path |
| --- | --- | --- | --- |
| CX-01 | **Role-aware Console shell and navigation** | `GET /v1/admin/effective-authority` (Shared administration/v1), authenticated session | No IAM-role inference; fail closed when authority cannot be read; no browser bearer; filtered links are hints, actions re-authorised. Initial implementation here. |
| CX-02 | **Accessible command palette** | Same caller-bound permitted route catalogue | Ctrl/Cmd+K, native modal semantics, focus/escape, query filtering, no foreign organisation search. Initial implementation here. |
| CX-03 | **Enterprise context switcher** | Explicit accessible organisation/tenant scopes, valid active relationship, CP context resolver | A selected tenant must be fetched and redeemed for that caller. No localStorage-authoritative context; no parent/sibling access; suspend/revocation clears context. First implement scoped list/read and membership contract, then UI. |
| CX-04 | **Organisation 360°** | organisation.view, canonical organisation/tenant/legal-actor/read APIs, provisioning and evidence references | First-party legal status never upgraded from applicant claim; separate organisation identity, incorporated legal entity, legal-actor mandates, relationships, accounts, tenants, markets and service subscriptions. Each tab shows authoritative provenance and empty/denied states. |
| CX-05 | **Searchable, filterable data tables** | Scoped and paginated CP list APIs | Server-side pagination/filter/sort; debounced query; URL-synchronised view; no cross-tenant aggregation without CP authorisation. Export is a separate permission and privacy decision. |
| CX-06 | **Guided onboarding** | Existing v2 application/admission/authorisation BFF and evolving evidence contracts | Save/resume, relevant evidence only, separate submit/review/decide/authorise; accessibility and mobile. Existing applicant flow is real but still staging-gated. |
| CX-07 | **Independent approval workbenches** | application.review / application.decide, identity and SoD policy, current CP decision APIs | Two-person segregation, evidence provenance, high-assurance and staleness feedback; no client-side grant or bypass. |
| CX-08 | **Provisioning timeline and plan comparison** | Versioned immutable plan digest, operation and tenant-provisioning references | Read immutable plan vs current desired state, attempts, failures, idempotency and blocked states; no approval-as-activation. |
| CX-09 | **Readiness intelligence visualisation** | CP readiness, drift and attested observation endpoints | Separate planned/observed/verified/ready; evidence age, source and time displayed. No synthetic all-green tiles or implied certifications. |
| CX-10 | **Audit investigation** | audit.view and query scoped read model | Authenticated, paginated, tamper-evident activity with actor, correlation, subject, reason and bounded timeline. Safe redaction and least privilege by default. |

These increments share a common design language but **do not** share
unverified domain authority. FE-04 is the initial implementation slice;
organisation read-model and permissions research is prerequisite to CX-03/04/05.

## Visual patterns

- Three progressive information depths: business summary, administrative
  detail, advanced platform diagnostics; never require a business applicant to
  understand engine bindings or topology.
- Shell: clear current-context label, permission-aware nav, balanced 15rem
  sidebar, keyboard palette, breadcrumbs, preserved responsive navigation.
- Organisation 360: identity overview, legal actor/mandate panel, corporate
  graph, tenants, markets, services, digital estates, people/access, readiness,
  activity; each independently source-backed and permission-scoped.
- Table density modes and mobile-safe card or scroll alternatives.
- Readiness/status: exact source/evaluation timestamp, unknown and blocked
  semantics, plain-language next actions and diagnostic reference.
- Command/action states: loading, empty, denied, stale, timeout, error,
  retry, irreversible confirmation and reviewer conflict.
- Accessibility: WCAG 2.2 AA target, keyboard, focus restoration, labels,
  sufficient contrast and reduced motion. Run axe + real-browser testing.

## CX-01 / CX-02 implementation boundary

The server-side layout retrieves the caller's own effective authority from
Control Plane with its server-held access token. The response is parsed
against the required Shared EffectiveAuthority fields and rejected if
principal mismatch, malformed or older than five minutes.

The response is projected to a **small navigation-only permission summary**;
grant IDs, scope IDs and bearer tokens are not sent to the browser. The
initial route catalogue contains *only implemented routes* and is limited to
Applicant, Review and Operations. Applicant is always visible for a valid
session; Review requires `application.review` or `application.decide`,
Operations requires `operation.view`. An unavailable authority report
fails closed on both privileged links. The command palette uses exactly
the same list. This UX layer does not authorise or change CP API permissions.

In particular, the existing `/api/workspace/v2` browser GET allowlist
is **unchanged**. The effective-authority request uses a server-owned,
literal BFF route directly from the authenticated server layout.

## Implementation and release gates

Do not publish invented organisation names, tenant IDs, status counts or
mocked health as production. Review source-proven migrations, actual
available endpoints and staging gates before each increment. Preserve
server-only BFF, exact origin, CSRF, introspection and IAM revocation.
Update and evaluate FE-03/FE-04/FE-07+ tests before product publication.
GitHub CI success is not an IAM/CP/AWS staging security acceptance.
