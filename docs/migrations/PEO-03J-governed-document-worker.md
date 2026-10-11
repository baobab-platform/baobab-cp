# PEO-03J — governed scanned-document extraction worker (ADR-BCP-023/026/027)

This increment adds a private `DocumentExtractionWorker` to `internal/service/verification`. It is a **fail-closed integration seam** for the certified extraction transport merged through #334; it is **not** a public HTTP route, a deployed OCR service, or government-source verification.

## Runtime sequence and authority

```text
IAM-verified, dedicated extractor workload (future deployment)
   |
   v
DocumentExtractionWorker.Extract(case, claim, evidence, idempotency)
   |-- verifies exact allowed client_id and workload actor
   |-- reads current VERIFYING case and UNDER_VERIFICATION claim
   |-- binds claim->EvidenceID and exact subject + purpose
   |-- reads current AVAILABLE Document EvidenceRecord, unexpired and with ArtifactID
   |-- calls approved OEV-02 custody reader, never accepts browser document bytes
   |-- requires CLEAN scan outcome + attestation + scanned timestamp
   |-- verifies EvidenceID + ArtifactID + SHA-256 of returned bytes
   |-- calls PEO-03H certified processor under deadline
   |-- requires DOCUMENT_CANDIDATE_ONLY standing and identical document digest
   |-- records encrypted candidate attempt using a transactional, immutable ledger
   v
Candidate material for an independent reviewer (NOT OEV-03 VERIFIED result)
```

The worker cannot start without **all** injected ports. It does not manufacture a `ScannedDocumentReader` or a ledger from applicant input. The custody port must use separately authenticated, independently attested malware scan results: a string saying CLEAN by itself is not independent malware protection. The ledger port must atomically recheck the current case, claim, evidence and scan state, deduplicate `(actor, idempotency_key)`, encrypt candidate values at rest under governed keys, and avoid exposing candidate plaintext in telemetry, events or metadata APIs. There is no route that can call this worker without these controls.

## Covered tests

Synthetic tests exercise valid non-authorising extraction; replay without re-reading content or calling OCR; replay with a different case; wrong IAM client/human actor; claim/evidence/subject mismatch; missing artifact; RESTRICTED evidence; quarantined or unaudited scan; artifact reference mismatch; changed bytes vs SHA-256; and processor attempting to return VERIFIED standing or a forged digest.

## Hard deployment dependencies

1. Bind a dedicated registered **staging**, then production, extractor workload and minimal workload-only scope through Shared and IAM. Never reuse the CIPC source-capture identity or privilege for unrelated document extraction.
2. Implement an OEV-02 authenticated `ScannedDocumentReader` backed by managed malware-scanned object custody, with evidence/subject/purpose ACLs and signed provenance of the clean scan. Do not treat `Evidence.Status=AVAILABLE` alone as proof that an artifact was scanned.
3. Implement `DocumentCandidateRepository` against a durable, encrypted, append-only attempt ledger. Hold case/claim/evidence locks at commit and enforce token identity / idempotency, retention, erasure and audit. Extracted text must not be inserted into public evidence metadata.
4. Deploy an independently certified processor and secrets through infrastructure, then run full e2e positive/negative tests in protected staging. Do not promote synthetic fixtures or credentials to production.

No AWS account, KMS key, CIPC subscription, SARS credentials or live official provider authority is assumed. Extraction is merely supporting evidence. The only authoritative CIPC source check remains the separately certified CIPC transport and OEV-03 reviewer decision.
