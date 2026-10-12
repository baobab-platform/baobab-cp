# CX-05A — exact-authority organisation search table

**Shared pin:** `2238596a4f79c70ca421bae82f0eb0655c8f3448`.
**APIs:** `GET /v1/organisations/directory/page` (nonproduction).

## Experience

A first, fully real and caller-filtered organisation data table, built with
Tailwind v4 layout, shadcn tokens, Lucide iconography, native table semantics
and browser GET forms. Server-side name search, status/verification filters,
deterministic name sorting, page size 10/25/50 and next/first navigation. The
page shows an evaluation timestamp, matched count, loading/error/empty states
and links to exact Organisation 360° records. Long content scrolls horizontally
on narrow screens without hiding columns or violating accessible row headers.

No browser bearer, source-wide unrestricted index, arbitrary resource IDs, IAM
role inference or user-supplied CP principal/tenant filter.

## Server data controls

The CP endpoint reevaluates ACTIVE human CP identity and effective grants,
requiring IAM `canonical:read` and the platform-administrator route gate.
Each candidate must have a current *exact* `organisation.view` grant anchored
on an organisation UUID; platform/group grants never expand subsidiaries.

It reads at most 500 bounded current candidate profiles, filters within the
authorised set, sorts by lowercased display_name + stable UUID tie-breaker
and returns at most 50 rows. No selective query can create authority; a cursor
must refer to an existing *currently authorised and filtered* result or the
page fails with 409. Invalid filter names, duplicates, oversized terms and
foreign cursor IDs fail closed. Sources changing between requests can reorder
pages: this first increment does not claim a durable paginated snapshot.

The query is not an unrestricted, general SQL LIKE search and does not use
full-text search across tenants. The current ≤500 exact-grant cap makes
the first increment safe for staged testing; large installations will need
a permission-joined indexed cursor read model with stable snapshot tokens,
rate limits, query-budget observability and performance proof.

## Acceptance

Go tests: independent permission set; query allowlist; no principal/tenant
filter; overlong query; sort/status/verification filters; stale/foreign cursor
rejection; deterministic results; no truncation.

Frontend tests: exact BFF GET query serialization, cannot select another
principal, no browser bearer, malformed filters, typed response integrity.
Existing UI axe, typecheck, lint, container build and all platform evidence
gates still apply.

Remaining CX-05 programme: cross-domain search, different resource tables,
column preference governance, high-volume read model, bulk operations
with independent permissions, export/redaction policy, and perf testing.
Organisation 360 legal-actor mandate and source-attested readiness are
independent CX-04C dependencies, not implied by this UI.
