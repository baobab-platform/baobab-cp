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

## Emitted asset budgets — initial static safeguard

The frontend CI runs `pnpm --filter @baobab-platform/cp-console perf:budget`
after the production build. The hard limits are aligned with the adopted
ZuriBeans/Thamani static-asset baseline:

| Metric | Maximum |
| --- | ---: |
| Total emitted client JavaScript | 800 KiB |
| Largest client JavaScript chunk | 250 KiB |
| Total emitted CSS | 64 KiB |
| Largest public raster image | 250 KiB |

The executable source of truth is
`frontend/scripts/check-performance-budget.mjs`. Its tests create temporary
production-like directory trees, traverse nested emitted files, exercise
successful and failing command execution, reject missing build output, and
verify that the JSON report survives threshold failures. The report is written
to `frontend/reports/static-budget.json` and retained as a CI artifact,
including on over-budget failures.

**This is an initial aggregate static-asset safeguard, not complete BXS
performance qualification.** It does not attribute downloaded assets to
individual routes, measure runtime transfer or interaction costs, or establish
route-level budgets. ADR-SHARED-040 §4.3 route-level budgets remain outstanding.
The next performance increment must capture route-specific emitted bundle
composition for the public landing route and representative protected Console
journeys, set justified per-route budgets, and retain before/after evidence.
Do not claim full BXS performance adherence from the aggregate gate alone.

Do not raise a limit to hide a regression: first inspect the emitted files and
reduce or split the responsible assets. Any intentional increase requires a
separately reviewed architecture/performance decision with before/after
evidence.

## Context freshness and protected journeys

The protected workspace periodically refreshes its server-rendered authority
snapshot while the session remains open and refreshes again when the tab regains
focus. A context removed from the latest caller-bound snapshot therefore
disappears from the selectable options after refresh. This is a user-experience
freshness measure only: it does not grant or preserve access. Every destination
and operation continues to perform its own current server-side CP/IAM checks;
revocation and session expiry must fail closed even between refreshes.

The browser qualification must use a dedicated non-production authenticated
identity and real caller-bound CP fixtures to verify grant revocation,
suspension, expiry, nested-route persistence, sibling-scope rejection and
destination-side denial. No auth bypass, mocked production grant or
browser-supplied bearer is permitted. Until those authenticated browser tests
run, the revocation experience is improved but not browser-certified.

## Playwright and Lighthouse qualification

Reusable estate practices include:

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

The browser-test increment SHALL add a pinned Playwright toolchain and cover:

1. Public landing: production build, CTA/navigation, no horizontal overflow,
   keyboard operation, axe and visual review at the five viewport widths.
2. CX-03 Console: exact organisation/tenant choices, unavailable authority,
   nested-route persistence, keyboard use, grant revocation/suspension denial
   and session expiry using the dedicated non-production identity.
3. Lighthouse: the public indexable landing route in a separate, single-worker
   throttled lab. Protected Console measurement is allowed only in a controlled
   authenticated non-production environment.
4. Public SEO assertions separately from protected Console noindex policy.
   A noindex route must not be penalised for correctly refusing indexing.

No Lighthouse score is represented as field Core Web Vitals, production
readiness or a service-level guarantee. Runtime Playwright/Lighthouse
qualification remains outstanding until the pinned toolchain and safe
authenticated CI environment are available.

## Lessons carried forward

ZuriBeans' visual regression history demonstrates that a changed screenshot
is not approval; its Lighthouse history demonstrates that competing workers
make throttled scores noisy. Thamani's isolated one-worker Lighthouse run and
static budget script provide a useful pattern, but its route targets and data
must not be copied blindly. Nabhold's published quality direction is
SSR-first, token-based and accessibility/browser-tested; estate-specific draft
or unmerged work is not treated as accepted evidence for CP.

For CX-03, browser evidence must prove behaviour at the protected boundary,
not just a select element. Unit tests remain mandatory for exact-scope grant
filtering, stale/foreign principal rejection, server-side resource reads,
nested path matching, sibling-prefix rejection and accessibility.
