# Baobab Platform public website — design, routing and publication gates

**Implementation status:** visual and informational website foundation, not an approved production launch.
**Repository:** `baobab-platform/baobab-cp`
**Decisions:** ADR-BCP-019 (Console), ADR-BCP-026 (progressive onboarding) and ADR-BCP-027 (Organisation-first tenancy).

## Purpose and boundaries

The public landing experience currently resides at `frontend/src/app/page.tsx` and its informational
routes use `frontend/src/app/[slug]/page.tsx`. This implements the requested public website
design inside the current repository without introducing a new runtime package, displacing
`/workspace`, exposing privileged Control Plane APIs or weakening the confidential-client
security boundary.

This is a **transitional co-located public presentation**, not a change to Control Plane domain
ownership. A long-lived independently deployed public website should ultimately be assigned
its own product/deployment authority (and, if appropriate, repository) through an explicit
architecture decision. The Control Plane Console remains **a platform-administration surface,
not a normal Digital Estate**. Do not expose the protected Console as a public website.

The public website has no API fetches, user account creation, contact-data intake or
browser credentials. Its sole Console entry points are standard links to the **existing**
`/workspace` and `/workspace/apply` routes, which retain IAM/BFF protections and
environment gates. When authentication is not enabled, Console access remains unavailable.

## Screens and information architecture

- `/` — full landing page: Azure-inspired mega-navigation, architecture visualization,
  six capability areas, progressive onboarding steps, enterprise autonomy, markets,
  developers, FAQs, and CTA into the existing protected Console.
- `/platform`, `/architecture` — platform model and engine responsibility boundaries.
- `/solutions`, `/industries`, `/markets` — business-centric solution positioning.
- `/developers`, `/documentation` — contracts, engineering and source references.
- `/start`, `/faq` — applicant entry guidance and authoritative caveats.
- `/trust`, `/status` — trust model and explicit **unavailable** status telemetry.
- `/company`, `/resources`, `/contact`, `/accessibility` — corporate and resource foundation.
- `/legal/privacy`, `/legal/terms`, `/legal/cookies` — clearly labelled
  publication placeholders, **not** approved legal instruments.
- `/workspace/**`, `/api/auth/**`, `/api/workspace/**` — existing protected
  Console and BFF. Do not attach site content to these routes.

Copy and IA are centralised in `frontend/src/features/public-site/content.ts`; the
public shell is in `SiteShell.tsx` and visual styles through Tailwind v4 utilities and Baobab semantic tokens. See [UI foundations](tailwind-shadcn.md).

## Inspiration and design

Inspired by Microsoft Azure's systematic product/solution site hierarchy, Stripe's
clear resource/catalogue organisation, Vercel's concise product storytelling and
Cloudflare's visibly separated trust documentation. These are inspiration, not copies.

Brand palette: forest green `#103D2D`, charcoal `#17252C`, ivory `#F7F6F0`
and Nabhold signature gold `#FCA300`. The public styles are deliberately scoped and
do not alter the CP Console's existing accessibility-tested token definitions.

Desktop navigation uses keyboard-operable native `details/summary` disclosures;
mobile navigation preserves all links without external UI dependencies.
The public header and footer are shared across routes.

## Figma Make / Design handoff

Prototype desktop at 1440px, tablet at 768px and mobile at 390px, checking 320px
reflow and 400% zoom. Use exactly the public routes above. The hero diagram is
**illustrative**, not a provider-health telemetry view. Keep the responsive mega-menu,
onboarding timeline, capability cards, trust disclaimer and Console link.
Prototype the protected Console separately from public pages. Do not add mock API
handlers, browser-managed tokens or fabricated legal evidence.

The design exploration should include empty/error states, active navigation,
keyboard focus, reduced motion and content expansion. Do not treat synthetic
Figma examples as actual company registrations or authorised tenant states.

## Required before public production publication

1. **Product/deployment architecture:** Confirm the permanent public-website
   ownership, origin, domain, DNS, TLS, CDN, static/dynamic build and rollback.
   The current co-located implementation does not establish those decisions.
2. **Legal/data protection:** Approve and publish a real privacy notice, website
   terms and cookie inventory from the responsible legal entity. Remove placeholders
   only after review; obtain consent for any future non-essential tracking.
3. **Conversion/contact:** Provide a verified public enquiry address or a
   protected, consent-aware, rate-limited CRM-backed intake route; owners and
   response SLAs must be defined. No fake email or no-op form.
4. **Operations:** Connect a separately trusted public incident/status feed and
   maintenance communications. Never display simulated uptime.
5. **Commercial/content:** Approve pricing, case studies, testimonials, release
   claims, legal actors, contact details and market availability before publication.
6. **Search and analytics:** `app/layout.tsx` intentionally sets
   `robots: noindex` and `app/robots.ts` disallows crawling until legal approval,
   real canonical domain, metadata, sitemap, consent and launch acceptance.
7. **Security:** Test the public-to-protected sign-in transition and verify Console
   redirection, cookie scope, BFF origin, logout, CSRF and IAM revocation.
8. **Quality:** Complete cross-browser and assisted-technology accessibility,
   performance (LCP / CLS / INP), mobile and route-link tests; validate header
   disclosures in production-like environments.
9. **Publishing:** Approve CI preview/deployment workflow, CDN caching strategy,
   content ownership, dependency update and rollback plan before launch.
10. **Readiness:** No homepage button, SEO status or approved marketing copy may
    elevate a tenant, legal verification or provider state to ACTIVE/VERIFIED.

The public website can be prototyped and reviewed now. It must remain no-index and
not be represented as a production-complete or legally approved service until all
publication gates pass.
