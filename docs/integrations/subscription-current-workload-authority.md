# CP ProductSubscription current workload authority (ADR-SHARED-044)

**Version:** `contracts/product/v1/current-authority.schema.json` in Shared commit `4cd53d2aaaa0f0cd46b08067ecc86b81cba2efe5`.

## Controlled nonproduction configuration

To mount routes only in development, test, integration, sandbox or staging:

```text
CP_SUBSCRIPTION_CURRENT_AUTHORITY_ENABLED=true
CP_SUBSCRIPTIONS_CURRENT_WORKLOAD_CLIENT_ID=baobab-subscriptions-workload
```

The existing CP `WORKLOAD_REGISTRY_FILE` must be an approved current registry that names that workload as ACTIVE and assigns **both** `subscription:current-workload-read` and `subscription:commercial-authority` under CP audience. IAM issuer/JWKS and current-status verification must remain enabled. Static client allowlist alone is not sufficient; the request-time registry guard also denies unregistered/revoked clients. Routes are not mounted in production or without a configured live reader.

## Endpoints and semantics

`GET /internal/subscriptions/v1/tenants/{tenantID}/product-subscriptions/{subscriptionID}/current` returns CP's tenant-fenced subscription ID, product ID, status, monotonic authoritative revision and current immutable classification ID/type/source/reference, together with the count-validated single live PRIMARY Organisation association. This is a read model; no payer, product plan, legal mandate or Kill Bill mapping is invented. Classification and revision are read from a **single PostgreSQL SELECT with immutable classification join**. Primary Organisation is independently checked and returned null when there is not exactly one ACTIVE PRIMARY.

`POST .../commercial-authority` accepts only `tenant_id`, `product_subscription_id`, `authoritative_revision`, `operation_id`, `operation_kind=ENSURE_SUBSCRIPTION` and `provider_kind=KILL_BILL`. It re-fetches current CP data and returns `outcome=DENIED` with an explicit reason. Even an ACTIVE COMMERCIAL subscription with matching revision and current Organisation receives `COMMERCIAL_TERMS_NOT_ATTESTED`, because CP alone does not own billing-account payer consent, tax, catalog/plan price, entity mandates, payments or native provider execution. No path through this v1 endpoint can authorize actual financial execution.

Both responses carry no-store, `evaluated_at`, `expires_at` (three seconds) and a fixed Shared policy reference. Wrong tenant returns a generic 404, absent snapshot returns 503, invalid context returns 400, wrong identity or current registry scope returns 403. No HTTP caching or durable positive permissions are allowed.

## Review and qualification

CP unit tests cover denied human/wrong client/revoked scope, stale revision, spoofed tenant, unclassified subscription and absence of primary organisation. Full CP ↔ IAM ↔ Subscriptions integrated acceptance is still required. In particular the current IAM workload registry must be amended/governed for the client and new scopes before a protected environment can use these endpoints. `ADR-SHARED-044` v1 is a preflight and workload read, **not** a commercial authorization grant.

**Follow-up:** accept an explicitly versioned positive commercial decision contract only after CP legal mandate, external payer, versioned pricing, Payments and Kill Bill provider resources can all be independently certified.
