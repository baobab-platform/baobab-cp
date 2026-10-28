# CP TMS-native execution authority — proposal-only PDP preflight

**State: HELD / Source scaffolding, not an accepted or registered endpoint.** Accepted TMS ADR-0004/0013/0020/0021 and Shared EA v2.3/EA-DEV govern the boundary. Shared [PR #409](https://github.com/baobab-platform/shared/pull/409) defines a **PROPOSED** authority shape. CP [issue #447](https://github.com/baobab-platform/baobab-cp/issues/447) is a design/implementation request, not approval.

A CP context validation or generic capability-resolution result does NOT confer a current legal actor mandate, Market participation, operation-specific booking purchase authority, approved provider release, or an execution lease. Native Fleetbase Company IDs, Open TMS org IDs and carrier identities cannot substitute for canonical CP Principal, Tenant, Organisation and legal actor.

The first-party internal/service/tmsauthority package provides a closed, bounded **candidate** request parser. The request carries context_id and operation/resource/binding/intent/plan references, NOT a caller-selected tenant/legal actor/mandate/Principal/market/permission. Parsed operations are limited to the types in Shared #409's proposed decision and explicitly match their canonical resource types. Duplicate, extra, absent, null and trailing JSON fields deny.

A separate Preflight only examines whether an externally supplied candidate has plausible issuer/subject, scoped legal/Market/binding currentness, valid current plan/lease generation and provider release. It does not verify those values came from a trusted source. The public package Evaluate ALWAYS returns HELD, canExecute=false, canActivate=false with zero effective lease lifetime, regardless of these candidate fields. No HTTP handler or CP route is mounted, no schema is added to accepted contracts, no native provider or carrier effect is invoked.

Tests prove wrong operation/resource, foreign scope injections, duplicate properties, expired/revoked facts and a full plausible synthetic candidate all remain non-authorising. Existing CP CI runs go test -race ./..., go vet, gofmt and PostgreSQL 17 integrations. Dedicated small proposed-contract CI job reports the fail-closed tests separately.

**Further implementation after formal acceptance:** Shared contract publication and versioned resource-server scopes; IAM issuance and independent CP context/subject validation; authoritative legal actor/mandate resolution, current provider binding and release, CP-owned operation lease, expiry/revocation, durable decision audit and explicit action-specific outcome. Integrate the handler only after approval and connected two-Tenant negative/positive proof. TMS's private PostgreSQL lease is not a CP-owned authorisation token.

**Explicit no-merge instruction:** leave all CP, IAM, Shared and TMS PRs open until directly authorised.
