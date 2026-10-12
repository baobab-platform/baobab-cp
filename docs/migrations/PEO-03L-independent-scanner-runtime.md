# PEO-03L / Closure Package 1 — independently scanned document attestation

**Authority:** Accepted ADR-BCP-026 and ADR-BCP-027; extends PEO-03H/J/K.
**Status:** Code-level scanner and custody composition. This is NOT externally certified malware scanning or activated production evidence verification.

## Canonical admission follow-up

[P0-02A / PEO-03L2](PEO-03L2-scanner-admission-custody.md) adds current
case/claim/evidence admission before scan/seal, a bounded canonical lock lease,
same-transaction proof/audit registration and expiry recheck before commit.
Use the new case/claim selectors when composing `ScannedArtifactIngestor`.
Token verification, scanner activation and protected acceptance remain open.

## Implemented increment

```text
Dedicated scanner workload — ONLY baobab-independent-scanner-staging
  |
  |  operator-pinned local clamd Unix socket, INSTREAM protocol
  v
ClamdUnixScanner.ScanDocument (8 MiB max; deadline; exact CLEAN reply only)
  |        infected / error / unavailable / malformed -> DENY
  v
IndependentDocumentAttester.ScanAndAttest
  |  exact MIME, SHA-256, evidence_id, artifact_id, scanner_id/version
  |  Ed25519 signing key held ONLY by scanner process (mounted mode 0600)
  |  timestamp truncated to PostgreSQL microseconds before signing
  v
ScannedArtifactIngestor.Ingest
  |  scanner client/actor and current case/claim/evidence lease; no applicant/browser route
  |  seal artifact into AES-GCM immutable custody
  |  register signed proof via DocumentLedger (lock and independent public key)
  v
PostgreSQL evidence.scanned_artifact + sealed document custody
  |
  v
Existing PEO-03K VerifiedScannedArtifactReader
  -> existing DocumentExtractionWorker
  -> certified-only OCR transport
  -> AES-GCM candidate ledger; never VERIFIED outcome
```

## Security boundaries

- Only `verification:scan-attest` scoped, separately registered staging scanner workload may submit scan proofs. The existing document extractor has only `verification:document-extract`; the CIPC verifier has only `verification:source-capture`. Registration remains PROVISIONED until IAM and CP demonstrate issuer/audience/exact client and scope enforcement. Code-level `AuditActor` checks are NOT a substitute for token verification.
- The scanner is a separate process/runtime from CP and OCR. Signing keys are mounted exclusively into it, not a CP environment variable or GitHub secret. The processor and CP hold only Ed25519 public keys, indexed by scanner ID/version. A positive scan never verifies incorporation, a legal actor, tax standing or corporate control.
- The clamd transport uses a local Unix socket only, exactly bounded INSTREAM chunks, explicit termination, socket-type check and bounded deadline. An arbitrary URL, a document-supplied CLEAN flag, an infected verdict or a broken/malformed response is rejected.
- `ScannedArtifactIngestor` seals bytes before committing the proof. A failed DB registration can create an **unreferenced encrypted object** which must be reconciled and destroyed by approved lifecycle tooling; it cannot promote the EvidenceRecord to AVAILABLE. The existing PEO-03K ledger validates the signature, current case, evidence and digests again.
- An IAM-protected scanner-runner entrypoint and a protected signed-proof intake API must still be composed with canonical admission/context and transport guards before network operation. No such endpoint is asserted to exist in this change.

## Tests and no-false-certification rule

Unit tests exercise real Unix-socket protocol framing with a fake clamd server, successful Ed25519-signature verification, failing FOUND/ERROR/malformed and no responses, world-readable and symlink key rejection, wrong scoped client and sealed-object failure. PostgreSQL 17 integration runs the synthetic scanner -> signing -> vault -> ledger -> OCR candidate pipeline, preserves exact case/claim/evidence restrictions and rejects revoked scans. All fixtures remain **synthetic** and never qualify a real enterprise.

## Still outstanding before Package 1 can be called operationally closed

1. Deploy and independently assess real ClamAV (or approved malware provider), pinned signature update policy, quarantine and outage response, scanner host/container isolation, read-only signer mount, and AV version attestations.
2. Wire authenticated IAM workload token issuance, independently governed scanner intake route, client allowlist, exact context/evidence admission and proof of revocation/negative claims in protected staging.
3. [P0-02C](P0-02C-durable-custody.md) implements S3 Object Lock/KMS custody, approved retention/holds, immutable version readback, key rewrap and fenced orphan/destruction jobs. Supply actual approved resources and independently demonstrate retention, erasure, escrow, key denial, rotation and residency acceptance; local custody is integration-only.
4. Bind the operator-certified OCR model OAuth/token hook and profile, run protected PostgreSQL/AV/processor/IAM/CP E2E with real identities; record response redaction, recovery and signed audit.
5. Obtain external account configuration/provider approvals and live operational evidence. Do not mark CIPC/SARS, PSP or corporate status VERIFIED from this workflow.

**Sequence rule:** This is Package 1 of eight. Complete its non-cloud implementation and protected staging acceptance; do NOT declare Package 2 completed or evaluate EA v2.1 on its behalf.

## P0-02B follow-up

[Authenticated private intake and isolated executable composition](P0-02B-authenticated-scanner-runtime.md) now implements the network/runtime boundary. CP holds only public signing trust and rechecks current IAM/registry/trust before custody/commit; independent scanner jobs retain exact signed proofs for explicit replay. P0-02C supplies durable cloud custody code; protected live/cloud acceptance remains open.
