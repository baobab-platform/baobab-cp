# CX-05H — protected live E2E and high-volume performance

`cx05-live-e2e.yml` is a manually dispatched, protected `staging`
GitHub Actions workflow. Its two scripts run **real HTTPS Control Plane
queries** with current IAM tokens. No local fixture or mock satisfies it.

## Protected settings

The existing six CX05 secrets must be configured and refer to independent
granted, denied and revoked IAM credentials, own and foreign CP organisation
IDs, and a live TLS origin. The granted principal needs **at least 5,001
simultaneous exact organisation grants**, not inferred corporate/group access.

Additional *environment variables*: `CX05_STAGING_PERF_P95_MS` (explicit
reviewed latency budget, 50–2500 ms); optional
`CX05_STAGING_PERF_SAMPLES` (64–1024, default 128).
Fail closed if budget, credentials, dataset or deployment are missing.

## Live checks

- Existing CX-05F suite: valid own source, denied foreign source, withdrawn
  token, invalid user authority selector, forged cursor, two-page keyset
  paging, oversized request denial and >500 directory count.
- New CX-05H suite: 128 successful real HTTPS authorised directory queries
  by default at **bounded concurrency four**, p50/p95/p99 and maximum response
  latency, explicit reviewed p95 threshold, nonduplicate bounded rows,
  consistent >=5001 visible count and zero failed/inconsistent requests.
- Workflow records only counts, aggregate latencies, code revision and SHA256
  in a 30-day artifact. Never publish resource names, user identities,
  token claims, raw response bodies or database contents.

## Unmet certification requirements

This change **provides executable proof gates**; it is not proof of a
completed staging run. Operational signoff also requires real revocation
latency, representative production data and peak volume, database
`EXPLAIN (ANALYZE, BUFFERS)` and post-migration plans, independent
performance review, and live protected export/bulk lifecycle negative tests
when the Shared-published authenticated routes are available.

Both scripts' PASS values are technical observations and **not automatic
production eligibility**. No workflow may claim a completed certificate
from GitHub unit tests or synthetic numbers.
