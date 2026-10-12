# Control Plane development and maintenance

The [root README](../../README.md) explains the whole CP repository; the
[Console README](../../frontend/README.md) covers TypeScript, React, Node and BFF setup.

- [CX-05 maintainer guide](cx05-maintainer-guide.md) explains the contracts,
  IAM-to-Console-to-CP-to-Postgres authority path, encrypted artifact lifecycle,
  common failures and the distinction between CI and protected staging proof.
- [Console architecture and detailed increments](../frontend/index.md)
  enumerate CX-01–CX-05 implementation and outstanding operational acceptance.
- [Accepted ADR index](../adr/index.md) governs authority, tenancy,
  organisational admission and independent approval decisions.
- [API contract lock](../../contracts.lock.yaml) names the exact Shared
  revision. Never edit generated TypeScript client types directly.

- [Scanner admission and custody](../migrations/PEO-03L2-scanner-admission-custody.md)
  explains lock order, same-transaction proof/audit, denial and orphan handling.
- [ADR-BCP-026/027 completion plan](../reconciliation/adr026-027-completion-plan-2026-10-12.md)
  records current cross-repository baselines and sequential acceptance tasks.

- [Authenticated scanner runtime](../migrations/P0-02B-authenticated-scanner-runtime.md) maps private intake, current IAM/registry/public trust, isolated signing and durable exact replay.

## Required maintenance discipline

For changed behavior, update the Shared contract where necessary, Go and
Console tests, operational runbook, README and code comments **in the same PR**.
Explain *why* an authority check, lock, transaction or redaction exists; avoid
comments that paraphrase syntax. Preserve negative tests when refactoring
security code, and do not bypass review/CI to correct stale docs.

- [Durable custody and lifecycle](../migrations/P0-02C-durable-custody.md) covers version-pinned S3/KMS, approved retention/holds, key rewrap and fenced orphan/deletion recovery.
