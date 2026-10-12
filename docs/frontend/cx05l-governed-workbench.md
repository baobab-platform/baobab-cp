# CX-05L/M — governed dataset Console workbench

The Console workbench is *implemented in source*, protected behind
`CX05_DATASET_CONSOLE_ENABLED=true`, and not enabled in production. The
separately deployed Control Plane API also requires `CX05_DATASET_API_ENABLED`
and a securely managed 256-bit CSV encryption key. Both flags default off.

## Flow

1. The same-origin, session-bound BFF obtains the current confidential OIDC
   token from encrypted Redis and re-introspects revocation; the browser never
   receives a CP bearer token.
2. Strict Zod DTOs and a fixed CP endpoint allowlist reject unrecognised
   paths, principal selectors and unsupported mutation types. POST enforces
   origin and CSRF; every request avoids caches and redirect forwarding.
3. Preview reads only independently granted organisation rows and versions.
   A bulk-only operator needs *bulk proposal* authority, not an export grant.
4. A proposal binds target IDs, purpose, recipient reference, exact source
   fingerprint, idempotency key and short expiry. An independent human
   reviewer inspects the full immutable plan, including metadata changes,
   before separately approving it with evidence.
5. CP rechecks maker/checker grants, session assurance, live source and revision
   at approval, atomic execution and every CSV retrieval. A completed export
   can be downloaded only by an authorised participant before expiry.

## Maintenance constraints

Do not convert this BFF into an arbitrary URL proxy, use group affiliation
as a privilege, put a token into a client component, drop the independent
review/CRITICAL step-up, or change the CSV field allowlist without a Shared
contract/POPIA review. User-facing status is not a substitute for CP audit.

References: `frontend/src/features/console/DatasetOperationsWorkbench.tsx`,
`frontend/src/features/console/dataset-command-model.ts`,
`frontend/src/app/api/workspace/datasets/route.ts`,
`frontend/src/server/bff/console-datasets.ts`,
`api/console_dataset_handler.go`.

No completed protected live IAM/CP E2E certification is asserted by this
code-level increment.
