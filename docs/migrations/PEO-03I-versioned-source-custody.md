# PEO-03I — Versioned source-response custody and re-verification

**Decision authority:** ADR-BCP-023/026, Shared evidence/v1, OEV-03 source observation provenance.

## Corrected invariant

A single EvidenceRecord is not a single SourceObservation. Re-verification may legitimately return a changed official record. Using the EvidenceRecord ID as the sole immutable AES-GCM vault object key makes the next response unrecoverable without overwriting historical bytes; overwriting would destroy evidence.

The `SealVersioned` custody adapter derives a distinct immutable, opaque `sealed-v2:` reference from the EvidenceRecord and a server-derived SHA-256 attempt binding (case, claim, evidence, independently authenticated workload principal/client, Idempotency-Key). Each attempt is encrypted with independent AES-GCM nonce/associated data; the prior sealed object is immutable. Same-attempt identical replay is safe; same-attempt changed plaintext is refused. A separate authorized observation records the resulting reference. `OpenVersioned` is internal-only and MUST be preceded by CP access controls that bind the reference to the current evidence/verification case.

Tests exercise two different official response versions against one EvidenceRecord, replay, failed overwrite, exact recovery and malformed reference rejection.

## Limits

This fixes the local/integration semantic bug, **not production custody**. Before production, a governed S3/KMS-backed implementation must add retention and erasure, strict IAM key conditions, encryption-key versions and rotation, tenant/subject ACLs, access logging, malware scanning, backups/recovery, and tamper-evident immutable reference/record linkage. Obtain protected staging proof; refuse provider certification/ACTIVE promotion otherwise. Orphaned sealed files after a DB commit failure require an audited reconciliation/garbage-collection process; do not erase them on retry.

The registered issuer source is not itself a positive verification result; reviewer checks and independent standing remain compulsory.
