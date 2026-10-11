# PEO-03F — certified official-source adapter runtime and secure custody

**Authority:** ADR-BCP-023/026; Shared evidence/v1; ADR-SHARED-019/022/028.

## Executable implementation

`internal/verification/authority` introduces a typed CIPC OAuth 2.0
client-credentials/HTTPS transport whose issuer origin, **both** endpoints,
subscription header, response-field mapping and specification version must
all be supplied by an independently reviewed official contract. Nothing is
populated by an applicant or inferred from a public marketing portal.

A frozen SHA-256 specification digest requires distinct human proposer and
approver, an effective expiry and recorded approval provenance. The client
refuses uncertified configurations, changed official origin, redirects,
plaintext HTTP, oversized responses, wrong company IDs and absent secrets.
OAuth credentials are resolved via a mounted-secret/provider interface at
request time. No tokens, taxpayer PINs or raw provider bodies enter audit
logs, error messages or trust decisions.

The proof is an observation with an exact response digest and source
transaction reference, **not** a `VERIFIED` EvidenceClaim. CP's independent
OEV-03 verification result and source-proof check remain mandatory.

`SealedVault` provides a local/integration AES-256-GCM custody adapter with
per-record random nonces, evidence-ID authenticated associated data,
restricted 0700 directory / 0600 files, atomic O_EXCL persistence,
authenticated re-open and ciphertext integrity tests. This is suitable for
isolated local integration. Before production, select a governed durable
storage implementation with encryption-key rotation, backup/recovery,
retention/erasure, malware scanning, POPIA classification, tenant access
control, access audit and S3/KMS integration **without widening the
canonical evidence contract**.

SARS TCS/eFiling/SOQS remains an **assisted** workflow: a taxpayer grants
an authorised reviewer access, the reviewer performs the actual official
lookup and records a sanitized evidence reference; a distinct reviewer
attests the observation. Never automate third-party logins, scrape
CAPTCHA, store PINs or infer a general commercial SARS machine API.
The existing SARS sources remain suspended until lawful access
and separate governed source certification are approved.

Reverification plans must use exact observation/case/claim/source identity,
expire past findings, and deny suspended/revoked/uncertified sources.
Reverification never refreshes a legal permission by replaying stale proof.

## Next integration increment

Wire an IAM-authenticated and **client-ID allowlisted** workload-only
internal endpoint that triggers server-side approved adapter lookup,
seals source evidence, validates the existing OEV case/claim/evidence,
and records an immutable observation. Human-assisted SARS witness/decision
routes require independent human role/scope and a durable append-only
two-person ledger, never a browser-provided `VERIFIED` result.

Local TLS fake issuer + PostgreSQL 17 + IAM invalid-token/client
tests must pass before calling the package integration-ready.

## External activation gates

CIPC portal: https://developer.cipc.co.za/ .
Public documentation confirms OAuth 2.0 and a read-only Companies API;
a real endpoint, subscription entitlement, field mapping and approved
example response have not been supplied and are **not assumed**.

SARS TCS guidance: https://www.sars.gov.za/individuals/manage-your-tax-compliance-status/how-to-verify-tax-compliance-status/
and https://www.sars.gov.za/guide-to-the-tax-compliance-status-functionality-on-efiling/ .
This implementation makes **no claim of official status verification**.
