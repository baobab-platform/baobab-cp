# IAM-N02 — proposed estate CP Principal reader: source-only reconciliation

**Re-reviewed:** 2 November 2026. **Status: HELD / PROVISIONED CANDIDATE.**
**Governing:** EA v2.3, ADR-SHARED-043, CP's accepted workload/Principal
authority and exact Shared contract-consumer pin. This is not a release,
protected permission, native browser qualification or acceptance receipt.

## Accepted CP runtime source remains unchanged

The authoritative CP `contracts.lock.yaml` retains **both** references to
accepted Shared commit `2fde03e2f5f3a8a6cbc1e2081b4859fdccc1bb53`.
Its embedded `internal/contracts/shared/identity/v1/workload-registry.yaml`
and `internal/contracts/shared/authorization/v1/scope-registry.yaml`
remain the **byte-for-byte accepted main copies**. The Foundation consumer
lock/embedded contract checks MUST continue to reject any attempt to pin a
Shared commit not reachable from Shared main.

Shared [#421](https://github.com/baobab-platform/shared/pull/421) commit
`38704895c3b0c2897d4c72d4b1353945d2bbb2ff` is an
**unmerged proposal**. The previous CP draft mistakenly promoted that
commit into both accepted CP lock references, while also replacing the
embedded runtime identity/scope registry. Foundation correctly failed:

`contracts.lock.yaml: source.commit 387048... is not on Shared main`.

The 2 November correction restores the accepted runtime copies and pin.
The candidate Shared workload registry is retained only under
`internal/contracts/candidates/n02/identity/v1/workload-registry.yaml`,
as inert **source-review data**.

## Exact proposed identity ceiling

- `baobab-dev-zuribeans-cp-principal-validator`: separately owned by
  ZuriBeans, development only, `PROVISIONED`, CP audience only,
  `identity:principal:resolve` as a declaration, delegated human subject
  audience `baobab-trade`. It is NOT an active workload, issued credential,
  CP Principal, role, purchasing permission or real registration.
- Existing `baobab-dev-zuribeans-bff`: separate Trade-audience
  buyer-handoff workload; must never inherit CP reader scope.
- `baobab-dev-zuribeans-native-browser-bff`: proposed separate
  authorization-code / PKCE human client, **not** a workload entry.

The Go tests load only the proposed candidate for exact separation and
negative admission cases; they independently verify that **no** candidate
identity is active in CP's accepted runtime registry. A candidate parser
success is not an IAM registration, a valid bearer or CP authorization.

## Remaining gates — no implicit authority promotion

1. Coordinate a **single reviewed closed workload inventory** across
   Shared #420/#421/#422 and respective IAM/CP/TMS consumers. Never merge
   incompatible independent allowlists or silently lose scopes.
2. Have the Shared proposal accepted and merged with its ADR-043 negative
   conformance tests. Review the **new Shared main SHA**, not an unmerged
   PR commit; update both CP consumer pin fields atomically and byte-for-byte
   copy every changed CP-consumed contract on a **separate reviewed PR**.
3. Require independent IAM client registration, real issuer issuance, native
   BFF/CP Principal resolver current authority and exact audience/scope,
   wrong subject, revoked, expiry and cross-tenant negatives.
4. Qualify the ZuriBeans browser authorization-code + S256 PKCE journey
   and the original INT03C/N02 cases on a disposable environment, with
   source pins, TLS and independent teardown/custody.
5. Require final-head Go/Shared Foundation CI and independent operator
   review. Protected staging/production grants remain denied.

**Rollback:** reverting this candidate must not change the accepted
embedded contract copy, published image or CP runtime authority.
