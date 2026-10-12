# CX-04 — source-backed Organisation 360 domain integration

**Authority:** ADR-BCP-018, ADR-BCP-019, ADR-BCP-020, ADR-BCP-026/027.
**Contract:** `shared/contracts/organisation/v1/overview.schema.json`,
`shared/contracts/control-plane/v1/openapi.yaml`, from pinned commit
`2f0d671973154b7c63b4c45ef5011de52e81553b`.

## Shipped source boundary

A read-only `GET /v1/organisations/{organisation_id}/overview` endpoint
assembles the canonical Organisation **profile**, bounded legal-entity
profiles, time-scoped corporate relationships, group memberships and
platform affiliations already persisted in CP's organisation repositories.

- The HTTP route is registered only for explicitly approved nonproduction
  environments and only when the repository and administrative-grant reader
  are configured. Its IAM bearer must carry `canonical:read` and the
  current account must pass the CP platform-administrator gate.
- The handler also resolves the actual CP principal, requires ACTIVE status,
  reads current AdministrativeGrants and source delegation, resolves
  effective relations, and requires an exact `organisation.view` grant for
  the requested organisation (honouring the environment restriction).
- All resource reads are tied to the exact organisation ID. Any foreign
  profile, relationship, membership or platform row causes a generic 503
  instead of a partial, cross-organisation leak. Missing authority/resources
  return indistinguishable generic unavailable responses.
- Each collection is bounded to 100 records; exceeding it fails closed.
  **This is not a paginated directory**. CX-05 requires a separate scoped
  server-paginated read API.
- Raw personal addresses, registration identifiers, evidence references,
  metadata and verifying principal details are removed from the DTO;
  the projection reports only source authority, verification state and
  evidence-count/provenance metadata.
- No CP registration VERIFIED flag is upgraded by frontend logic, and
  a verified legal entity is not equivalent to an accepted legal-actor
  operating mandate.

The Console revalidates the exact organisation scope for every destination
view, then uses the server-only BFF to query the overview; there is no
browser-exposed generic action or bearer token. shadcn/ui and Lucide cards
display verified, pending, conflicted, unknown and unavailable distinctly.

## Outstanding before claiming the *entire* Organisation 360 graph complete

The following remain governed by different sources. They MUST NOT be
fabricated, inferred from a holding-company name, or rendered as zero when
the needed API read is absent:

1. **Authorised tenant discovery**: bounded reverse
   `organisation → tenant` mapping with effective temporal validity, scope,
   and `tenant.view` independent grants (group-level grants may use
   dynamic descendants only with CP relation resolution).
2. **Markets**: market-participation state scoped to actual mapped tenant
   and recorded market, not a global market registry.
3. **Subscriptions**: tenant-scoped canonical ProductSubscription read
   projection, distinct from plan approval/ERP provisioning.
4. **Digital Estates**: tenant-scoped estate catalogues guarded by
   `digital-estate.view` and CP data-residency authority.
5. **Legal actor mandates**: distinct LA-05/LA-07 assessed and accepted
   operating mandates, and approved independently verified CIPC/registry
   evidence. No public generic legal mandate list currently certified.
6. **Provider readiness**: provider observations/plan digest/drift from
   governed rollout, not a green icon inferred from tenant desired state.

See [CX-03/CX-04 scope](cx03-cx04-organisation-360.md); this increment
closes the existing **core organisation source integration** but not
those separate cross-domain read-model contracts.
