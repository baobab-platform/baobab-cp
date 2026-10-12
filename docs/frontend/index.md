# CP Console (frontend)

The CP Console is the administrative frontend defined by ADR-BCP-019. It lives in `frontend/`; see
`frontend/README.md`.

- [Current FE-00 rebaseline (9 October 2026)](fe-00-refresh-2026-10-09.md): present route, API, IAM and gate priorities.
- [Historical FE-00 discovery snapshot (26 September 2026)](fe-00-architecture-lock.md): the route map, authority, readiness and
  contract matrices, the historical gap list, the BFF threat model, risks and the original gate sequence.
- FE-01 foundation: tooling, configuration, image and CI (`frontend/README.md`).
- FE-02 design system: tokens and accessible components (`frontend/README.md`, "Design system").
- [Public platform website foundation and publication gates](public-platform-website.md): website information architecture, Console links, Figma handoff and honest release criteria.

- [Tailwind v4, Lucide and shadcn/ui integration](tailwind-shadcn.md): semantic tokens, primitive ownership, accessibility and migration boundaries.

- [Console experience programme v1](cp-console-experience-programme.md): CX-01–CX-10, architecture boundaries and production acceptance prerequisites.

- [CX-03/CX-04: scoped enterprise context and Organisation 360](cx03-cx04-organisation-360.md): implemented reads, isolation and remaining canonical discovery dependencies.

- [CX-04: governed Organisation 360 source integration](cx04-authoritative-integration.md): legal profiles, corporate/group/platform relationships, explicit remaining domain read contracts.

- [CX-04B exact authority directory and tenant service integration](cx04b-directory-tenants.md): bounded source-backed directory, tenant mappings, markets, subscriptions and Digital Estates.

- [CX-05A: caller-filtered organisation search tables](cx05a-authorised-organisation-table.md): read-only server-side filtering, stable order, cursor paging, accessibility and scale limits.

- [CX-04C: independently governed mandates and readiness](cx04c-mandates-readiness.md): exact permission boundaries, source provenance, freshness and external acceptance limits.

- [CX-03B: caller-bound enterprise navigation context](cx03b-enterprise-context.md): exact organisation and tenant grants, PRIMARY mapping validation, independently visible markets and invalidation semantics.
