# CX-03 / CX-04 — exact-scope context navigation and Organisation 360

**Repository:** baobab-platform/baobab-cp
**Authority:** ADR-BCP-019, ADR-BCP-018, ADR-BCP-020, ADR-BCP-026/027,
Shared locked control-plane/v1 and administration/v1 contracts.
**Rollout:** nonproduction protected Console only; live IAM/CP staging proof
remains an independent acceptance gate.

## Implemented

1. An authenticated, responsive Console context selector with actual
   exact-level `organisation.view` and `tenant.view` grants from
   `GET /v1/admin/effective-authority`. This server-read, caller-bound
   snapshot is valid for a maximum of five minutes and fails closed.
2. No global enumeration from PLATFORM or CORPORATE_GROUP grants; no
   derived parent-child membership, dynamic descendant rights or blind
   assumption that a holding company can read its autonomous subsidiaries.
   Only explicitly anchored, exact scoped IDs appear as options.
3. Selecting an option navigates, without storing an authority-bearing
   context token in a cookie, localStorage, query filter or browser bearer.
   The destination page independently rechecks the current caller's
   valid grant and then fetches the exact CP resource server-side.
4. Organisation 360 renders actual `GET /v1/canonical-entities/{id}`,
   and conditionally retrieves `GET /v1/canonical-entities/{id}/iam-organisations`.
   Organisation audit is fetched **only** after an exact organisation-scoped
   `audit.view` grant is confirmed, and then obtained from CP using
   `GET /v1/organisations/{id}/audit?limit=25`.
   Responses are parsed into a small safe presentation projection.
5. Tenant context reads `GET /v1/tenants/{id}`; the registry's desired
   and observed states are displayed separately. Neither is presented
   as certified operational readiness.
6. Read-only source provenance, accessible information cards, responsive
   layout, clear unavailable/denied/empty states and boundary tests.
7. The browser's existing `/api/workspace/v2` read allowlist remains
   unchanged; the new CP reads occur only inside authenticated Server
   Components using the existing secure BFF upstream transport.

## Architectural limits requiring a later integration

The repository has individual entity/tenant read contracts but does **not**
currently expose a verified caller-scoped, paginated unified organisation
directory that the Console can responsibly use for broad enterprise search
or platform-level switching. It also lacks an approved, holistic
Organisation 360 read model that unifies corporate relationships,
legal-actor mandate acceptance, independently verified legal status,
market participation, subscriptions, tenant association and digital estate
bindings for the exact caller.

Therefore:
- UI does not pretend organisation `ACTIVE` is legally verified.
- A tenant `legal_entity_id` is a reference, not a family-tree proof.
- IAM organisation links are not Console permission grants.
- No operating tenant is inferred from a corporate relationship.
- Empty or denied audit and IAM reads are not silently interpreted as no
  governance or no activity.
- No applicant data, unrestricted administrative listing, synthetic tenant,
  screenshot evidence or external provider assertion is fabricated.

## Acceptance tests and future increments

Run `pnpm --filter @baobab-platform/cp-console typecheck`,
`lint`, `test`, `build`, plus Go and repository evidence CI.

Negative tests must cover foreign principal, stale grant, unauthorised scope,
malformed path, expired grant, wrong canonical entity type, mismatched
response identifier, lost source access and mobile keyboard usability.

The next substantive tranche should create/adopt bounded CP list-and-summary
contracts: caller-permitted organisation contexts (including separately
governed group membership), linked tenant discovery, legal mandate and
verification projection, market/subscription summaries and page-scoped
audited searches. Only then connect global Organisation 360 tabs,
server-driven data tables and multi-market context selection.
