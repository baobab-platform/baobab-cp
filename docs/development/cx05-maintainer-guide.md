# CX-05 governed datasets — maintainer guide

**Snapshot:** 12 October 2026 · Control Plane + Console, nonproduction code
implementation · protected staging and production certification **pending**.

## Boundaries and call path

```text
Human browser ── HttpOnly session + same-origin CSRF ──> Next.js Console BFF
    │                                                  │
    │  never bearer credentials                       │ server-only bearer
    └───────────────────────────────────────────────────┘
                                        ↓
              Go CP IAM human verifier / live principal / exact grant + ACR
                                        ↓
       Published Shared JSON/OpenAPI ──> CP dataset preview/proposal routes
                                        ↓
       PostgreSQL SERIALIZABLE plan + approval + audit + source revision
                                        ↓
                   AES-256-GCM CSV ciphertext / retention purge
```

All data-producing reads and state-changing commands derive the maker/checker
from a validated human token. The browser may request an organisation ID,
**never** assert its own principal, grant, corporate authority, assurance or
engine tenant context.

## Source-file map

| Responsibility | Authoritative implementation |
| --- | --- |
| Shared API and strict DTOs | `baobab-platform/shared/contracts/control-plane/v1/openapi.yaml`, `console-dataset.schema.json` |
| Pinned Shared revision and generated client | `contracts.lock.yaml`, `internal/contracts/shared/`, `frontend/src/generated/control-plane/openapi.d.ts` |
| HTTP: current human + grant/assurance checks | `api/console_dataset_handler.go`, `internal/administration/` |
| Immutable plan, maker/checker, CSV sanitisation | `internal/domain/console_dataset_operation.go` |
| Postgres transaction, audit, replay protection | `internal/repository/postgres_console_datasets.go` |
| Encrypted storage, retrieval and purge | `internal/repository/console_dataset_cipher.go` and migration `000123` |
| Confidential browser session and revocation | `frontend/src/server/auth/runtime.ts` |
| Fixed URL allowlist and server-only CP request | `frontend/src/server/bff/console-datasets.ts` |
| Same-origin POST, CSRF, bounded download | `frontend/src/app/api/workspace/datasets/route.ts` |
| Client DTOs and independent review UI | `frontend/src/features/console/dataset-command-model.ts`, `DatasetOperationsWorkbench.tsx` |
| Live protected staging proof | `.github/workflows/cx05-live-e2e.yml`, `scripts/cx05-live-e2e.mjs`, `cx05h-perf.mjs` |

## Operation sequence and invariants

1. **Preview:** CP checks current, exact `organisation.view` plus the distinct
   `organisation.export` or `organisation.bulk.propose` authority; it
   returns redacted rows and a source fingerprint. A bulk-only viewer can
   request versions without any export right.
2. **Propose:** CP derives the maker from IAM, verifies each target, refuses
   duplicate IDs and changed fingerprints, and inserts a durable, uniquely
   idempotent `PENDING_APPROVAL` plan with audit.
3. **Inspect and approve:** A different human reads the immutable plan before
   submitting evidence. CP separately checks the maker's still-current
   authority and the checker's `organisation.bulk.approve` or export
   permission, and enforces session ACR, AMR and freshness. **HIGH/CRITICAL
   assurances cannot be replaced by an administrative role**.
4. **Execute:** CP revalidates the source/revisions, approved values and
   current grants under a serializable transaction and records completion
   and audit together. The only bulk mutation is an allowlisted internal
   metadata label; legal status, registration, evidence and payments are
   outside this command. Replays must not re-execute writes.
5. **Deliver:** CSV is redacted, formula-safe, bounded and encrypted under
   AES-256-GCM with plan digest as associated data. Each retrieval requires
   fresh authorization and is audited; an expired or tampered artifact is
   denied. Background purge discards expired ciphertext while retaining
   the audit/approval record.

**Hard separation:** being a Nabhold group member, tenant owner, or mere
`organisation.view` recipient does not grant export, bulk approval or
cross-subsidiary access. No arbitrary tenant headers or browser-controlled
identity can override CP.

## Local developer and CI workflow

From the repository root:

```sh
make dev-up && make migrate
make test
make test-integration
make frontend-install
make frontend-typecheck frontend-lint frontend-test frontend-build
```

The Go integration suite requires a real developer PostgreSQL 17; missing
fixtures should fail rather than substituting a mock. `frontend` scripts and
the pinned client generator are driven through the root pnpm workspace.

After **any** Shared API schema change: merge Shared first, pin its immutable
commit in both lock references, refresh embedded OpenAPI and the generated
`openapi.d.ts`, and run the contract/router drift checks. Never edit the
generated TypeScript client as a source-of-truth.

### Troubleshooting without bypassing security

| Symptom | Check | Never do |
| --- | --- | --- |
| 404 or unavailable workbench | Auth/Console flags, current OIDC session, exact grant | Remove nonproduction or grant gates |
| 403/404 on approval | Independent checker, current maker permission, HIGH/CRITICAL ACR | Fall back to role/group access |
| Stale preview or conflict | Source fingerprint and `console_metadata_version` | Ignore optimistic revisions |
| CSV unavailable | Plan state/expiry, keyring, current principals, purge | Persist plaintext or reuse wrong digest |
| Client drift failure | Pinned Shared OpenAPI, generated client, router fixture | Silence the checker |
| Large directory search denied | Exact-grant count <=20k, no inherited group grants | Raise limits without index/load evidence |

## Operational acceptance — not satisfied by CI

Review and configure a protected `staging` environment, real IAM human maker
and checker, provider assurance/CRITICAL step-up, CP, TLS Redis, PostgreSQL,
independently approved encryption key management, key rotation and backup
retention. Run the `cx05-live-e2e.yml` workflow against >=5,001
independently authorised organisations and establish the agreed p95 budget,
revocation, foreign-organisation isolation, multi-page consistency, export
download, independent approval and atomic bulk negative tests. Record actual
run IDs and independent decision evidence; no credentials, live rows or PII
belong in GitHub artifacts. **No recorded successful protected end-to-end
certification means the platform remains gated.**

## Comment and review conventions

Document the *reason* for a scope check, safety bound, transaction lock,
redaction or failure mode in source. Exported Go types/functions should have
GoDoc; TS interfaces and security-sensitive helpers should have short JSDoc.
Do not comment on self-explanatory assignments. A change in a boundary needs
matching negative-security tests, docs and Shared contract review. Comments
cannot themselves be evidence that authority or encryption has been verified.
