# CX-04B — permission-filtered directory and tenant-owned service projections

**Source authority:** CP ADR-BCP-018/020/026/027. Shared schema and OpenAPI
pinned to `9edcab1c219c83b34a9be688b2fc5605f4288cf7`.

## Completed with this increment

- `GET /v1/organisations/directory` enumerates only actual CP
  organisation profiles named by **the current caller's effective EXACT
  organisation.view grants**. Role+scope and issuer-bound principal remain
  mandatory. The directory has no user-supplied principal, ID or tenant filter.
  An oversized directory fails closed; no group, platform, or applicant
  claims are expanded to other organisations.
- Existing `GET /v1/organisations/{organisation_id}/overview` exposes the
  legal profile, verification provenance (no raw evidence), corporate
  relationships, group membership and platform affiliation.
- A reverse primary-tenancy query uses only effective
  `registry.tenant_organisation_mapping` records, not company names or
  default legal entity coincidence. In addition to the organisation grant,
  each included tenant requires its own current EXACT `tenant.view`
  grant; no sibling, parent or corporate-group implicit read.
- Market participations, ProductSubscriptions and Digital Estates are each
  read only for the selected authorised tenant **and** only when that
  tenant has its own current exact `market.view`,
  `subscription.view` or `digital-estate.view` grant. Distinct
  `markets_visible`, `subscriptions_visible`, and `estates_visible`
  flags let the UI explain permission-denied vs truly empty data.
- All reads have bounded result sizes and fail closed on stale mappings,
  cross-tenant source substitution or incomplete collections. CP remains
  the permission and tenancy authority; frontend only presents information.
- Server-only CP BFF, shadcn/ui cards, Baobab tokens, Lucide icons, a
  protected directory and responsive tenant/service presentation.

## Not yet authorised, implemented or certified

- **Legal-actor mandates:** LA-05/LA-07 require their own governed,
  permission-filtered assessment/reference read model. The current
  mutate-only legal-actor endpoints cannot be treated as public listing.
- **Verified external registration:** source-backed verification must
  remain a recorded CP *verification state*, never an unchecked CIPC
  lookup or a claim that an entity is automatically in good standing.
  The legal entity panel shows source, verified_at and evidence count.
- **Production readiness:** showing observed tenant state, subscribed
  products and marketplaces does not certify provider readiness.
  A dedicated readiness projection needs approved plan digests,
  evidence age, observed drift, status and independent
  `readiness.view` grants.
- **Large organisations:** 100-resource bounds are deliberately fail
  closed. CX-05 will add server-driven bounded pagination/filter/sort
  only after reviewing privacy and stable cursor contracts.
- **Staging:** all overview and directory endpoints are nonproduction
  gated. Their existence and green CI are not AWS, IAM or PSP E2E proof.

Do not start unrestricted CX-05 cross-tenant searches based only on this
directory's exact scope. CX-05 must be a separate scoped API programme.
