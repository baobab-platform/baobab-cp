# Disposable Subscriptions current-state fixture

The separate `development-bootstrap` binary accepts `--subscriptions-phase`
only against an already bootstrapped, marked ADR-SHARED-043 database with a
current run lease. Supply the same required run/issuer/maker/checker arguments
as the original bootstrap. It accepts no resource ID, source SQL or provider
activation switch. Production images do not contain this command.

`prepare` creates one COMMERCIAL synthetic ProductSubscription/classification
for the CP-created Nabhold test tenant; its distinct external test tenant is
returned for tenant-denial probes. Repeating preparation returns the same IDs.
`read` observes the persisted tuple. `advance` moves revision 1 to 2; `suspend`
moves revision 2 to 3/SUSPENDED. Out-of-order/repeated transitions refuse.
Classifications are immutable. All state is destroyed with the owned database.

This fixture supports genuine IAM/CP reads and engine persistence/replay
exercises. It deliberately establishes no INTERNAL eligibility, verified legal
standing, payer/terms approval, capability entitlement, monetary provider or
production readiness. INTERNAL positive cases require their actual independent
CP prerequisites; an unverified synthetic company is not a continuing grant.
