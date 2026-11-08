# IAM-N02/N04/N07 — accepted Shared authority inventory and CP source adoption

**Reconciled:** 4 November 2026. **State:** accepted SOURCE; development workload
identities remain `PROVISIONED` and unadmitted. This review updates
[`baobab-cp#455`](https://github.com/baobab-platform/baobab-cp/pull/455)
under EA v2.3, ADR-SHARED-042/043 and CP's existing Principal, context,
administrative-grant and contract-consumer controls.

## Authoritative source and reviewed change boundary

Shared [#430](https://github.com/baobab-platform/shared/pull/430) merged to
`main` as **`7f547ccdd759208355ef9224d2177929f4bf224e`**, replacing
competing Shared #420/#421/#422 development registry proposals. Do not pin
the historical PR head `1dc7a7eba0c197159473864c5cd8bea7637e4a8c`:
a pull-request head is not an accepted mainline contract.

CP's prior accepted Shared source was
`2fde03e2f5f3a8a6cbc1e2081b4859fdccc1bb53`. Comparing that
commit to the accepted merge traverses 55 Shared commits. Among the files
declared in CP `contracts.lock.yaml`, only these two changed:

1. `contracts/identity/v1/workload-registry.yaml`
2. `contracts/authorization/v1/scope-registry.yaml`

The prior embedded copies were independently confirmed byte-for-byte equal
to their files at the prior accepted source. The new embedded copies are
byte-for-byte copies from the accepted Shared merge commit, not assembled
from overlapping proposed branches. Both
`contract_bundle.source_commit` and `source.commit` now pin the same
accepted merge SHA. No unrelated CP-consumed contract was changed.

The newly published Shared `native-browser-client-registry.yaml` is an
IAM-owned browser-client declaration. CP does **not** admit it as a
client-credentials workload, nor is it an authority-bearing CP registry.
It is deliberately not added to CP's runtime workload registry.

## Closed development-only identity inventory

| Identity | Owner | Allowed declared ceiling | Runtime status |
| --- | --- | --- | --- |
| `baobab-dev-iam-runtime-reader` | IAM | CP `identity-runtime:read` | PROVISIONED |
| `baobab-dev-zuribeans-cp-principal-validator` | ZuriBeans | CP `identity:principal:resolve`, subject audience `baobab-trade` | PROVISIONED |
| `baobab-dev-tms-subject` | TMS | `context:resolve`, `legal-actor:assess`, TMS transport `tms:booking:submit` | PROVISIONED |
| `baobab-dev-tms-validator` | TMS | CP `context:validate`, validated audience `baobab-tms` | PROVISIONED |

The separate `baobab-dev-zuribeans-native-browser-bff` has an
authorization-code/S256 PKCE profile in Shared, **not** in the CP workload
registry. It also remains PROVISIONED without a real browser, issuer or CP
authority receipt.

`PROVISIONED` grants no active issuer/CP workload admission. CP must deny
the declared scopes, subject delegation, audience validation and all
development/staging/production runtime admission until there is separately
governed activation. The existing ACTIVE Trade buyer validator and
ZuriBeans buyer-handoff workload remain separate and retain their original
limited authorities.

A CP-facing bearer, an IAM client registration, a CP Principal,
`AdministrativeGrant`, market mandate, buyer purchase or TMS booking
authority are **not interchangeable**. `tms:booking:submit` has
`grants_authority=false` and cannot perform native transport booking.

## Inert candidate preservation and independent regression

The pre-existing candidate path
`internal/contracts/candidates/n02/identity/v1/workload-registry.yaml`
remains for review history. It is intentionally identical to the accepted
runtime registry bytes, but its
`internal/contracts/candidates/n02/source.json` now identifies the
**merged** Shared main commit and remains
`source_review_only=true`, `runtime_authority=false`. Candidate retention
cannot override CP's accepted contract lock.

Go regression tests verify:

- exact candidate SHA-256 and accepted source provenance;
- equality between candidate review bytes and CP's accepted embedded
  workload contract;
- both immutable CP Shared source-lock fields;
- no runtime admission, delegated subject audience, scope or environment
  for any four PROVISIONED workload clients;
- no accidental upgrade of the existing Trade buyer handoff into a CP
  Principal-resolution caller;
- browser client isolation from workload registration.

Contract currency is *source adoption*, not execution qualification.

## Remaining integration and acceptance gates

1. CP current-head CI must independently pass Go, Shared contract lock,
   Foundation, Security, Readiness and applicable generated-client checks.
   The N02 candidate remains fail-closed.
2. IAM must register genuinely distinct native Hydra clients and obtain
   independently read-back, still-valid issuer credentials within a
   disposable owner-approved development run. Shared registry declarations
   are not issuer credentials.
3. CP must independently establish and verify current Principal, grant,
   context/market and legal-actor authority for the exact operator and
   operation. No PROVISIONED identity should be silently promoted.
4. Qualify the ZuriBeans browser authorization-code/S256, Kratos AAL2,
   recovery, revocation and negative paths against actual native services,
   with original same-run N02/INT-03C evidence.
5. Qualify TMS N07 issuer/CP/native-resource denials separately from
   **executable** TMS booking, which remains fenced until its own
   native provider, mandate and idempotent commitment requirements pass.
6. Infrastructure #69 must independently source-pin, retain private
   originals and prove complete teardown. Its old candidate lock files
   cannot be treated as accepted-source runtime proof.
7. Protected staging/production and provider-support promotion remain
   explicitly unauthorized.

**Disposition:** Reconcile source under CP #455 and keep that PR open for
final-head review. Do not merge #455 merely because a closed contract
inventory compiles. Reverting CP #455 restores the previous accepted source
pin and embedded copies without granting or withdrawing live credentials;
none are provisioned by this change.
