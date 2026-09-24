# ADR-BCP-019 — Control Plane Administrative Frontend, Organisation Onboarding Experience and Repository Composition Model

**Status:** Accepted — Normative Platform Architecture  
**Date:** 2026-09-23  
**Decision Owners:** Baobab Platform Architecture  
**Primary Repository:** `baobab-platform/baobab-cp`  
**Runtime Authority:** Baobab Control Plane  
**Canonical Contract Authority:** `baobab-platform/shared`  
**Identity / Authentication Authority:** `baobab-platform/baobab-iam`  
**Frontend Runtime:** Node.js 24 LTS baseline  
**Frontend Framework:** Next.js Active-LTS line / React stable line / TypeScript  
**Backend Runtime:** Existing Go Control Plane — retained  
**Primary Database:** PostgreSQL 17 — Control Plane authoritative state  
**Decision Type:** Control Plane product architecture, administrative user experience, organisation admission and provisioning control surface

**Depends On / Reconciles With:**

- ADR-BCP-001 — Baobab Control Plane Parent Implementation Contract and Derived Artefacts
- ADR-BCP-002 — Capability-Centric Baobab Platform Architecture and Digital Estate Consumption Model
- ADR-BCP-003 — Capability Registry, Grants, Scopes, Bindings and Deterministic Resolution Model
- ADR-BCP-004 — Context, Market, Geography, Legal-Entity and Digital Estate Resolution Model
- ADR-BCP-005 — Product, Capability Composition, Subscription, Entitlement and Digital Estate Provisioning Model
- ADR-BCP-006 — Capability Provider Lifecycle, Engine Topology, Health, Failover and Migration Model
- ADR-BCP-007 — Control Plane APIs, Capability Resolution Contracts, Caching, Resolution Assertions and Service-to-Service Consumption Model
- ADR-BCP-008 — Control Plane Audit, Observability, Reconciliation, Readiness and Operational Governance Model
- ADR-BCP-009 — Capability-Centric Security, Isolation, Residency, Revocation and Failure Semantics
- ADR-BCP-010 — Modular Control Plane Architecture, Governance Boundaries and Evolution Model
- ADR-BCP-011 — Market Participation, Trade Lanes and Cross-Market Trading Model
- ADR-BCP-012 — Intercompany and Inter-Branch Trading, Legal-Entity Relationship and Internal Settlement Model
- ADR-BCP-014 — Canonical Counterparty Identity, Roles and Relationships Model
- ADR-BCP-016 — Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution
- ADR-BCP-017 — Organisation Admission, Subscription Classification and Tenant Onboarding Lifecycle Model
- ADR-BCP-018 — Canonical Organisation, Corporate Group, Platform Account and Tenant Relationship Model
- `BCP-TS-ONBOARDING-001` — Baobab Control Plane Tenant Onboarding & Provisioning Technical Specification
- Applicable Baobab IAM ADRs, especially IAM ADR-0005, ADR-0006, ADR-0008, ADR-0009, ADR-0015, ADR-0016 and ADR-0017
- Applicable contracts from `baobab-platform/shared`

**External Architecture and Security References:**

- RFC 10017 / BCP 212 — OAuth 2.0 for Browser-Based Applications
- RFC 9700 — OAuth 2.0 Security Best Current Practice
- OWASP Application Security Verification Standard 5.0
- WCAG 2.2
- OpenTelemetry semantic conventions
- AWS SaaS Lens — Tenant Onboarding
- AWS Prescriptive Guidance — Multi-product SaaS tenant management using a common control plane
- Keycloak Organisations documentation

---

# 1. Executive Decision

Baobab SHALL provide a first-class **Control Plane Administrative Frontend**, hereafter referred to as the:

```text
Baobab Control Plane Console
```

or simply:

```text
CP Console
```

The CP Console SHALL reside in the **same repository** as the Go Control Plane:

```text
baobab-platform/baobab-cp
```

It SHALL NOT be created as a separate `baobab-console` repository.

The architectural model SHALL be:

```text
                  baobab-platform/baobab-cp
                             │
               ┌─────────────┴─────────────┐
               │                           │
               ▼                           ▼
        CONTROL PLANE API             CP CONSOLE
              Go                     Next.js/React
               │                           │
               │                      Server/BFF
               │                           │
               └─────────────┬─────────────┘
                             │
                     CONTROL PLANE DOMAIN
                             │
                  PostgreSQL / Reconciliation
                             │
             ┌───────────────┼────────────────┐
             ▼               ▼                ▼
          Baobab IAM     Provider/Engine   Infrastructure
                          Integrations       Boundaries
```

The Control Plane backend remains the authoritative source of:

```text
organisation state
tenant state
platform relationships
subscription state
entitlements
capability configuration
desired state
provisioning state
readiness
audit
context resolution
```

The CP Console is the **human control surface** over that authority.

The governing principle is:

> **The frontend explains and operates the Control Plane; it does not become a second Control Plane.**

A second governing principle is:

> **The CP Console SHALL expose platform concepts to non-technical administrators in business language while preserving the exact canonical semantics, authority boundaries and security rules defined by the Control Plane ADRs.**

A third governing principle is:

> **One repository does not imply one runtime, one container or direct code coupling. The Control Plane API and CP Console SHALL remain independently deployable components joined through explicit network contracts.**

---

# 2. Why This Decision Is Required

The Control Plane has matured beyond a machine-only runtime.

Existing architecture now requires human decisions and administrative workflows around:

```text
organisation admission
organisation verification
corporate structure
platform relationship
PlatformAccount configuration
subscription classification
tenant creation
market participation
Digital Estate registration
capability activation
security requirements
residency requirements
isolation
identity administration
provisioning
readiness
suspension
reclassification
offboarding
```

These activities cannot safely remain:

```text
curl commands
SQL statements
manual configuration
developer-only scripts
GitHub operations
direct Keycloak administration
provider-specific administration
```

as Baobab begins serving non-technical administrators and external organisations.

AWS SaaS guidance similarly treats tenant onboarding as a repeatable orchestration process involving multiple platform components and notes that it may be provider-managed or customer-initiated. A common control plane is specifically useful when multiple products require shared tenant lifecycle, security, provisioning and operational management.

Baobab therefore requires a human-facing administrative control plane.

---

# 3. Existing Repository Audit

As of this ADR, `baobab-platform/baobab-cp` is already an established Go repository.

Its important existing structure includes:

```text
baobab-cp/
├── api/
├── cmd/
│   ├── controlplane/
│   └── migrate/
├── internal/
│   ├── auth/
│   ├── config/
│   ├── domain/
│   ├── reconcile/
│   ├── repository/
│   ├── resolver/
│   ├── service/
│   └── store/
├── schemas/
├── docs/
├── scripts/
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── contracts.lock.yaml
```

This layout is already appropriate for the Go application.

This ADR SHALL therefore **not** require:

```text
baobab-cp/backend/
```

merely to make the repository visually symmetrical.

Moving:

```text
api/
cmd/
internal/
```

under `/backend` would create unnecessary:

```text
import churn
Dockerfile changes
workflow changes
documentation churn
tooling churn
path churn
merge conflicts
```

without producing meaningful architectural isolation.

The frontend SHALL instead be introduced as a new peer:

```text
frontend/
```

at repository root.

---

# 4. Refinement of the Existing “No UI” Boundary

Existing Control Plane documentation historically states that `baobab-cp` does not own:

```text
UI / end-user surfaces
```

This ADR refines that statement.

The Control Plane still SHALL NOT own:

```text
ZuriBeans buyer experience
ZuriBeans supplier experience
Thamani consumer experience
ERP operational screens
commerce operational screens
CMS publishing screens
customer self-service commerce UI
supplier commercial workflow UI
ordinary business-domain interfaces
```

However:

```text
Control Plane administration
organisation admission
tenant onboarding
platform configuration
platform readiness
platform governance
```

are inherently Control Plane concerns.

The revised boundary SHALL therefore be:

> **`baobab-cp` does not own business-domain or Digital Estate end-user interfaces. It does own the administrative human interface required to operate the Control Plane itself.**

---

# 5. CP Console Is Not a Digital Estate

The CP Console SHALL NOT be modelled as an ordinary Digital Estate.

A Digital Estate normally represents a legal-entity or business-facing experience that consumes Baobab capabilities.

Examples include:

```text
ZuriBeans Public Estate
ZuriBeans Buyer Portal
ZuriBeans Supplier Portal
Thamani Storefront
Nabhold Corporate Estate
```

The CP Console is different.

It is:

```text
a platform-administration surface
```

attached directly to the Control Plane bounded context.

Conceptually:

```text
                  BAOBAB PLATFORM
                        │
            ┌───────────┴───────────┐
            │                       │
            ▼                       ▼
       CONTROL PLANE            ENGINES
            │                       │
            ▼                       ▼
        CP CONSOLE           Digital Estates
```

This distinction SHALL remain explicit.

---

# 6. Product Boundary

The Control Plane shall now be understood as one product with multiple runtime components:

| Component | Responsibility | Runtime |
|---|---|---|
| CP Domain / API | Authoritative platform state and commands | Go |
| CP Reconciler | Desired-state convergence | Go |
| CP Migration Runner | Database evolution | Go |
| CP Console | Human administration experience | Next.js / React |
| CP Console BFF | Browser session and secure API mediation | Next.js server runtime |
| PostgreSQL | Authoritative Control Plane persistence | PostgreSQL 17 |

The repository boundary is:

```text
one product
one bounded context
one version-control repository
```

while the deployment model is:

```text
multiple independently deployable components
```

---

# 7. Target Repository Structure

The target repository SHALL evolve toward:

```text
baobab-cp/
│
├── api/
│   └── existing Go HTTP/API implementation
│
├── cmd/
│   ├── controlplane/
│   └── migrate/
│
├── internal/
│   ├── auth/
│   ├── config/
│   ├── domain/
│   ├── governance/
│   ├── orchestration/
│   ├── reconcile/
│   ├── repository/
│   ├── resolver/
│   ├── service/
│   └── store/
│
├── frontend/
│   ├── src/
│   │   ├── app/
│   │   │   ├── (auth)/
│   │   │   ├── (applicant)/
│   │   │   ├── (operator)/
│   │   │   ├── (organisation)/
│   │   │   └── api/
│   │   │
│   │   ├── components/
│   │   │   ├── primitives/
│   │   │   ├── forms/
│   │   │   ├── navigation/
│   │   │   ├── feedback/
│   │   │   └── data-display/
│   │   │
│   │   ├── features/
│   │   │   ├── applications/
│   │   │   ├── organisations/
│   │   │   ├── corporate-groups/
│   │   │   ├── platform-accounts/
│   │   │   ├── onboarding/
│   │   │   ├── tenants/
│   │   │   ├── markets/
│   │   │   ├── digital-estates/
│   │   │   ├── subscriptions/
│   │   │   ├── access/
│   │   │   ├── integrations/
│   │   │   ├── readiness/
│   │   │   ├── audit/
│   │   │   └── diagnostics/
│   │   │
│   │   ├── server/
│   │   │   ├── auth/
│   │   │   ├── session/
│   │   │   ├── cp-client/
│   │   │   └── observability/
│   │   │
│   │   ├── generated/
│   │   │   └── control-plane/
│   │   │
│   │   └── lib/
│   │
│   ├── public/
│   ├── tests/
│   │   ├── unit/
│   │   ├── integration/
│   │   └── e2e/
│   ├── package.json
│   ├── tsconfig.json
│   ├── next.config.ts
│   └── Dockerfile
│
├── schemas/
│
├── docs/
│   ├── adr/
│   ├── architecture/
│   ├── frontend/
│   ├── operations/
│   ├── reconciliation/
│   └── security/
│
├── scripts/
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── contracts.lock.yaml
```

The exact frontend subdirectory structure MAY evolve.

The architectural boundaries SHALL NOT.

---

# 8. Frontend Technology Baseline

The initial frontend SHALL use:

| Concern | Decision |
|---|---|
| Language | TypeScript, strict mode |
| UI runtime | React stable |
| Application framework | Next.js Active LTS |
| Router | App Router |
| Node runtime | Node.js 24 LTS |
| Package management | pnpm preferred |
| Rendering strategy | Server-first |
| API access | Server-side typed CP client |
| Browser authentication | BFF-mediated OIDC session |
| Testing | Unit + integration + Playwright |
| Accessibility | WCAG 2.2 AA |
| Observability | OpenTelemetry-compatible |
| Security verification | OWASP ASVS 5.0 risk-based baseline |

Node.js 24 is presently an LTS release, and Next.js 16 is presently the Active-LTS major. Production deployments SHOULD stay within supported LTS lines instead of tracking experimental or canary versions.

The ADR SHALL NOT freeze a specific patch release.

Patch and minor updates SHALL be governed through dependency management, CI and security policy.

---

# 9. Server-First Frontend

The Console SHOULD use server rendering and server-side data access by default.

Conceptually:

```text
Browser
   │
   ▼
Next.js Server
   │
   ▼
CP API
```

Client-side JavaScript SHOULD primarily be used for:

```text
interactive forms
dialogs
progress updates
filters
tables
local UI state
accessible interactive controls
```

rather than making the entire console a browser-only SPA.

This reduces:

```text
browser token exposure
client bundle size
duplicated data-fetch logic
accidental API leakage
```

and produces a cleaner security boundary.

---

# 10. Backend-for-Frontend Authentication Architecture

The CP Console SHALL use a **Backend-for-Frontend architecture** for browser authentication.

RFC 10017 describes the BFF as the strongest of the three principal OAuth architectures for browser-based applications and defines its central property as keeping OAuth access and refresh tokens out of browser application code.

The target flow SHALL be:

```text
┌───────────────────────────────┐
│            Browser            │
│                               │
│  receives secure session only │
│  no OAuth token in JS storage │
└───────────────┬───────────────┘
                │
                ▼
┌───────────────────────────────┐
│       CP Console / BFF        │
│                               │
│ OIDC confidential client      │
│ session management            │
│ API mediation                 │
│ CSRF protection               │
└───────┬────────────────┬──────┘
        │                │
        ▼                ▼
   Baobab IAM        Baobab CP API
    Keycloak               Go
```

The browser SHALL NOT store Baobab privileged access tokens in:

```text
localStorage
sessionStorage
IndexedDB
ordinary JavaScript variables intended for persistence
```

The BFF SHALL use OAuth Authorization Code flow as a confidential client.

RFC 10017 requires a BFF to operate as a confidential client and recommends strongly protected cookies including `Secure` and `HttpOnly`; it also requires appropriate CSRF defenses.

---

# 11. Browser Session Security

Production session cookies SHALL normally use:

```text
Secure
HttpOnly
SameSite=Strict
Path=/
```

and SHALL avoid a broad `Domain` attribute unless a formally reviewed deployment requirement exists.

Session identifiers SHALL be:

```text
opaque
unpredictable
revocable
short enough to limit exposure
```

Browser-visible session state SHALL NOT become the source of authorization truth.

The server SHOULD prefer server-side token custody.

If encrypted client-side session state is ever used, it SHALL undergo explicit security review and SHALL NOT place plaintext OAuth tokens in browser-accessible storage.

---

# 12. CP Remains the Authorization Authority

The BFF SHALL NOT become an authorization authority.

The complete model remains:

```text
Baobab IAM
    │
    │ Who is this?
    ▼
CP Console BFF
    │
    │ Secure session / request mediation
    ▼
Baobab Control Plane
    │
    │ In what platform context is this permitted?
    ▼
Control Plane command/query
```

The BFF MAY hide or disable controls for usability.

However:

> **A hidden button is not an authorization control.**

The Go API SHALL independently authorize every consequential operation.

---

# 13. User Classes

The CP Console SHALL support distinct administrative experiences.

| Actor | Purpose |
|---|---|
| Applicant | Applies for organisation admission |
| Applicant Collaborator | Contributes evidence or application data where explicitly invited |
| Admission Reviewer | Reviews organisation applications |
| Platform Operator | Operates organisation/tenant lifecycle |
| Platform Approver | Approves high-impact changes |
| Security Administrator | Handles security-sensitive platform settings |
| Organisation Administrator | Manages authorised organisation-level platform configuration |
| Organisation Auditor | Read-only access to permitted audit/readiness data |
| Support Operator | Limited support and diagnostics |
| Platform Auditor | Cross-platform read-only audit capability |

These roles are conceptual.

Actual authorization SHALL remain capability/context based and SHALL follow Baobab IAM and CP authorization ADRs.

---

# 14. Three User Experiences in One Console

The CP Console SHALL support three principal experiences.

```text
                        CP CONSOLE
                            │
          ┌─────────────────┼─────────────────┐
          │                 │                 │
          ▼                 ▼                 ▼
      APPLICANT          PLATFORM        ORGANISATION
      WORKSPACE          OPERATIONS       ADMINISTRATION
```

## 14.1 Applicant Workspace

Used before platform admission.

Typical functions:

```text
create application
save draft
enter organisation details
submit evidence
define intended operations
respond to information requests
submit application
view application status
withdraw application
```

## 14.2 Platform Operations Workspace

Used by Baobab personnel.

Typical functions:

```text
review applications
verify organisation information
classify platform relationship
associate corporate structures
assign PlatformAccount
approve/reject admission
review onboarding plans
approve production activation
monitor provisioning
inspect readiness
manage lifecycle
investigate drift
suspend/decommission
```

## 14.3 Organisation Administration Workspace

Used after activation by delegated customer administrators.

Typical functions:

```text
view organisation profile
manage authorised organisation administrators
view tenants
view subscribed services
request approved changes
manage permitted domains
view market configuration
view Digital Estates
configure permitted integrations
review readiness
review audit/activity
```

These experiences SHALL use the same application codebase but SHALL NOT expose the same privileges.

---

# 15. Business Language SHALL Lead the UX

The CP Console is intended for non-technical administrators.

Therefore implementation terminology SHALL be progressively disclosed.

For example:

| User-Facing Term | Internal Platform Concept |
|---|---|
| Organisation | Organisation / CanonicalEntity |
| Corporate structure | CorporateRelationship / CorporateGroup |
| Baobab account | PlatformAccount |
| Environment | Tenant/environment context |
| Services | ProductSubscription / CapabilityComposition |
| Available functions | CapabilityGrant |
| Service provider | CapabilityProvider |
| Runtime | Engine / EngineInstance |
| Markets | MarketParticipation |
| Website / portal | DigitalEstate |
| Data-location requirement | DataResidencyPolicy |
| Security isolation | IsolationProfile |
| Setup progress | Provisioning / Reconciliation / Readiness |
| Platform issue | Drift / readiness failure |

The UI SHALL NOT ordinarily ask:

```text
Select CapabilityBinding
Select EngineInstance UUID
Choose MappingScope
Choose database schema
Choose provider topology
```

unless the user is an authorised advanced platform operator.

---

# 16. Progressive Disclosure

The Console SHALL provide at least three information depths:

```text
Level 1 — Business View
Level 2 — Administrative Detail
Level 3 — Platform Diagnostics
```

Example:

```text
BUSINESS VIEW
Commerce is ready.

ADMINISTRATIVE DETAIL
Trade service is active for South Africa.

PLATFORM DIAGNOSTICS
capability = commerce.order.manage
binding = ...
provider = baobab-trade
engine_instance = ...
readiness = READY
```

This permits non-technical users and platform engineers to use the same product without forcing one mental model onto the other.

---

# 17. Organisation Admission Is a Workflow, Not CRUD

ADR-BCP-017 established that:

```text
registration
application
admission
subscription
tenant provisioning
authorization
```

are distinct.

The Console SHALL preserve those distinctions.

The canonical business process SHALL be presented as:

```text
Applicant Registration
        │
        ▼
Start Application
        │
        ▼
Organisation Information
        │
        ▼
Legal / Corporate Evidence
        │
        ▼
Operational Requirements
        │
        ▼
Submit
        │
        ▼
Admission Review
        │
   ┌────┴─────┐
   │          │
REJECTED    APPROVED
              │
              ▼
      Platform Classification
              │
              ▼
      Onboarding Configuration
              │
              ▼
      Provisioning Plan
              │
              ▼
         Approval
              │
              ▼
           Apply
              │
              ▼
       Reconciliation
              │
              ▼
        Readiness Check
              │
              ▼
         Activation
              │
              ▼
      Organisation Handover
```

---

# 18. Applicant Registration

Registration SHALL remain an IAM concern.

The CP Console SHALL redirect or integrate with Baobab IAM for identity creation and authentication.

The Console SHALL NOT:

```text
store passwords
validate passwords itself
implement MFA itself
implement passkeys itself
become another identity provider
```

Keycloak remains credential authority.

Keycloak's organisation facilities may manage organisation-linked identity membership and invitations, but an IAM Organization remains distinct from Baobab's canonical Organisation and Tenant models. Keycloak itself supports realm- and organisation-level administration of organisation memberships, invitations and organisation-specific authentication context.

---

# 19. Organisation Identity Capture

The organisation onboarding UX SHALL support:

```text
official name
display/trading name
organisation form
jurisdiction
registration identifiers
registered address
principal operating locations
contact information
website/domain information
legal evidence
tax or regulatory identifiers where required
```

Fields SHALL be:

```text
jurisdiction-aware
schema-driven where practical
validated server-side
auditable
```

The frontend SHALL NOT invent legal truth.

Information provided by an applicant SHALL initially remain:

```text
application evidence
```

until admitted and promoted into canonical platform state according to ADR-BCP-017 and ADR-BCP-018.

---

# 20. Corporate Structure

The onboarding experience SHALL support both simple and complex organisations.

Simple:

```text
Acme Ltd
```

Complex:

```text
Acme Holdings
├── Acme Foods
├── Acme Logistics
├── Acme Uganda
└── Acme South Africa
```

The UI SHALL allow administrators to describe:

```text
parent
subsidiary
affiliate
controlled entity
branch where applicable
```

without implying authorization.

The UI SHALL visibly reinforce:

```text
Corporate relationship
        !=
data-access relationship
```

and:

```text
Common parent
        !=
shared tenant access
```

This is particularly important for external customers with their own subsidiaries.

---

# 21. Platform Relationship and Platform Account

After organisation identity is established, authorised Baobab personnel SHALL classify:

```text
PlatformRelationship
PlatformAccount
```

separately from corporate ownership.

The user experience may display this as:

```text
Relationship to Baobab
Account arrangement
Covered organisations
```

The model SHALL support:

```text
Nabhold internal entity
external customer
partner
managed entity
future affiliate classes
```

without special-casing Nabhold in application logic.

---

# 22. Requirement Discovery

The Console SHALL gather business requirements before producing technical configuration.

Typical business questions may include:

```text
Which countries will you operate in?

Which legal entities will participate?

Which Baobab services do you require?

Do you need commerce, ERP, CMS or intelligence capabilities?

Which websites or portals will consume those services?

Do you have data residency requirements?

Do contractual obligations require dedicated isolation?

Do you require sandbox/UAT before production?

Which administrators should initially be authorised?

Which identity federation requirements exist?

Which integration endpoints are required?
```

The user SHALL describe needs.

The Control Plane SHALL derive technical consequences.

---

# 23. Service Selection

A non-technical administrator SHOULD select:

```text
products
services
business capabilities
```

rather than low-level capability bindings.

Example:

```text
☑ B2B Trade
☑ ERP
☑ Content Management
☑ Intelligence
```

The CP then derives:

```text
ProductSubscription
        │
        ▼
CapabilityComposition
        │
        ▼
CapabilityGrant
        │
        ▼
CapabilityBinding
        │
        ▼
Provider
        │
        ▼
EngineInstance
```

The frontend SHALL not duplicate this derivation.

---

# 24. Market Participation

Market selection SHALL capture business intent:

```text
South Africa
Uganda
Kenya
...
```

but SHALL NOT directly determine:

```text
legal entity
deployment region
data residency
currency authority
tax treatment
```

The UI SHALL preserve the distinctions defined by ADR-BCP-004 and ADR-BCP-011.

The administrator may answer:

```text
"We need to trade in Uganda."
```

The Control Plane determines which additional canonical configuration is required.

---

# 25. Digital Estate Registration

The Console SHALL provide a guided mechanism for authorised administrators to register Digital Estates.

Example:

```text
Name: Acme Buyer Portal
Type: B2B Portal
Owner: Acme Foods Ltd
Markets: Uganda, South Africa
Domains: buyer.acme.example
Required Services:
  - Trade
  - ERP document access
```

The Console SHALL NOT treat a Digital Estate as a Tenant merely because the estate belongs to one organisation.

---

# 26. Security, Residency and Isolation Requirements

The user experience SHALL ask business/compliance questions rather than infrastructure questions.

Preferred:

```text
"Must data remain within a particular jurisdiction?"
```

rather than:

```text
"Select PostgreSQL cluster."
```

Preferred:

```text
"Does your contract require dedicated data isolation?"
```

rather than:

```text
"Choose schema-per-tenant."
```

The Control Plane SHALL map requirements to:

```text
DataResidencyPolicy
IsolationProfile
provider eligibility
region eligibility
```

according to platform policy.

---

# 27. Initial Administrator Establishment

Organisation onboarding SHALL explicitly establish initial administrative authority.

The process SHALL answer:

```text
Who is the initial Organisation Administrator?

Was that authority verified?

Which organisation may that administrator manage?

Which tenants are included?

What administrative capabilities are granted?

Is stronger authentication required?
```

The applicant SHALL NOT automatically become the permanent organisation administrator simply because that person filed the application.

The reviewer/approval process SHALL establish administrative authority explicitly.

---

# 28. Provisioning Plan Preview

Before consequential provisioning, the Console SHALL present a human-readable plan.

Example:

```text
Organisation:
Acme Holdings Ltd

Tenants to create:
2

Markets:
South Africa
Uganda

Services:
Trade
ERP
Pulse

Digital Estates:
Buyer Portal
Supplier Portal

Identity:
1 initial organisation administrator

Security:
Dedicated database isolation
South Africa data-residency requirement

Changes:
14 resources will be created
3 provider configurations will be established
2 IAM organisation projections will be configured
```

The same screen MAY offer a technical expansion panel for authorised operators.

---

# 29. Plans and Changesets

The frontend SHALL use Control Plane plan/change abstractions rather than issuing a chain of unrelated mutations.

Preferred:

```text
User Intent
    │
    ▼
Changeset
    │
    ▼
Validation
    │
    ▼
Impact Analysis
    │
    ▼
Plan
    │
    ▼
Approval
    │
    ▼
Apply
```

Not:

```text
Create tenant
Create grant
Create binding
Create engine mapping
Create market
Create IAM link
hope nothing fails
```

The plan is the unit of understanding.

The changeset is the unit of controlled mutation.

---

# 30. High-Impact Approval

Certain operations SHOULD require stronger controls and MAY require maker-checker approval.

Examples include:

| Operation | Recommended Control |
|---|---|
| Production tenant activation | Approval |
| Tenant decommissioning | Two-step approval |
| Isolation downgrade | Security approval |
| Residency policy relaxation | Security/compliance approval |
| Initial privileged admin grant | Approval |
| Corporate ownership reassignment | Approval |
| PlatformAccount reassignment | Approval |
| Cross-organisation access change | Strong approval |
| Bulk entitlement changes | Impact review |
| Provider migration | Explicit plan approval |
| Emergency override | Break-glass procedure |

OWASP ASVS 5.0 includes multi-user approval for high-value business logic among its highest-assurance controls.

The exact approval matrix SHALL remain policy-driven.

---

# 31. Provisioning Is Asynchronous

Provisioning SHALL be treated as a potentially long-running operation.

The browser SHALL NOT rely on:

```text
one HTTP request remaining open
until the entire platform is provisioned
```

Instead:

```text
Submit
  │
  ▼
Operation Accepted
  │
  ▼
Provisioning Operation ID
  │
  ▼
Progress / Events
  │
  ├── IAM configuration
  ├── provider configuration
  ├── mappings
  ├── grants
  ├── bindings
  ├── reconciliation
  └── readiness
  │
  ▼
Final State
```

Initial implementations MAY use controlled polling.

Server-Sent Events or another suitable streaming mechanism MAY be adopted later.

A message broker SHALL NOT be introduced merely to make the frontend appear real-time.

---

# 32. Provisioning States

The user-facing progression SHOULD include:

```text
Draft
Submitted
Under Review
Approved
Planning
Awaiting Approval
Provisioning
Verifying
Ready
Active
Degraded
Blocked
Suspended
Offboarding
Decommissioned
```

These SHALL map to canonical backend state.

The frontend SHALL NOT independently infer lifecycle state.

---

# 33. Readiness Before Activation

The Console SHALL not equate:

```text
provisioning command succeeded
```

with:

```text
tenant is ready
```

Activation SHALL occur only according to backend readiness policy.

Conceptually:

```text
Provision
    │
    ▼
Observe
    │
    ▼
Reconcile
    │
    ▼
Evaluate Readiness
    │
    ├── BLOCKED ──► Remediate
    │
    ├── DEGRADED ─► Review policy
    │
    └── READY
          │
          ▼
        ACTIVE
```

---

# 34. Post-Activation Organisation Dashboard

After activation, an Organisation Administrator SHOULD land on a plain-language summary.

Conceptually:

```text
┌────────────────────────────────────────────────┐
│ ACME HOLDINGS                                  │
│ Active                                         │
├────────────────────────────────────────────────┤
│ Organisation                                  │
│ ✓ Verified                                     │
│                                                │
│ Markets                                        │
│ South Africa     Active                        │
│ Uganda           Active                        │
│                                                │
│ Services                                       │
│ Trade            Ready                         │
│ ERP              Ready                         │
│ Pulse            Ready                         │
│                                                │
│ Digital Estates                                │
│ 3 active                                       │
│                                                │
│ Administrators                                 │
│ 4 active · 1 invitation pending                │
│                                                │
│ Platform Readiness                             │
│ All required services ready                    │
└────────────────────────────────────────────────┘
```

Technical details remain available through drill-down.

---

# 35. Control Plane Console Information Architecture

The long-term navigation model SHOULD converge around:

| Area | Purpose |
|---|---|
| Home | Current work, alerts, readiness |
| Applications | Admission workflow |
| Organisations | Canonical organisation administration |
| Corporate Structure | Organisation relationships |
| Platform Accounts | Commercial/administrative grouping |
| Tenants | Isolation/consumption boundaries |
| Markets | Participation and geography |
| Services | Products/subscriptions |
| Digital Estates | Estate registration and readiness |
| People & Access | Delegated administrative relationships |
| Integrations | Platform integrations and credential references |
| Provisioning | Plans, changesets and operations |
| Readiness | Current operational readiness |
| Activity | Audit history |
| Diagnostics | Advanced platform-level detail |
| Settings | Permitted Control Plane configuration |

Different actors SHALL receive different navigation.

---

# 36. Organisation Administration Is Not Domain Administration

The Console SHALL NOT become:

```text
ERP administration
Trade administration
CMS administration
warehouse administration
accounting administration
order administration
supplier commercial administration
```

For example, an organisation administrator MAY see:

```text
ERP service = READY
```

but SHALL not post:

```text
journal entry
```

from the CP Console.

They MAY see:

```text
Trade service = ACTIVE
```

but SHALL not edit:

```text
product price
```

through the CP Console.

Those actions remain in the owning engine or Digital Estate.

---

# 37. Keycloak Administration Boundary

The CP Console SHALL similarly avoid becoming a generic Keycloak Admin Console.

Keycloak remains responsible for:

```text
authentication flows
credentials
passkeys
MFA
identity federation
realm mechanics
identity sessions
```

The CP Console MAY orchestrate Baobab-owned identity workflows through supported Baobab IAM contracts.

It SHALL NOT require non-technical organisation administrators to understand:

```text
Keycloak realm internals
client scopes
protocol mappers
authentication executions
realm management
```

Keycloak already provides organisation membership and invitation capabilities. Baobab should consume or orchestrate them where consistent with its IAM ADRs rather than recreating a second identity system.

---

# 38. Typed Contract Boundary

The frontend SHALL NOT maintain handwritten duplicates of Control Plane API models where those models are canonically defined in Baobab contracts.

The preferred chain is:

```text
baobab-platform/shared
       │
       │ canonical OpenAPI / schemas
       ▼
contracts.lock.yaml
       │
       ▼
baobab-cp implementation
       │
       └──────────────┐
                      ▼
            generated frontend types/client
```

Generated artifacts MAY live under:

```text
frontend/src/generated/control-plane/
```

They SHALL be reproducible.

They SHALL NOT be manually edited.

CI SHALL detect contract drift.

---

# 39. Same Repository Does Not Permit Internal Go Coupling

The following is prohibited:

```text
frontend imports Go internal/domain files
frontend reads Go database models
frontend reads SQL migrations to infer state
frontend calls PostgreSQL directly
```

The architectural boundary remains:

```text
Frontend
   │
   ▼
Network Contract
   │
   ▼
Go Control Plane
```

The repository is shared.

The runtime abstraction remains explicit.

---

# 40. No Direct Database Access

The frontend and its BFF SHALL NEVER query the Control Plane PostgreSQL database directly.

Prohibited:

```text
Browser → PostgreSQL

Next.js → PostgreSQL CP domain tables
```

Required:

```text
Browser
   │
   ▼
Console/BFF
   │
   ▼
CP API
   │
   ▼
Application/Domain Service
   │
   ▼
Repository
   │
   ▼
PostgreSQL
```

This rule protects:

```text
authorization
audit
business invariants
idempotency
versioning
future storage evolution
```

---

# 41. Purpose-Built Administrative APIs

The Go Control Plane MAY introduce purpose-built administrative query and command endpoints where existing runtime APIs are unsuitable for human administration.

Examples:

```text
ApplicationSummary
OrganisationSummary
OnboardingPlan
ProvisioningProgress
ReadinessSummary
ImpactAnalysis
AuditTimeline
```

These SHOULD be read models or commands over existing domain authority.

They SHALL NOT duplicate domain logic inside frontend code.

The API MAY therefore distinguish:

```text
runtime/workload APIs
administrative APIs
```

without creating a second Control Plane.

---

# 42. No Generic Proxy

The BFF SHALL NOT expose a generic:

```text
/proxy?url=...
```

or arbitrary pass-through mechanism.

Every BFF-accessible operation SHALL be explicitly bounded.

This prevents the frontend tier from becoming:

```text
an accidental SSRF gateway
an authorization bypass
an uncontrolled API gateway
```

---

# 43. Same-Origin Preference

The preferred production topology is:

```text
https://control.baobab.example/
```

with the browser communicating only with the Console origin.

Conceptually:

```text
Internet
   │
   ▼
control.baobab.example
   │
   ▼
CP Console / BFF
   │
   ├── OIDC ───────────► Baobab IAM
   │
   └── private API ────► baobab-cp Go service
```

This reduces:

```text
CORS complexity
token exposure
browser API surface
cross-origin configuration
```

The Go CP service MAY continue exposing separate service APIs through trusted infrastructure for workload clients.

---

# 44. Multi-Tenant Security

The frontend SHALL NOT determine tenant authorization merely because the browser supplies:

```text
tenant_id
organisation_id
market_id
estate_id
```

Such identifiers are selectors.

They are not proof of authority.

The CP backend SHALL validate requested context against:

```text
principal
membership
grants
platform relationships
tenant
legal entity
market
estate
policy
```

according to existing ADRs.

---

# 45. Context Visibility

Because cross-organisation mistakes are dangerous, every administrative screen operating within organisational context SHOULD visibly display:

```text
Organisation
Tenant
Environment
Market where relevant
```

Example:

```text
ACME HOLDINGS
Production
South Africa
```

Dangerous operations SHOULD repeat the effective context in the confirmation screen.

---

# 46. Cross-Organisation Navigation

Platform operators who can access multiple organisations SHALL not experience silent context switching.

Changing:

```text
Organisation A
        ↓
Organisation B
```

SHOULD be explicit.

Unsaved state SHALL not silently transfer across context.

The UI SHOULD clear context-specific caches when switching organisations.

---

# 47. Authenticated Data Caching

Authenticated Control Plane information SHALL be:

```text
private by default
not publicly cacheable
not shared across users
not shared across tenants
```

Framework caching SHALL be explicitly reviewed.

Sensitive server-rendered pages and administrative API responses SHOULD default to:

```text
no-store
```

or an equivalently safe scoped strategy unless a specific cache has been proven context-safe.

A generic framework cache SHALL never become an accidental cross-tenant data channel.

---

# 48. Optimistic Concurrency

Administrative configuration is subject to concurrent modification.

The Control Plane APIs SHOULD support version-aware mutation using mechanisms such as:

```text
resource version
ETag / If-Match
changeset version
expected state
```

The frontend SHALL detect stale updates.

It SHOULD present:

```text
"This configuration changed after you opened it."
```

rather than silently overwriting another administrator's work.

---

# 49. Idempotency

Consequential commands SHALL support idempotency where applicable.

Example:

```text
POST ActivateTenant
Idempotency-Key: ...
```

Retrying due to:

```text
browser refresh
network timeout
proxy retry
user double-click
```

SHALL NOT create duplicate tenants, grants, subscriptions or provisioning jobs.

Buttons initiating consequential commands SHOULD enter a clear pending state immediately.

---

# 50. Draft Persistence

Long onboarding flows SHALL support resumable drafts.

Draft state SHOULD be persisted server-side against the appropriate aggregate:

```text
ClientApplication
OnboardingRequest
Changeset
```

Sensitive onboarding data SHALL NOT depend solely on browser storage.

The user SHOULD be able to:

```text
save
leave
return later
continue safely
```

---

# 51. Evidence and File Uploads

Organisation admission may require documentation.

The Control Plane aggregate SHALL continue storing:

```text
evidence references
```

rather than arbitrary document binaries.

Where uploads are supported:

```text
Browser
   │
   ▼
Controlled upload workflow
   │
   ▼
Object/document storage
   │
   ├── validation
   ├── malware scanning where appropriate
   ├── access controls
   └── retention policy
   │
   ▼
EvidenceReference in CP
```

Secrets or evidence contents SHALL not appear in logs.

---

# 52. Integration Credentials

The Console MAY support secure configuration of integration credentials.

However:

```text
secret value
```

SHALL NOT become ordinary frontend state.

The preferred model is:

```text
secret submitted securely
        │
        ▼
approved secret-management boundary
        │
        ▼
SecretReference
        │
        ▼
Control Plane configuration
```

After submission, secrets SHOULD normally be masked and not recoverable through the UI.

---

# 53. Destructive Operations

Destructive operations SHALL require strong friction proportional to consequence.

Examples:

```text
decommission tenant
remove organisation
revoke privileged administrator
remove production market
disable mandatory service
```

Controls MAY include:

```text
impact preview
step-up authentication
typed confirmation
maker-checker approval
cooldown
recovery period where technically possible
```

A decorative modal with only:

```text
"Are you sure?"
```

is insufficient for high-impact operations.

---

# 54. Step-Up Authentication

Sensitive operations SHOULD be capable of requiring stronger authentication.

Potential triggers include:

```text
production activation
privileged access grant
security policy reduction
residency change
tenant decommissioning
break-glass operation
```

The Console SHALL rely on Baobab IAM assurance mechanisms.

It SHALL NOT implement an independent MFA challenge system.

---

# 55. Support Access and Impersonation

The platform SHALL NOT introduce opaque administrator impersonation.

If future support requirements justify delegated support sessions, such functionality SHALL require:

```text
explicit authority
limited scope
time limit
visible support indicator
audit trail
reason
appropriate approval
```

Where feasible, support personnel SHOULD inspect diagnostic/read models instead of impersonating customer administrators.

---

# 56. Audit

Every consequential action initiated through the Console SHALL produce backend audit evidence consistent with ADR-BCP-008.

An audit event SHOULD make it possible to determine:

```text
who
did what
to which resource
under which context
using which client
when
from which request/correlation chain
with what result
under which approval
```

The frontend MAY display the audit history.

The frontend SHALL NOT be the authoritative audit store.

---

# 57. Correlation

A user-visible operation SHOULD carry a stable correlation identifier.

Example:

```text
Onboarding operation ONB-2026-000451
Correlation: 01K...
```

If an error occurs, the Console SHOULD show a safe support reference.

Example:

```text
We could not complete this step.

Reference:
01KAB...

No changes were applied.
```

The browser SHALL NOT display internal stack traces.

---

# 58. Observability

The Console and BFF SHOULD participate in platform observability.

Telemetry MAY include:

```text
page/request latency
BFF latency
CP API latency
failed commands
onboarding completion time
provisioning duration
readiness failure frequency
frontend JavaScript failures
Web Vitals
```

OpenTelemetry provides common semantic conventions across traces, metrics and logs and includes browser-oriented semantic conventions.

Telemetry SHALL NOT include:

```text
passwords
tokens
secret values
uploaded evidence content
sensitive identifiers without justified need
```

---

# 59. User-Facing Operational Status

The Console SHALL distinguish:

```text
business lifecycle state
```

from:

```text
technical readiness
```

Example:

```text
Organisation: ACTIVE
Trade capability: READY
ERP capability: DEGRADED
```

The system SHALL not collapse these into one green/red badge.

---

# 60. Drift Visibility

ADR-BCP-008 defines drift as a first-class platform concept.

The Console SHALL provide appropriate drift visibility to platform operators.

Conceptually:

```text
Expected:
Trade service active

Observed:
Provider configuration missing

Result:
DRIFT

Severity:
DEGRADED

Action:
Reconcile
```

Organisation administrators MAY receive a simpler interpretation where detailed infrastructure data is inappropriate.

---

# 61. Explainability

Where the Control Plane blocks an operation, the Console SHOULD display a useful explanation.

Instead of:

```text
403
```

prefer:

```text
This organisation cannot activate the ERP service in Uganda yet.

Reason:
Required data-residency configuration has not been approved.
```

Technical diagnostics MAY add:

```text
RESIDENCY_POLICY_UNSATISFIED
```

for authorised operators.

---

# 62. Error Contract

The frontend SHALL consume structured error responses.

Error handling SHALL distinguish:

```text
validation error
authentication required
authorization denied
conflict
policy failure
not ready
dependency unavailable
rate limited
internal failure
```

User messages SHALL be plain-language.

Logs and operator diagnostics MAY contain deeper technical context.

---

# 63. Accessibility

The CP Console SHALL target:

```text
WCAG 2.2 Level AA
```

W3C defines WCAG as applying to web sites, web applications and other digital content, with AA incorporating both A and AA requirements.

Accessibility SHALL be treated as part of correctness rather than visual polish.

Mandatory design considerations include:

```text
keyboard operation
visible focus
semantic HTML
screen-reader labels
error association
non-colour-only status
sufficient contrast
accessible tables
accessible dialogs
predictable navigation
reduced-motion consideration
logical headings
```

---

# 64. Accessibility Testing

Automated accessibility testing SHOULD be integrated into end-to-end tests.

Playwright supports automated accessibility scanning with axe; its documentation correctly notes that automated testing cannot identify every accessibility problem, so manual and inclusive testing remain necessary.

The quality model SHALL therefore include:

```text
automated checks
+
keyboard testing
+
screen-reader testing
+
manual review
```

---

# 65. Responsive Design

The Console is an administrative application and will primarily be used on desktop and laptop screens.

However, core functions SHALL remain usable on smaller screens.

Mobile support SHALL NOT mean compressing wide administrative tables until they are unreadable.

Responsive patterns SHOULD include:

```text
stacked detail views
card alternatives
horizontal table containment
progressive disclosure
responsive navigation
```

No critical approval or security information SHALL disappear solely because the screen is narrow.

---

# 66. Internationalisation

The initial interface MAY launch in English.

The architecture SHALL nevertheless be internationalisation-ready from the beginning.

Avoid hard-coding:

```text
date formats
number formats
currency formats
country names
timezone assumptions
plural rules
```

User-facing text SHOULD use message keys or another maintainable localisation strategy.

Market-specific business terminology MAY later be localised without altering canonical backend concepts.

---

# 67. Timezones

The Control Plane SHALL store canonical timestamps independently of display timezone.

The frontend SHOULD present:

```text
user-local timezone
```

or explicitly configured organisation/platform timezone while preserving access to the canonical timestamp where appropriate.

Approval/audit screens SHOULD make timezone context unambiguous.

---

# 68. Security Verification Baseline

The CP Console is a privileged administrative surface.

OWASP ASVS 5.0 SHALL be used as a security verification framework. OWASP describes ASVS as a basis for testing web application security controls and secure-development requirements.

The initial goal SHOULD be:

```text
ASVS Level 2 baseline
```

with selected Level 3 controls applied to:

```text
high-value provisioning
production activation
destructive lifecycle actions
privileged-access changes
security-policy changes
```

This risk-based approach avoids pretending every screen has equal criticality.

---

# 69. Browser Security

The Console SHALL adopt strong browser defenses including, as appropriate:

```text
HTTPS only
HSTS
strict Content Security Policy
frame-ancestors restrictions
anti-clickjacking
secure cookies
CSRF protection
referrer policy
permissions policy
MIME sniffing protection
dependency integrity controls
```

Third-party JavaScript SHOULD be minimised.

Advertising, consumer analytics trackers and unrelated third-party widgets SHALL NOT be included in the privileged Console.

---

# 70. Content Security Policy

The application SHOULD target a strict CSP.

The architecture SHOULD avoid designs requiring:

```text
unsafe-eval
unrestricted script-src
arbitrary inline scripts
uncontrolled remote script hosts
```

Any CSP relaxation SHALL be documented and security-reviewed.

---

# 71. XSS Risk

The frontend SHALL treat organisation names, evidence metadata, URLs, descriptions and administrator-provided text as untrusted input.

It SHALL:

```text
escape output by default
avoid unsafe HTML injection
sanitise any deliberately rendered rich text
validate URLs
restrict dangerous protocols
```

No administrative privilege makes user-provided content trustworthy.

---

# 72. CSRF

Because the BFF uses cookie-based session semantics, CSRF SHALL be explicitly addressed.

RFC 10017 requires BFF deployments to implement appropriate CSRF defenses.

Controls SHALL include suitable combinations of:

```text
SameSite cookie policy
origin validation
CSRF tokens where appropriate
same-origin API design
strict CORS
unsafe-method protections
```

---

# 73. Rate Limiting and Anti-Automation

Administrative endpoints SHALL have appropriate abuse protection.

Particular protection SHOULD apply to:

```text
login initiation
application creation
evidence upload
invitations
provisioning requests
expensive plan generation
search/export
```

Protection SHALL distinguish legitimate administrative automation from abuse.

---

# 74. Audit Export and Data Export

Export functions MAY become necessary for:

```text
audits
compliance
support
customer governance
```

Exports SHALL be:

```text
authorised
scoped
audited
rate-controlled
```

Large exports SHOULD run asynchronously.

The Console SHALL not silently export data for tenants outside the caller's authorised scope.

---

# 75. Frontend Design System

The CP Console SHALL own its design tokens and reusable UI primitives inside the repository.

The design system SHOULD provide:

```text
typography
spacing
layout
forms
buttons
tables
badges
alerts
dialogs
drawers
navigation
progress
status indicators
empty states
loading states
error states
```

Accessible third-party primitives MAY be adopted.

The Console SHALL NOT become structurally dependent on an external admin-dashboard template whose domain model dictates Baobab architecture.

---

# 76. No Generic CRUD Admin Generator as Architecture

An admin generator MAY help prototype.

It SHALL NOT determine the production architecture.

Baobab's workflows include:

```text
admission
approval
planning
impact analysis
provisioning
reconciliation
readiness
corporate hierarchy
multi-tenant authorization
```

These cannot safely be reduced to:

```text
Create
Read
Update
Delete
```

against database rows.

---

# 77. No Keycloak Admin UI Reuse for CP Domain

Keycloak's administrator console SHALL NOT be repurposed as Baobab's organisation-onboarding console.

Reason:

```text
IAM Organisation
      !=
Canonical Organisation
      !=
Tenant
      !=
PlatformAccount
```

Identity management is only one portion of the onboarding lifecycle.

---

# 78. No Direct Engine Administration

The Console SHALL NOT call:

```text
Medusa admin APIs
iDempiere APIs
Payload APIs
Pulse internals
```

directly from the browser.

Provider provisioning SHALL be orchestrated through appropriate Baobab control-plane/integration boundaries.

This preserves:

```text
audit
provider neutrality
replacement ability
policy enforcement
```

---

# 79. Future Organisation Complexity

The frontend SHALL anticipate:

```text
single-company customer
corporate group
holding company
subsidiaries
joint ventures
managed entities
multi-country organisations
organisations with multiple tenants
one PlatformAccount covering several organisations
several PlatformAccounts related to one corporate group
```

The visual model SHALL therefore avoid assuming:

```text
one customer
=
one tenant
=
one legal entity
=
one organisation
```

---

# 80. Future Delegated Administration

Baobab SHOULD anticipate delegated administration such as:

```text
Group Administrator
Organisation Administrator
Tenant Administrator
Market Administrator
Read-only Auditor
Integration Administrator
```

without implementing every role immediately.

The UI architecture SHALL permit privilege-aware navigation and action visibility.

Backend authorization remains authoritative.

---

# 81. Future Partner/Managed-Service Model

A future Baobab customer may authorize an external professional or managed-service provider to administer limited platform configuration.

The architecture SHOULD be able eventually to represent:

```text
Administrator Principal
        │
        ▼
Delegated Administrative Relationship
        │
        ├── scope
        ├── duration
        ├── permissions
        └── organisation
```

without sharing customer credentials.

This is a future capability, not part of the initial implementation.

---

# 82. Future API-Driven Onboarding

The Console SHALL be treated as one client of Control Plane APIs.

The backend SHALL remain capable of future:

```text
API-driven onboarding
partner automation
bulk onboarding
migration tooling
infrastructure-driven provisioning
```

Therefore:

> **The frontend workflow SHALL NOT become the only place where onboarding rules exist.**

---

# 83. Future Bulk Operations

Future enterprise scale may require:

```text
bulk organisation import
bulk market activation
bulk administrator invitation
bulk tenant migration
bulk subscription changes
```

Bulk operations SHALL reuse controlled plans/changesets rather than browser loops over single-resource endpoints.

---

# 84. Future Commercial Integration

The Control Plane currently governs subscription classification and entitlement.

It SHALL NOT become a billing ledger.

Future billing, contract or CRM systems MAY integrate with:

```text
PlatformAccount
ProductSubscription
commercial classification
```

through canonical contracts.

The frontend MAY later display relevant commercial state without taking ownership of accounting or billing.

---

# 85. Future Self-Service

Baobab MAY progressively allow customers to self-initiate more changes.

The safe evolution is:

```text
Provider-managed
        │
        ▼
Customer-requested + provider-approved
        │
        ▼
Policy-approved self-service
```

not:

```text
Expose every CP mutation because a UI exists.
```

Policy determines which actions become self-service.

---

# 86. Repository Development Environment

The existing Dev Container SHALL evolve to support both runtime families.

Current Go development support SHALL be retained.

The environment SHALL additionally provide:

```text
Node.js 24 LTS
pnpm
TypeScript tooling
frontend editor support
Playwright dependencies where practical
```

Conceptually:

```text
Codespace
   │
   ▼
baobab-dev
   │
   ├── Go toolchain
   ├── Node 24
   ├── pnpm
   ├── Docker
   ├── GitHub CLI
   └── shared tooling
```

The developer SHALL not need separate Codespaces for the API and Console.

---

# 87. Development Ports

The development environment MAY initially use:

```text
Go CP API      :8080
CP Console     :3000
```

Production routing SHALL not depend on these development ports.

The Dev Container SHOULD forward both during frontend development.

---

# 88. Root Makefile

The root `Makefile` SHALL remain the primary developer entry point.

It SHOULD evolve toward targets such as:

```text
make build
make test
make lint

make backend-build
make backend-test

make frontend-install
make frontend-dev
make frontend-build
make frontend-test
make frontend-lint
make frontend-typecheck

make test-e2e
make dev-up
make dev-down
```

Exact names MAY vary.

The principle is:

> A developer at repository root should be able to build and verify the entire Control Plane product.

---

# 89. Containerisation

The existing root Dockerfile SHALL continue building the Go runtime unless a later implementation decision provides a compelling reason to rename it.

A dedicated frontend Dockerfile SHALL be introduced:

```text
frontend/Dockerfile
```

The frontend runtime SHOULD use:

```text
Node.js 24 LTS
multi-stage build
minimal production dependencies
non-root runtime user
```

where compatible with the selected Next.js deployment mode.

---

# 90. Independent Runtime Images

The same repository SHALL produce at least:

```text
ghcr.io/baobab-platform/baobab-cp
```

for the Go Control Plane and conceptually:

```text
ghcr.io/baobab-platform/baobab-cp-console
```

for the Console.

Final package naming MAY be aligned with organisation-wide GHCR conventions.

The two artifacts SHOULD carry:

```text
same git SHA
compatible release metadata
SBOM/provenance
```

while remaining independently deployable.

---

# 91. Deployment Topology

Production SHOULD support:

```text
                     Internet
                        │
                        ▼
                 CP Console/BFF
                        │
                 trusted network
                        │
                        ▼
                Baobab CP API
                        │
             ┌──────────┴──────────┐
             ▼                     ▼
        PostgreSQL             Baobab IAM
                                  │
                                  ▼
                           Provider ecosystem
```

The Go runtime APIs required by engines MAY continue to be exposed through separate service-to-service ingress.

The browser does not need direct network access to those endpoints.

---

# 92. Release Compatibility

Frontend and backend are developed in one repository but MAY be deployed at different moments.

Therefore contract evolution SHALL remain backward compatible across ordinary rolling deployment windows.

Preferred deployment order for additive contract changes:

```text
1. Deploy backward-compatible backend
2. Verify backend
3. Deploy frontend consuming new contract
4. Remove deprecated contract only after migration period
```

A same-repository commit SHALL NOT justify breaking runtime compatibility casually.

---

# 93. Feature Flags

Risky or incomplete frontend capabilities SHOULD be controlled by server-authoritative feature or capability state.

A frontend environment variable SHALL NOT substitute for authorization.

Feature flags may govern:

```text
rollout
preview
experimental UX
```

but never:

```text
security permission
tenant entitlement
legal authority
```

---

# 94. CI Architecture

CI SHALL evolve from Go-only verification to polyglot product verification.

Conceptually:

```text
                   PULL REQUEST
                        │
        ┌───────────────┼─────────────────┐
        ▼               ▼                 ▼
     Contracts        Backend           Frontend
        │               │                 │
        │          Go test/race      typecheck/lint
        │          vet/security       unit tests
        │               │                 │
        └───────────────┼─────────────────┘
                        ▼
                  Integration Tests
                        │
                        ▼
                    E2E Tests
                        │
               ┌────────┼────────┐
               ▼        ▼        ▼
            Auth     A11y     Negative
                     Tests    Authz Tests
                        │
                        ▼
                  Image Builds
                        │
                        ▼
                  Required Gate
```

---

# 95. Reusable CI

Repository CI SHALL continue consuming reusable organisation-wide workflows from:

```text
baobab-platform/shared
```

where appropriate.

The Control Plane SHALL not duplicate organisation-wide:

```text
dependency scanning
action pinning
SBOM generation
container scanning
secret scanning
provenance
policy gates
```

merely because a frontend has been added.

The shared CI foundation MUST support the repository as a:

```text
Go + Node/TypeScript
```

polyglot consumer.

---

# 96. Required Frontend CI Checks

At minimum:

```text
dependency install with locked versions
TypeScript type checking
linting
unit tests
production build
dependency vulnerability scanning
secret scanning
static analysis
contract generation/drift check
```

Before production readiness:

```text
Playwright E2E
accessibility checks
authentication flow tests
authorization-negative tests
critical onboarding journey
```

SHALL also become required.

---

# 97. End-to-End Test Scenarios

Minimum high-value E2E scenarios SHOULD include:

```text
Applicant creates and resumes application

Applicant cannot access platform-operator screens

Reviewer can review but cannot perform unrelated privileged actions

Rejected application cannot proceed to provisioning

Approved application produces onboarding request

Onboarding plan can be reviewed before apply

Provisioning progress survives browser refresh

Tenant cannot become ACTIVE before readiness permits

Organisation administrator cannot access another organisation

Corporate parent relationship does not grant subsidiary data access

Expired/revoked session fails closed

Destructive operation requires required approval

Concurrency conflict is surfaced instead of overwritten

Audit record exists after consequential operation
```

---

# 98. Cross-Tenant Security Testing

CI SHALL include deliberate negative tests such as:

```text
Organisation A user
requests Organisation B resource ID
```

Expected:

```text
DENY
```

Testing only successful-path authorization SHALL be considered insufficient.

---

# 99. Accessibility Gates

Automated accessibility checks SHOULD run on:

```text
login/session screens
application wizard
organisation form
review page
provisioning page
dashboard
tables
dialogs
error states
```

The platform SHALL complement automated testing with manual assessment.

---

# 100. Performance

The CP Console does not require public-site SEO optimisation.

It does require responsive administration.

Target qualities SHOULD include:

```text
fast initial shell
small client bundles
responsive navigation
fast form interaction
streamed/loading feedback for slow queries
pagination for large datasets
virtualisation only when genuinely necessary
```

Server Components SHOULD be preferred where they meaningfully reduce browser JavaScript.

---

# 101. Search

As organisations scale, the Console SHALL require search.

Initial search SHOULD use appropriate Control Plane APIs and PostgreSQL capabilities.

The frontend SHALL NOT introduce:

```text
OpenSearch
Elasticsearch
dedicated search infrastructure
```

solely for administrative convenience.

A dedicated search engine may be added only when demonstrated scale requires it.

---

# 102. Pagination

Large resources SHALL use server-side pagination.

Examples:

```text
organisations
applications
tenants
audit events
provisioning operations
drift records
```

The Console SHALL not fetch an unbounded platform-wide dataset into browser memory.

---

# 103. Dashboard Read Models

The CP API MAY provide purpose-built read models to avoid inefficient browser orchestration.

For example:

```text
OrganisationDashboard
├── identity_summary
├── tenant_summary
├── market_summary
├── subscription_summary
├── estate_summary
├── administrator_summary
├── readiness_summary
└── current_alerts
```

This is acceptable because it is a projection of Control Plane state.

It SHALL not create a new authority.

---

# 104. Forms

Complex forms SHALL provide:

```text
clear labels
help text
field-level validation
summary validation
save/resume
review before submission
```

Validation SHALL occur both:

```text
client-side for usability
server-side for authority
```

Client-side validation SHALL never be trusted as the only validation.

---

# 105. Confirmation and Review

Before submission of consequential configuration, the user SHOULD receive a structured review page.

Example:

```text
Organisation
Acme Holdings Ltd

Corporate Structure
4 subsidiaries

Requested Markets
ZA
UG

Requested Services
Trade
ERP

Administrators
Jane Doe
Peter Smith

Security Requirements
South Africa residency
Dedicated database

Submit for review
```

The review page is a business control, not decoration.

---

# 106. Plain-Language Failure States

Failure states SHOULD identify:

```text
what failed
what was not changed
whether retry is safe
whether intervention is required
reference identifier
```

Example:

```text
ERP setup could not be verified.

Trade setup completed successfully.
ERP activation remains blocked.
No duplicate setup will be created if you retry.

Reference: PRV-01K...
```

This is particularly important for partial provisioning.

---

# 107. Partial Failure

The UI SHALL represent partial provisioning honestly.

Prohibited:

```text
"Onboarding complete"
```

if:

```text
Trade = READY
ERP = FAILED
IAM = READY
```

Preferred:

```text
Onboarding requires attention

2 of 3 required services are ready.
ERP provisioning failed during verification.
```

Backend state remains authoritative.

---

# 108. Retry and Resume

The Console SHOULD offer:

```text
Retry safe step
Resume reconciliation
Request operator review
```

only where the backend explicitly declares those operations safe.

The frontend SHALL not invent retry semantics.

---

# 109. Offboarding

Organisation offboarding SHALL receive the same discipline as onboarding.

Conceptually:

```text
Offboarding Request
        │
        ▼
Impact Analysis
        │
        ▼
Approval
        │
        ▼
Revoke Access
        │
        ▼
Suspend Capabilities
        │
        ▼
Provider Deprovisioning
        │
        ▼
Retention / Data Policy
        │
        ▼
Verification
        │
        ▼
Decommissioned
```

The Console SHALL communicate irreversible consequences before execution.

---

# 110. Suspension

Suspension SHALL be distinct from deletion.

The UI SHOULD distinguish:

```text
SUSPEND
temporarily deny operational access

DECOMMISSION
perform governed permanent lifecycle exit
```

A support operator SHALL not casually use decommissioning to solve a temporary problem.

---

# 111. Change Management After Onboarding

Onboarding is only the first instance of a broader configuration lifecycle.

The same plan/change model SHOULD eventually support:

```text
add market
remove market
add service
change subscription
add subsidiary
register Digital Estate
change isolation requirement
change residency
add administrator
provider migration
tenant suspension
```

This reduces the risk of building a one-time wizard that becomes useless after activation.

---

# 112. Onboarding as First Changeset

Conceptually:

```text
Initial Onboarding
      =
first major Control Plane Changeset
```

Future modifications are subsequent changesets.

This provides architectural continuity:

```text
Create
Change
Suspend
Reinstate
Offboard
```

through one governance model.

---

# 113. Organisation Lifecycle View

The Console SHOULD eventually provide a timeline such as:

```text
2026-10-03   Application created
2026-10-05   Application submitted
2026-10-08   Admission approved
2026-10-09   Tenant provisioning approved
2026-10-09   Sandbox ready
2026-10-15   Production activation approved
2026-10-16   Production active
2027-02-04   Kenya market requested
2027-02-06   Kenya market activated
```

This is derived from audit and lifecycle state.

It SHALL not become a second audit system.

---

# 114. Architecture Diagram — End-to-End Human Administration

```text
                        HUMAN ADMINISTRATOR
                                │
                                ▼
                     ┌─────────────────────┐
                     │     CP CONSOLE      │
                     │    Next.js/React    │
                     └──────────┬──────────┘
                                │
                        secure BFF session
                                │
                  ┌─────────────┴──────────────┐
                  │                            │
                  ▼                            ▼
            BAOBAB IAM                  BAOBAB CP API
             Keycloak                        Go
                                                │
                         ┌──────────────────────┼─────────────────────┐
                         │                      │                     │
                         ▼                      ▼                     ▼
                    Foundation             Governance          Orchestration
                         │                      │                     │
                         └──────────────────────┼─────────────────────┘
                                                │
                                                ▼
                                         Desired State
                                                │
                                                ▼
                                         Reconciliation
                                                │
                                                ▼
                    ┌───────────────────────────┼──────────────────────┐
                    ▼                           ▼                      ▼
               Baobab Trade                Baobab ERP            Other Engines
                    │                           │                      │
                    └───────────────────────────┼──────────────────────┘
                                                │
                                                ▼
                                            Readiness
                                                │
                                                ▼
                                            CP Console
```

---

# 115. Architecture Diagram — Authority Boundaries

```text
IDENTITY AUTHORITY
Baobab IAM
    │
    │ identity / authentication assurance
    ▼

PLATFORM AUTHORITY
Baobab Control Plane
    │
    │ organisation / tenant / entitlement /
    │ context / provisioning / readiness
    ▼

DOMAIN AUTHORITY
Trade / ERP / CMS / Pulse / future engines
    │
    │ business operations
    ▼

BUSINESS OUTCOME
```

The frontend exists alongside these authorities.

It is not another authority.

---

# 116. Architecture Diagram — Repository vs Deployment

```text
GIT REPOSITORY
baobab-platform/baobab-cp
│
├── Go Control Plane
│
└── CP Console
      │
      ▼
BUILD
│
├── baobab-cp image
│
└── baobab-cp-console image
      │
      ▼
DEPLOYMENT
│
├── CP API service
│
└── Console/BFF service
```

Therefore:

```text
same repo
!=
same process
```

and:

```text
same repo
!=
same container
```

---

# 117. Alternatives Considered

## Alternative A — Separate `baobab-console` Repository

**Rejected initially.**

Advantages:

```text
independent ownership
independent release cadence
hard repository boundary
```

Disadvantages:

```text
cross-repo API coordination
duplicate workflows
contract-version coordination
more PR choreography
more repository administration
onboarding backend/frontend changes cannot be atomic
```

The frontend and backend are one bounded product.

A separate repository currently adds ceremony without useful autonomy.

---

## Alternative B — Move Existing Go Code Under `/backend`

**Rejected.**

This creates repository churn without improving runtime boundaries.

The existing Go structure is already conventional and coherent.

Add:

```text
/frontend
```

instead.

---

## Alternative C — Embed a React SPA Into the Go Binary

**Rejected as the primary architecture.**

Advantages:

```text
single artifact
simple small deployments
```

Disadvantages:

```text
release coupling
weaker frontend server/BFF separation
less flexible deployment
harder independent scaling
frontend build embedded into backend image
encourages browser-token architecture
```

Independent runtime artifacts are preferred.

---

## Alternative D — Browser SPA Calling CP Directly

**Rejected for the privileged Console.**

It would expose more OAuth responsibility to browser JavaScript.

A BFF provides a stronger token-custody boundary and is consistent with current OAuth browser BCP guidance.

---

## Alternative E — Keycloak Admin Console

**Rejected.**

Keycloak manages identity.

It does not own:

```text
canonical organisation
PlatformAccount
Tenant
ProductSubscription
CapabilityGrant
MarketParticipation
DigitalEstate
ProvisioningPlan
Readiness
```

---

## Alternative F — Generic Low-Code Admin Tool

**Rejected as the platform's canonical administration experience.**

Low-code tooling may assist internal experiments.

It SHALL NOT become the security or product boundary for Baobab's Control Plane.

---

# 118. Consequences

## Positive

This decision provides:

```text
one cohesive Control Plane product
atomic frontend/backend changes
shared contracts
single architecture history
consistent CI
simpler developer onboarding
non-technical administration
safer organisation onboarding
repeatable tenant lifecycle
improved auditability
clear readiness visibility
foundation for self-service
```

## Negative

It introduces:

```text
Node/TypeScript into a Go repository
more complex CI
a second runtime image
browser security responsibilities
session-management responsibilities
frontend accessibility obligations
polyglot developer tooling
```

These are accepted costs.

They are preferable to requiring human administrators to operate Baobab through raw APIs and infrastructure tools.

---

# 119. Implementation Programme

Implementation SHALL be incremental.

No gate SHALL silently alter existing Control Plane semantics.

## Gate CPFE-00 — Architecture and Contract Lock

Audit:

```text
baobab-cp
shared
baobab-iam
current administrative APIs
current onboarding implementation
current CI
current infrastructure routing
```

Produce:

```text
frontend API matrix
authorization matrix
route map
contract gap list
BFF threat model
```

No large code movement.

---

## Gate CPFE-01 — Repository Foundation

Introduce:

```text
frontend/
Node 24 tooling
pnpm lock
TypeScript strict
Next.js Active LTS
devcontainer support
Makefile targets
frontend CI
frontend Dockerfile
```

No production onboarding workflow yet.

---

## Gate CPFE-02 — Console Shell and Design System

Implement:

```text
application shell
navigation
layout
accessible primitives
status model
error model
loading model
responsive foundations
```

No privileged mutations yet.

---

## Gate CPFE-03 — Authentication and BFF

Implement:

```text
Baobab IAM OIDC integration
confidential-client BFF
secure sessions
login
logout
session expiry
CSRF protection
step-up hooks
```

Validate with security testing before proceeding.

---

## Gate CPFE-04 — Typed CP Client and Authorization Boundary

Implement:

```text
generated/validated CP client
contract drift CI
principal context
authorisation-negative tests
safe error translation
correlation IDs
```

Browser-to-Go direct authenticated API calls SHALL not be introduced as a shortcut.

---

## Gate CPFE-05 — Applicant Workspace

Implement ADR-BCP-017 experience:

```text
create application
draft/save
organisation data
evidence references
requirements
submission
status
information requests
withdrawal
```

---

## Gate CPFE-06 — Admission Review

Implement platform operator workflow:

```text
review queue
evidence review
information request
verification
decision
approval/rejection
classification
audit
```

---

## Gate CPFE-07 — Organisation Structure and Platform Account

Implement ADR-BCP-018 concepts:

```text
Organisation
LegalEntityProfile
CorporateRelationship
CorporateGroup
PlatformRelationship
PlatformAccount
```

with strict separation from authorization.

---

## Gate CPFE-08 — Onboarding Configuration

Implement guided configuration of:

```text
markets
services
Digital Estates
administrators
security requirements
residency requirements
integration requirements
```

Use business terminology.

---

## Gate CPFE-09 — Planning and Impact Analysis

Implement:

```text
desired configuration
changeset
validation
impact analysis
human-readable plan
technical drill-down
approval state
```

No uncontrolled direct mutation.

---

## Gate CPFE-10 — Provisioning and Readiness

Implement:

```text
apply
operation tracking
progress
partial failure
retry where safe
reconciliation
readiness
activation
```

Activation SHALL remain backend-governed.

---

## Gate CPFE-11 — Organisation Administration

Implement post-activation:

```text
organisation dashboard
tenant summary
markets
services
Digital Estates
administrator management
integration status
readiness
activity
```

within delegated scope.

---

## Gate CPFE-12 — Operational Console

Implement platform-level:

```text
readiness
drift
reconciliation
operations
audit
diagnostics
support references
```

with progressive disclosure.

---

## Gate CPFE-13 — Accessibility and Security Hardening

Complete:

```text
WCAG 2.2 AA review
Playwright accessibility testing
ASVS review
browser security headers
CSP
CSRF testing
XSS testing
cross-tenant testing
dependency review
session review
```

---

## Gate CPFE-14 — Production Packaging and Rollout

Complete:

```text
runtime images
CI/CD
deployment routing
staging
non-production IAM integration
observability
runbooks
rollback
SLOs
production readiness
```

---

# 120. Migration Rule

The introduction of the frontend SHALL NOT block or destabilise existing engine consumers of CP.

Existing runtime functions such as:

```text
context resolution
capability resolution
tenant lifecycle
reconciliation
```

SHALL continue operating independently of the Console.

If the Console is unavailable:

```text
runtime engine requests
```

SHOULD continue where they do not depend on a human administrative action.

The frontend is a control interface.

It is not part of every business transaction's runtime path.

---

# 121. Availability Boundary

The following SHALL therefore be distinguished:

```text
CP Runtime Availability
        !=
CP Console Availability
```

Example:

```text
CP Console unavailable
CP runtime healthy
Trade context resolution healthy
```

Existing tenants SHOULD continue operating.

Administrative changes may temporarily be unavailable.

This is a desirable failure mode.

---

# 122. Disaster Recovery

The frontend is largely stateless except for controlled session state.

Authoritative onboarding/provisioning state SHALL reside in Control Plane persistence or other designated platform authorities.

Loss of a frontend instance SHALL NOT lose:

```text
applications
plans
approvals
provisioning state
audit
readiness
```

The Console SHALL be horizontally replaceable.

---

# 123. Definition of Done for the Initial CP Console

The initial Control Plane Console SHALL NOT be declared production ready until it can demonstrate:

| Requirement | Required |
|---|---:|
| Secure IAM login | Yes |
| BFF session architecture | Yes |
| No browser OAuth-token persistence | Yes |
| Applicant workflow | Yes |
| Admission review | Yes |
| Organisation model | Yes |
| Tenant onboarding request | Yes |
| Plan preview | Yes |
| Provisioning progress | Yes |
| Readiness display | Yes |
| Initial organisation admin handover | Yes |
| Audit trail | Yes |
| Cross-tenant negative tests | Yes |
| Type-safe CP contract | Yes |
| WCAG 2.2 AA target | Yes |
| Accessibility automated tests | Yes |
| Security verification | Yes |
| Production container | Yes |
| CI required gates | Yes |
| Runbooks | Yes |
| No direct DB access | Yes |
| No direct engine administration | Yes |

---

# 124. Architectural Invariants

The following SHALL remain non-negotiable:

```text
Frontend
    != Control Plane authority

Applicant
    != Tenant

Organisation
    != Tenant

Organisation
    != LegalEntity

Organisation
    != IAM Organization

Corporate relationship
    != authorization

PlatformAccount
    != authorization boundary

Subscription
    != CapabilityGrant

CapabilityGrant
    != CapabilityBinding

Market
    != deployment region

DigitalEstate
    != Tenant

CP Console
    != Digital Estate

CP Console
    != ERP admin

CP Console
    != Trade admin

CP Console
    != Keycloak admin

Same repository
    != same runtime

Same repository
    != direct internal coupling
```

---

# 125. Final Target Mental Model

The resulting Baobab architecture SHALL be understood as:

```text
                              BAOBAB PLATFORM
                                     │
                    ┌────────────────┴────────────────┐
                    │                                 │
                    ▼                                 ▼
           BAOBAB CONTROL PLANE                  DOMAIN ENGINES
                    │                                 │
      ┌─────────────┼─────────────┐          ┌────────┼────────┐
      │             │             │          │        │        │
      ▼             ▼             ▼          ▼        ▼        ▼
    Go API      Reconciler    CP Console    Trade     ERP      CMS ...
                                  │
                                  │
                                  ▼
                           Human Governance
                                  │
              ┌───────────────────┼────────────────────┐
              ▼                   ▼                    ▼
          Applicants        Baobab Operators      Organisation
                                                    Admins
```

The Console makes the Control Plane understandable.

The Go backend makes it authoritative.

IAM establishes identity.

The engines perform domain work.

The contracts keep the ecosystem coherent.

---

# 126. Final Decision

Baobab SHALL reorganise `baobab-platform/baobab-cp` into a **polyglot Control Plane product repository** containing:

```text
existing Go Control Plane
+
new TypeScript/Next.js CP Console
```

without unnecessarily relocating the existing idiomatic Go code.

The Console SHALL provide the human administration layer for:

```text
organisation admission
organisation modelling
platform relationships
tenant onboarding
subscription configuration
market configuration
Digital Estate registration
administrative identity establishment
provisioning
reconciliation
readiness
lifecycle governance
audit
```

It SHALL be designed first for **non-technical administrators**, with technical details available progressively to authorised operators.

It SHALL use a secure BFF browser architecture, remain API-contract-bound to the Go Control Plane, preserve existing Baobab authority boundaries, and remain independently deployable from the backend.

The strategic outcome is:

> **Baobab's Control Plane will no longer be merely an API that engineers can operate. It will become an administratively usable platform product through which organisations can be admitted, configured, provisioned, governed, changed and eventually offboarded safely throughout their entire Baobab lifecycle.**

And the architectural discipline remains:

> **Humans express intent through the Console. The Control Plane validates and governs that intent. Reconciliation makes desired state real. Readiness determines whether the promise has actually been delivered.**