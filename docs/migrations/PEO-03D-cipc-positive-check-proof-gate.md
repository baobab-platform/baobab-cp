# PEO-03D — bound positive CIPC verification checks

**Scope:** first official-source verification acceptance gate for
ADR-BCP-023/026, following PEO-03B/03C. This is a runtime protection for
new checks; it does not certify a live CIPC subscription.

The existing `RecordVerificationCheck` repository command used to accept
a positive official-source result so long as a reviewer supplied a known
source ID and source record reference. A reference string is not independent
proof of consulting CIPC.

For **positive CIPC checks**, the repository now requires a matching,
immutable `evidence.authoritative_source_observation` recorded in the same
case and for the same claim, CIPC source, method and official source reference.
Its EvidenceRecord must still be AVAILABLE, its observation unexpired,
and synthetic observations must belong to an explicitly permitted local
test environment. Ambiguous/missing source observations refuse the check.

The repository **derives** the check's evidence ID, observation time and
freshness deadline from the immutable ledger. Reviewers cannot lengthen
the freshness or substitute an unrelated evidence ID. The ordinary
OEV-03 reviewer self-verification, evidence-subject/purpose and
`RecordVerificationResult` maker/checker rules still apply.

The PostgreSQL integration test covers forged positive source references,
successful proof binding and no automatic VERIFIED result.

## Remaining work (not falsely considered complete)

- Protected IAM-authenticated, registered and certified adapter ingestion
  (a repository port is not itself a secure ingress surface);
- CIPC OAuth 2.0, actual authorised Companies API response mapping, rate
  limits, consent and source-response custody; SARS assisted operator
  workflows and approved portal access;
- Per-source policy publication, source revocation/expiry reconciliation,
  regulatory source records and cross-engine evidence references;
- Trade/ERP/Payments point-of-use consumer certification and protected
  synthetic E2E staging proofs, pending AWS and external provider approvals;
- Broader issuer activation beyond CIPC, with independent claim-specific
  approvals rather than interpreting `SUSPENDED` as a usable source.

This gate does not give Pulse authority to establish a legal fact, or
Trade Docs authority to determine legal sufficiency: ADR-SHARED-019/022/028.
