# PEO-06 — progressive onboarding Console delivery

**Normative scope:** ADR-BCP-026 §7, §14 PEO-06; ADR-BCP-027 scoped legal actors; ADR-BCP-019 BFF and frontend acceptance; FE-00 rebaseline of 9 October 2026.

**Status (12 October 2026):** PEO-06A showcase, PEO-06B provider-neutral OIDC and durable session foundation, PEO-06C applicant workflow, PEO-06D independent governance workflow and PEO-06F synthetic accessibility/keyboard tests are merged. The PEO-06E plan-digest and readiness extension is implemented in a separate review increment. All are **code-level increments**, not evidence of a completed protected IAM/AWS multi-browser acceptance or production activation. Package 5 PEO-05A–D remains merged with protected live certification outstanding.

## Delivery gates

| Gate | Implementation | Exit and authority |
| --- | --- | --- |
| PEO-06A | Nonproduction-only responsive progression and examples | Semantic screen, accessible selectors, no invented evidence applicability, no live forms, browser tests |
| PEO-06B / FE-03 | Provider-neutral OIDC confidential client, server-held short-lived session, PKCE/state/nonce, callback verification, logout, CSRF, revocation | Server-held bearer only; production session backend and IAM client configuration independently verified; security tests |
| PEO-06C / FE-06 | Authenticated applicant draft/create/read/edit/submit, reasoned evidence applicability | CP v2 routes only, owned data, status and errors, no fake universal tax gates; protected server BFF |
| PEO-06D / FE-07 | Separate reviewer/decider queue and recorded decision; requester/authoriser hand-off | Role+scope+membership checks on CP, maker/checker separation; no v1-v2 shortcut |
| PEO-06E / FE-09 | Tenant onboarding and provisioning view, plan digest, current revision and operation polling | Current CP source data only, blocked/pending/revoked differentiated; no READY-to-ACTIVE shortcut |
| PEO-06F | Cross-browser accessible mobile/keyboard integration, evidence provenance and negative security | WCAG 2.2 AA, responsive, 320px, CSRF, token confinement, privileged-route denial |

Do not announce the Console as live until FE-03 is complete and CI + protected browser/e2e evidence passes. Client JSX never grants an entitlement. The only authoritative providers are CP/IAM/Subscriptions/Trade/ERP and legal-policy sources within their boundaries.

## PEO-06A implementation

- `/onboarding` is strictly unavailable in `CONSOLE_ENVIRONMENT=production` (404) and shown from the Console home in nonproduction.
- Seven controlled stages cover identity → business → interests → requirements → independent admission → separate tenant authorisation → capability readiness.
- Five business examples demonstrate why evidence is purpose-specific, not a uniform PAYE/UIF/VAT/registration checklist. They are **not** capability keys, provider offers, legal advice, eligibility calculations or source assertions.
- Lifecycle copy makes explicit `Submitted`, `Approved`, `Authorised`, `Provisioning`, `Ready` and `Active` are distinct. This page cannot create, save, review or activate anything.
- All choices are local volatile screen state without PII, browser storage, persisted session or direct CP API access. Future authenticated workflows must use the generated CP client through the BFF.
- Unit tests check responsive semantic flow, accessibility, no approval action, example semantics, and production 404.

## Remaining implementation / acceptance

**Coding status:** FE-03B/FE-03C, applicant and reviewer BFF, governed commands, read-only operation polling and synthetic accessibility regressions have shipped through reviewed PRs #348, #349 and #350. The supplementary PEO-06E immutable plan/readiness view and provenance tests are pending this PR's acceptance.

**Operational evidence remains outstanding:** live approved IAM client, signing keys and introspection; protected Redis HA, TLS/ACL and encryption-key custody; 2-replica callback replay, session revocation, same-site cookie and CSRF verification; current reviewer/decider/requester/authoriser admission across distinct humans; production-approved CP feature gates; actual provider and evidence sources; and automated real Chromium/Firefox/WebKit responsive WCAG 2.2 AA/320px browser E2E. The synthetic jsdom axe suite does **not** replace real browser testing. No enabling of the production auth runtime is authorised by this programme. The permanent Keycloak federation architecture remains behind the provider-neutral OIDC BFF: browser JavaScript never receives IAM or CP bearer credentials.

**Future gate:** run independently approved protected staging scenarios and attach immutable test artifacts. Only then assess FE-03/PEO-06 operational acceptance and production activation.

CI: `pnpm --filter @baobab-platform/cp-console typecheck`, `lint`, `test`, `build`, and the pinned contract/client drift gate. Protected end-to-end credentials and browser tests require an independently configured staging environment.
