# CX-05G — Durable governed dataset operations (internal execution adapter)

## Implemented

- Migration `000122` adds a durable immutable-plan ledger with (maker, idempotency key) uniqueness and a dedicated version for *console metadata only*. No canonical organisation status, legal fact or verified evidence can be bulk-changed.
- `ProposeConsoleDataset`: authenticated actor-bound immutable plan, source fingerprint, exact per-target permission input, idempotent proposal and transactional audit; source projections come from current CP database records, not submitted caller-owned rows.
- `ApproveConsoleDataset`: independently authenticated checker, immutable plan digest, CURRENT maker/checker permissions, fresh source fingerprint and durable decision and audit.
- `ExecuteConsoleDataset`: PostgreSQL SERIALIZABLE transaction locks the approved plan and canonical source rows; either stores redacted bounded CSV or executes a complete set of optimistic-versioned internal metadata changes. It atomically records the receipt and audit with the data change; a completed plan cannot execute twice.
- `ReadConsoleDatasetExport`: only maker/checker, valid TTL and current exact grants; source freshness and audited retrieval. There are no unauthorised raw-evidence columns or legal certification fields in the export.
- PostgreSQL integration tests exercise replays, self approval, preapproval refusal, maker/checker grants, denied foreign target, atomic revisions, download revocation and expiry.

## Scope / operational blocks

**Later implementation note (12 October 2026):** CX-05K and CX-05L/M have now
added versioned Shared API contracts, authenticated nonproduction CP handlers,
current-grant evaluation and a CSRF-protected Console BFF and workbench.
This section describes the **original CX-05G delivery boundary**, not the
current complete code inventory. Runtime flags are still off by default;
protected cross-service staging acceptance is outstanding.

The proposed plan documents and redacted CSV are currently stored in PostgreSQL. Production publication additionally requires managed encryption, retention/purge policy, privacy/legal review, quotas and operational backups. The presence of this code is NOT permission to enable any direct download API.

The domain code's permission callback must be wired to *current* authenticated per-principal, exact-organisational CP grant evaluation. Neither API callers nor arbitrary consumer code may supply an allow-all predicate in production. Live user-facing use stays disabled until that enforcement and independent protected acceptance exist.

## CX-05H prerequisite

Run protected IAM-authenticated negative tests and >=5,001 exact grants, explain-plan and latency distributions against deployed CP after the migration and its indexed read path are active. GitHub CI and synthetic Postgres fixtures are not live performance certification.
