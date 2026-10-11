# PEO-03H — certified document extraction boundary

**Architecture:** ADR-BCP-023/026/027 and Shared evidence/v1. **Dependency:** CP #332/#333. This slice is an executable transport/validation component, not a production document processing service or a claim-verification decision.

## Implemented

- `internal/verification/authority/document_extraction.go` sends an already-scanned and `AVAILABLE` PDF, PNG or JPEG to an exact, operator-certified HTTPS extraction processor. Endpoint, origin, processor identity and version are bound to an immutable SHA-256 specification digest; distinct maker/checker and expiry are required.
- No browser URL or processor response is accepted as authority. The adapter enforces document signature and 8 MiB maximum, no redirects, bounded 10-second deadline, bearer injection at invocation, JSON response maximum 64 KiB, document SHA-256 echo and a six-field extraction allowlist.
- Responses remain `DOCUMENT_CANDIDATE_ONLY` even for high-confidence fields. No code mutates a `VerificationClaim`, `VerificationResult`, `LegalEntity`, taxpayer status or tenant.
- TLS fixture tests cover successful non-authorising extraction and quarantined evidence, invalid MIME and magic, expired/self approval, and specification tampering.

## Still required for end-to-end operation

1. Implement the artifact reader behind existing OEV-02 authenticated and malware-scanned evidence custody, and restrict this adapter to a background worker with a recorded `EvidenceID` and resolved source purpose. Do not accept artifact bytes directly from public commands.
2. Define and certify the selected document processing runtime/model and server-to-server token profile; deploy and protect extraction secrets under IAM/infrastructure.
3. Persist candidate field/provenance records under a separately governed extraction ledger with revision/idempotency, reviewer workflow, retention, redaction and audit; bind the final validated source proof to OEV-03.
4. The CIPC HTTP/OAuth lookup remains the authoritative independent verification; OCR never suffices to prove incorporation. Establish certified cloud object-store/KMS custody, source revocation and independent staging evidence before any production switch.

No real CIPC credentials, tax PINs or AWS account values are included. Do not enable a public extraction route.
