# Platform L2 — dependency integration

Aligned with [baobab-platform/infrastructure](https://github.com/baobab-platform/infrastructure) integrated testing (Phase C).

## Preferred path (platform topology)

Use infrastructure Compose for Postgres + RabbitMQ — the same topology shared/staging environments use:

```bash
# Sibling clones: ../infrastructure, ../shared
make dev-up-infra
make dev-env-infra   # paste DATABASE_URL / RABBITMQ_URL into .env
make migrate
make test-integration
```

`INFRASTRUCTURE_DIR` defaults to `../infrastructure`.

## Standalone path (day-to-day)

```bash
make dev-up
make test-integration
```

Still valid for fast iteration; prefer `dev-up-infra` before relying on behavior specific to the platform stack.

## Contract lock (Phase B / EA-01)

```bash
make check-contract-lock   # SHARED_CONTRACTS_DIR=../shared
```

CI runs `contract_lock.py check --mode enforce` on every PR.

## Platform L3

Critical-path multi-engine smoke is owned by infrastructure (`make platform-l3`). Optional:

```bash
export PLATFORM_CP_URL=http://127.0.0.1:8080/health   # example
cd ../infrastructure && make platform-l3
```
