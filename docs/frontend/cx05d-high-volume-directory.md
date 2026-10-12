# CX-05D — indexed high-volume caller-scoped organisation directory

The previous `GET /v1/organisations/directory/page` bounded loop remains for
test doubles and compatibility. When a real PostgreSQL `DirectoryIndexReader`
is installed, CP uses a single read-only REPEATABLE READ database transaction
per HTTP request, intersecting all rows and the cursor anchor with the
authenticated caller's **currently effective exact `organisation.view` grants**.

- Supports up to 20,000 individually authorised organisation identifiers per
  request, rather than the old 500-record in-memory fallback. A larger
  authority set fails closed; it is never silently truncated.
- The indexed read model uses case-insensitive name search, verification/state
  filters, deterministic name/UUID ordering, keyset `after` paging and bounded
  page sizes (≤50). A stale or revoked cursor returns conflict.
- Untrusted user principal, tenant, corporate group and arbitrary sort
  selectors remain prohibited. Effective status, delegation, conditions,
  environment, IAM principal and relationship checks happen **before**
  collecting IDs; SQL cannot create grants.
- The count and page see the same PostgreSQL snapshot within one request.
  This is not a persisted cross-request snapshot: concurrent status, name
  or authority changes can make older cursors stale. A signed, persisted
  snapshot contract would be needed for immutable multi-request traversal.
- Add the migration index before deployment and evaluate EXPLAIN/ANALYZE
  with a representative ≥5,000-grant fixture. Local and GitHub unit checks
  are not live staging load evidence.

Export and bulk operations require **separately governed permissions**;
merely having organisation.view never authorises either.
