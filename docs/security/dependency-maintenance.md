# BA-SEC-DEP-001 — Control Plane dependency maintenance adoption

**Governing standard:** [Baobab Dependency & Security Maintenance Policy v1.0](https://github.com/baobab-platform/shared/blob/feat/dev11-observability-source-contract-20261102/docs/governance/baobab-dependency-security-maintenance-policy-v1.0.md), Approved 5 November 2026; Shared #429 remains a proposed *implementation PR*, not an immutable released validator.  
**Implementation scope:** CP #460, Node 26/pnpm 12 DEV-10 companion; source additions are not deployment certification.  
**Responsible review:** Platform Engineering + Security; domain tests remain under CP/IAM authority.  
**Status:** SOURCE_COMMITTED; consumer and CI evidence remain separate.

## Source/manifests reviewed

| Ecosystem | Actual source | Dependabot policy entry |
| --- | --- | --- |
| GitHub Actions | `.github/workflows/*.yml` and reusable workflows | `github-actions` at `/` |
| Go | root `go.mod` / `go.sum`, Go 1.27.x | `gomod` at `/` |
| CP Console JS/TS | root `package.json`, `pnpm-workspace.yaml`, `pnpm-lock.yaml`; `frontend/package.json` | `npm` at `/`, NOT duplicate frontend entries |
| CP service image | root `Dockerfile` (Go builder, distroless runtime) | `docker` at `/` |
| CP Console image | `frontend/Dockerfile` (Node 26 builder/runtime) | `docker` at `/frontend` |
| Compose services | root `docker-compose.yml` | `docker-compose` at `/` |
| Development toolchain | `.devcontainer/devcontainer.json` / `Dockerfile.development` | Reviewed by DEV-10/baobab-dev authority, not independently auto-promoted |

**Protection:** Routine patch/minor groups explicitly use `applies-to: version-updates`; majors and all Docker changes remain separately reviewed; no `target-branch` override or blanket security ignore is introduced. All ecosystems have weekly, staggered Africa/Johannesburg schedules and positive routine PR limits. Event-driven security fixes do not wait for those times.

**Conformance:** `.github/workflows/enforce-dependabot-config.yml` continues calling pinned *accepted Shared* workflow at `31de2bc3dcd56128c019c640dc7e12c10ae9ca69`, now with `required-ecosystems: github-actions,gomod,npm,docker,docker-compose`. This is currently the structural **baseline**, not yet the Shared #429 `strict` implementation. Switch to a reviewed/released immutable strict validator **after #429 merges and is verified**; do not pin an open PR as a released security authority.

## P0: pnpm 12 dependency graph is a known security-visibility blocker

**Known GitHub limitation (as documented 20 October 2026):** While Dependabot version updates support pnpm 12, a pnpm 12 multi-document `pnpm-lock.yaml` is not parsed by GitHub's dependency graph. Consequently Dependabot Alerts and **security update PRs may not be created for those dependencies**, even if normal version update PRs succeed. GitHub's docs explicitly note the exception: https://docs.github.com/en/code-security/reference/supply-chain-security/supported-ecosystems-and-repositories#pnpm.

CP #460 intentionally adopts **Node 26.13.0 / pnpm 12.16.1** with a **two-document** project/package-manager lockfile. This is a direct collision between the toolchain migration and BA-SEC-DEP-001's security update assurance. Do not report `github-actions,gomod,npm,docker,docker-compose` structural conformance as security-alert conformance.

**Mitigations implemented in this PR:**

- Existing `.github/workflows/frontend.yml` runs `pnpm install --frozen-lockfile` and `pnpm audit --audit-level high` on relevant PRs, plus lint, build, tests, and container build.
- Existing `.github/workflows/foundation.yml` provides Trivy, package-native security gates, secrets and applicable policy scanning.
- New `.github/workflows/dependency-audit-pnpm.yml` performs a **daily** pinned-runtime frozen installation and **fresh** `pnpm audit --audit-level high` on the default branch, failing on scanner errors or detected High/Critical findings. It labels the multi-document dependency graph state **UNKNOWN**, rather than claiming no alerts.
- New `scripts/tests/test_dependency_maintenance.py` tests config/manifest coverage, routine grouping, security blind-spot attribution, and scheduled native audit wiring without the CI network.

**Residual gate:** Even with native audit, GitHub Alerts/security-PR automation remains **unqualified** until an independently checked dependency graph shows real `frontend` lockfile dependency inventory and a witnessed security alert/update path (or a separately approved and effective compensating process with a recorded expiry). Native audit does not discover every advisory or prove legal/runtime acceptance. **Do not merge #460 solely on this policy change**.

## P0/P1 review obligations

1. **Security:** Confirm dependency graph, alerts, security updates, and private GitHub Packages permissions in repository settings and log the verification; source cannot prove settings.
2. **CP dependency owner:** Prove scheduled Dependabot npm/pnpm 12 version update and Go update jobs operate with actual manifests, correct default branch, and separate major review. Observe errors, cooldown or private-package resolution failures.
3. **Security:** Reconcile full-SHA GitHub Actions with advisory/release mapping and verify image digests; a SHA-pin is immutable, not automatically free of CVEs.
4. **CP owner:** Retain JWT/OIDC, tenant isolation, budget/authority refusal, provisioning replay, provider and event persistence tests when dependency versions change; successful installation alone is not authority evidence.
5. **DEV-10:** Review pnpm 12 lockfile compatibility with GitHub's security index, private BXS package integrity, Node 26 ABI/runtime and tested consumer paths; major upgrades require separate approval.
6. **Release owner:** Keep development PR tests distinct from any protected environment image promotion; record native artifacts, SBOM, digest, rollback and approval.

## Verification and states

The source config now requests complete *manifest-observed* Dependabot coverage, but this change does not itself demonstrate remote Dependabot service execution. Track independently: `SOURCE_COMMITTED`, `CI_VERIFIED`, `SECURITY_ALERT_PATH_VERIFIED`, `CONSUMER_QUALIFIED`, `PRODUCTION_VERIFIED`, `BLOCKED`, and `WAIVED`. No waiver is invented for the pnpm 12 gap. A Security-approved, expiring mitigation is necessary for any protected rollout with unverified alerts.

Run: `python3 scripts/tests/test_dependency_maintenance.py`. Verify updated reusable configuration check on the **final PR head** in addition to CI for Go, Console, and Foundation. No polling/monitoring is implied by this document.
