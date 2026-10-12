# CX-03B — caller-bound organisation/tenant/market navigation

**Authority:** ADR-BCP-018/019/020/026/027, IAM ADR-IAM-0033 and pinned Shared CP contracts.
**Scope:** nonproduction, read-only human Console experience; **not** workload PlatformContext issuance.

## Behaviour implemented

- New protected `/workspace/organisations/[organisationId]/contexts/[tenantId]` page,
  linked only from the actual source-backed Organisation 360 PRIMARY tenant associations.
- Each request independently checks a current authenticated Console session, fresh
  `/v1/admin/effective-authority`, exact `organisation.view` **and** exact
  `tenant.view` scopes, a live CP `/v1/organisations/{id}/overview` and
  `/v1/tenants/{id}`. There is **no grant expansion** from group ownership,
  organisation affiliation, platform roles or IAM organisation membership.
- The CP overview verifies current PRIMARY mappings and exposes market participation
  **only** with an independent exact tenant `market.view` permission. An explicitly
  selected `?market=ZA` must exist uniquely in that current scoped CP response;
  no market is silently defaulted and no user-supplied market ID is redeemed.
- A mismatched, duplicated, removed or secondary mapping, stale (>5 min), future
  or missing source, withdrawn market permission, explicit suspended/revoked
  tenant/organisation state, invalid selector, or lost resource grant fails closed.
- Dynamic server rendering, non-prefetched protected context links, no cookies,
  localStorage or URL-controlled authorisation data, and no persistence/reuse of a
  former selection. The browser sees only redacted display and reference data.
- Security tests cover wrong associations and records, missing or ambiguous markets,
  selector injection, stale sources, suspension and revocation; existing CP Overview
  and permission tests cover source-level delegated authority and tenant isolation.

## Explicit boundary

The Control Plane's workload `PlatformContextHandler` uses workload
`context:resolve` with its own attestation and persistence semantics. **It is not
called from the browser or misrepresented as a human session scope.** This
read-only navigation is a presentation context. Every follow-on API command
must independently resolve its own caller-bound, tenant/market-appropriate
execution authority in CP. A URL, selected market or displayed context never
authorises a command, activates a tenant, or upgrades legal/provider status.

A CP Console account with only a tenant grant (and no independent organisation
grant) retains its pre-existing exact tenant record view; it cannot claim an
organisation/market relationship through this joined context route.

## Acceptance still external

Real IAM login and revocation timing, representative CP market/grant/mapping
data, cross-account source failure, nonproduction HTTPS/Redis staging and
live tenant transitions require environment acceptance. CI and mock unit
tests are not proof of live integration or production enablement.
