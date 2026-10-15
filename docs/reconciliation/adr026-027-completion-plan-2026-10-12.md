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

### Remaining P0-04B sequence after P0-04B-02

| Set | Owning boundary and evidence still required |
| --- | --- |
| Trade protected execution | Binding intake/activation and the native cart consumption fence have source/native seam coverage (§12–13); full protected order/payment paths, current external standing, qualified merchant readiness and separately admitted workloads remain open |
| ERP | Native trusted context/readiness providers and atomic posting/historical actor acceptance |
| Payments | Protected authenticated transport, qualified merchant/provider and full native money lifecycle |
| Subscriptions | Actual invoice responsibility, protected billing and provider handoff |
| Trade Docs | Deployed immutable versioning, actual issuer/signing and atomic outbox workflow |

P0-04B-02 closes the authenticated intake and independent activation implementation
boundary (section 12). The old candidate scope strings and IAM/evidence gateways
are retired; the canonical human CP vocabulary is registered in Shared and actual
CP admission/owned-context/legal-actor routes are composed in Trade. Intake stays
default-off in recognised nonproduction; no human client, grant, workload or provider
assignment is created and production checkout remains closed.
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
of workload credential configuration. Trade #148 additionally reads legacy immutable
scope directly for withdrawal when no original proposal snapshot exists; no historical
proposal is invented and those rows cannot be approved or activated.

Native PostgreSQL 17/Medusa acceptance uses real native carts with explicitly synthetic
CP ports. It covers independent activation, denied current seller/completed cart,
legacy history, concurrent/replayed transitions, context rewrites, append-only proof,
withdrawal and pre-COMMIT admission expiry rollback. This is runtime/contract seam
proof, not actual IAM grant, mandate, workload admission or merchant certification.

**P0-04B remains open.** Section 13 records the next bounded Trade native cart
consumption/lease/replay fence. Full protected order/payment paths and independent
qualified merchant readiness remain separate outstanding execution gates. Real P0-03 corporate/control/mandate records, authorised human
client/grant decisions and workload/provider acceptance remain outstanding. ERP,
Payments, Subscriptions and Trade Docs execution-point boundaries in section 11
remain separate outstanding sets.

Implementation validation at Trade head `ffa977a2e0a4dbb8ffa145d80791b102692799a7`:
489 source tests passed, typecheck/lint/format/build and standalone authority assets
passed. All 32 final-head checks and all applicable workflows passed, with the
path-scoped temporary formatter legitimately skipped. The full native PG17/Medusa
commerce suite and binding regression passed in CI run `38426513869`, core job
`115397216139`. Trade #147 merged at `1e380b749b35b2f4af26fdf9f7c2717ac5f7c0a7`.
Shared conformance covers 89 checks against the owning commit, including exact
generated bytes. Programme receipt CP #391 merged at
`dce58413350c9b1682338a471b51a65923c78826` after its final-head checks passed
and review findings were resolved.

Legacy withdrawal follow-up Trade #148 head `2efaf3bcaf70b5e818fd3d19a9254580b0ae7518`:
492 source tests pass. Its PG17 acceptance seeds an expired ACTIVE row under the
actual original owner migration in an isolated test schema, applies the actual
lifecycle/activation migrations, denies approval/activation and withdraws with
current human authority while retaining null snapshots and original historical
fields. It does not disable triggers or manufacture old audit receipts; the schema
and new receipt remain until disposal of the explicitly marked CI database.
All 32 final-head checks passed (24 success, 8 applicable skips), including full
PG17/native core CI run `38431741561`, job `115415390207`; its logs explicitly
record the actual legacy schema upgrade, authenticated intake and lifecycle
acceptance. Review was clear before #148 merged at
`d8d517c17069eca7e4abb7d9d5920c9a1ec9c30b`. This merged baseline is the parent
of #149; legacy withdrawal is not a pending dependency.


## 13. P0-04B-03 — native cart consumption and COMMIT lease fence

Trade [#149](https://github.com/baobab-platform/baobab-trade/pull/149) follows the
merged binding intake/activation and legacy withdrawal work. The actual Medusa
completion saga records a durable authorization after the installed validate hook
checks current server-owned binding, CP legal actor, negative-only documentary
standing and independent Payments readiness. Ever-activated carts consume one
receipt at their `completed_at` transition under the same per-cart advisory fence
as revocation. Current ACTIVE revision, native coordinates, binding/context
expiry and the legal/documentary/merchant leases are checked with the database
clock; a deferred constraint repeats these checks immediately before COMMIT.
Expiry between UPDATE and COMMIT rolls back completion and consumption together.
Compensation releases the consumed receipt and voids outstanding ones; retry
requires fresh authorization. Guarded replay rechecks current standing; revoked
replay denies while preserving the historical order. A replay shortcut requires
the live native order/link; a bare completion timestamp cannot bypass fresh
authorization or be replaced on an activated cart.

The native PG17 acceptance uses real carts, the actual `completeCartWorkflow`,
applied migrations and durable receipt storage. External CP/Payments ports are
explicitly synthetic. Database-only cases exercise revocation, missing receipt,
lease expiry, direct cart-completion bypass, append-only history, multiple
outstanding receipts, compensation, concurrent completion and deferred COMMIT
expiry. Guard-enabled cases exercise the actual installed validate hook for
completion/replay and documentary, merchant, missing-binding and revoked-replay
denials. These seams do not qualify any real IAM principal/grant, legal mandate,
workload, merchant or provider. Generated Redis state is excluded from source.

**Boundary:** Medusa creates orders and commits cart completion, native links,
events and payment steps separately. This local cart fence is not a serializable
cross-engine transaction or a fence on direct order/payment execution. External
revocation after assessment is bounded by the shortest recorded lease, including
the at-most-two-second documentary lease. Never-activated/unbound SQL completion
also remains outside the trigger. Production remains hard-denied. Wider P0-04B,
qualified provider/workload acceptance, genuine P0-03 and P0-02C stay open.

Continuation source validation: 501 tests in 91 source files, 89 Shared conformance
checks at unchanged pin `5dcff30`, typecheck/lint/format/build and authority assets,
plus contract/ports/isolation/conformance/ZB-04 checks passed locally. Published
Trade head is `c6790e2c973bb3611424d25bf337f7fd38a4260d`; the final-head PG17
receipt is [CI run 38512966743](https://github.com/baobab-platform/baobab-trade/actions/runs/38512966743),
core job `115685410256`. That final-head run passed the full native commerce
suite and explicitly logged guarded checkout/COMMIT-expiry acceptance. All 33
checks were clear (25 success, 8 legitimate skips), all five applicable workflows
passed and the path-scoped temporary formatter was skipped. Review was clear
before #149 merged at `5e1cc718354e02c92658cf8779fa00068dbe6af1`. This programme
receipt independently requires its own final-head checks and clear review before
merge; the native proof remains synthetic-port acceptance, not certification.

Next bounded owning-engine set: ERP native trusted context/readiness, atomic
posting and historical actor acceptance. Trade full order/payment execution and
real operational qualification remain separately open.

## 14. P0-04B-04 ERP posting authority — 14 October 2026

Owner decision (14 October 2026): CP owns canonical authority (tenant, operating
Organisation, verified legal entity, mandate, role/activity/market, capability,
engine instance); ERP owns native placement (`AD_Client`/`AD_Org`) and accounting
execution. Provisioning (`ErpAssignment`) is not posting authority, and an ERP
placement never creates authority: a holding company's placement cannot post for
Thamani or ZuriBeans without their own current mandate. Proof standard: Gates A
(unit/contract), B (PostgreSQL), C (JVM bundle) on every PR; Gate D (a real
iDempiere/PostgreSQL posting run) before "atomic posting verified"; Gate E
(operational) before production.

| Set | PR | Merge | Content |
|---|---|---|---|
| Contract | Shared [#304](https://github.com/baobab-platform/shared/pull/304), [#306](https://github.com/baobab-platform/shared/pull/306) | `8733be2`, `92541c9` | ADR-SHARED-034; `erp/v2` posting-authority request/decision (opaque placement reference, no vendor ids, ≤30 s lease, role/activity bound per class), closed operation catalogue, scope `erp-posting-authority:resolve` granted only to the PROVISIONED ERP staging assessor; #306 uncaps decision evidence to match mandates |
| CP resolver | CP [#393](https://github.com/baobab-platform/baobab-cp/pull/393) | `366fe6e` | `/internal/erp/v1/posting-authority/resolve` (flag, nonproduction, allowlist); live engine-instance binding; LA-04 resolution with mandate lineage; immutable `registry.erp_posting_authority_decision` (000125) recorded before disclosure |
| ERP enforcement | ERP [#96](https://github.com/baobab-platform/baobab-erp/pull/96) | `c3ed799` | ADR-ERP-022: maker/checker native placement, document provenance, attribution lineage, deferred COMMIT fences on `Fact_Acct` and completion; placement/readiness providers; CP decision consumption. Final-head [CI run 38551670539](https://github.com/baobab-platform/baobab-erp/actions/runs/38551670539) (Gate B `native-finance-postgres`, 10 tests) |
| Gate D | ERP [#98](https://github.com/baobab-platform/baobab-erp/pull/98) | `d17ac3a` | CI job `idempiere-posting-integration`: shipped image + CI-only driver posts GardenWorld GL journals on a fresh database against a synthetic CP over TLS; asserts persisted state |

**Gate D receipt:** final-head [CI run 38560332080](https://github.com/baobab-platform/baobab-erp/actions/runs/38560332080),
job `115839541014`, evidence artifact `11890764990`: 19/19 checks, `Gate D failures: 0`,
on the real deployable image. Covered: valid authority (CO, posted, balanced
`Fact_Acct`, attribution with CP `decision_id`); authority lost between draft and
completion as no mandate, revoked, other legal entity, other tenant, wrong market,
wrong activity/role, expired lease and documentary hold (each refused, no CO, no
ledger); unplaced native org (creation refused); failure injected after `Fact_Acct`
writes and before commit (nothing survives); three concurrent postings; retry
after commit (no duplicate); background forced repost under authority (rebuilt,
newly attributed) and after revocation (refused, committed ledger intact); direct
SQL `Fact_Acct` insert and completion (refused at COMMIT).

**Defects Gate D found that A–C could not** (fixed in #98): the JVM finance veto,
registered through `ModelValidationEngine.addModelChange` with client 0, never
fired for a real `AD_Client` (iDempiere keys such listeners per client) — now a
synchronous OSGi `EventHandler` on the BEFORE topics; the native triggers failed
on iDempiere's `NUMERIC(10)` ids (forward migration N0004; the Gate B fixture now
uses real types); and Automatic Period Control writes `C_AcctSchema` from
`MPeriod.isOpen`, so governed clients must use standard period control
(ADR-ERP-022 §6).

**Gate status:** A–C passed on every PR; **D passed for GL journals** (atomic
posting verified for `erp.gl-journal.post` on synthetic placement, mandate and
CP). **E open.**

**Boundary:** Gate D is synthetic-port acceptance, not legal or operational
certification; synthetic placements and mandates are never evidence of legal
standing. Customer invoice, payment, allocation, shipment and sales-order classes
still need their own Gate D scenarios; purchase-side and configuration classes
remain denied pending a Shared catalogue revision; costing writes are
unclassified. The posting-activity strings need owner review. Real placements
(named maker/checker), real mandates, IAM workload admission, the Python
order-to-cash wiring and production enablement stay open; the CP route remains
nonproduction-only.

## 15. P0-04B-05 ERP remaining operation classes — 14 October 2026

ERP [#99](https://github.com/baobab-platform/baobab-erp/pull/99) (merge `67bdced`) extends §14 from GL journals to
every other catalogued class: `erp.sales-order.complete`, `erp.shipment.complete`, `erp.customer-invoice.issue`,
`erp.payment.record` and `erp.allocation.record`. It also closes two defects that exploration of ERP `main` found.

Owner decisions (14 October 2026):
- **Receipts only.** Permission to record money received is not permission to disburse it. A payment is
  governed only when `IsReceipt='Y'`, on every route. An allocation is governed only between sales invoices and
  receipts. Bank statements and the cash journal have no class and are refused. Before #99, an outbound vendor
  payment classified as `erp.payment.record`: a P0 authorisation-correctness defect.
- **Derived costing.** A native costing write may inherit authority from a catalogued root ERP operation only
  when ERP proves it is a deterministic consequence of that operation, in the same transaction and under the
  same placement, provenance and legal-actor attribution. Without that proof it is denied. Independent costing
  or revaluation operations need their own catalogued authority (ADR-ERP-022 §4.2).
  - Before #99, `M_Cost`/`M_CostDetail` were hard-denied, so no sales shipment could post.
  - Now a shipment's cost detail inherits the shipment's own CP decision. It never gets a costing decision of
    its own.
  - `M_Cost` is covered only for the exact bucket derived in the transaction: cost type, cost element and
    attribute-set instance.

Database (N0005):
- completion and `Fact_Acct` COMMIT fences require an attribution of the source table's own class, with the
  governed direction holding;
- a governed document's direction cannot change;
- the append-only `derived_write_attribution` table;
- COMMIT fences on `m_costdetail` and `m_cost` (insert, update and delete) and on `c_allocationline`.

Codex review found three P1 gaps (cost deletes, allocation re-pointing, bucket granularity). All three were fixed
before merge.

**Evidence.**
- **Gate B:** 20 PostgreSQL tests; the new ones fail without N0005.
- **Gate D:** final-head [CI run 38603012390](https://github.com/baobab-platform/baobab-erp/actions/runs/38603012390),
  job `115986718366`, artifact `11914013564`. **60/60 checks, `Gate D failures: 0`**, on the real deployable
  iDempiere 13 image against a synthetic CP.
  - **Valid chain:** order → shipment, posted with 2 derived cost details → invoice → receipt with automatic
    allocation → manual allocation. Each document reaches CO, is posted and balanced, and is attributed to its
    own class.
  - **Lost authority:** revoked, wrong activity, other legal entity, expired and documentary hold, each at
    completion of order, shipment, invoice and payment. All refused, with no ledger and no costing.
  - **Shipment failure and retry:** an injected failure after ledger and cost rows leaves nothing behind. A retry
    or a forced repost under authority duplicates nothing. A forced repost after revocation is refused.
  - **Money out and purchases:** outbound payment creation and posting, an allocation against a vendor invoice,
    and posting a purchase receipt are all refused. So is a direction flip.
  - **Direct SQL:** an outbound completed payment, a payment `Fact_Acct` row, an `M_CostDetail` insert, an
    `M_Cost` update or delete, and re-pointing an allocation are all refused at COMMIT.
  - The 19 GL-journal checks from §14 still pass.

**Gate status.**

| Gate | Status |
|---|---|
| A–C | Pass |
| D | **Pass for all six catalogued classes**, on synthetic placement, mandate and CP |
| E | Open |

**Boundary.** This is synthetic-port acceptance, not certification.
- **Refused for a placed client until Shared catalogues them:** vendor disbursement (for example
  `erp.vendor-payment.disburse`), other purchase-side flows, bank statements, the cash journal, and inventory,
  production, landed-cost and cost-adjustment operations. This is intended.
- **Owner review needed:** the activity strings.
- **Still open:** real placements and mandates, IAM workload admission, the Python order-to-cash wiring, Gate E
  and production. The CP route stays nonproduction-only.

## 16. P0-04B-06 Payments money-movement authority — 15 October 2026

Owner decisions (14–15 October 2026):
- **Two layers, both must permit.** CP decides legal authority: the responsible legal entity, organisation,
  principal and mandates. Payments decides financial validity, approval, readiness and safety for the specific
  movement. Neither overrides a denial by the other (ADR-SHARED-035).
- **Governing rule:** every independently initiated, consequential money movement must have current,
  operation-specific legal authority from CP and independent financial execution approval from Payments.
  Accounting authorization, group ownership, historical approvals and absence of restrictions never imply
  permission to move funds.
- **Operation classes.** `payments.authorization.create`, `payments.capture.execute`, `payments.refund.execute`
  and `payments.authorization.void` are functional at P0. `payments.payout.execute` is catalogued and
  fail-closed. Treasury transfers are excluded; chargebacks are P1.
- **The proof is a protocol, not distributed atomicity.** Gate D1 (CI) runs the real Payments service, the real
  CP, real signed workload tokens, PostgreSQL 17 and a deployed HyperSwitch with a controlled test provider.
  Gate D2 (protected staging with a PSP sandbox) and Gate E (production) follow.
- **The D1 provider.** HyperSwitch's own, unmodified Stripe connector points at a Baobab deterministic test
  processor that never contacts live rails. It is confined to integration by an allowlist and a build check.
- **Protected environments stay closed.** Staging and production return 503 on every financial route.
  Activating them is a separate, approved change, never a flag.

| Set | PR | Merge | Content |
|---|---|---|---|
| Contract | Shared [#309](https://github.com/baobab-platform/shared/pull/309) | `2dc9a32` | ADR-SHARED-035; `payments/v2` request and decision with a Payments-computed instruction fingerprint over 11 canonical fields, a lease of 30 s or less, `provider_permissions_granted` always false; the closed catalogue with payout non-activatable; golden fingerprint vectors |
| CP resolver | CP [#400](https://github.com/baobab-platform/baobab-cp/pull/400) | `8c129a1` | `/internal/payments/v1/money-movement-authority/resolve` (flag, nonproduction, allowlist); payout refused before any context, binding or mandate read (the denial is still recorded before it is returned); the fingerprint recomputed and a mismatch denied; `baobab-payments` engine-instance binding; LA-04 resolution; immutable `registry.payments_money_movement_decision` (000126) recorded before disclosure |
| Payments PEP | Payments [#37](https://github.com/baobab-platform/baobab-payments/pull/37) | `5031659` | One enforcement point: CP decision and documentary veto, a reservation, a fresh decision at the execution point, one provider send, an immutable observation; unknown outcomes reconciled, never re-sent. Migration 0003 fences decisions, provider activation, financial limits, payout and the COMMIT lease, even against direct SQL as the runtime role |
| Gate D1 | Payments [#38](https://github.com/baobab-platform/baobab-payments/pull/38) | `0eb75ba` | Public routes through the PEP in development and integration only (activation policy; staging and production still 503); native intent journal (0004); harness and CI job `payments-gate-d1` |

**Gate D1 receipt:** final-head [CI run 38688158345](https://github.com/baobab-platform/baobab-payments/actions/runs/38688158345),
job `116267690853`, evidence artifact `11951105614`: **16/16 scenarios, `Gate D1 failures: 0`**. The run used the
release Payments image, the CP release binary at `8c129a1`, RS256 tokens from a test OIDC issuer, TLS on every hop,
PostgreSQL 17, and `hyperswitch-router` v1.127.0 (commit `25b7a9b`) driving its Stripe connector against the test
processor.
- **Lifecycle:**
  - manual authorisation, full capture and full refund, with one provider call and two CP decisions per movement;
  - one partial capture, with the remainder released and partial refunds up to the captured amount;
  - void, and a declined card.
- **Refusals, none reaching the provider:**
  - overcapture, double capture, capture after void, void after capture;
  - an over-limit refund;
  - a replay or a changed request under the same key;
  - a forged legal entity, platform account or tenant, a missing scope, an invalid token or the wrong audience.
- **Authority:**
  - CP revokes the mandate between the reservation's decision and the execution-point decision: denied, reserved
    but never dispatched, and the denial recorded by CP;
  - CP unavailable: 503 and retryable;
  - provider activation withdrawn: Payments refuses although CP authorised.
- **Recovery:**
  - **crash window:** Payments killed after the processor captured and before the outcome was recorded; after a
    restart the retry reconciles and captures once;
  - **lost response:** the capture stays unknown, is never re-sent and keeps holding the balance;
  - **dropped request:** proven absent, then re-sent once on a fresh CP decision.
- **Bypass:**
  - payout has no route;
  - as the runtime role, a payout reservation, a dispatch without a decision, a history rewrite and provider
    self-activation are all refused, and even the owner cannot rewrite history;
  - staging and production refuse to start with the native backend.

**Defects and findings Gate D1 surfaced:**
1. **Lost response in HyperSwitch.** After a lost capture response, HyperSwitch's Stripe connector retries
   without an idempotency key. The processor refuses the retry, and HyperSwitch records the payment as `failed`
   although it was captured. Payments had journalled such a refusal as `REJECTED`, which would release the
   balance. Fixed in #38: a refusal is journalled only when the provider's record proves no effect; otherwise the
   movement stays unknown and holds the balance. Gate D2 must show whether a real PSP sandbox heals this.
2. **Context lifetime.** CP's runtime context lives 15 minutes by default; Payments accepts at most 10. The
   harness sets `PLATFORM_CONTEXT_TTL=5m`. A deployment must set it, or the two bounds must be aligned.
3. **Card numbers in logs.** HyperSwitch's sample configuration logs at DEBUG, which writes card numbers to its
   console. The harness runs it at INFO, as a deployment must.
4. **Stale provider record.** After a connector timeout, HyperSwitch's record reads `processing` until it syncs.
   Reconciliation now reads with `force_sync`.

**Gate status.**

| Gate | Status |
|---|---|
| A–C | Pass |
| D1 | **Pass for authorisation, capture (full and one partial), void and refund**, on a synthetic tenant, mandate, IAM and provider |
| D2 | Open (protected staging, PSP sandbox, real IAM) |
| E | Open |
| Payout | Fail-closed by design |

**EA v2.2 classification.** Shared's [EA Implementation Plan v2.2](https://github.com/baobab-platform/shared/blob/main/docs/architecture/EA-implementation-plan-v2.2-ADR-SHARED-036-integration-reconciliation.md)
(accepted 15 October 2026 under ADR-SHARED-036, Shared [#310](https://github.com/baobab-platform/shared/pull/310)
and [#311](https://github.com/baobab-platform/shared/pull/311)) supersedes v2.1 for active execution. Under it:
- "D1" and "D2" are Payments-local gate names. D1 is engine-owned, real-component integration evidence with
  synthetic data for native HTTP money-movement enforcement. It is **not EA Gate D**: v2.2 states that no
  Gate D/E is satisfied by a real-component test or a deterministic PSP fixture. EA Gate D for Payments needs
  the relevant L4 profile and then protected staging acceptance.
- D1 is **not L4**. It ran in Payments' own CI, not on the Infrastructure-owned INT-01 runner. The Payments
  scenarios it proves (legal and financial veto, no bypass at COMMIT or dispatch, idempotency, crash and
  provider reconciliation) are inputs to FIN-04 and INT-08 once INT-01 exists. The receipt already names what
  an L4 receipt requires: candidate and dependency SHAs, Shared pins, the router image, the profile, the
  simulated provider, the scenario IDs and the evidence artifact.
- No score changes. EA-R03 remains PART: Payments D2, Subscriptions, Trade Docs and the unmapped ERP and Trade
  classes are open.

**Boundary.** Gate D1 is integration acceptance, not certification and not EA Gate D.
- **Synthetic seeds:** where CP has no admin API, rows were inserted directly and recorded as synthetic in the
  evidence: the human identities, the legal-entity verification, the tenant's market assignment and the
  `baobab-payments` engine binding. Tenant, market and mandate went through CP's admin API, with four-eyes
  approval and three distinct humans for the mandate.
- **Registry:** the workload registry used is a CI-only snapshot; the Shared registry keeps the Payments assessor
  PROVISIONED.
- **Not covered by D1:** signed webhook replay (no native public route; it stays covered by
  `verified-webhook-settlement`).
- **Still open:** Gate D2, Gate E, real IAM, real mandates, merchant certification, payout activation,
  chargebacks (P1) and production. The CP route remains nonproduction-only, and protected Payments routes still
  return 503.
