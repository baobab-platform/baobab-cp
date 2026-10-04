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
make test                 # unit tests
make test-integration     # *_integration_test.go against Postgres
make check-contract-lock  # EA-01 consumer lock (needs ../shared)
```

### Local topology options

`make dev-up` starts a standalone Postgres+RabbitMQ pair defined in this repo's own
`docker-compose.yml` — self-contained, no other repo required, good for day-to-day
iteration.

**Preferred for platform L2** (same topology as shared/staging):

```bash
git clone https://github.com/baobab-platform/infrastructure.git ../infrastructure
cd ../infrastructure/compose && cp .env.example .env   # then edit secrets
cd ../../baobab-cp
make dev-up-infra
make dev-env-infra       # paste into .env
make migrate
make test-integration
```

See [docs/testing/platform-l2.md](docs/testing/platform-l2.md).

`INFRASTRUCTURE_DIR` (default `../infrastructure`) overrides the sibling path.

## Relationship with other repositories

| Repository | Relationship |
|---|---|
| `baobab-platform/shared` | Contract source of truth; this repo pins consumption in `contracts.lock.yaml` (EA-01). |
| `baobab-platform/infrastructure` | Provisions Postgres, RabbitMQ, APISIX; owns platform L3 harness. |
| Product engines | Consume context resolution; must fail closed if unresolved. |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes to `/v1/context/resolve` require an ADR.

## License

Apache-2.0. See [LICENSE](LICENSE).
