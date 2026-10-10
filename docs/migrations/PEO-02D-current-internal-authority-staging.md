# PEO-02D — Current INTERNAL authority provider-to-consumer acceptance

**Status:** Implementation branch; controlled staging only. Consumer: baobab-subscriptions.
Contract dependency: Shared PR #273; CP PR #318; Subscriptions PR #35.
Founding lifecycle dependencies: CP #312 → #315 → #316, Shared #272, Subscriptions #34.

## Invocation and authority

`GET /internal/subscriptions/v1/tenants/{tenantID}/product-subscriptions/{subscriptionID}/internal-authority?classification_reference={reference}`

The caller must possess a valid workload token **for the Control Plane audience**, registered and currently active in CP's workload registry, scoped to `subscription:internal-authority`, with `client_id` matching the configured baobab-subscriptions workload client. A token for baobab-subscriptions' **own** API audience is not interchangeable. The endpoint is disabled unless both the environment and the explicit feature configuration permit it.

CP returns an uncached current assessment bound to the exact tenant, CP ProductSubscription and *current* classification reference. It re-evaluates INTERNAL eligibility using authoritative relationships and sponsorship evidence, verifies the live tenant PRIMARY Organisation is unchanged, and denies an old subscription reference after reclassification or re-pointing. The three-second expiry is informational; consumers must query on each write and never assume an event or a previous positive response is lasting authority. The result is **not** a new subscription classification, onboarding approval, billing discount grant or transferable capability.

## Deployment controls

| Component | Required configuration | Enforcement |
| --- | --- | --- |
| Control Plane | `BAOBAB_ENVIRONMENT=staging`, `PEO_INTERNAL_AUTHORITY_ENABLED=true`, `PEO_SUBSCRIPTIONS_WORKLOAD_CLIENT_ID` | Environment allow-list and fail-closed named workload |
| IAM / workload registry | CP-audience token with `subscription:internal-authority`; separately approved workload registry lifecycle and scope grant | Valid signature, issuer, audience, expiry, client ID and scope |
| Subscriptions | `BAOBAB_ENVIRONMENT=staging`, `PEO_INTERNAL_AUTHORITY_ENABLED=true`, `PEO_CP_INTERNAL_AUTHORITY_BASE_URL`, `PEO_CP_INTERNAL_AUTHORITY_TOKEN_FILE` | Absolute token-file path supplied by IAM sidecar; HTTPS CP URL, no redirects, three-second deadline |
| Shared | Review and merge #273, then synchronize consumer lock and referenced schema | Exact governance contract before a broader deployment |

Do not use a static bearer token, application log, browser/session token, IAM provider-internal ID or customer-managed environment variable to manufacture positive authority.

## Synthetic-only acceptance matrix

1. INTERNAL classification with current valid CP-approved sponsorship and matching tenant/organisation: positive fresh decision, zero-charge operation permitted; subsequent calls independently revalidate.
2. Suspension/revocation or effective_to expiry of that sponsorship: next INTERNAL `ensure`, `resume`, and `usage` reject, including exact Idempotency-Key replay.
3. A changed current classification or re-pointed PRIMARY Organisation: reject previously accepted classification references.
4. Token missing, expired, wrong audience, wrong client ID, withdrawn workload registration or missing scope: reject.
5. CP down, timeout, TLS failure, wrong JSON payload, stale timestamp, mismatched tenant/subscription/reference or overlong lease: reject without fallback.
6. Event redelivery, delay, reordering or broker outage: must not override the authoritative point-of-use decision. Prove dispatcher/consumer event convergence separately.
7. No real NABHOLD founding grants, ZURIBEANS tenant, operating legal-actor mandates, ERP postings or production enablement during these tests.

**Not yet demonstrated:** AWS staging environment proof; live IAM token rotation and workload registration, event dispatch and durable consumer receipt; load/recovery testing; exact Shared #273 main lock convergence; LA-03B v2 onboarding request → tenant registration. Keep production and real-entity admission denied until independently accepted.
