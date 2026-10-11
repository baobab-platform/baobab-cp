# PEO-03B — authoritative source adapter boundary (ZA-first)

**Status:** implementation candidate, synthetic-only acceptance. This is the first
runtime adapter seam, not proof of a live CIPC or SARS connection.

## Authority and responsibility

- Shared `evidence/v1` is canonical (ADR-BCP-023); CP is the owner of the
  evidence and verification-case ledger. The existing `SourceByID`,
  `ValidateCheck`, `RecordVerificationCheck` and
  `RecordVerificationResult` remain authoritative: **an observation is NOT a
  claim's VERIFIED result.**
- CIPC Companies API is publicly documented at https://developer.cipc.co.za/
  but no operational subscription, OAuth client, fixed official route schema or
  approved field mapping is supplied. `CIPCCompaniesAdapter` therefore
  receives a typed `CompanyRegistry` from the composition root rather than
  making invented endpoint calls.
- `CIPCCompaniesAdapter.LookupCIPC` supports `LEGAL_NAME`,
  `REGISTRATION_IDENTIFIER` and `ENTITY_STATUS` only. It binds official
  registration number, source reference, provenance, SHA-256 evidence digest,
  current observation and expiry to the original case, claim, purpose and
  organisation. It refuses mismatches, outages, unapproved adapters and
  synthetic observations outside isolated testing.
- SARS TCS and VAT are different sources and claims. SARS TCS needs an
  **authorised** taxpayer PIN and an authorised verifier using SARS eFiling
  or SOQS; the PIN itself must never become application evidence, telemetry,
  URL or an observation value. An independently witnessed official-portal
  observation can be recorded without automating a portal login. SARS source
  entries remain SUSPENDED in Shared until independently activated and CP
  contract-pinned.
- A human reviewer/decider must still record OEV-03 VerificationCheck and
  VerificationResult with normal permissions, source trust and maker/checker
  controls. No default platform admission, restriction release or provider
  permission is granted by this package.

## Local integration sequence (no AWS)

1. Run tests under `go test ./internal/verification` with a synthetic,
   explicit `AllowSynthetic=true` CIPC registry. No actual official endpoint
   is contacted.
2. Validate the check candidate through the existing OEV-03 evidence lifecycle.
   It cannot self-verify an applicant claim or borrow another case, subject,
   purpose, market or source.
3. Run the independent existing evidence verification result/decision tests.
4. Re-test when Shared PEO-03A is fully pinned in CP. The separate SARS source
   IDs are reserved but must not be accepted as active automatically.

## Next bounded increments (still required)

- **PEO-03C:** immutable source-observation persistence linked to source
  responses (encrypted artefacts), claim/case/version and operation IDs;
  idempotency and replay auditing. A CP human/workload authenticated intake
  API must *not* permit applicants to forge source checks. Freeze raw evidence
  digests and source adapter versions, prohibit raw TCS PIN fields, and
  quarantine incoming content pending malware scanning.
- **PEO-03D:** CIPC OAuth 2.0 transport and Companies API response mapper
  against CIPC-issued reference examples and an approved subscription;
  SARS eFiling TCS-assisted two-person workflow with authorised operator
  evidence custody; VAT-assisted lookup separately; genuine provider
  credentials only via runtime secret injection.
- **PEO-03E:** production-safe governed source certification, regular
  re-verification, source revocation, conflict reviews, expiry and outbox
  event propagation; append-only facts and negative holds retained.
- **PEO-03F:** fully authenticated Docker/PG17/IAM test stack spanning
  applicant -> evidence -> official/synthetic verifier -> reviewer -> policy
  -> restrictions -> Trade/ERP/Payments PEP; include cross-tenant, cross-market,
  expiry, replay, outage and withdrawal negatives. AWS role/account IDs only
  for cloud deployment and protected staging acceptance.

No official company registration, taxpayer compliance or VAT status is
asserted from the unit fixtures.
