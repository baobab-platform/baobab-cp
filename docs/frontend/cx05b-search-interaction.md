# CX-05B — debounced, URL-synchronised organisation search

The authenticated organisation directory is a native, server-authorised
`GET /workspace/organisations` form. Its search input now progressively
enhances GET submission with a 450-ms idle debounce; Enter and the visible
Search action continue to submit immediately.

- Each submission updates the URL with the existing allowlisted
  `q`, `sort`, `status`, `verification` and `limit` fields.
- The previous `after` cursor is intentionally not included in the
  search form, so new filters cannot silently reuse a stale page cursor.
- Navigation uses Next.js server-rendered results, no browser bearer, no
  direct privileged CP API calls and no localStorage preference authority.
- The input preserves a native label, visible help, keyboard operation,
  server-side safe limit (80 characters) and cleanup of pending debounce
  when removed. With JavaScript disabled, the form remains functional.
- The existing CP directory endpoint continues to recompute exact current
  `organisation.view` access, filtering, sorting and cursor validity
  on every request; no user-supplied principal or tenant filter is accepted.

**Still remaining CX-05C+:** indexed >500-organisation snapshots, additional
resource-domain tables, column preference governance, separately authorised
bulk operations/exports and full browser/performance staging acceptance.
Do not assert completion of the whole CX-05 programme from this UX increment.
