# P0-04B-02 authenticated Trade binding intake

Shared #300 registers closed scope/digest admission and four human CP scopes,
with exact nondelegated HIGH `organisation.trade-binding.*` permissions. They
are assigned to no client or profile. CP mounts the fixed
`/internal/trade-binding/v1/{propose,approve,activate,revoke}/authorize` routes
only with `TRADE_BINDING_INTAKE_ENABLED=true` in a recognised nonproduction
profile. Production/unknown and default-off profiles return no route.

Every call cryptographically verifies the actual human IAM token at CP audience,
resolves its existing ACTIVE canonical human by issuer+subject, reads current
grants, and requires a DIRECT exact Organisation grant and current HIGH assurance.
There is no role fallback, delegation or implicit tenant/sibling scope. The
response caps expiry at five seconds, token expiry and matching grant validity.
It echoes the exact native scope and deterministic command digest, supplies the
canonical human and token provenance, and always denies provider permissions.

Trade reads this response directly and never redeems browser-provided receipts.
It retains the response in each atomic local decision/outbox receipt and rechecks
human authority after native/context/legal I/O. This is an admission decision,
not verification of a documentary evidence reference. Existing CP workload
context resolution and legal actor assessment independently check ACTIVE admitted
workload identity, current tenant/Organisation/market, bounded RUNTIME purpose,
context ownership and current mandate. Revocation does not require the old
mandate, tenant or context to remain active.

No real corporate record, mandate, workload activation, human client assignment,
merchant readiness, canonical event publication or production enablement is
included. Follow-on P0-04B protected native checkout acceptance remains open.
