# CX-05E — independently governed dataset exports and bulk changes

Implemented as **domain validation/formatting ports**, not deployable API routes:

- Shared pin feb5eedee8942d122f1ab1f494f77645f98d4e41; `organisation.export`, `organisation.bulk.propose`, and `organisation.bulk.approve` require exact organisation scope and are never inherited from parent/group or an ordinary view grant.
- Immutable plan digest binds maker, recipient reference, purpose, versioned metadata values, exact target IDs, idempotency key, redacted source fingerprint and a maximum 24h TTL.
- Maker/checker separation, independent per-target permissions and pre-reserved durable audit mandatory; deny if a source, grant, field, revision or decision changes.
- Redacted-only CSV fields, 1,000-row/2 MB output budgets, CSV formula escaping, no evidence or registration secrets.
- Bulk restricted to allowlisted internal metadata labels: at most 50 targets and optimistic source versions. Execution is a single required transaction port, not a best-effort loop.
- Go security regressions prove denials on tampering, stale source, missing rights, self approval, absent audit reservation, missing transaction port and expired plan.

**Not enabled:** No HTTP handler, batch transaction adapter, independent durable approval/audit ledger, credentials, download link, or live protected staging acceptance is claimed here. Control Plane must implement and test these before production. Frontend remains read-only.
