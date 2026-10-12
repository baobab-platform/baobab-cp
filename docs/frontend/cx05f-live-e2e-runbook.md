# CX-05F — protected live acceptance (not synthetic certification)

The workflow `CX-05 Protected Live E2E Acceptance` is a manually dispatched,
`environment: staging`-protected workflow, not an automatic Go/Console CI test.
It makes **real** HTTPS API requests to a CP deployment authenticated with
three separate human IAM credentials: currently authorised, separately
unauthorised, and independently revoked.

Required protected secrets:
`CX05_STAGING_CP_URL`, `CX05_STAGING_GRANTED_TOKEN`,
`CX05_STAGING_DENIED_TOKEN`, `CX05_STAGING_REVOKED_TOKEN`,
`CX05_STAGING_OWN_ORG_UUID` and `CX05_STAGING_FOREIGN_ORG_UUID`.
The permitted human principal must have **at least 5001 current exact organisation grants**
and matching authoritative registry profiles. The foreign organisation must
be outside that principal's grants. A revoked token must have previously
been valid and must now be revoked. Do not substitute fake identities or
sample UUIDs: the workflow intentionally fails.

Checks: >500 profile visibility with non-duplicating keyset paging and
bounded count, cross-organisation detail denial, another principal cannot
enumerate, revoked credentials deny, untrusted identity selector rejection,
foreign cursor rejection, oversized page rejection. No mutation or fabricated
fallback. Workflow refuses local/HTTP endpoints, redirects, duplicate
principal credentials, missing source, and unavailable CP service.

The archived report includes only check names/statuses, numbers of visible
rows, git revision and SHA256 digest. It contains no names, resource IDs,
access tokens, bearer claims or response bodies. PASS is a test result,
**not production eligibility**. Independent security/operations approval,
live negative revocation latency, AWS/provider certification and market
evidence are separate release gates.

For exports and bulk commands, add further protected scenarios **after**
the exact Shared-authorised implementations exist and are provisioned.
A passing read-only E2E proof must not be presented as proof of export/bulk
SoD or settlement.
