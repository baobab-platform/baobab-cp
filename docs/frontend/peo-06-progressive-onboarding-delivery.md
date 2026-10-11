# PEO-06 — progressive onboarding Console delivery

**Normative scope:** ADR-BCP-026 §7, §14 PEO-06; ADR-BCP-027 scoped legal actors; ADR-BCP-019 BFF and frontend acceptance; FE-00 rebaseline of 9 October 2026.

**Status:** PEO-06A is a non-authorising, accessible, browser-rendered *showcase increment*, not the applicant/operator workflow. Package 5 PEO-05A–D is merged; its protected live AWS/IAM/provider acceptance remains external.

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

All **PEO-06B–F coding gates remain outstanding** after this first bounded increment. In particular, no one should reuse the older Keycloak-specific admin-session assumptions: ADR-IAM-0033 establishes provider neutrality with permanent Keycloak federation. A proper OIDC confidential client and protected, revocable cross-instance session store are required before a live applicant form; generating a fake bearer/token or writing it to localStorage is prohibited.

CI: `pnpm --filter @baobab-platform/cp-console typecheck`, `lint`, `test`, `build`, and the pinned contract/client drift gate. Protected end-to-end credentials and browser tests require an independently configured staging environment.
