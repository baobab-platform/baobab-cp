# FE-00: CP Console discovery and architecture lock

- **Gate:** FE-00 of the CP Console implementation prompt, which is ADR-BCP-019 §119 Gate CPFE-00 ("Architecture and
  Contract Lock").
- **Status:** Complete for this audit. It must be re-run before each later gate starts, because the matrices below
  change as backend and contract work lands.
- **Audited:** 2026-09-26. This gate produces no frontend code (ADR-BCP-019 §119).

| Input | State audited |
|---|---|
| `baobab-platform/baobab-cp` | `main` at `72c9baa`. No open PRs except this one. The router is unchanged since `18ed31a` (#169). |
| `baobab-platform/shared` | `main` at `d22c664`, which includes workload identities (#102). The CP lock pins `3c30305`, which adds the phase 1 and 2 administrative OpenAPI (#103, #104). |
| `baobab-platform/baobab-iam` | `main` at `6c885a7`. Only a Dependabot PR is open. **All Keycloak work is on hold pending the Keycloak-to-Ory migration ADR.** |
| ADRs | BCP-017 through BCP-023 in `docs/adr/`. |
| Existing frontend assets | None. There is no `frontend/` directory, and no Node tooling, Makefile target or CI job for one. |

## 1. Dependency map

```text
                    ┌───────────────────────────────┐
  Browser ─cookie─► │ CP Console / BFF (frontend/)  │
                    │  Next.js App Router, Node 24  │
                    └──────┬─────────────────┬──────┘
                           │ OIDC            │ server-side typed client
                           │ (confidential)  │ (generated from OpenAPI)
                           ▼                 ▼
        ┌───────────────────────┐   ┌─────────────────────────┐
        │ IdP (Keycloak today;  │   │ baobab-cp Go API        │
        │ Ory per pending ADR)  │   │  final authorization    │
        │  BLOCKER B1           │   │  76 human routes today  │
        └───────────────────────┘   └───────────┬─────────────┘
                                                │
                  contracts.lock.yaml ◄── shared: OpenAPI + schemas
                                             BLOCKER B2 (admin OpenAPI)
```

- **Runtime dependencies.** The BFF depends on the IdP (login, logout, token custody) and on the CP API. It never talks
  to PostgreSQL, engines or the IdP's admin API (ADR-BCP-019 §§37, 40, 78).
- **Build-time dependencies.** The frontend depends on the Shared OpenAPI, pinned through `contracts.lock.yaml`, from
  which its client is generated (§38; prompt §61).

## 2. Route map

This maps the prompt's §15 information architecture onto the backend that exists today. "Area" is the Console section.
The route groups follow ADR-BCP-019 §7.

| Area | Console route (proposed) | Route group | CP API it would read or command |
|---|---|---|---|
| Home | `/` | shared | None yet. Home must not show invented KPIs (prompt §102). It needs a principal/authority read model (G5). |
| Applications (applicant) | `/applications`, `/applications/new`, `/applications/[id]` | `(applicant)` | `GET/POST /v1/client-applications`, `GET/PATCH /v1/client-applications/{id}`, `POST …/submit`, `…/response`, `…/withdraw` |
| Applications (review) | `/admission`, `/admission/[id]` | `(operator)` | `GET /v1/admission/applications[/{id}]`, `POST …/begin-validation`, `…/information-request`, `…/begin-review`, `…/cancel`, `GET/POST …/decision` |
| Onboarding | `/onboarding`, `/onboarding/[requestId]` | `(operator)` | `GET/POST /v1/tenant-onboarding-requests`, `POST …/authorisation`, `…/cancellation`, `…/fulfilment` |
| Organisations | `/organisations`, `/organisations/[id]` | `(operator)` | `POST/GET /v1/canonical-entities[/{id}]` plus lifecycle commands, `GET /v1/organisations/{id}/audit`. **There is no list or search endpoint (G4).** |
| Corporate structure | `/organisations/[id]/structure` | `(operator)` | None over HTTP. Corporate relationships and groups are service-level only (G3). |
| Organisation reconciliation | `/organisations/reconciliation` | `(operator)` | `POST /v1/organisation-reconciliation`, `GET /v1/organisation-resolution-candidates[/{id}]`, `POST …/decision`, `GET /v1/organisation-drift` |
| Platform accounts | `/platform-accounts/[id]` | `(operator)` | `GET /v1/platform-accounts/{id}`, `POST …/status`. No list endpoint (G4). |
| Tenants | `/tenants/[id]` | `(operator)`, `(organisation)` | `GET /v1/tenants/{id}`, `POST …/suspend`, `…/activate`, `…/decommission`; platform-account bindings; counterparty roles; organisation admission. No list endpoint (G4). |
| Markets | `/markets` | `(operator)` | None. Shared declares `/markets`, but the CP does not implement it (G2). |
| Services | `/tenants/[id]/services` | `(operator)` | `GET /v1/entitlements`, and the classification endpoints `GET /v1/product-subscriptions/{id}/classification` and `GET /v1/tenants/{t}/products/{p}/classification`, plus classify and reclassify commands. |
| Digital estates | `/tenants/[id]/estates` | `(operator)` | None (G3). |
| People & access | `/access` | `(operator)`, `(organisation)` | None. There are no ADR-BCP-020 AdministrativeGrants (G6). |
| Integrations | `/integrations` | `(operator)` | `POST/GET` IAM organisation links and external references (canonical scopes). |
| Changes and approvals | `/changes`, `/approvals` | `(operator)` | None. There are no ADR-BCP-021 changesets (G6). |
| Operations | `/operations`, `/operations/[id]` | `(operator)`, `(organisation)` | Tenant provisioning only: `/v1/tenants/{t}/provisioning[/{id}]` plus apply, retry, cancel, readiness and drift. There is no generic ADR-BCP-022 Operation resource (G6). |
| Readiness | `/tenants/[id]/readiness` | `(operator)`, `(organisation)` | `GET …/provisioning/{id}/readiness` |
| Activity | `/activity` | `(operator)` | `GET /v1/organisations/{id}/audit` only. No cross-resource audit query (G6). |
| Diagnostics | `/diagnostics` | `(operator)` | `POST /v1/capabilities/explain`, `GET /v1/organisation-drift`, provisioning drift |
| Settings | `/settings` | shared | Console-local preferences only |
| Auth | `/login`, `/logout`, `/auth/callback` | `(auth)` | IdP (B1) |

## 3. Actor and authority matrix

In the Control Plane, the scope is necessary but never sufficient: `requireAdminRole` also requires a realm role.
`cp:platform-admin` authorises everything. `cp:tenant-admin` authorises only tenant-scoped routes, and only for a tenant
where the caller has an ACTIVE workforce membership.

| User class (prompt §14) | Scopes it needs | Extra CP check | Reachable today |
|---|---|---|---|
| Applicant / collaborator | `application:read`, `application:write` | The caller must own the application | Applicant routes. No collaborator model. How applicants register is undecided (R6). |
| Admission reviewer | `admission:review` | `cp:platform-admin` | Review queue and commands |
| Platform approver | `admission:decide`, `subscription:classify` | `cp:platform-admin`; never the applicant | Decision, classification |
| Onboarding requester | `onboarding:request` | `cp:platform-admin` + client role `onboarding-requester` | Request, cancel, fulfil |
| Onboarding authoriser | `onboarding:authorise` | `cp:platform-admin` + client role `onboarding-authoriser`; never the requester | Authorise |
| Platform operator | `tenant:write`, `tenant:read`, `canonical:read`, `canonical:write`, `capabilities:explain`, `subscription:read` | `cp:platform-admin` | Tenants, organisations, provisioning, diagnostics |
| Organisation administrator | `tenant:read`, `tenant:write` | `cp:tenant-admin` + membership of that tenant | Tenant-scoped routes only (the tenant, its lifecycle, provisioning, readiness, drift, entitlements) |
| Organisation auditor, platform auditor | none defined | none | Nothing: there is no read-only auditor authority (G6) |
| Security administrator, support operator | none defined | none | Nothing. Grants and support access are ADR-BCP-020 and §55 scope (G6). |

What this means for the Console:

- **Navigation.** It must be driven by the principal's effective authority as the CP reports it, never by role names
  hard-coded in the frontend (prompt §9). No endpoint reports that authority yet (G5).
- **Reviewers hold too much authority.** Every privileged route requires `cp:platform-admin`, which on its own
  authorises every `tenant:write` route. A reviewer therefore holds far more than review, and only ADR-BCP-020 grants
  fix that (R3).

## 4. Backend readiness matrix

| Feature area (ADR) | Backend | Endpoint stable enough to consume | Frontend blocked |
|---|---|---|---|
| Applicant workspace (017) | Yes: draft, edit, submit, respond, withdraw, list own | Yes | Only on B1 |
| Admission review and decision (017, 020 §§34-35) | Yes | Yes | Only on B1 |
| Tenant onboarding handoff (017 §§22-24) | Yes | Yes (registration now requires an AUTHORISED request) | Only on B1 |
| Organisation identity (018) | Partial: create, get, lifecycle, IAM links, external references | Get and commands yes; no list or search | Lists blocked (G4) |
| Corporate structure (018 ORG-04/05/06) | Service and database only; no HTTP | No | Yes (G3) |
| Platform accounts (018 ORG-07) | Partial: get, status, tenant binding | Yes; no list | Lists blocked (G4) |
| Tenants and lifecycle (018, 019 §§109-110) | Yes: register, get, suspend, activate, decommission | Yes; no list | Lists blocked (G4) |
| Subscription classification (018 ORG-11) | Yes: classify, reclassify, explain | Yes | Only on B1 |
| Provisioning, readiness, drift (019 §§31-33) | Yes, per tenant | Yes | Only on B1 and B2 |
| Markets (019 §24) | No HTTP | No | Yes (G2) |
| Digital estates, services selection (019 §§23, 25) | No HTTP | No | Yes (G3) |
| Administrative grants (020) | No | No | Yes (G6) |
| Changesets, impact, approvals (021) | No | No | Yes (G6) |
| Durable operations, `/v1/admin` namespace (022) | No (provisioning operations only) | No | Yes (G6) |
| Evidence and verification records (023) | References only; no document store | No | Evidence upload blocked (G7) |
| Audit timeline (019 §56) | Per organisation only | Yes | Cross-resource activity blocked (G6) |

## 5. Contract readiness matrix

At this audit, the Shared `control-plane/v1` OpenAPI described only `POST /tenants` and `POST
/tenants/bootstrap-registrations`: **2 of the Control Plane's 68 human routes**. Phase 1 (Shared #103) adds the 21
applications, admission and onboarding operations, and phase 2 (Shared #104) the 9 tenant and classification
operations, phase 3a (Shared #105) the 19 organisation, counterparty, platform account, IAM link, drift and
audit operations, phase 3b (Shared #106) the 6 canonical registry operations and the capability explanation, and phase
3c (Shared #108) the external reference and mapping operations, so 68 of the now 76 human routes are described. `api.TestOpenAPIDescribesTheRouter` keeps
the count honest: it fails when a served route is neither described nor listed as undescribed.

| Family | Shared schemas | Shared OpenAPI paths | CP implements | Generated client possible |
|---|---|---|---|---|
| Client applications, admission | `admission/v1` (application, decision, lifecycle, events) | Applications and Admission tags (15 operations) | yes | Yes |
| Tenant onboarding | `admission/v1` onboarding schemas | Onboarding tag (6 operations) | yes | Yes |
| Tenant registration | `control-plane/v1` tenant-registration and bootstrap schemas | `POST /tenants`, `POST /tenants/bootstrap-registrations` | yes | Yes |
| Tenant read and lifecycle | `control-plane/v1` `tenant.schema.json` | Tenants tag (5 operations) | yes | Yes |
| Organisation links, reconciliation, counterparties, platform accounts, drift, audit | `organisation/v1` | Organisations, Counterparties, Platform accounts, Diagnostics and Audit tags (19 operations) | yes | Yes |
| Canonical registry, capability explanation | `control-plane/v1` `canonical-entity.schema.json`, `capability-explanation.schema.json` | Canonical registry tag (6 operations), `explainCapability` | yes | Yes |
| External references, mappings | `control-plane/v1` `canonical-mapping.schema.json` | Mappings tag (11 operations) | yes, except `resolveMapping` (G2) | Yes |
| Corporate structure | `organisation/v1` | none | service level only (G3) | No |
| Subscription classification | `product/v1` (records, explanations, commands) | Classification tag (4 operations) | yes | Yes |
| Markets, mappings | `control-plane/v1` | `/markets…`, `/mappings…`, `/resolution/mappings` | **no** | Generating would describe routes that do not exist (G2) |
| Errors | `errors/v1` problem details | referenced | yes (`application/problem+json`) | Yes |

## 6. Contract and backend gap list

| ID | Gap | ADR | What is needed | Owner |
|---|---|---|---|---|
| **B1** | The BFF must be an OIDC **confidential** client (ADR-BCP-019 §10). The only workforce client, `baobab-control-plane-admin`, is public with PKCE, and its redirect URIs are `localhost:3003`, while ADR-BCP-019 §87 puts the Console on `:3000`. | 019 §§10-11 | A confidential Console client with its redirect URIs and back-channel logout, decided by the Keycloak-to-Ory migration ADR | IAM, after the Ory ADR |
| **B2** | The administrative OpenAPI is partial. Phases 1 to 3c (Shared #103 to #106 and #108) describe every family but provisioning; the 8 provisioning routes of the 76 human routes still have no description, and `api.TestOpenAPIDescribesTheRouter` lists them. | 019 §38, 022 §§17-20 | Shared `control-plane/v1` paths for provisioning (which first needs a tenant manifest schema) and the integration families, tagged by family (022 §20). The CP drift test is in place. | Shared, then CP |
| G2 | Shared declares `/markets` and `/resolution/mappings`, which the CP does not serve (the mapping administration is served since phase 3c). | 022 §18 | Either implement them or mark them as not yet implemented, so the generated client cannot call routes that do not exist | Shared or CP |
| G3 | Corporate relationships, corporate groups, digital estates and service selection have no HTTP surface. | 018, 019 §§20, 23, 25 | Administrative query and command endpoints | CP |
| G4 | There are no list or search endpoints for organisations, tenants or platform accounts. | 022 §§30, 53 | Scope-filtered, paginated list read models | CP |
| G5 | There is no "who am I and what may I do" read model. | 019 §12, prompt §9 | A principal and effective-authority endpoint for authority-aware navigation (never used as the final authorization check) | CP |
| G6 | There are no AdministrativeGrants, changesets, approvals, durable Operations, `/v1/admin` namespace, cross-resource audit or auditor authority. | 020, 021, 022 | The ADR-BCP-020 to 022 backend programmes | CP |
| G7 | There is no evidence document store; evidence is carried as references only. | 023, 019 §51 | A document store and an upload flow | CP and infrastructure |
| G8 (closed by Shared #105) | `canonical:read`, `canonical:write`, `capabilities:explain` and `metrics:read` are used by the CP but not registered in Shared. `tenant:read` is registered with audience `baobab-cp`, while the CP's audience is `baobab-control-plane`. | ADR-0007 | Register the scopes and correct the audience | Shared |
| G9 (closed by Shared #108 and phase 3c) | The CP's external references (`POST /v1/canonical-entities/{id}/external-references`, `GET /v1/external-references`) use a local shape (`id`, `native_type`, `external_url`, upper-case status, `metadata`) that conflicts with Shared's `canonical-mapping` `externalReference`, embed the canonical entity in the reference, and serve a reverse lookup under a resource name. | 022 §§17-18, 018 ORG-10 | Phase 3c: conform to Shared's `externalReference`, link through Mapping, a Shared-defined reverse resolution, an additive migration that reports legacy rows instead of guessing `system_namespace` (validated against Shared's `external-systems.yaml`, on the topology identifiers ADR-SHARED-012 reconciled); IAM organisation links stay on `IamOrganisationReference` | Shared, then CP |
| G10 | The CP's capability registry accepts keys such as `erp.receivables`, wider than Shared's `capability/v1` `<domain>.<resource>.<action>` grammar. The explanation refuses non-canonical keys. | 022 §18, BCP-003 | Align the registry's key grammar with Shared and migrate or report non-conforming keys | CP |

## 7. BFF threat model

| Threat | Where | Control required | Gate |
|---|---|---|---|
| Access or refresh token theft from browser storage or XSS | Browser | Tokens are held only server-side; the browser gets an opaque `Secure; HttpOnly; SameSite=Strict` session cookie. Strict CSP, no inline scripts. | FE-03, FE-17 |
| CSRF on state-changing requests | BFF | SameSite=Strict plus an origin check plus a per-session CSRF token on every mutating request | FE-03 |
| The BFF becoming an open proxy (SSRF, authorization bypass) | BFF | One explicitly bounded handler per operation; no generic pass-through (§42) | FE-05 |
| Authorization in the frontend used as the security control | BFF, UI | The CP decides on every call; hidden actions are not a control; negative authorization tests per route | FE-04, FE-17 |
| Actor spoofing through headers | BFF → CP | The CP derives the actor from the token only (022 §§13-14); the BFF never sends identity headers | FE-05 |
| Cross-tenant data leakage through caching | BFF | No shared cache of authenticated responses; caches keyed to the session; changing context clears state (prompt §§11, 58) | FE-04 |
| Session fixation or replay | BFF | Rotate the session at login; server-side revocation; IdP back-channel logout | FE-03 |
| Duplicate commands after a retry | BFF → CP | Forward `Idempotency-Key` and `If-Match` unchanged. The CP enforces idempotency keys on its command routes, and `If-Match` on canonical entities and provisioning. | FE-05 |
| Leaking secrets to the browser | Build | Separate server-only and public environment variables, validated at startup; no secret uses the `NEXT_PUBLIC_` prefix | FE-01 |
| Clickjacking | Browser | `frame-ancestors 'none'` | FE-17 |

## 8. Risks

| ID | Risk | Mitigation |
|---|---|---|
| R1 | The IdP migration changes the login and session flow after FE-03 is built. | Do not build FE-03 until the Ory ADR is decided. Keep the OIDC client behind a server-only port, so the provider is one adapter. |
| R2 | Generating a client from the current OpenAPI would produce types for routes that don't exist (G2) and none for the routes that do (B2). | FE-05 waits on B2. No hand-written API types in the meantime (prompt §93). |
| R3 | The coarse `cp:platform-admin` requirement means the Console cannot give a reviewer less than full platform authority. | Show authority honestly. ADR-BCP-020 grants are the real fix (G6). |
| R4 | Screens built before G3, G4 and G6 exist would invent local models. | Build only verticals whose backend is ready (§9). Unready areas get a shell and a documented dependency (prompt §2). |
| R5 | Two runtimes in one repository increase CI time and blur ownership. | Path-filtered frontend CI; independent images (ADR-BCP-019 §90). |
| R6 | Applicant sign-up is undefined: applicants use the workforce verifier today. | The ADR-BCP-017 applicant identity flow must be decided with the IdP migration. |

## 9. Implementation sequence

| Gate | Can start now | Depends on |
|---|---|---|
| FE-01 Foundation: `frontend/`, Next.js Active LTS, TypeScript strict, pnpm, Node 24, lint, tests, build, Dockerfile, environment validation, path-filtered CI, Dev Container and Makefile targets | **Yes** | none |
| FE-02 Design system: tokens, primitives, status, error, loading and empty states, WCAG 2.2 AA | **Yes** | FE-01 |
| FE-03 Authentication and BFF | No | B1 (Ory ADR and a confidential client) |
| FE-04 Global shell and context | Shell layout yes; authority-aware navigation no | G5 |
| FE-05 Generated client and drift CI | Yes for applications, admission and onboarding; the CP drift test exists | FE-01; the remaining B2 phases for other families |
| FE-06 Applicant workspace; FE-07 Admission review; tenant onboarding | Backend ready | FE-03, FE-05 |
| FE-08 Organisation administration | Partial | G3, G4 |
| FE-09 to FE-12 People and access, changesets, approvals, operations | No | G6 |
| FE-13 Readiness and activation | Backend ready per tenant | FE-05 |

The critical path is **B2 (admin OpenAPI) → FE-05 → FE-06 and FE-07**, with **B1 (Ory ADR) → FE-03** running alongside
it. FE-01 and FE-02 can start now without either.

## 10. Traceability matrix (initial)

| ADR | Frontend requirement | Route or feature | API | Tests | Status |
|---|---|---|---|---|---|
| BCP-017 | Applicant workspace | Applications | `/v1/client-applications` | E2E | Backend and contract ready; B1 |
| BCP-017 | Admission review | Admission | `/v1/admission/applications` | E2E, security | Backend and contract ready; B1 |
| BCP-017 | Onboarding handoff | Onboarding | `/v1/tenant-onboarding-requests` | E2E, security (separation of duties) | Backend and contract ready; B1 |
| BCP-018 | Organisation | Organisations | `/v1/canonical-entities` | E2E | Partial (G4) |
| BCP-018 | Corporate structure | Corporate structure | none | E2E | Blocked (G3) |
| BCP-018 | Platform account | Platform accounts | `/v1/platform-accounts` | E2E | Partial (G4) |
| BCP-019 | Readiness | Readiness | `…/provisioning/{id}/readiness` | Integration | Backend ready; B1, B2 |
| BCP-020 | Administrative grants | People & access | none | Security | Blocked (G6) |
| BCP-021 | Changesets, approvals | Changes, approvals | none | E2E, security | Blocked (G6) |
| BCP-022 | Operations | Operations | provisioning only | Integration | Partial (G6) |
| BCP-023 | Evidence | Applications | references only | E2E | Partial (G7) |
