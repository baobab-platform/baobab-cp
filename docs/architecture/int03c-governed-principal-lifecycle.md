# INT-03C CP-owned canonical Principal deactivation (EA v2.3)

**Status: source implementation for controlled nonproduction qualification; live runtime acceptance not yet demonstrated.**

This feature is a narrowly scoped identity-lifecycle *operation*, not a
general Principal administration interface. The only eligible external
identity issuer is `https://hydra.int03.test`; the only subjects are the
two independently registered development-only INT-03C proof clients in
Shared. All protected routes are **absent** unless
`INT03C_CP_PRINCIPAL_DEACTIVATION_ENABLED=true` and CP's environment is
`development`, `test`, `integration` or `sandbox`. Staging,
production, missing configuration and unknown environments are refused.

**CP-owned, two-human provenance registration precedes deactivation.**
`POST /v1/admin/int03c/provenance` requires a currently authorised
ACTIVE human maker and the actual existing ACTIVE canonical workload
Principal, private Hydra issuer, exact allowed subject, 32-hex run nonce,
40-hex source revision and SHA-256 digest of original source evidence.
A separate ACTIVE human checker calls
`POST /v1/admin/int03c/provenance/{provenanceID}/approve` with the
unchanged evidence digest within 90 minutes. The database has immutable
provenance/approval tables and checks maker/checker SoD, current exact
external issuer linkage and both human lifecycle states.

Neither of these operations creates a Principal or its external link.
Until a real CP-approved, already linked Principal and independently
approved provenance exist, **every deactivation request is denied**.
The proposal and execution paths both revalidate the same exact
provenance record, including the run nonce and source revision.
The request digest also binds the maker, idempotency key, intended
DISABLED state and fixed approval deadline. These are essential
per-run authority boundaries, not a substitute for external immutable
artifact retention or actual runtime verification.

**Maker POST:**
`/v1/admin/int03c/principals/{principalID}/deactivation-requests`
requires a current CP platform administrator with `changeset:write`, a
canonical Principal UUID, the exact expected private issuer, named subject,
32-hex run ID, immutable 40-hex source revision, and a unique UUID
idempotency key. It returns a 15-minute digest-bound PENDING plan. The
database checks that this Principal is an existing ACTIVE workload with
the exact ACTIVE external (issuer,subject) link, and that the maker is an
ACTIVE canonical human. Replaying a request may retrieve only the
unchanged pending plan; no INSERT retry modifies its original fields or
extends the approval window.

**Independent checker POST:**
`/v1/admin/int03c/deactivation-requests/{requestID}/approve`
requires a different current ACTIVE CP human platform administrator with
`changeset:approve`, plus the exact immutable plan digest. The store
serializes the review transaction, rechecks the target ACTIVE status,
exact external issuer link, current independent checker and expiry,
atomically updates the Principal to DISABLED, disables all its active
external links and writes an append-only two-human audit record. A replay,
stale/foreign digest, second checker attempt or partially disabled link
fails closed. No path reactivates a Principal.

**Independent checker GET:**
`/v1/admin/int03c/principals/{principalID}`
requires `changeset:read` plus a current CP human platform
administrator, and returns the canonical persisted status of an eligible
proof Principal. This is only a source read-back of CP authority: live
introspection and context denial are separate acceptance proof.

No test harness may directly INSERT or UPDATE CP Principal SQL as a
substitute for this operation, and no claim of production/staging
readiness or actual seven-case PASS follows from Go source unit tests.

**Remaining deployment tests:** Run CP migrations against PostgreSQL;
perform actual human maker/checker authorization and transaction
concurrency tests; check issuer/subject and nonproduction denial,
stale digest and replay under Docker, positive CP Context before
deactivation, denial of valid unexpired IAM bearer after deactivation,
and independent current Principal read-back. An isolated JSON or mock
response does not count. Finally verify complete IAM, CP, runtime and
secret teardown; retain independent original-byte evidence.
