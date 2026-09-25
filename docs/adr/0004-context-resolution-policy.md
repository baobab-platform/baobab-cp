# ADR-0004: Executable Tenant Context Resolution Policy

**Status:** Accepted  
**Date:** 2026-09-01  
**Repository:** `baobab-platform/baobab-cp`  
**Depends on:** ADR-0003; Shared ADR-0003 and ADR-0004  
**Contract:** `baobab-platform/shared/contracts/control-plane/v1/context-resolution.schema.json`

## Context

Product engines need an authoritative answer to two questions before admitting
tenant work: whether the authenticated tenant is operational, and whether that
tenant is entitled to the requested Baobab product. Accepting caller headers,
falling back to a default tenant, or treating a requested subscription as an
active entitlement would all break the platform isolation boundary.

The canonical request contains only `product_id`. Tenant identity comes from a
verified workload token, not from the request body. Infrastructure terminates
workload mTLS, while the application must still validate the token and make the
authorisation decision.

## Decision

`POST /v1/context/resolve` implements the Shared v1 contract.

Admission requires all of the following:

1. an asymmetrically signed OIDC token issued for the configured Control Plane
   audience;
2. `actor_type=workload`, an authorised-party/service identity in `azp`, and
   the `context:resolve` scope;
3. a canonical Control Plane-minted `tenant_id` claim;
4. both desired and observed tenant lifecycle state equal to `active`; and
5. the requested product subscription state equal to `active`.

Every authenticated context policy result is written to the append-only audit
store in the same database transaction as the authoritative read. The audit
records actor, workload client, token identifier, tenant/product target,
correlation identifier, result and policy decision. If audit persistence
fails, resolution fails closed. Invalid or unverifiable tokens cannot supply a
trusted tenant or actor for the audit store; those attempts produce structured
security logs containing no token value.

Successful responses may be cached for exactly 15 seconds, as declared by the
Shared contract. After expiry, an unavailable Control Plane is a denial; there
is no fail-open or stale-while-error path.

All HTTP failures use the canonical RFC 9457 problem-details shape. Denials do
not reveal whether the tenant, lifecycle state, or entitlement caused the
decision.

## Consequences

- A caller cannot select a tenant in the JSON body or through an identity
  header.
- `requested`, suspended, missing, unknown and unreconciled states are denied.
- Product engines receive only affirmative `entitled: true` responses; denials
  are errors rather than negative tenant-data disclosures.
- PostgreSQL is on the synchronous request path and must meet the endpoint SLO.
- Workload OIDC configuration is explicit and may use an issuer distinct from
  the human-administration issuer.

## Rejected alternatives

- **Trust an `X-Tenant-ID` header.** Rejected because gateway routing metadata
  is not authenticated platform identity.
- **Accept a tenant in the request body.** Rejected because it enables confused
  deputy and cross-tenant probing failures.
- **Treat `requested` entitlements as usable.** Rejected because desired state
  is not reconciled operational state.
- **Return detailed denial reasons.** Rejected because they disclose tenant and
  entitlement state to callers.
- **Sign the response in this slice.** Rejected because the Shared v1 response
  contract contains no signature and TLS plus authenticated short-lived lookup
  is the approved boundary. Adding a signature later requires a versioned
  contract decision.

## Follow-up

1. Trade PR A5 will consume this endpoint and prove fail-closed caching.
2. Tenant registration and entitlement lifecycle reconciliation must be
   brought fully into line with the current Shared identity contract before
   production onboarding.
3. Integration tests with PostgreSQL and the infrastructure mTLS gateway remain
   required before production cutover.

