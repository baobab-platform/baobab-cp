# Foundation 2: executable control-plane core

Foundation 2 replaces the legacy polyglot monolith scaffold with a focused Go service. The first vertical slice implements the canonical `POST /v1/tenants` contract from [`baobab-platform/shared`](https://github.com/baobab-platform/shared), PostgreSQL desired state, idempotent operations, an append-only audit record, and a transactional outbox record.

Infrastructure manifests remain in [`baobab-platform/infrastructure`](https://github.com/baobab-platform/infrastructure). Cross-repository schemas remain in `baobab-platform/shared`. This repository owns only control-plane runtime logic and its own database migrations.

The administrative bearer token is a bootstrap authentication boundary. OIDC validation and workload mTLS are Foundation 3 security work; production deployment must not proceed with the bootstrap token.
