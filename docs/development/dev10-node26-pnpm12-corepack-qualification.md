# DEV-10 — CP Console Node 26 / pnpm 12 / Corepack 0.36 qualification

**Status:** development migration candidate; NOT connected acceptance. Owner PR: [CP #460](https://github.com/baobab-platform/baobab-cp/pull/460).

## Versions and governance

| Concern | Candidate | Evidence boundary |
| --- | --- | --- |
| Console build + frontend CI | Node.js **26.13.0** | Exact pinned Node Docker builder SHA and `frontend/.node-version` |
| Container runtime | Distroless **nodejs26-debian13:nonroot** | Immutable candidate digest; Docker image build, pull, signature, user and runtime smoke must independently pass |
| Package manager | **pnpm 12.16.1** | Root `packageManager`, env + project documents in `pnpm-lock.yaml`, frozen install |
| Bootstrap | **Corepack 0.36.0** | Explicit install: Node 26 does not bundle Corepack; strict version assertions in Docker and CI |
| Node API types | **@types/node 26.6.5** | Frontend manifest + lockfile + TypeScript typecheck |
| Dev Container | Node **26.13.0** feature, signed **baobab-dev:1.5.1** GA immutable image (source-pinned) | Source checks do not prove live Codespaces/registry compatibility |
| Go authority runtime | Go **1.27**, PostgreSQL **17** | Unchanged Go/domain contracts; native PostgreSQL-backed tests remain required |

pnpm 12 has a **two-document** lockfile: the first independently pins the package-manager/platform-binary integrity and the second retains the complete project dependency graph. Our source regression refuses an absent manager document or a truncated project document. **A scanner that only reads the first document is not credible**; retain `pnpm audit` in the frontend CI alongside Shared native security checks. No CI job may quietly skip a failed private BXS registry read.

## CI acceptance before merge

1. `frontend/.node-version`, workspace `engines`, root `packageManager`, source and lockfile are consistent.
2. Reconcile and inspect the resolved pinned Docker build/runtime image digests. Independently pull the Node 26 distroless image and verify non-root execution. Do not infer an image digest from another project's source alone.
3. `pnpm install --frozen-lockfile` must succeed against GitHub Packages; the generated CP OpenAPI client matches Shared; TypeScript, lint, unit/axe, Next build, `pnpm audit --audit-level high`, and Docker build all pass.
4. Codespaces source gate verifies Node/Corepack/pnpm pins. A fresh disposable Codespace must separately prove real private-package access, Dev Container initialization, frozen install, non-public port defaults, source revision and clean teardown.
5. Go CI, race/PG17 integration, TMS authority preflight and Foundation/security checks remain intact. No Dev Container or frontend upgrade grants a CP IAM authority, activates a tenant/engine, or admits production.
6. Resolve all latest-head CI failures and re-run after the final accepted dependency image/pin update. Existing green checks from Node 24 do not certify the Node 26 migration.

## Migration to baobab-dev:v1.5.1 — published GA; consumer proof still required

The signed **v1.5.1 GA** image was published after RC2 qualification; publication and signature evidence are recorded in Shared's DEV-10 register. The development image is now pinned to `ghcr.io/baobab-platform/baobab-dev:1.5.1@sha256:faf9022ef06288767651a0ec1071d5199bc2b645f2cf7ada50477774dd297eed`. This source-level pin does not prove a hosted Codespace consumer:

- Inspect its immutable manifest digest and provenance; verify the published toolchain actually exposes **Node 26.13.0, Corepack 0.36.0 and pnpm 12.16.1** to the non-root Codespaces user.
- Verify the exact selected immutable digest and its signing/provenance independently; image pin, Codespaces tests, and GHCR workflow must agree. Retain the explicit Node 26 feature and Corepack bootstrap until hosted proof confirms effective precedence and installed tool versions.
- Run a disposable GitHub Codespace and confirm read-only access to the *published* BXS packages with no credentials in tracked files, prebuilds or logs, then delete the test resource.
- Keep DEV-10/DEV-14 connected acceptance **HELD** until real hosted development proof succeeds; source CI alone does not discharge that gate. Do not turn a source unit fixture or successful builder image into protected staging/production acceptance.

## Repeatable checks

```sh
node --version
corepack --version
pnpm --version
node --test scripts/codespaces/*.node.mjs
pnpm install --frozen-lockfile
pnpm --filter @baobab-platform/cp-console check:cp-client
pnpm --filter @baobab-platform/cp-console typecheck
pnpm --filter @baobab-platform/cp-console lint
pnpm --filter @baobab-platform/cp-console test
pnpm --filter @baobab-platform/cp-console build
pnpm audit --audit-level high
```

Do not remove the tested version constraints or waive the registry and security controls just to obtain a green badge.
