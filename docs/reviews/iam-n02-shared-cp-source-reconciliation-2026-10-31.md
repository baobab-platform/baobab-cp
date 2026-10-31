# IAM-N01/N02 — coordinated Shared-to-CP consumer source reconciliation

**Review class:** source-only development proposal, **not** live IAM/CP/estate
acceptance. No merge/promotion until all mandatory final-head CI and
independent PR review.

## Immutable source tuple and comparison

- Prior accepted CP `contracts.lock.yaml` Shared source:
  `2fde03e2f5f3a8a6cbc1e2081b4859fdccc1bb53`.
- N02 coordinated source: Shared #421 commit
  `38704895c3b0c2897d4c72d4b1353945d2bbb2ff`.
- GitHub compare of that exact range: **49 Shared commits**, 54 changed files
  including tooling, architecture proposals, assessments and evidence.
  The entire current CP `contracts.lock.yaml` inventory was reconciled
  against that file list. **Only two existing CP-consumed contract files
  changed** (neither YAML parser behavior nor the CP Principal resolution
  interface was silently extended):

  | Canonical Shared source | CP embedded copy | Result |
  | --- | --- | --- |
  | `contracts/identity/v1/workload-registry.yaml` | `internal/contracts/shared/identity/v1/workload-registry.yaml` | Copied source bytes, exact SHA-bound |
  | `contracts/authorization/v1/scope-registry.yaml` | `internal/contracts/shared/authorization/v1/scope-registry.yaml` | Copied source bytes, exact SHA-bound |

- New separate `identity/v1/native-browser-client-registry.yaml` is a
  **review candidate** not consumed by the existing CP Go runtime, never
  loaded as a workload authority or granted a business operation.
- The consumer lock's two source commit references were updated **together**
  to the exact Shared #421 commit. No floating `main` or branch reference
  and no test bypass were introduced.
- The old Shared/CP default on `main` continues unchanged until review
  and merge. N04 Shared #420's separately proposed IAM runtime reader is
  not silently inherited by this N02 source.

## Fail-closed development allocation

`baobab-dev-zuribeans-cp-principal-validator` is separately owner-bound,
`environment: development`, audience `baobab-control-plane`,
scope **only** `identity:principal:resolve`, and delegated subject
audience **only** `baobab-trade`. Its canonical status remains
`PROVISIONED`. The CP workload registry implementation grants scopes and
subject audiences only when a workload is `ACTIVE`; the new Go tests assert
zero effective scope, zero subject audiences and zero environment admission.
The existing Next.js buyer BFF is a distinct workload; the native browser
client is a distinct authorization-code **non-workload** registration.
Neither inherits the new Principal reader's scope.

**Development credential activation is still a separate native action.**
An isolated Hydra OAuth client must be independently registered, observed
with an exact owner/run/audience/scope, issued a time-bounded credential,
and revoked/deleted after the run. CP must positively enforce its own
current workload and Principal, and cannot rely on a PROVISIONED source
entry. This PR **does not** activate the registration to make CI pass.

## Acceptance and teardown separation

N02's connected chain requires the first-party ZuriBeans UI with real
Kratos login, MFA and recovery, actual Hydra authorization code and S256
PKCE exchange, Go IAM BFF and current CP Principal resolution, and
all seven original browser/INT03C cases in one immutable disposable run.
Infrastructure #69's separate N02 candidate lock MUST pin this CP head
and Shared #421 together; a mismatch fails its protected
`EMBEDDED_CONTRACT_DRIFT` check.

Source-level Go tests, Shared tests, static NGINX syntax and prior DEV-04
runtime checks are necessary but NOT connected N02 evidence. The final
original must bind the actual estate source, ingress digest, certificate
trust, IAM/CP/Shared source tuples, run ID, issuer readbacks and
independent host teardown. Unexecuted cases remain BLOCKED.

**Review hold:** final-head Go CI, exact contract test, external reviewer,
development registration + first-party real browser evidence, and
independently verified teardown. Protected environments and N04 provider
support remain unqualified.
