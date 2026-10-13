# ADR-BCP-026/027 completion plan and audited baseline

Date: 12 October 2026. Basis: the supplied handover, current main/branches/open
PRs, Accepted ADR-BCP-026 and ADR-BCP-027, ADR-IAM-0033 sections 123–144,
and Shared's EA v2.1 reconciliation. This is an implementation sequence and
evidence register, not a new architecture decision or production approval.

**EA v2.1 document status:** the current Shared document explicitly says
“proposed revision” and requires governance acceptance. Use its sequencing
framework without silently changing that status. Accepted CP/IAM ADRs remain
normative. LA-07 Console increments do not complete operational LA-07.

## 1. Audited immutable repository baselines

| Repository | Audited main | Findings relevant to completion |
|---|---|---|
| CP | `4927f362858c2f3e17b33faacd430113690f78d1` initially; `f2a1279391713b558d93eac73b3f5b0dfb246b51` after #379 | Organisation-first runtime, mandate lifecycle, evidence/custody and applicant/Console foundations exist. Protected qualification and native source certification remain separate. |
| Shared | `e875e738d2dd3022964ee539327ff647712dcd18` | #297 scope readback merged. Canonical admission/v2, organisation/v2 and scanner workload vocabulary already exist. |
| IAM | `ad7989ca085831303c265f943ef2b97735f911ad` | Provider-neutral runtime; permanent Keycloak federation; distinct source/scanner/extractor profiles. Scanner/extractor remain PROVISIONED. |
| Trade | `aae2b78fad685ec675978e52a2b34cfaab3353b5` | Native Medusa validate hook and protected staging denial implemented; trusted cart binding/PSP/IAM acceptance still required. |
| ERP | `b7ee0894e03a6a3ac9af4161838542f47779eecf` | Native JVM financial veto and atomic attribution exist; trusted context/readiness OSGi providers and native live posting proof missing. |
| Payments | `82d472b790c5e007c4d93c569c6ba70c74d3f807` | Journal/CP/HyperSwitch composition exists for bounded integration; protected runtime, TLS, issuer/merchant certification and full financial lifecycle not accepted. |
| Subscriptions | `66fc159423447ecc16c55c9fb1f9eabe654abc96` | Protected command origin/current INTERNAL checks exist; protected billing and legal-actor/provider proof remain open. |
| Trade Docs | `04a51f38d22c05b7232de4dc4c08f3506a1ebd38` | LA-05F issuer policy is reference code. No deployed issuance/signing/outbox runtime. |
| Infrastructure | `a7e5b842b87060695fa7ac47b0d2106e0a0a2331` | Protected synthetic/negative staging scripts and runbooks exist. Actual account/roles/issuer/endpoints are operator prerequisites. |

Repository audit means code/documents/contract and branch inspection, not a
live deployment audit. No cloud secrets or actual corporate facts were supplied
or invented. Provider documentation identifies the remaining operational gaps.

## 2. Branch and open PR reconciliation

- CP had one open PR, [#379](https://github.com/baobab-platform/baobab-cp/pull/379).
  Fixed generated-reference drift, Go formatting and contract-valid immutable
  scope readback. Addressed its review finding. All six final-head workflows
  passed at `1d57591ecf0c167b0406617c408ceec46d956f57`; merged as
  `f2a1279391713b558d93eac73b3f5b0dfb246b51`. The readiness gate was not weakened.
- Shared had dependency PRs #290–294; IAM had no open PR. Inspect each dependency
  diff and final-head CI separately when changing that repository. A major
  toolchain/action update is not automatically an onboarding prerequisite.
- Trade #135–144, ERP #89–94 and Infrastructure #29–33 were dependency PRs.
  Payments, Subscriptions and Trade Docs had no open PRs. These inventories
  are dated snapshots, not live promises that no newer PR exists.
- CP `feat/peo03h-extraction-clean` and `feat/peo03i-versioned-source-custody`
  retain unsquashed history, while current main already has their functional
  components. Do not merge their old bases wholesale over subsequent fixes.
- CP/IAM `feat/phase-b-c-platform-testing`, Shared RTD branches and engine
  actor/lock branches retain historical work. Reuse by current diff and
  semantics; branch ancestry alone does not mean missing implementation.
- Refresh main, open PRs, review threads and relevant branches before each new
  task set. Own fixes for any CP PR opened/continued in this programme, and
  merge only the reviewed immutable head with every applicable CI gate green.

## 3. Contract-consumer convergence findings

Comparing every path declared in each consumer lock with current Shared main:

| Consumer | Current pin | Declared paths changed since pin | Required action |
|---|---|---:|---|
| CP | `e875e738d2dd3022964ee539327ff647712dcd18` | 0 | Keep embedded bytes/generated closure synchronized. |
| IAM | `f2790abb8833fb40fdc7ef1d300faf887920fae4` | 0 | Older pin is compatible for its declared paths; do not bump for appearance alone. |
| Trade | `6899a2d8f143bf23d36c4f4ac904e0a1e10c3cea` | 8 | Review tenancy, first-party registry, context/OpenAPI, events and workload semantics. |
| ERP | `9e6e40739914b37465a7aa5ff463797d3c82d1cd` | 6 | Review OpenAPI/assignment/admission/finance-baseline/signed-delivery/event changes. |
| Payments | `6899a2d8f143bf23d36c4f4ac904e0a1e10c3cea` | 1 | Capability domain differs; additionally declare the newer actor/context/documentary contracts actually consumed by current code. |
| Subscriptions | `d8e544e2b3197a4d4f74446e6c7acba3772df934` | 1 | Evidence domain differs; retain current eligibility and exact producer semantics. |

These are byte-difference counts, not automatic incompatibility verdicts or
proof that undeclared consumed contracts converge. Payments' old nine-path
lock does not cover its new CP legal-actor/documentary transport. Native
support must not be published from a passing stale lock.

## 4. Sequential implementation sets and exits

| Order | Set | Concrete implementation and completion evidence |
|---|---|---|
| 1 | P0-01 — scope integration | #379 fixed/reviewed/merged. Keep scope arrays read-only, source-backed and independently permission filtered. Completed at code/CI level. |
| 2 | P0-02A — scanner admission/custody | Before scanning/sealing, hold current case → claim → evidence locks, bind subject/purpose/artifact and independent scanner actor. Reuse the transaction for immutable proof/audit; deny direct registration and expiry at commit. Unit/race and PG17 denial/replay/lock tests. |
| 3 | P0-02B — authenticated runtime composition | Implemented Shared #298/#299 contract and [CP private intake/isolated command](../migrations/P0-02B-authenticated-scanner-runtime.md), current JWT/introspection/registry/trust and durable exact replay; protected acceptance pending. Remaining operator evidence: separate scanner process/signer, issuer/audience/scope/current registry checks, source/extractor identity separation, cancellation and revocation negatives. Never mount scanner private keys in CP/OCR. |
| 4 | P0-02C — durable custody/source acceptance | Approved S3 Object Lock/KMS or equivalent adapter; retention/orphan reconciliation/key rotation, quarantine and deletion evidence; certified official-source/OCR transport. Contract/component code can precede account credentials; actual certification cannot. |
| 5 | P0-03 — truthful founding records and mandates | Use existing registration/control/mandate workflows; independent Nabhold/Thamani facts and separate corporate control evidence; four Organisations/Tenants; explicit roles/markets/activities/dates and different maker/checker. Real decisions and supplied evidence required. |
| 6 | P0-04A — native contract convergence | Audit actual consumed schemas, update exact engine locks/embedded contracts, regenerate consumers, prove positive and negative wire compatibility. Separate PR per owning engine. |
| 7 | P0-04B — execution-point enforcement | Trade binding issuance/revocation, ERP trusted native context/readiness and atomic posting, Payments protected authenticated TLS/money lifecycle, Subscriptions invoice responsibility, Trade Docs actual versioned signing/outbox. Native bypass, stale replay, ambiguity, expiry and wrong-market denials. |
| 8 | P0-05 / CX-06 | Backend-derived requirement applicability, grace/expiry and source standing; resume/edit without losing prior declarations; accessible authenticated journey. No frontend eligibility calculations. |
| 9 | P0-05 / CX-07 | Independent sponsorship, verification, mandate, restriction and appeal queues; exact Shared commands, maker/checker and caller-filtered readback. |
| 10 | P0-05 / CX-08 | Authorised request → immutable plan → operation → retry/blocked provider timeline with canonical causal references and denied/empty/stale states. |
| 11 | P0-05 / CX-09 | Distinct planned/observed/verified/restricted/stale/eligible/activated facts, each sourced from its owner; no global compliance or activation badge. |
| 12 | P0-05 / CX-10 | Permission-filtered audit investigation across case/decision/mandate/subscription/provisioning/provider references; redaction, pagination and immutable provenance. |
| 13 | P1-01 | Actual protected IAM/CP/engine deployment, exact active workload profiles, certified providers and independently recorded integration evidence. Infrastructure PRs do not establish deployment. |
| 14 | P1-02 | Four-business protected qualification, revocation/isolation, representative volume, failure/replay/drift, migration/backfill, rollback/restore and historical actor attribution. |
| 15 | P1-03 / PEO-07 / LA-07 | Capability-specific independent legal/provider/security/operations signoff and EA-09B. No blanket whole-tenant promotion. |

P0-02B implements code/runtime composition, while real scanner activation and protected acceptance remain unproven. P0-02C implements durable S3 Object Lock/KMS custody, approved retention/holds,
key rewrap and fenced orphan/deletion jobs. See the [custody runbook](../migrations/P0-02C-durable-custody.md).
Cloud deployment/acceptance and certified source/OCR transport remain independently open.

P0-02A is a bounded security closure of existing PEO-03L code. It is not full
P0-02 completion, a new canonical capability or live scanner acceptance.
P0-03 may be blocked on genuine records while independent consumer code work
continues in order. Each code set gets source comments, relevant documentation,
review, tests, commit/push and a distinct PR. Do not combine unrelated engine
activations into one administrative merge.

## 5. Evidence register and authentic prerequisites

Track each gate separately as architecture accepted, contract converged, code
merged, database migrated, synthetic/integration tested, protected staging
accepted, legal/provider evidence accepted and operationally certified. Every
positive entry needs an immutable commit/pin, migration, run and authorised
review reference. No percentage or mock-success metric substitutes for these.

Required external inputs include authorised registration evidence for Nabhold
and Thamani; independent control/ownership proof; real scoped mandate decisions;
approved scanner/OCR/source certification and keys; AWS account/OIDC role/state
configuration; staging DB/issuer/endpoints; certified merchant/settlement/native
provider facts; and separately authenticated security/operations acceptance.
Obtain these through approved operator/evidence channels, never committed secret
values or invented fixtures. None is created by this completion plan.

The four founding businesses retain Option B autonomy. ZuriBeans and Equator
remain operating Organisations without invented incorporation. Documentary
grace applies only to enumerated platform documents, never statutory/PSP/legal
actor authority. No successful scan or OCR candidate verifies incorporation.

## 6. P0-03 / P0-04A follow-up — 13 October 2026

Re-audited CP `88b896d6a73607882e9dc4bff4745142b13a5e3f`, Shared
`a9c53087d63caa356edf54ec8037ba1713c4827f`, IAM
`ad7989ca085831303c265f943ef2b97735f911ad`, and Trade. The initial baseline
tables above remain dated audit history; this follow-up records subsequent work.

P0-03's existing registration, independent corporate-control and scoped-mandate
workflows were reviewed against ADR-BCP-026/027. Nabhold's declared CIPC reference
`2026/029839/07` and Thamani's `2026/291672/07` have documentary references in
their repositories (`nabhold:docs/go-live/evidence/G01/production/2026-01-16-cipc-registration.md`
and `thamani:docs/legal/evidence/2026-04-09-cipc-registration.md`). Those references do not establish independent acceptance
in CP. Genuine protected-runtime submissions, independent control evidence and
authorised maker/checker decisions have not been supplied for this execution.
No founding verification or mandate was invented. ZuriBeans and Equator retain
operating-business standing without invented incorporation.

Following the permitted independent-consumer sequence, Trade
[#145](https://github.com/baobab-platform/baobab-trade/pull/145) implements the
first P0-04A engine slice at reviewed head
`fa4322f5c4c12a63a42c5706c3af8c5d78274e15`: exact Shared pin `a9c53087`, explicit legal-actor,
documentary and merchant-readiness consumption, byte-identical generated schema
closure/provenance and runtime wire validation before scope, actor, provider and
freshness checks. README, ADR follow-up and checkout maintainer notes were
updated. See the [Trade implementation/audit plan](https://github.com/baobab-platform/baobab-trade/blob/main/docs/plans/P0-04A-trade-contract-convergence.md).

Trade #140 (pg) and #143 (ajv-formats) were reviewed and merged with final-head
checks green before implementation. Trade #135–138 (setup-node, Foundation,
CodeQL and upload-artifact) subsequently passed full diff/workflow review,
final-head checks and review-thread inspection, and were merged. The exact
workflow updates are incorporated in the implementation head above. The full
REST response reported clean mergeability; an initial summary adapter mapped
unknown mergeability to false, which did not establish a conflict. Remaining
#139/#142 require failed-gate repair; #141/#144 require a separately reviewed
Node 26 upgrade. No waiver
or exception extension was introduced. CP had no other open PR at this audit.

Trade local checks passed: 459 unit tests, 88 exact-pin conformance tests,
typecheck, lint, formatting and Medusa build; imported schema assets load from
the standalone built validator. The Docker image consumes the compiled server
root and loads validators using production dependencies during image build.
Regeneration removes obsolete schemas and conformance checks the complete
directory inventory. Hosted final-head CI and review are mandatory for merge.

Trade's staging assessor remains PROVISIONED and lacks the documentary
assessment scope; the ACTIVE production profile lacks both private assessment
scopes. Missing scopes and existing mapping/market scope gaps require separate
Shared/IAM/CP admission review. No consumer pin update activates a workload.
ERP is the next owning-engine P0-04A slice, followed by Payments, Subscriptions
and Trade Docs. P0-03 genuine acceptance, P0-04B native live execution and
P0-02C cloud/source/OCR certification remain independently open.

## 7. P0-04A ERP follow-up — 13 October 2026

ERP [#95](https://github.com/baobab-platform/baobab-erp/pull/95) implements the
next owning-engine contract-convergence slice at head
`db50884e574fe60a9ef6fc324dd4db4f5b8319dd`, following Trade #145. Its reviewed
baseline is `0b412844f01b6a01c924bea438364282edf90d28` after ERP dependency
PRs #89–94 were inspected and merged with complete final-head CI green and
no unresolved review findings. The builder upgrade retains Java 17 targeting.
CP/Shared/IAM were rechecked at `05cad8e1e0f6594de0b4978edc57b7463068867a`,
`a9c53087d63caa356edf54ec8037ba1713c4827f` and
`ad7989ca085831303c265f943ef2b97735f911ad`, respectively.

ERP advances its lock from Shared `9e6e407` to `a9c53087`, reviewing six changed
declared files and declaring the previously omitted legal-actor/documentary
consumers, workload vocabulary and their reference closure. Seventeen exact
Shared schemas with hash/entry provenance are generated for Python and the native
JVM; regeneration removes obsolete files. Cached, format-enforcing validators
check closed wire shapes and stored-context UUIDs before existing independent
scope, actor, freshness, negative-veto, finance readiness and atomic attribution
checks. Native CP response retention is bounded to 16 KiB and a complete
three-second response deadline. No schema is downloaded at runtime.

A real installed Python wheel and a standalone privately shaded OSGi JAR are
checked for original bytes/inventory and validator operation. JVM probes also
check Java 17 class targeting and isolated resource visibility. The synthetic
144-vector matrix is independently compared with the pinned Shared owner, Python
and JVM. Local tests passed; hosted CI additionally runs real PostgreSQL and
HTTP slow-body/oversized/redirect checks. The initial hosted run exposed a missing
runtime dependency installation in an auxiliary unit workflow and a malformed
positive inbox fixture; these were repaired without weakening validation.
README files, source comments, ADR follow-up and maintenance notes were updated.
See the [ERP maintenance/audit plan](https://github.com/baobab-platform/baobab-erp/blob/main/docs/plans/P0-04A-erp-contract-convergence.md).
Final-head CI and resolved review are mandatory before either PR merges.

ERP's staging assessment profile has the required context/legal/documentary
scopes but remains PROVISIONED. Its ACTIVE production workload lacks both private
assessment scopes. This convergence admits no workload and certifies no native
posting. Trusted context/readiness OSGi providers, genuine legal/control/mandate
facts, actual Equinox/native posting/rollback and direct-bypass acceptance remain
separate P0-03/P0-04B gates. Historical ERP branches are not merged wholesale over
later JVM/attribution safeguards.

Next consumer sequence: Payments, Subscriptions, then Trade Docs, with distinct
owning-engine PRs. P0-04A as a whole remains open until those slices converge;
cloud/source/OCR and protected runtime/provider acceptance remain independent.


## 8. P0-04A Payments follow-up — 13 October 2026

Payments [#36](https://github.com/baobab-platform/baobab-payments/pull/36) implements
the next owning-engine slice after ERP #95, starting at
`82d472b790c5e007c4d93c569c6ba70c74d3f807`. The implementation head is
`3bc4e8c3331f9547712856b06492e1c1ab2bade8`: all 26 checks and five workflows
passed, with no unresolved review findings. It merged as
`f065ddaf9dc141aba222f74f8b2481b8f2e877f0`. The audit rechecked CP
`cadf8ce51bf416234d7a954b66414b30966dedd1`, Shared
`a9c53087d63caa356edf54ec8037ba1713c4827f` and IAM
`ad7989ca085831303c265f943ef2b97735f911ad`. At the audit cutoff before opening
Payments #36 and this companion PR, Payments/CP/IAM had no open PRs.
Shared dependency PRs #290–294 remain independent. Historical Payments branches
were compared against current journal/CP/HyperSwitch and protected refusal work;
no old tree was merged wholesale.

The old nine-path lock changes one declared capability-domain file additively
(`regulations`) and omitted actual context/legal/documentary/readiness schemas.
The new exact pin declares 19 files with complete transitive closure, immutable
Git-blob generation, stale inventory removal, SHA-256 provenance and generated
Rust includes. Offline, format-asserting validators check both HTTP and
consequential injected-policy boundaries before existing context/actor/operation,
freshness, double-refresh and independent PSP checks. Merchant readiness uses the
owner schema and retains the 503 certification refusal. CP/IAM JSON rejects
duplicate members recursively; body retention is bounded while reading under the
complete deadline, including chunked and slow responses.

A persisted 130-vector synthetic matrix independently compares original Shared
owner Git blobs with Rust and the standalone release executable. Docker builder
probes validate the actual compiled binary without schema retrieval. Native
PostgreSQL CI is triggered by authority/contract changes. README, ADR-PAY-0018,
source comments and [maintenance/audit notes](https://github.com/baobab-platform/baobab-payments/blob/main/docs/plans/P0-04A-payments-contract-convergence.md)
were updated. Local owner/generation/inventory/syntax checks pass. Hosted
final-head validation passed 55 Rust tests, all 130 wire vectors, the actual
standalone release probe, formatting/Clippy, container and native PostgreSQL
integration. The initial formatter findings were applied without relaxing the
gate. Immutable evidence: [Rust CI 38356152921](https://github.com/baobab-platform/baobab-payments/actions/runs/38356152921),
[Foundation 38356154286](https://github.com/baobab-platform/baobab-payments/actions/runs/38356154286)
and [native PostgreSQL 38356152992](https://github.com/baobab-platform/baobab-payments/actions/runs/38356152992).
This companion document still requires its own final-head CI/review before merge.

Payments has a PROVISIONED staging assessor with all three context/legal/documentary
scopes; there is no production Payments assessment profile at this pin. Pinning
contracts activates no workload and certifies no money movement. Public routes
remain a protected-denied, in-memory sandbox; bounded local native journal and
signed-webhook/settlement components do not supply protected TLS, genuine records,
merchant/beneficiary/provider certification, full lifecycle or operational proof.
P0-03 authentic founding/mandate acceptance and P0-04B live runtime acceptance
remain independent, as do P0-02C cloud/source/OCR gates.

Next consumer sequence: Subscriptions, then Trade Docs. P0-04A as a whole stays
open until those owning-engine slices converge.


## 9. P0-04A Subscriptions follow-up — 13 October 2026

Subscriptions [#40](https://github.com/baobab-platform/baobab-subscriptions/pull/40)
implements the next owning-engine slice after Payments #36, starting at
`66fc159423447ecc16c55c9fb1f9eabe654abc96`. Its implementation head is
`374935303c7cac31b5a547ef23dac3a7bad18a21`: all 26 checks and four workflows
passed with no unresolved review findings. It merged as
`52c69906343d7b1afee08a88b8d3afe118af0428`. This audit rechecked CP
`9e782760e58b554fe48560ac449ba8ee944bf6ec`, Shared
`a9c53087d63caa356edf54ec8037ba1713c4827f` and IAM
`ad7989ca085831303c265f943ef2b97735f911ad`. Before opening this task's PRs,
Subscriptions/CP/IAM had no open PRs. Shared dependency PRs #290–294 remain
independent. Historical authority/inbox/lock branches already have their useful
components on main; their old trees would remove subsequent migrations,
verified-client replay and protected command-origin safeguards.

The existing twenty-path lock differs only in six additive evidence document
classes. Exact pin `a9c53087` now declares 22 files including workload/scope
vocabulary, full immutable reference closure and hash provenance. Generated
resources replace stale files; the packaged Java registry disables remote
retrieval, asserts formats and permits only declared schema files. Twelve owned
entries cover current INTERNAL eligibility, the GET tuple's three property
grammars, the event envelope and seven founding payloads. Validation precedes
existing tenant/subscription/reference/current lease, producer/type/subject and
durable receipt checks. Valid negative wire facts still deny. Events never grant
classification or current eligibility.

Strict JSON rejects duplicate members recursively and trailing input. CP body
retention is bounded during chunked reads to 16 KiB under a complete three-second
deadline; credential files are read anew with an 8 KiB cap. Redirect-enabled
injected clients are refused. Existing zero-charge/no-payment, verified-client
raw-byte replay, revision/tenant isolation and protected command producer rules
remain in force. Source comments, README, ADR-SUB-0016, ADR alignment and
[maintenance/audit notes](https://github.com/baobab-platform/baobab-subscriptions/blob/main/docs/plans/P0-04A-subscriptions-contract-convergence.md)
were updated.

A persisted 144-vector matrix independently compares immutable owner Git blobs
with Java across all twelve entries. Isolated packaged JAR and Docker probes
check original byte/inventory, Java 21 targeting, actual runtime libraries and
resource ownership without source/test classes or issuer/database configuration.
Tests cover malformed wires, valid foreign/stale authority, slow/oversized bodies
and invalid events leaving no PostgreSQL receipt. Local owner/generation/syntax
checks pass. Hosted final-head CI passed 48 Java tests with no failures or skips,
144 owner/Java vectors, actual isolated JAR probes and container checks. Three
review findings on the subscriber test helper's name/interface annotations were
fixed and resolved without changing behaviour. Immutable evidence:
[Java/PostgreSQL CI 38367320601](https://github.com/baobab-platform/baobab-subscriptions/actions/runs/38367320601)
and [Foundation 38367322553](https://github.com/baobab-platform/baobab-subscriptions/actions/runs/38367322553).
This companion document requires its own final-head CI/review before merge.

The only Subscriptions workload at this pin is PROVISIONED for Payments; it has
no CP audience or `subscription:internal-authority` scope. The PROVISIONED CP
billing producer's profile supplies no usage/founding-intake admission. These
require separate Shared/IAM/CP review, never a consumer pin or synthetic fixture.
No legal incorporation/mandate facts, chargeable invoice responsibility, Kill Bill,
Payments handoff, complete billing lifecycle or protected runtime/provider
qualification is asserted. Genuine P0-03, protected P0-04B and P0-02C
cloud/source/OCR gates remain independent.

Next owning-engine consumer: Trade Docs. P0-04A stays open until that final slice
converges; programme completion and operational certification remain separate.


## 10. P0-04A Trade Docs follow-up — 13 October 2026

Trade Docs [#13](https://github.com/baobab-platform/baobab-trade-docs/pull/13)
implements the final current-consumer slice at reviewed head
`25c85d5a277f354203ae5ccdda0fcfdb13e0911d`. Audited main was
`04a51f38d22c05b7232de4dc4c08f3506a1ebd38`, with only main and no open PRs
before this task. CP main `afeab1114cdbeb375b57230f0894b826e78c076a` had no
open PRs before this companion; Shared is unchanged at
`a9c53087d63caa356edf54ec8037ba1713c4827f`.

The LA-05F issuer reference previously lacked a consumer lock and used manual
wire checks. It now packages twelve exact declared Shared files with full
relative reference closure, three owned validation entries and SHA-256 provenance.
Regeneration reads immutable Git blobs, removes stale resources and checks the
complete inventory. Both assessment passes validate the canonical legal request/
response and stored context UUID before existing independent issuer, operation,
provider, thirty-second lease and final mandate-equality checks. UUID/date-time
formats, closed fields, policy provenance and negative-resolution conditions are
asserted; no wire-valid denial becomes issuance authority. Runtime schema
retrieval is disabled. Source comments, README, RTD source/test reference evidence,
ADR follow-ups and [maintenance notes](https://github.com/baobab-platform/baobab-trade-docs/blob/main/docs/plans/P0-04A-trade-docs-contract-convergence.md)
are updated. Proposed technology/issuer/API ADRs remain Proposed.

Local validation passed fourteen tests, 45 immutable-owner/reference wire vectors,
exact generation, compile/RTD checks and a real installed reference wheel tested
outside the source checkout. Original byte/inventory and executable validator
checks require no Shared checkout, issuer or database. Local Python is 3.12;
hosted CI verified Python 3.14, all fourteen tests, the exact bundle and installed
wheel's 45 wire cases. All five final-head checks passed, including RTD, CodeQL,
secret scanning and action pinning; no review findings remained. Trade Docs #13
merged as `73cd1c168be39305c2e238b1f0a5b72da7821539`. Immutable CI evidence:
[issuer/reference 38382816870](https://github.com/baobab-platform/baobab-trade-docs/actions/runs/38382816870)
and [RTD 38382817441](https://github.com/baobab-platform/baobab-trade-docs/actions/runs/38382817441). Each published blob and the complete tree match the reviewed
local commit. This companion requires its own CI/review before merge.

Trade Docs remains ARCHITECTURE_ONLY, with a PROVISIONED staging assessor and no
running document API, authenticated CP transport, PostgreSQL immutable versioning,
signer, customs gateway, outbox or publisher. P0-04A is complete for the current
five-engine consumer scope following this final reviewed/CI-verified merge; it does not
complete the wider programme, native implementation or live certification.

Next scheduled set is P0-04B execution-point enforcement: audit remaining Trade
binding/revocation, ERP trusted native context/readiness, Payments protected money
lifecycle, Subscriptions invoice responsibility and Trade Docs actual versioned
issuance/signing/outbox against each owner's current ADRs and open PRs. Do not
substitute synthetic success for protected acceptance or infer capability support
from a reference wheel. Genuine P0-03 founding/control/mandate decisions and
P0-02C cloud/source/OCR qualification remain independently open.


## 11. P0-04B-01 Trade lifecycle follow-up — 13 October 2026

The first bounded P0-04B set is Trade [#146](https://github.com/baobab-platform/baobab-trade/pull/146)
at reviewed head `73456750a494a0f7a33c8096a747e718c7ac6777`. Trade baseline is
`1eaddb9a67fbd05b56311b1dc22e8efe151d0ab4`; CP is
`778958615aee984c1a933039114dc4ce0a19a5c5`. Shared/IAM are unchanged at
`a9c53087d63caa356edf54ec8037ba1713c4827f` and
`ad7989ca085831303c265f943ef2b97735f911ad`. All eight audited repositories
had no open PRs before this task. Older Trade branches were inspected;
`feat/la-05c3-trusted-native-cart-runtime` diverges from main, so current C3–C8
and P0-04A components are retained rather than overwritten by its old base.

Trade's native completeCart policy already rechecks binding, legal actor,
documentary veto and independent Payments readiness. The C5 command persistence
was uncomposed, its decision/audit DDL was only a reference, and approval remained
inactive with placeholder context/activity. This increment installs the real
Medusa migration/internal persistence path and closes storage/expiry bypasses;
it does not complete P0-04B execution enforcement or admit live checkout.

The empty storage module now always migrates while CP/IAM/Payments checkout
clients and credentials remain lazy and protected by the existing deployment
policy/guard. PostgreSQL fences each cart before the row lock, freezes complete
original proposal provenance and revision, and compares approval to the stored
JSONB snapshot. Current-clock expiry is checked after waits/I/O and again by the
deferred COMMIT receipt constraint. Every committed row change requires its
append-only decision and local unpublished candidate audit record. Generated CRUD
cannot activate, rewrite, delete, replay or bypass those receipts. Approval remains
PROPOSED; no RUNTIME UUID or legal mandate is fabricated. Legacy rows are not
silently certified/backfilled. Recorded history blocks an unsafe destructive down
migration.

The command service now requires all named scope fields and array-valued scopes,
valid calendar timestamps, fresh evidence and current identity/command authority
plus CP context after evidence I/O. Revocation does not require the legal mandate
to remain valid. Idle pool failures are handled without logging driver errors or
credentials. Source comments, README, accepted Trade checkout ADR follow-up and
[maintenance/rollback notes](https://github.com/baobab-platform/baobab-trade/blob/main/docs/plans/P0-04B-01-trade-binding-lifecycle.md)
are updated. No canonical capability, event producer, broker publication or scope
admission is added.

Local validation passed 462 source tests, 88 exact-pin Shared conformance tests,
typecheck/lint/format, Medusa build and packaged schema validation. The actual
native Medusa/PostgreSQL lifecycle regression passed on final Trade head
`73456750a494a0f7a33c8096a747e718c7ac6777`, including deferred pre-COMMIT expiry.
Its immutable proof is [CI 38388095844, native job 115264762027](https://github.com/baobab-platform/baobab-trade/actions/runs/38388095844/job/115264762027).
The wider native commerce suite also passed. All 33 final-head checks were
clear (25 succeeded, eight non-applicable checks skipped); all five applicable
workflows passed, with only the path-inapplicable temporary formatter skipped.
No unresolved review findings remained. Trade #146 merged as
`71e62d0a300444a1c1d474b5346b14c0f22abfe8`. P0-04B-01 code/native storage proof
is complete; authenticated activation and the wider execution programme remain open. Test discovery
selects source directories so an earlier build's CJS test copies do not become
extra suites. The hosted native proof applies the actual Medusa migrations to disposable
PostgreSQL 17 and verifies lifecycle, immutable scope/provenance, duplicate proposal/
approval and approval/revocation races, direct activation/audit denial, lock-wait
expiry, audit-I/O rollback and expiry immediately before COMMIT. Fixtures are
synthetic storage proof and remain inactive; no IAM/CP/legal/provider or successful
checkout acceptance is asserted. Each published Git blob and the complete tree
match the reviewed local commit. All applicable final-head CI/review must pass
before either owning PR is merged.

### Remaining P0-04B sequence

| Set | Owning boundary and evidence still required |
| --- | --- |
| P0-04B-02 | Trade authenticated command intake using approved Shared/IAM vocabulary; actual cart/region/channel and CP-issued owned RUNTIME handle; independently approved versioned activation/revocation |
| Trade protected execution | Real native completeCart/replay/revocation/bypass proof, current CP actor/documentary standing, qualified merchant readiness and separately admitted workloads |
| ERP | Native trusted context/readiness providers and atomic posting/historical actor acceptance |
| Payments | Protected authenticated transport, qualified merchant/provider and full native money lifecycle |
| Subscriptions | Actual invoice responsibility, protected billing and provider handoff |
| Trade Docs | Deployed immutable versioning, actual issuer/signing and atomic outbox workflow |

Current `trade:legal-seller-binding:*` scopes are local candidates, absent from
Shared's registry. Candidate IAM/evidence HTTP gateways are not a deployed
approved authority. Do not enable a human mutation endpoint or activation from
these stubs. P0-04B-02 must resolve that contract/admission boundary explicitly.
Genuine P0-03 corporate/control/mandate records and P0-02C cloud/source/OCR
qualification remain independently open, along with protected operational
acceptance. An approved inactive proposal is not permission to transact.

## 12. P0-04B-02 — authenticated binding intake and CP-owned activation

Shared #300 (`5dcff30de1bc6142ad4b8801bbe3b2b66a0670ae`) defines closed human
CP scope/digest admission and exact nondelegated Organisation HIGH permissions,
without client, profile or workload assignments. IAM #112 (`119f602`) aligns scope
and compiled federation provenance; CP #389 (`beb11b0`) verifies actual IAM tokens,
ACTIVE canonical human identity, current exact grant and assurance on every call.
CP #390 (`c739d8e`) uses the established nonproduction allowlist and proves positive
routing in development/test/integration/sandbox/staging; production/unknown and disabled-by-default configurations deny. The pin and profile review findings are corrected and resolved.

Trade #147 preserves original proposal/checker history and admits commands only
through that owning CP endpoint. Native Medusa supplies cart channel/region/currency;
authenticated non-withdrawal commands hold the native cart row through COMMIT.
Activation requires a third canonical principal, authenticated maker/checker
receipts, a fresh bounded Trade-owned CP RUNTIME context and current seller fact.
The CP seller source is re-resolved after final human/native I/O; observed mandate
revocation or change denies while the first fact is still unexpired.
Versioned activation, decision and local unpublished audit records commit atomically;
current-clock deferred constraints deny expired admission/native coordinates/legal
leases. Old unauthenticated storage approvals cannot be promoted. Revocation retains
all prior proof and remains possible after old context/mandate expiry and removal
of workload credential configuration.

Native PostgreSQL 17/Medusa acceptance uses real native carts with explicitly synthetic
CP ports. It covers independent activation, denied current seller/completed cart,
legacy history, concurrent/replayed transitions, context rewrites, append-only proof,
withdrawal and pre-COMMIT admission expiry rollback. This is runtime/contract seam
proof, not actual IAM grant, mandate, workload admission or merchant certification.

**P0-04B remains open.** The next bounded Trade set is protected native checkout
acceptance with commit-point revocation/lease/replay fencing and independent qualified
merchant readiness. Real P0-03 corporate/control/mandate records, authorised human
client/grant decisions and workload/provider acceptance remain outstanding. ERP,
Payments, Subscriptions and Trade Docs execution-point boundaries in section 11
remain separate outstanding sets.

Implementation validation at Trade head `ffa977a2e0a4dbb8ffa145d80791b102692799a7`:
489 source tests passed, typecheck/lint/format/build and standalone authority assets
passed. The native PG17/Medusa binding regression is part of CI run `38426513869`,
core job `115397216139`; final native/core workflow acceptance must remain green
before the implementation PR and this programme receipt merge. Shared conformance
covers 89 checks against the owning commit, including exact generated bytes.
