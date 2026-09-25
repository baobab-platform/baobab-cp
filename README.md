# Baobab Control Plane (`baobab-cp`)

> The authoritative source of tenant lifecycle, entitlement, and desired-state truth for the Baobab ecosystem.

**Status:** A4 — executable, fail-closed tenant context resolution (see [ADR-0004](docs/adr/0004-context-resolution-policy.md)).
**Architecture:** [ADR-0003 — Multi-Tenant, Production-Ready Control Plane Architecture](docs/adr/0003-multi-tenant-control-plane-architecture.md)

---

## What this repository is

`baobab-cp` decides *who a tenant is, what state they're in, and what they're entitled to use* — and reconciles that decision against reality. It does not process commerce, ERP, or research-intelligence business logic; those live in their own product engines and consume this repository's decisions over the network.

If you are looking for:
- **Commerce logic** → [`baobab-platform/baobab-trade`](https://github.com/baobab-platform/baobab-trade)
- **ERP logic** → [`baobab-platform/baobab-erp`](https://github.com/baobab-platform/baobab-erp)
- **Research intelligence** → [`baobab-platform/baobab-pulse`](https://github.com/baobab-platform/baobab-pulse)
- **Canonical contracts** (schemas this repo implements against) → [`baobab-platform/shared`](https://github.com/baobab-platform/shared)
- **Infrastructure provisioning** (Terraform, APISIX bootstrap, RabbitMQ/Postgres/Redis topology) → [`baobab-platform/infrastructure`](https://github.com/baobab-platform/infrastructure)
- **Local dev container image** → [`baobab-platform/baobab-dev`](https://github.com/baobab-platform/baobab-dev)

...you want one of those repositories instead. This one is intentionally narrow.

## Responsibilities

| Owns | Does not own |
|---|---|
| Tenant lifecycle (provision, suspend, reinstate, decommission) | Business data of any kind |
| Tenancy hierarchy metadata (Tenant Group → Tenant → Business Unit → Function → Team) | UI / end-user surfaces |
| Product entitlements per tenant | Infrastructure provisioning mechanics (that's `baobab-platform/infrastructure`) |
| Desired-state reconciliation (APISIX routes, per-tenant Postgres boundaries) | Canonical contract *definitions* (that's `baobab-platform/shared` — this repo implements against them) |
| Auditable provisioning history | Product-specific integrations |
| Lifecycle/entitlement event publication | — |
| Authenticated tenant-context resolution for product engines | — |

## Architecture at a glance

```
                     ┌────────────────────────────----┐
                     │        baobab-platform/shared          │
                     │  canonical contracts (OpenAPI, │
                     │  AsyncAPI, JSON Schema)        │
                     └───────────────┬────────────----┘
                                     │ implements
┌────────────────────────────────────▼────────────────────────────────────-┐
│                              baobab-cp                                   │
│                                                                          │
│   API layer (REST)         Reconciler loop         Event outbox          │
│   /v1/context/resolve      desired vs actual        → RabbitMQ           │
│   /v1/tenants              → APISIX / Postgres                           │
│   /v1/entitlements                                                       │
│                                                                          │
│                    PostgreSQL (authoritative)                            │
└───────────┬──────────────────────────────────────────┬──────────────────-┘
            │ resolves context for                     │ provisioned by
            ▼                                            ▼
┌───────────────────────────-┐                 ┌────────────────────────────-┐
│ baobab-trade / baobab-erp  │                 │   baobab-platform/infrastructure    │
│ baobab-pulse (consumers)   │                 │  Terraform · APISIX · RMQ   │
└───────────────────────────-┘                 └────────────────────────────-┘
```

See [ADR-0003](docs/adr/0003-multi-tenant-control-plane-architecture.md) for the full rationale, including why REST over gRPC for v1, why fail-closed context resolution is a contract not an implementation detail, and why a bespoke reconciler rather than a full Kubernetes operator at this stage.

## Tech stack

| Concern | Choice | Why |
|---|---|---|
| Language / runtime | Go | [ADR-0001](docs/adr/0001-go-control-plane-runtime.md) |
| HTTP | `net/http` + `chi` router | minimal, idiomatic, no framework lock-in |
| Database | PostgreSQL 17 via `pgx` | authoritative store; matches `baobab-platform/infrastructure`'s provisioned topology |
| Migrations | plain SQL, embedded and applied by a small in-repo runner (`internal/store/postgres/migrate.go`) | reviewable diffs, no external migration-tool dependency |
| Messaging | RabbitMQ + transactional outbox — **planned, not yet implemented** | see [the audit](docs/reconciliation/shared-control-plane-audit.md#5-event-architecture) for current status |
| Gateway integration | APISIX Admin API client — **planned, not yet implemented** | control plane will reconcile routes it owns |
| AuthN | OIDC (admin API); infrastructure-terminated mTLS + OIDC workload tokens | see ADR-0003 §7 |
| Observability | OpenTelemetry (traces, metrics, logs) | org-wide observability contract (`baobab-platform/shared`) |
| Config | environment variables, validated at startup | 12-factor, container-friendly |
| Testing | standard `testing`; a real-PostgreSQL integration test package exists (`internal/repository/postgres_integration_test.go`) | real dependencies over mocks-only where practical |
| CI/CD | reusable workflows from `baobab-platform/shared` | org-wide standardisation |
| Container | multi-stage Dockerfile, distroless final stage | minimal attack surface |

## Repository structure

```
baobab-cp/
├── api/                          # HTTP layer: router, handlers, middleware (chi)
├── cmd/
│   ├── controlplane/             # API server entrypoint
│   └── migrate/                  # migration-runner entrypoint
├── internal/
│   ├── auth/                     # OIDC token verification, principal context
│   ├── config/                   # startup configuration + validation
│   ├── domain/                   # core domain model, framework-free
│   ├── reconcile/                # desired-state reconciliation loop
│   ├── repository/                # mapping/capability/topology repository (Postgres + in-memory)
│   ├── resolver/                  # context/mapping/capability resolution pipeline
│   ├── service/                   # application services (canonical entity, resolution)
│   └── store/
│       ├── postgres/               # tenant/entitlement store + embedded SQL migrations
│       └── store.go                # TenantStore interface
├── docs/
│   ├── adr/                        # this repo's local ADR register
│   ├── architecture/
│   ├── reconciliation/             # audit of this repo against baobab-platform/shared
│   └── security/
├── Dockerfile
├── Makefile
├── go.mod
├── .env.example
└── README.md
```

`internal/domain` contains no framework or infrastructure imports — it is the part of this
codebase that should be easiest to test and hardest to accidentally couple to a specific
database or transport. There is currently no `pkg/contracts` (generated `baobab-platform/shared`
clients) or dedicated `internal/events`/`internal/gateway` package — RabbitMQ publication
and the APISIX admin client are not yet implemented; see
[`docs/reconciliation/shared-control-plane-audit.md`](docs/reconciliation/shared-control-plane-audit.md)
for the current gap list.

## Getting started

```bash
git clone https://github.com/baobab-platform/baobab-cp.git
cd baobab-cp
cp .env.example .env
make dev-up      # starts local Postgres 17 + RabbitMQ via this repo's docker-compose.yml
make migrate
make run
```

Run tests:

```bash
make test              # unit tests
make test-integration  # runs the *_integration_test.go suites against `make dev-up`'s Postgres
```

### Local topology options

`make dev-up` starts a standalone Postgres+RabbitMQ pair defined in this repo's own
`docker-compose.yml` — self-contained, no other repo required, good for day-to-day
iteration. Its database/user name (`baobab_control`) and RabbitMQ vhost (`nabhold`)
deliberately match what `baobab-platform/infrastructure`'s own compose topology provisions for
this repo, so switching between the two options below doesn't require renaming
anything in your `.env` beyond the password.

To instead run against the real topology this repo talks to in shared/staging
environments — useful before relying on behavior that's specific to that setup (real
credential rotation, the shared RabbitMQ vhost, eventually APISIX route reconciliation):

```bash
git clone https://github.com/baobab-platform/infrastructure.git ../infrastructure  # sibling clone
cd ../infrastructure/compose && cp .env.example .env   # then edit in real dev secrets
cd ../../baobab-cp
make dev-up-infra        # brings up baobab-platform/infrastructure's real postgresql + rabbitmq
make dev-env-infra       # prints the DATABASE_URL/RABBITMQ_URL to paste into .env
make migrate
make run
```

`INFRASTRUCTURE_DIR` (default `../infrastructure`) overrides where these targets look for
that repo if it isn't cloned as a sibling directory. `make dev-down-infra` /
`make dev-logs-infra` mirror the standalone targets.

## Relationship with other repositories

| Repository | Relationship |
|---|---|
| `baobab-platform/shared` | Contract source of truth. `baobab-cp` implements the OpenAPI/AsyncAPI schemas defined there; it never redefines them locally. |
| `baobab-platform/infrastructure` | Provisions the Postgres, RabbitMQ, and APISIX instances `baobab-cp` depends on and reconciles against. `baobab-cp` never provisions its own infrastructure. |
| `baobab-platform/baobab-trade` | Will consume the implemented `POST /v1/context/resolve` boundary in PR A5; it must fail closed if unresolved or its 15-second success cache expires. Consumer, not a dependency of this repo. |
| `baobab-platform/baobab-erp` | Contract-level consumer of canonical identifiers; does not yet call this repo's context-resolution API directly (open question — see ADR-0003 §14). |
| `baobab-platform/baobab-pulse` | Consumer of tenant/entitlement context (integration not yet established — repository is pre-Foundation). |
| `baobab-platform/baobab-dev` | Provides this repository's local/CI development container image. |
| Digital-estate frontends (`baobab-platform/nabhold`, `zuribeans`, `thamani`, `equator-estate`) | Consume Baobab exclusively through product-engine APIs; do not call `baobab-cp` directly in the general case. |

## Security

- The implemented `/v1/context/resolve` boundary requires infrastructure-terminated mutual TLS plus a scoped workload token carrying canonical tenant and service identity.
- Secrets are never committed. `.env` is for local development only and must never contain production credentials — see [SECURITY.md](SECURITY.md).
- Every provisioning and lifecycle-transition action is written to an append-only audit log before being acknowledged.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes to `/v1/context/resolve`'s contract (including its fail-closed behaviour) require an ADR, not just a PR — it is depended upon by production consumers.

## License

Apache-2.0. See [LICENSE](LICENSE).

## Foundation 4

Codespaces uses the v1.2.6 full profile with a temporary Go feature until a
native Go profile is published. The SHA-pinned `foundation` workflow enforces
contract compatibility, reproducibility, ownership, and security scanning.
