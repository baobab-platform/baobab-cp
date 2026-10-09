# Baobab Control Plane

**Control Plane API + CP Console** · [Architecture decisions](docs/adr/index.md) · [Console implementation](docs/frontend/index.md) · [Shared contracts](https://github.com/baobab-platform/shared)

Baobab Control Plane (\`baobab-cp\`) owns the platform's **canonical organisation and tenant control state**: admission and onboarding, governed tenancy and relationships, capability entitlements, provisioning desired state, readiness, and platform administration. Its Go API is the authority; the co-located Next.js Console is a separately deployed human interface over that API.

> **Readiness (9 October 2026):** The Go backend is under active implementation, with substantial runtime and API support. The CP Console has its foundation, design system and generated client, **but no production-ready authentication flow or operational workspace yet**. Neither a passing backend CI run nor the existence of a Console container certifies end-to-end production onboarding.

## Repository at a glance

| Component | Location | Responsibility | Current maturity |
| --- | --- | --- | --- |
| Control Plane API | \`api/\`, \`cmd/controlplane/\` | Authenticated APIs, authoritative policy checks and administrative commands | Substantial implementation; individual routes remain subject to runtime/contract verification |
| Domain and persistence | \`internal/\` | Canonical state, lifecycle services, Postgres repositories, reconciler and events | Actively evolving; do not infer production acceptance |
| Migrations | \`internal/store/postgres/migrations/\` | Versioned Postgres schema changes | Real migrations; run before the API |
| CP Console | \`frontend/\` | Next.js App Router application, future confidential-client BFF | FE-01/02/05 foundations; no authenticated operator journeys |
| Contract pin | \`contracts.lock.yaml\` | Exact authoritative \`shared\` revision | Enforced by contract and generated-client tests |
| Architecture & programme | \`docs/adr/\`, \`docs/frontend/\` | Decisions, design constraints and progress | Consult accepted amendments and latest implementation evidence |

### Architecture and authority

\`\`\`text
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
\`\`\`

The diagram describes the intended Console session boundary; its OIDC login, callback, session, logout and token custody **are not yet implemented in the frontend**. The browser must never hold privileged API credentials or call privileged CP endpoints directly.

### What CP owns — and what it does not

CP owns the governance and canonical state of **Organisations, their authorised relationships, Platform Accounts, Tenants, Markets, entitlements and capability topology**. It records desired-state plans, approvals, execution operations, readiness, drift, audit and evidence/verification decisions through its bounded APIs. It manages platform-side authority without replacing IAM's authentication authority.

CP **does not** own commerce orders and catalogues (Trade), financial accounting and ERP transactions (ERP), content (CMS), market intelligence (Pulse), subscription pricing/billing authority (Subscriptions), or production infrastructure execution (Infrastructure). CP also does not declare Shared's canonical schemas.

In particular:

- **Organisation, LegalEntity, CorporateGroup, PlatformAccount and Tenant are distinct.** A group relationship is not an access grant.
- Admission approval, authorised onboarding, provisioning, readiness and activation are **distinct lifecycle decisions**.
- A subscription or approved plan does not by itself prove a healthy provider, a deployed engine or an active tenant.
- [ADR-BCP-026](docs/adr/ADR-BCP-026%20%E2%80%94%20Progressive%20Enterprise%20Onboarding%2C%20Founding-Group%20Exemption%20and%20Capability-Specific%20Evidence%20Governance.md) and [ADR-BCP-027](docs/adr/ADR-BCP-027%20%E2%80%94%20Organisation-First%20Tenancy%2C%20Operating%20Business%2C%20Trading%20Style%20and%20Legal-Actor%20Responsibility%20Model.md) set the *new* organisation-first/progressive-onboarding target. Their acceptance is not proof that every migration, authorisation and operational gate is complete.
- Engine-specific IDs, provider choices and grants are **derived from authorised intent**, never supplied as authoritative user preferences.

## Current implementation and Console backlog

The backend includes API/domain work for admission, organisation resolution, tenant registration/onboarding, markets, mappings, subscriptions/classification, capability registry and resolution, provider topology, changesets, AdministrativeGrants/effective authority, durable operations, evidence/verification, engine releases and deployment observations. **Presence of handlers is not a blanket claim that all programmes have completed production certification**; consult the current tests and accepted ADRs for each operation.

The Console currently has:

- Next.js 16 / React 19 / TypeScript strict with Node.js 24, pnpm workspace and an independent Dockerfile.
- Owned accessible UI primitives, semantic status tokens, component tests and a non-production \`/design-system\` showcase.
- An OpenAPI-generated client from the pinned Shared contract, server-only CP client, fail-closed unsupported-operation handling and Console CI.
- A placeholder \`/\` page: **no live operational dashboard, admission review or tenant administration**.

Next gates are (1) refresh the FE-00 architecture and API/authority matrix, (2) finish IAM confidential-client integration and FE-03 browser/BFF session security, (3) build the contextual shell and authority-aware navigation, then (4) deliver the applicant → admission → onboarding → provisioning → readiness vertical slice. No fake production APIs or browser-owned administrative authority.

See the [Console overview](docs/frontend/index.md), [dated FE-00 rebaseline](docs/frontend/fe-00-refresh-2026-10-09.md) and [frontend developer guide](frontend/README.md).

## Technology and runtime boundaries

| Area | Implemented choice |
| --- | --- |
| Backend | Go (\`go.mod\` declares Go 1.27), \`net/http\` / chi, pgx |
| Primary state | PostgreSQL 17 |
| Migrations | In-repository SQL migration runner |
| Identity | OIDC-verified admin/workload identities; IAM is the identity authority |
| Events | Event ingress/outbox and provider integrations as bounded runtime facilities; verify deployment and delivery readiness separately |
| Console | Next.js 16, React 19, strict TypeScript, Node 24, pnpm |
| CI | GitHub Actions for Go and Console, plus Shared contract enforcement |
| Infrastructure | Containers and separate deployment topology in \`baobab-platform/infrastructure\` |

Never assume a local RabbitMQ, APISIX or Postgres container represents the production deployment.

## Get started — Go API

A recent Go toolchain compatible with \`go.mod\`, Docker with Compose and PostgreSQL 17 are required.

\`\`\`bash
git clone https://github.com/baobab-platform/baobab-cp.git
cd baobab-cp
cp .env.example .env
make dev-up       # local PostgreSQL and RabbitMQ for development
make migrate      # apply the embedded SQL migrations
make run          # API, default :8080
\`\`\`

Set real local-development OIDC configuration in \`.env\` (see the example); the server may require additional environment and workload configuration for particular runtime profiles. Never use the example credentials in a deployed environment.

\`\`\`bash
make test
make test-integration   # requires the documented Postgres test environment
make lint
\`\`\`

If you need the sibling Infrastructure Compose topology rather than this repository's developer stand-in, consult \`make dev-up-infra\` and \`make dev-env-infra\` in the [Makefile](Makefile).

## Get started — CP Console

Use Node.js 24 and Corepack. Run commands from the **repository root** because it owns the pnpm workspace.

\`\`\`bash
make frontend-install
CONSOLE_ENVIRONMENT=development CP_API_BASE_URL=http://localhost:8080 make frontend-dev
# open http://localhost:3000
make frontend-typecheck frontend-lint frontend-test frontend-build
make frontend-image
\`\`\`

This runs the **Console foundation and its placeholder**, not an authenticated administrative application. Its \`/healthz\` endpoint is liveness, **not platform readiness**. The frontend must not be exposed as a working administration experience before FE-03 and protected-route tests are complete.

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

For changes: [CONTRIBUTING.md](CONTRIBUTING.md). For vulnerabilities: [SECURITY.md](SECURITY.md). See [LICENSE](LICENSE). Sensitive evidence, secrets, bearer tokens and live tenant information must not enter test fixtures, screenshots or the browser bundle.
