# DEV-07 CP-owned buyer identity fixture

Requires the Shared #405 development registrations and the existing guarded
`development-bootstrap` baseline in a fresh marked, leased disposable store.
The production CP image does not include this command. No HTTP approval bypass,
provider activation, buyer membership, purchasing scope or legal verification is
added. All Trade changes remain in Trade #176.

After ordinary bootstrap, invoke the same private command with
`--buyer-identity-phase prepare`, supplying the exact original run, issuer,
maker/checker subjects and fixed lease. CP checks the actual store marker,
current bootstrap lease, distinct ACTIVE canonical maker/checker links, native
ZuriBeans estate/tenant and actual Trade instance. It owns creation of the
validator Principal, its single leased tenant assignment, and an ACTIVE BFF
binding bounded by the bootstrap deadline. The binding carries both synthetic
maker/checker provenance; this is ADR-SHARED-043 development provenance, not a
claim of protected independent-human approval.

`prepare` is idempotent. `read` checks the complete current pairing without
adopting drift. `suspend`, `restore` and `revoke` operate only on this run's
exact binding, incrementing its database-guarded revision. REVOKED is terminal.
Unknown phase, wrong issuer/subject, missing topology, changed state, expired
lease, unmarked store and any protected environment refuse. The fixture never
creates a substitute estate ID or marks an instance/provider ready. Existing
prefixed binding IDs remain compatible alongside native CP UUID references.

Infrastructure must register real IAM clients from IAM's owner builder, enable
the existing buyer-resolution/estate-binding feature flags only in the disposable
runtime, obtain actual tokens, exercise Trade #176 and independently witness
teardown. A prepared CP fixture or passing unit test does not qualify INT-05.
The fixture readback is private run state; only the sanitized Trade/Infrastructure
receipts belong in uploaded evidence.

PostgreSQL 17 CI exercises prepare/replay, exact topology IDs, wrong subject,
protected/expired refusal, suspension/restoration and terminal revocation inside
the existing isolated development-store integration test. Local Go unit/build
checks skip database integration if TEST_DATABASE_URL is absent; do not call that
an observed native database run. Restore reviewed source pins to roll back; do
not weaken binding or lease checks.
