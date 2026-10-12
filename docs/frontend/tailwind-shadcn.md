# Tailwind CSS v4 + Lucide React + shadcn/ui — UI foundation

**PR:** baobab-cp #353. **Scope:** public website styling and additive components.
**Authority:** ADR-BCP-019, ADR-BCP-026/027, CP Console FE-02.

## Why this is an additive integration

The CP Console already has owned, accessibility-tested primitives and semantic
CSS variables at `frontend/src/styles/tokens.css`. We retain them. Introducing
shadcn/ui does **not** replace or re-export Console forms and protected commands.
Tailwind v4 now handles the public website layout and presentation components,
and shadcn components are checked-in source under `frontend/src/components/ui`.

- Tailwind v4 uses `frontend/postcss.config.mjs` and `@import "tailwindcss"`
  in `frontend/src/styles/globals.css`. It requires no `tailwind.config.js`.
- `@theme inline` aliases are **references**, not new authoritative colours.
  Primary, border, muted, focus, danger and status semantics resolve from
  existing Console tokens; public brand tokens are added alongside them.
- shadcn/ui is configured in `frontend/components.json`, with `new-york`
  styling, CSS variables, Lucide iconography and the mature **Radix** base.
  Components are owned code, not a black-box `shadcn-ui` runtime dependency.
- Installed source primitives: Button (including Radix Slot asChild),
  Card with title/description, and labelled Badge with explicit CP status.
- `frontend/src/lib/utils.ts` provides the expected `cn` helper.
- Lucide icons are static imports with accessible names supplied by adjacent
  text; decorative icons are marked aria-hidden.
- Existing `@/components` Console controls, CP server-only BFF, protected
  workspace routes and generated API clients are untouched.

## Design and accessibility

Shadcn defaults are adapted to Baobab's established dark forest, Nabhold gold,
neutral surfaces and semantic status palette. Never invent universal
`verified`, `healthy` or `active` state in a UI primitive. The semantics
are in the data received from CP, not in a frontend badge variant.

The public website is server-renderable and uses Tailwind for responsive
layout, spacing, typography, colour utilities, mobile navigation and cards.
The older public-site CSS module has been removed; the Console CSS Modules
remain until a separately tested migration.

Preserve 320px reflow, keyboard disclosures, WCAG 2.2 AA,
reduced-motion behaviour, semantic heading hierarchy and noindex until launch.

## Install and validate

From the **repository root**:

```sh
corepack enable
pnpm install --frozen-lockfile
pnpm --filter @baobab-platform/cp-console typecheck
pnpm --filter @baobab-platform/cp-console lint
pnpm --filter @baobab-platform/cp-console test
pnpm --filter @baobab-platform/cp-console build
```

Dependencies and the lockfile must be committed together; the pnpm
workspace enforces release-age restrictions and frozen-lockfile CI.

For future shadcn components, use the pinned Radix base and review
`shadcn add` changes (tokens, imports, a11y, bundle impact) rather than
running a wholesale template migration. Maintain existing Console
component interfaces until an explicitly scoped replacement PR with tests.

## Security boundaries

- No public website code calls the Go Control Plane or stores auth tokens.
- `/workspace` and `/api/workspace` continue to require the authorised
  IAM/OIDC confidential-client server-side session.
- Neither a button, status badge nor architecture illustration can set
  admission, legal verification, plan approval, provider health or activation.
- Public publication gates still apply (see `public-platform-website.md`).

## Security baseline and UI roll-out

PostCSS is pinned at **8.5.28** in the package manifest and workspace overrides to remove the reported high-severity source-map path traversal vulnerabilities affecting <=8.5.17. New shadcn primitives are used as presentation only; no admin authority changes. The new Console frame provides responsive, visually refined work surfaces using real, existing routes. Role-specific privileges remain with CP/IAM. Do not show fabricated metrics, contexts or green-status indicators.
