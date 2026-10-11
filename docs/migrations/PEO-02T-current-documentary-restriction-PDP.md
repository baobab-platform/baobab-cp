# PEO-02T — Current negative-only documentary remediation assessment

This is the next ADR-BCP-026 PEO-02S increment. It exposes **no positive authority**.
A query returning `blocked=false` establishes only that the CP restriction
ledger has no ACTIVE negative scope for the queried Organisation, capability and
market. It is **never** a licence, validated legal actor, merchant certification,
verified evidence, billing entitlement or permission to execute a transaction.

## Nonproduction configuration and access

The private `POST /internal/documentary/v1/restrictions/assess` endpoint is absent
unless ALL the following are true:

- `PEO_DOCUMENTARY_SCOPE_PDP_ENABLED=true`
- `BAOBAB_ENVIRONMENT` is explicitly one of development/test/integration/sandbox/staging
- `PEO_DOCUMENTARY_SCOPE_CONSUMERS` lists explicit, pre-reviewed workload client IDs
  (comma-separated; do not use a wildcard)
- CP has the registered workload-identity registry, identity/context repositories
  and live PostgreSQL restriction reader configured.

The caller needs a validated IAM workload token with the **separate**
`documentary:restriction-assess` scope, active client registration with that
scope, exact allowlisted client ID, and a canonical current runtime
`PlatformContext` owned by the calling workload. An expired, cross-market,
unowned, provisioning or non-active tenant context fails closed. Context's
PRIMARY Organisation must equal the tenant's live PRIMARY Organisation.

Example *synthetic-only* request:

```json
{
  "context_id": "019a0300-0000-7000-8000-000000000001",
  "capability": "trade.checkout",
  "market": "ZA",
  "operation_reference": "synthetic/order-123"
}
```

Response includes exact organisation, context, scope and operation reference,
`blocked`, `authority_granted:false`, policy reference, evaluation and
short (<= 2 seconds) expiry. Both allow and deny results are `no-store`.
Database, identity or context failures produce a denial or 503, never an
optimistic `blocked=false`.

## Consumer acceptance boundary

Trade, ERP, Payments and Trade Docs MUST independently call this endpoint
at their irreversible point of use **and** separately verify the legal actor,
provider certification, entitlements and any other capability permissions.
Consumers must deny missing/stale responses, mismatched context/scope,
`blocked=true`, expiry, or transport errors. The response is a *veto* only.

The endpoint itself does not certify the consumers or turn on provider
write paths. Enabling it requires IAM scope registration, private service
routing and live authenticated synthetic staging tests. No external
Regulations evidence is implied.
