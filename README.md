# Baobab Control Plane

**Control Plane API + CP Console + public website foundation** · [Architecture decisions](docs/adr/index.md) · [Console implementation](docs/frontend/index.md) · [Shared contracts](https://github.com/baobab-platform/shared)

Baobab Control Plane (`baobab-cp`) owns the platform's **canonical organisation and tenant control state**: admission and onboarding, governed tenancy and relationships, capability entitlements, provisioning desired state, readiness, and platform administration. Its Go API is the authority; the co-located Next.js Console is a separately deployed human interface over that API.

> **Implementation snapshot — 12 October 2026:** The CP API and protected Console/BFF are implemented for organisation-first navigation, Organisation 360°, indexed directory and governed datasets. Dataset operations are **nonproduction-only and explicitly disabled by default**; protected IAM/AWS staging acceptance, verified high-volume performance and unrestricted production enablement remain unproven. Passing CI certifies the commit's tests, not live onboarding or issuer integration.

## Repository at a glance

| Component | Location | Responsibility | Current maturity |
| --- | --- | --- | --- |
| Control Plane API | `api/`, `cmd/controlplane/` | Authenticated APIs, authoritative policy checks and administrative commands | Substantial implementation; individual routes remain subject to runtime/contract verification |
| Domain and persistence | `internal/` | Canonical state, lifecycle services, Postgres repositories, reconciler and events | Actively evolving; do not infer production acceptance |
| Migrations | `internal/store/postgres/migrations/` | Versioned Postgres schema changes | Real migrations; run before the API |
| CP Console | `frontend/` | Next.js App Router, confidential session/BFF, governed workspace | Authenticated code-level journeys, CX-01–05 components; staging proof and production publication remain gated |
| Contract pin | `contracts.lock.yaml` | Exact authoritative `shared` revision | Enforced by contract and generated-client tests |
| Architecture & programme | `docs/adr/`, `docs/frontend/` | Decisions, design constraints and progress | Consult accepted amendments and latest implementation evidence |

### Architecture and authority

```text
                    Baobab IAM
                identity / federation
                         |
                    authenticated
                         v
 Browser -- secure session --> CP Console / BFF (Next.js)
                                     |
                               server-only API
                                     v
                         Control Plane API (Go)
                         organisation / tenancy
                         authority / provisioning
                         registry / operations
                                     |
                             PostgreSQL 17
                                     |
                     canonical events / integrations
                                     |
                Trade · ERP · CMS · Pulse · other engines

   shared = canonical contract authority
   infrastructure = deployment / infrastructure authority
```

The Console's OIDC login/callback/logout and Redis-backed confidential session boundary are now implemented as nonproduction-gated code. The browser must never hold privileged CP bearer credentials or call privileged CP endpoints directly. The login flow, IAM introspection, current grants and CP/AWS runtime have **not** been certified together in protected staging.

### What CP owns — and what it does not

CP owns the governance and canonical state of **Organisations, their authorised relationships, Platform Accounts, Tenants, Markets, entitlements and capability topology**. It records desired-state plans, approvals, execution operations, readiness, drift, audit and evidence/verification decisions through its bounded APIs. It manages platform-side authority without replacing IAM's authentication authority.

CP **does not** own commerce orders and catalogues (Trade), financial accounting and ERP transactions (ERP), content (CMS), market intelligence (Pulse), subscription pricing/billing authority (Subscriptions), or production infrastructure execution (Infrastructure). CP also does not declare Shared's canonical schemas.

In particular:

- **Organisation, LegalEntity, CorporateGroup, PlatformAccount and Tenant are distinct.** A group relationship is not an access grant.
- Admission approval, authorised onboarding, provisioning, readiness and activation are **distinct lifecycle decisions**.
- A subscription or approved plan does not by itself prove a healthy provider, a deployed engine or an active tenant.
- [ADR-BCP-026](docs/adr/ADR-BCP-026%20%E2%80%94%20Progressive%20Enterprise%20Onboarding%2C%20Founding-Group%20Exemption%20and%20Capability-Specific%20Evidence%20Governance.md) and [ADR-BCP-027](docs/adr/ADR-BCP-027%20%E2%80%94%20Organisation-First%20Tenancy%2C%20Operating%20Business%2C%20Trading%20Style%20and%20Legal-Actor%20Responsibility%20Model.md) set the *new* organisation-first/progressive-onboarding target. Their acceptance is not proof that every migration, authorisation and operational gate is complete.
- Engine-specific IDs, provider choices and grants are **derived from authorised intent**, never supplied as authoritative user preferences.

## ADR-BCP-026/027 completion programme

The [audited completion plan](docs/reconciliation/adr026-027-completion-plan-2026-10-12.md)
records CP/Shared/IAM and native engine baselines, branch/PR reconciliation,
contract-consumer differences, sequential task sets and authentic acceptance
prerequisites. CP #379 is merged with all final-head workflows green.

P0-02B adds a default-off authenticated private scanner intake and a separately
executed `cmd/independent-scanner` command. JWT/introspection, fresh exact
workload profile, current approved public trust and canonical admission are
rechecked before custody/commit; CP never loads the signing key. Exact signed
jobs support reviewed replay after ambiguous delivery. See the
[scanner runtime runbook](docs/migrations/P0-02B-authenticated-scanner-runtime.md).
P0-02C adds explicit S3 Object Lock/KMS custody, a durable intent/version ledger,
versioned retention policies, legal holds, envelope-key rewraps and fenced
orphan quarantine. The report-first `cmd/document-custody` workload uses
separately approved snapshot jobs and a durable cutoff before confirmed
version-specific deletion. Staging no longer accepts local custody. See the
[durable custody runbook](docs/migrations/P0-02C-durable-custody.md).
The committed scanner remains PROVISIONED; protected live activation is not
implied by code or CI.

P0-03 founding-record and mandate workflows exist, but genuine protected
acceptance and independent human decisions remain pending. The first P0-04A
consumer slice, [Trade #145](https://github.com/baobab-platform/baobab-trade/pull/145),
converges consequential wire validation to Shared `a9c53087` with generated
schema provenance. It grants no workload admission or live certification. ERP
[PR #95](https://github.com/baobab-platform/baobab-erp/pull/95) adds matching exact
Shared schemas to Python and the native JVM financial guard, with installed-wheel
and isolated-JAR checks. Payments [PR #36](https://github.com/baobab-platform/baobab-payments/pull/36)
adds exact CP authority/readiness schemas to the standalone Rust runtime,
with 130 independent owner wire vectors and packaging checks. This admits no
workload or certified money movement. Subscriptions [PR #40](https://github.com/baobab-platform/baobab-subscriptions/pull/40)
adds exact current INTERNAL and founding event schemas, with 144 owner wire
vectors and isolated JAR checks. It grants no workload admission, invoice legal
responsibility or provider readiness. Trade Docs is the final remaining consumer
slice; see the completion plan follow-ups for exact status.

The scanner pipeline now binds canonical case/claim/evidence admission before
custody, holds row locks through proof/audit registration and rechecks expiry
before commit. Direct registration without the lease denies. See the
[scanner admission maintainer note](docs/migrations/PEO-03L2-scanner-admission-custody.md).
This is code-level evidence governance; real scanner/OCR/source acceptance,
corporate decisions and protected certification remain open.

## Current implementation and Console backlog

The backend includes API/domain work for admission, organisation resolution, tenant registration/onboarding, markets, mappings, subscriptions/classification, capability registry and resolution, provider topology, changesets, AdministrativeGrants/effective authority, durable operations, evidence/verification, engine releases and deployment observations. **Presence of handlers is not a blanket claim that all programmes have completed production certification**; consult the current tests and accepted ADRs for each operation.

The Console implementation includes:

- Next.js 16, React 19, Node.js 24, strict TypeScript, tested accessible primitives and the separately deployed informational landing page.
- An OIDC confidential-client login/callback/logout flow, encrypted Redis-backed server session, revocation introspection, same-origin CSRF validation and a server-only Control Plane BFF. Activation is **explicitly gated to nonproduction**.
- CX-01–CX-04 role-filtered navigation, accessible command palette, exact-scope enterprise context switching and Organisation 360° read models with independent governance/source reconciliation.
- Organisation 360° displays permission-filtered mandate activity, market and capability restrictions from immutable CP records (Shared #297). Readback never grants transaction authority; engines must resolve the current mandate again before execution. See [scope integration](docs/frontend/cx04c-mandates-readiness.md).
- CX-05 server-indexed organisation directory (up to 20,000 *independently granted* candidates, 50-row pages), enterprise record tables, and a read-only-until-authorised dataset operations experience.
- Governed export and bulk operations: Shared-authored API schemas; CP human IAM/ACR and exact-grant guards; immutable approval digests; durable PostgreSQL proposals, auditable download, atomic versioned metadata mutation, AES-256-GCM CSV at rest and expiry cleanup. See the [maintainer guide](docs/development/cx05-maintainer-guide.md).

**Current release boundaries:** `CX05_DATASET_API_ENABLED` and `CX05_DATASET_CONSOLE_ENABLED` default off; CP refuses production activation of these routes and requires a 32-byte managed encryption key. Independently verified IAM step-up, provider evidence, protected staging E2E, database-performance evidence, key rotation and backups are not established by CI. Do not treat an authenticated UI preview or corporate-group membership as a grant. The completed CX gates are *implementation increments*, not unrestricted operational acceptance.

See the [Console overview](docs/frontend/index.md), [dated FE-00 rebaseline](docs/frontend/fe-00-refresh-2026-10-09.md) and [frontend developer guide](frontend/README.md).

## Technology and runtime boundaries

| Area | Implemented choice |
| --- | --- |
| Backend | Go (`go.mod` declares Go 1.27), `net/http` / chi, pgx |
| Primary state | PostgreSQL 17 |
| Migrations | In-repository SQL migration runner |
| Identity | OIDC-verified admin/workload identities; IAM is the identity authority |
| Events | Event ingress/outbox and provider integrations as bounded runtime facilities; verify deployment and delivery readiness separately |
| Console | Next.js 16, React 19, strict TypeScript, Node 24, pnpm |
| CI | GitHub Actions for Go and Console, plus Shared contract enforcement |
| Infrastructure | Containers and separate deployment topology in `baobab-platform/infrastructure` |

Never assume a local RabbitMQ, APISIX or Postgres container represents the production deployment.

## Get started — Go API

A recent Go toolchain compatible with `go.mod`, Docker with Compose and PostgreSQL 17 are required.

```bash
git clone https://github.com/baobab-platform/baobab-cp.git
cd baobab-cp
cp .env.example .env
make dev-up       # local PostgreSQL and RabbitMQ for development
make migrate      # apply the embedded SQL migrations
make run          # API, default :8080
```

Set real local-development OIDC configuration in `.env` (see the example); the server may require additional environment and workload configuration for particular runtime profiles. Never use the example credentials in a deployed environment.

```bash
make test
make test-integration   # requires the documented Postgres test environment
make lint
```

If you need the sibling Infrastructure Compose topology rather than this repository's developer stand-in, consult `make dev-up-infra` and `make dev-env-infra` in the [Makefile](Makefile).

## Get started — CP Console

Use Node.js 24 and Corepack. Run commands from the **repository root** because it owns the pnpm workspace.

```bash
make frontend-install
CONSOLE_ENVIRONMENT=development CP_API_BASE_URL=http://localhost:8080 make frontend-dev
# open http://localhost:3000
make frontend-typecheck frontend-lint frontend-test frontend-build
make frontend-image
```

This runs the Console development application; privileged workspace routes require the separately configured, live IAM issuer, confidential-client settings, TLS Redis session store and current CP authority. With those absent they fail closed. `/healthz` indicates liveness, **not production readiness**. Do not set the governance feature flags or use fabricated tokens to bypass acceptance.

## Related repositories

| Repository | Contract/operational relationship |
| --- | --- |
| [shared](https://github.com/baobab-platform/shared) | Owns canonical OpenAPI, event and JSON contracts; CP pins and implements them |
| [baobab-iam](https://github.com/baobab-platform/baobab-iam) | Authentication, issuer and federation; CP evaluates administrative authority |
| [baobab-subscriptions](https://github.com/baobab-platform/baobab-subscriptions) | Subscription and commercial domain authority; CP consumes governed entitlements |
| [baobab-trade](https://github.com/baobab-platform/baobab-trade) | Commerce engine; consumes scoped platform context and capabilities |
| [baobab-erp](https://github.com/baobab-platform/baobab-erp) | ERP engine; independent operational data and accounting authority |
| [baobab-cms](https://github.com/baobab-platform/baobab-cms) | Content engine |
| [baobab-pulse](https://github.com/baobab-platform/baobab-pulse) | Intelligence engine |
| [infrastructure](https://github.com/baobab-platform/infrastructure) | Staging/production infrastructure, deployment and operational evidence |

## Documentation, contribution and security

Start with the [ADR register](docs/adr/index.md). Read the **accepted** BCP-017 through BCP-027 decisions and amendments, plus the applicable Shared contracts, before changing a governed lifecycle. The [FE-00 historical snapshot](docs/frontend/fe-00-architecture-lock.md) is not a current readiness report.

For changes: [CONTRIBUTING.md](CONTRIBUTING.md) and [developer documentation](docs/development/index.md). New governed operations must update their Shared schema/pin, API and BFF tests, runbook, README and maintenance comments in the same PR. Never claim successful live staging without recorded evidence.  For vulnerabilities: [SECURITY.md](SECURITY.md). See [LICENSE](LICENSE). Sensitive evidence, secrets, bearer tokens and live tenant information must not enter test fixtures, screenshots or the browser bundle.
