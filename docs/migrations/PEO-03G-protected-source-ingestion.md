# PEO-03G — protected issuer intake, SARS-assisted witnessing and local test sequence

The Control Plane has two separately gated entry points.

**Source ingestion:** `POST /internal/evidence/v1/source-observations/cipc`
requires a cryptographically verified IAM workload token with
`verification:source-capture`, an **ACTIVE** WorkloadRegistry entry granting
the same scope, a specifically configured client-ID allowlist, and an exact
server-side CIPC spec/cert. Unknown environments, absent dependencies and
production currently do not mount the route. Body allows only pre-existing
`verification_case_id`, `claim_id`, `evidence_id`, plus an Idempotency-Key
header. Callers NEVER choose `source_id`, outcome, legal identity, OAuth
endpoint or raw issuer response.

The server checks OEV-03 case/claim status, source, purpose, operating subject,
market and AVAILABLE evidence, uses the approved certified CIPC transport,
encrypts the response via local AES-GCM custody, then records an append-only
source observation. It returns a **non-authorising** receipt only. Retry of a
committed request is resolved by (authenticated actor,idempotency key),
and a different case/claim/evidence binding cannot replay that receipt.

**Assisted SARS witnessing:**
`POST /v2/admin/assisted-official-witness` uses a separately scoped
`verification:write` IAM human and platform administrator role.
`POST /v2/admin/assisted-official-witness/{intentID}/decision` requires
`verification:decide`, an independently authenticated DIFFERENT human and
a bounded reason-code. A durable PostgreSQL witness ledger preserves the
case/claim/source/evidence, official observation time, expiry, source proof
digest, witness and checker. It **never persists a TCS PIN**, uses
strict JSON allowlists and never automatically changes OEV-03 claim standing.
SARS TCS, VAT, employer registration and customs sources still remain
SUSPENDED; an ATTESTED record is **not VERIFIED tax compliance**.

Both are default OFF. Proposed configuration:

```text
BAOBAB_ENVIRONMENT=integration|staging
PEO_SOURCE_CAPTURE_ENABLED=true
PEO_SOURCE_CAPTURE_CLIENT_IDS=<registered IAM workload client ID>
CIPC_APPROVED_SPEC_FILE=<reviewed JSON in mounted config>
CIPC_APPROVED_CERT_FILE=<second-human approved spec-digest JSON>
CIPC_CLIENT_ID_FILE=<0600 OAuth client ID>
CIPC_CLIENT_SECRET_FILE=<0600 OAuth secret>
CIPC_SUBSCRIPTION_FILE=<0600 subscription secret>
CIPC_CUSTODY_KEY_FILE=<0600 raw 32-byte AES key>
CIPC_CUSTODY_DIR=<local encrypted volume directory>
PEO_ASSISTED_WITNESS_ENABLED=true
```

Never commit those secrets or expose them to application frontends.

**No-AWS acceptance**: run Go unit/test integration against the local
HTTPS synthetic issuer, isolated Postgres 17 `TEST_DATABASE_URL` and
fake/IAM-negative token tests. The real CIPC adapter remains disabled unless
an authentic issuer specification, allowed HTTPS origin, field mapping and
subscription have been approved. A real SARS eFiling/SOQS operator must
perform the authorised official verification; Baobab does not scrape or
automate the human portal.

**Additional production gates**: secret/key rotation, certified durable
KMS-backed custody, exact Shared contract pin sync, provider certification
publication, regular re-verification worker and durable retries, source
revocation propagation, cross-engine Regulations/ERP/Trade Docs PEP
conformance, protected IAM-authenticated AWS staging proof. None is
implicitly claimed by local synthetic tests.
