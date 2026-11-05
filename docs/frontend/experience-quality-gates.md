# CP Console experience quality gates

**Authority:** ADR-BCP-019, ADR-SHARED-040 and the accepted BXS-05E adoption.
**Scope:** the existing public landing site and protected CP Console. These
checks evolve both surfaces; they do not authorise a rebuild or a design-system
fork.

## Experience-system enforcement

The CP Console SHALL use the Baobab Experience System as a governed foundation:

- Keep the immutable `@baobab-platform/experience-tokens@0.3.1` and
  `@baobab-platform/experience-ui@0.3.1` package pins and their lockfile
  integrity records. Do not silently upgrade, fork or replace them.
- Keep the namespaced BXS token and component styles imported by the root
  layout. Console-owned semantic tokens remain authoritative for Console
  components; BXS styles are additive and scoped.
- Keep component and accessibility coverage in
  `tests/unit/bxs-experience.test.tsx`, and contrast enforcement in
  `tests/unit/tokens.test.ts`. A visual polish change is not accepted by
  bypassing these tests or introducing unscoped brand overrides.
- Presentation never becomes authority: BXS status, disclosure or navigation
  components cannot imply a grant, tenant activation, legal verification,
  readiness or provider certification.

## Emitted asset budgets

The frontend CI now runs `pnpm --filter @baobab-platform/cp-console perf:budget`
after the production build. The hard limits are deliberately aligned with the
already-used ZuriBeans/Thamani estate baseline:

| Metric | Maximum |
| --- | ---: |
| Total emitted client JavaScript | 800 KiB |
| Largest client JavaScript chunk | 250 KiB |
| Total emitted CSS | 64 KiB |
| Largest public raster image | 250 KiB |

The executable source of truth is
`frontend/scripts/check-performance-budget.mjs`; its regression tests ensure
that overages fail and thresholds are not accidentally raised. The report is
written to `frontend/reports/static-budget.json` and retained as a CI artifact,
including when the gate fails. Do not raise a limit to hide a regression:
first inspect the emitted files and reduce or split the responsible assets.
If a future intentional increase is justified, require a separately reviewed
architecture/performance decision with before/after evidence.

## Playwright and Lighthouse qualification

The estate reviews found reusable practices in ZuriBeans and Thamani:

- responsive browser coverage at 320, 390, 768, 1024 and 1440 CSS pixels;
- keyboard and axe/WCAG checks as independent evidence;
- visual snapshots that are reviewed, never auto-approved by a blanket update;
- functional E2E separated from visual, accessibility and throttled Lighthouse
  jobs;
- one isolated Chromium worker for Lighthouse after functional E2E, with
  JSON reports retained as evidence;
- performance >=90, accessibility and best-practices >=95, and CLS <=0.1
  as a starting lab profile; SEO >=95 only for indexable public pages.
  LCP may be recorded for diagnosis but one lab run is not field p75 LCP/INP.

The next browser-test increment SHALL add a pinned Playwright toolchain and
cover both public and protected journeys without weakening security:

1. Public landing: real production build, CTA/navigation, no horizontal
   overflow, keyboard operation, axe, and visual review across the five
   viewport widths.
2. CX-03 Console: use a dedicated non-production authenticated test identity
   and real caller-bound CP fixtures. Verify exact organisation/tenant choices,
   disabled/unavailable authority, nested-route persistence, keyboard use,
   and revocation/suspension denial. Do not mock a grant into production UI or
   use a browser-supplied bearer.
3. Lighthouse: run the public indexable landing route in a separate,
   single-worker throttled lab. The protected Console route may only be
   measured in a controlled authenticated non-production environment; never
   weaken auth or expose a test-only bypass to obtain a score.
4. Keep public SEO assertions distinct from protected Console noindex policy.
   A noindex route must not be penalised for correctly refusing indexing.

No Lighthouse score is represented as field Core Web Vitals, production
readiness or a service-level guarantee. The current PR adds the deterministic
static asset gate; Playwright/Lighthouse runtime qualification remains a
separate next increment because the repository does not yet pin a browser
test toolchain or provide a safe authenticated CI test environment.

## Lessons carried forward

ZuriBeans' visual regression history demonstrates that a changed screenshot
is not approval; its Lighthouse history demonstrates that competing workers
make throttled scores noisy. Thamani's isolated one-worker Lighthouse run and
static budget script provide a good pattern, but its route targets and data
must not be copied blindly. Nabhold's published quality direction is
SSR-first, token-based and accessibility/browser-tested; any estate-specific
draft or unmerged PR is not treated as accepted evidence for CP.

For CX-03, the browser suite must prove behaviour at the protected boundary,
not just a pretty select element. The existing unit tests remain mandatory:
exact-scope grant filtering, stale/foreign principal rejection, server-side
resource reads, nested path matching, sibling-prefix rejection and axe checks.
