# PEO-03C — durable source observations and verified registration resolution

**Scope:** AWS-independent PostgreSQL 17 evidence custody increment.
This is not an official CIPC/SARS production integration or a tenant activation.

The immutable `evidence.authoritative_source_observation` table binds an
observation to an existing OEV-03 VerificationCase, EvidenceClaim and AVAILABLE
EvidenceRecord. The repository refuses cross-case, cross-Organisation,
cross-purpose and cross-source data, missing/expired evidence, inactive
claims, stale observations, reuse of an idempotency key with different
evidence and synthetic material without explicit nonproduction allowance.
Raw responses, SARS taxpayer PINs, bearer tokens and document bodies are
excluded from the table. The table's update/delete trigger protects source
provenance even from accidental direct mutation.

`RecordAuthoritativeSourceObservation` serializes idempotent writes,
commits audit metadata, and returns the canonical `aobs_...` identifier.
It does **not** record an OEV-03 VerificationResult or set a claim to VERIFIED.

`ResolveVerifiedRegistration` is the missing same-company authority link
for the PEO-03B CIPC adapter: it reads a current, independently VERIFIED
`REGISTRATION_IDENTIFIER` result from the **same verification case and
Organisation** and requires a positive CIPC check actually cited in that
result. It fails closed on expired, revoked, ambiguous or missing results,
and never takes a company number directly from the client for other claims.

The PG17 test creates a unique synthetic case, AVAILABLE source evidence,
immutable candidate, duplicate-idempotency replay, conflicting-source deny,
synthetic-in-real deny, an independently source-verified registration-result
fixture and revocation. Its dataset is artificial; the test requires an
isolated disposable database configured via `TEST_DATABASE_URL` because
source-observation rows are intentionally append-only.

## Next runtime work — PEO-03D/03E

- Add an authenticated, workload-scoped CP observation intake endpoint that
  ONLY receives proof returned by a registered, certified source connector.
  Do not expose a generic applicant-supplied `source_id` or untrusted
  `outcome=MATCHED` endpoint.
- Persist signed/secure source response artefacts in governed object storage
  and link to OEV-03 EvidenceRecord; quarantine/scanning, retention and
  POPIA data-minimisation rules apply. Do not store SARS TCS PINs.
- Authorised CIPC connector: configured approved OAuth 2.0 subscription,
  fixed official Companies API routes and validated response mapping, using
  recorded official fixtures. No route or OAuth token endpoint is invented.
- SARS TCS: official assisted eFiling/SOQS verification until SARS approves
  another integration channel, independent witness/decider, secure PIN custody.
  SARS VAT is separately certified; source entries remain SUSPENDED until
  source policy approval and exact Shared contract synchronization.
- Enforce source-observation ID on every source-backed positive OEV-03 check,
  keep the reviewer/decider independent, and run protected local multi-engine
  admission → onboarding → current restriction → Trade/ERP/Payments tests.
