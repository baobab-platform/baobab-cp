# P0-02A / PEO-03L2 — canonical scanner admission before custody

Authority: Accepted ADR-BCP-023/026/027 and ADR-IAM-0033. Extends existing
PEO-03L; this label is an implementation increment, not a canonical API or
capability registration. No schema migration or public route is added.

## Change and reason

Previously the private scanner pipeline scanned/sealed bytes before the
PostgreSQL registrar checked only evidence metadata. It did not bind that
custody write to a current verifying case, claim, subject or purpose. A
scanner identity and signed CLEAN proof were insufficient admission controls.

`ScannedArtifactIngestor.Ingest` now requires case and claim IDs and enters
`DocumentLedger.WithScanAdmission` before any antivirus/custody call. The
ledger locks canonical case → claim → evidence, requires a VERIFYING case,
UNDER_VERIFICATION claim, linked DOCUMENT evidence, matching subject/purpose
and exact artifact, independent non-asserter scanner, and AVAILABLE or
QUARANTINED non-expired evidence. Restriction/deletion/expiry denies admission.

The 15-second callback receives a package-private transaction lease bound to
repository, actor/client and case/claim/evidence/artifact. Registration verifies
that lease, the independent signature and current admission; it writes the
proof and audit using the SAME transaction. Direct registration and retained
callback contexts fail closed. Exact proof replay requires current admission
again. Expiry is rechecked before commit, since row locks cannot stop time.

A scan does not make quarantined evidence AVAILABLE, establish a verification
result, approve a mandate or activate a workload. Admission checks are domain
controls; real IAM token verification and active governed scanner profiles
remain prerequisites to any future network composition.

## Maintenance and failure semantics

- Only the callback transaction may register a proof; opening a second ledger
  transaction inside it would deadlock against the canonical locks.
- Callback work is synchronous and bounded. Do not retain/use the callback
  context after return or publish a signed proof before commit succeeds.
- Scan, custody and database errors return generic denial. No document text,
  scanner private key or raw provider credentials enter logs or audit metadata.
- Proof and audit commit atomically. Storage is an external side effect:
  failed registration/commit can leave an unreferenced encrypted object.
  Reconciliation/destruction remains an independently governed operational
  requirement, not an automatic success or object-status promotion.
- Scanner signer stays in the isolated scanner process. CP/extractor use only
  public trust anchors; this increment does not wire a network intake endpoint.

## Verification and remaining acceptance

The unit pipeline proves admission denial causes zero scan/custody/register
calls and retains wrong-client, infected, unavailable and seal-failure negatives.
The existing PG17 scanner → vault → ledger → OCR candidate test now also covers
foreign subject/purpose, unlinked evidence, case/claim state, independent actor,
artifact mismatch, restriction/destruction/expiry, direct registration denial,
exact replay, retained-context denial, concurrent restriction lock exclusion and
expiry crossing before commit. Fixture company facts and keys are synthetic.

Use `TEST_DATABASE_URL` on a disposable PostgreSQL 17 database and the exact
`SHARED_CONTRACTS_DIR`; run `go test -race ./...`, `go vet ./...` and the unchanged
resolution readiness script. Local missing DB/socket support is not a test pass;
required hosted CI must demonstrate those integrations before merge.

Still outstanding: actual IAM-issued active scanner credentials/current revocation,
real AV signature/update policy, cloud deployment/retention/restore acceptance,
approved OCR and official-source certification,
protected acceptance and independent signoff. See the
[complete sequenced plan](../reconciliation/adr026-027-completion-plan-2026-10-12.md).

## P0-02B follow-up

[Authenticated private intake and isolated executable composition](P0-02B-authenticated-scanner-runtime.md) now implements the network/runtime boundary. CP holds only public signing trust and rechecks current IAM/registry/trust before custody/commit; independent scanner jobs retain exact signed proofs for explicit replay. P0-02C supplies durable cloud custody code; protected live/cloud acceptance remains open.

P0-02B implements authenticated private intake and isolated scanner composition.
[P0-02C](P0-02C-durable-custody.md) adds the shared artifact fence, durable intent/
version attachment, retention/holds, rewrap and approved orphan/destruction jobs.
Local custody remains explicit integration-only; protected acceptance is separate.
