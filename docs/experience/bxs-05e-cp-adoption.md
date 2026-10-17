# BXS-05E — Control Plane Console Shared Experience adoption

Accepted ADR-SHARED-040 governs presentation, not Control Plane authority.
The approved Shared 0.3.1 publication ([workflow](https://github.com/baobab-platform/shared/actions/runs/39073176845)) and independent SHA512/export verification ([receipt](https://github.com/baobab-platform/shared/actions/runs/39073362603))
precede this immutable-version consumer increment.

## Scope

- The root workspace installs `experience-tokens@0.3.1` and `experience-ui@0.3.1`
  from private GitHub Packages, transitively using `experience-geo-core@0.3.1`, with exact registry-verified SHA512 integrity pins.
- The existing CP design-system showcase alone exercises the Shared
  `StatusLabel` and keyboard-native `Disclosure` in a scoped
  `data-bx-theme=baobab-platform` subtree.
- The design-system route remains unavailable in production: its data
  is illustrative and must never be promoted to live IAM/context decisions.
- CP's production shadcn, Base UI, semantic console tokens, access checks,
  BFF, approval workbenches and rendering remain unchanged.

## Security and reproducibility

GitHub Actions has read-only `packages` permission. The trusted
`setup-node` registry configuration provides a short-lived npm token;
the committed `.npmrc` has **only** a registry URL. Container builds
pass a BuildKit secret consumed from temporary memory-backed npm config;
no token passes via Docker ARG, copied app file, or runtime image ENV.

CI must prove the exact lock, shared-primitive unit/axe tests, React
19.3 Base UI compatibility, Node 24, frontend and Go tests,
image/SBOM/container checks, and Shared Foundation gates before merge.

Thamani remains a separate estate with an independent React/branding
upgrade decision; no company theme is silently adopted from source-observed
design tokens. Production tenant readiness is never inferred from a
prototype badge, disclosure, or other client-side presentation.
