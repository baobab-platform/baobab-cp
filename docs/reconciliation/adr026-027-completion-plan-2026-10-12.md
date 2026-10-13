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
`d42c8e6c8be1b6287b76f8152e67021576e54629`: exact Shared pin `a9c53087`, explicit legal-actor,
documentary and merchant-readiness consumption, byte-identical generated schema
closure/provenance and runtime wire validation before scope, actor, provider and
freshness checks. README, ADR follow-up and checkout maintainer notes were
updated. See the [Trade implementation/audit plan](https://github.com/baobab-platform/baobab-trade/blob/main/docs/plans/P0-04A-trade-contract-convergence.md).

Trade #140 (pg) and #143 (ajv-formats) were reviewed and merged with final-head
checks green before implementation. Remaining dependency PRs require conflict
refresh, failed-gate repair or a separately reviewed Node 26 upgrade. No waiver
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
