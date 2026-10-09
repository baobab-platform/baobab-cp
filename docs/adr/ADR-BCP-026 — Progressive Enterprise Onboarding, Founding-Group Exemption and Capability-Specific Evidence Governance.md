# ADR-BCP-026 — Progressive Enterprise Onboarding, Founding-Group Exemption and Capability-Specific Evidence Governance

**Status:** Accepted — Normative Platform Architecture (2026-10-09)  
**Decision approval:** Accepted at the platform decision owner's direction on 2026-10-09. Implementation, security certification and individual legal-actor authorisations remain distinct downstream gates.  
**Date:** 2026-10-09  
**Decision owners:** Baobab Platform Architecture / Control Plane / Security / Nabhold Group Governance  
**Primary repository:** baobab-platform/baobab-cp  
**Canonical contract authority:** baobab-platform/shared  
**Runtime authority:** Baobab Control Plane, with domain-specific authorities retained by IAM, Subscriptions and engines  
**Initial markets:** South Africa (ZA), Uganda (UG)  
**Decision type:** Foundational amendment to admission, organisation identity, evidence applicability and founding-group onboarding  
**Amends:** ADR-BCP-017 (admission preconditions and channels), ADR-BCP-018 (pre-incorporation representation and first-party eligibility), ADR-BCP-019 (progressive UX), ADR-BCP-020/021 (policy-based low-risk authorisation boundaries), ADR-BCP-023 (purpose-specific evidence and deferrals), and applicable onboarding provisions in BCP-TS-ONBOARDING-001  
**Preserves:** ADR-BCP-003/005/009/011/024; ADR-SHARED-012/013/014/015; ADR-IAM-0033  
**Implementation state:** Accepted architectural decision; phased contracts and runtime work are underway. Acceptance does not certify founding documentary deferrals or production onboarding.
**Amendment A1 (2026-10-09):** The decision owner revised the one-time founding-group documentary grace maximum from **12 to 24 calendar months**, prospective for the unimplemented PEO-02 runtime. This amendment does not change the start instant, scope, independent approval, statutory/provider boundaries or anti-reset rule. Previously issued deferrals, if any, require individually authorised review rather than automatic extension.

---

## 1. Context and business intent

Baobab Platform was conceived first as the enterprise platform for Nabhold Group Africa and its independently operated subsidiaries: ZuriBeans, Thamani Global and Equator & Estate Co. The initial objective was operational enablement, shared capabilities and accountable governance, **not platform subscription revenue**. Serving other African enterprises is a strategic expansion of that capability; commercial subscription pricing will be decided separately.

The existing architecture correctly distinguishes Organisation, LegalEntity, CorporateGroup, PlatformRelationship, PlatformAccount, Tenant, ProductSubscription, CapabilityGrant and IAM authority. However, a universal documentary-verification sequence before tenant registration can unnecessarily exclude first-party businesses awaiting documents and lawful emerging enterprises whose legal form or chosen service does not call for those documents.

South African PAYE and UIF illustrate the broader design problem: employer-specific evidence should depend on an enterprise's actual employment and remuneration circumstances. No timeless statutory monetary threshold or universal PAYE/UIF document gate shall be hard-coded into general platform admission. Applicable legal thresholds belong to separately versioned domain/regulatory content.

The new onboarding design shall be **business-first, progressively evidenced, capability-specific, African-first and operationally rigorous**. It shall not make Baobab a general law-enforcement or regulatory body, and it shall not neglect legal duties applying to Baobab or its individual engines/providers.

## 2. Decision summary

1. **First-party eligibility:** Nabhold Group Africa and its legitimately sponsored founding-group businesses may enter a governed internal onboarding path without supplying every ordinary documentary item up front. This exemption is an admission/evidence-process policy, not an assertion that incorporation or regulatory obligations are satisfied.
2. **Twenty-four-month evidence grace:** Each eligible founding-group organisation may receive a one-time, bounded 24-calendar-month deferral for specifically enumerated *platform documentary requirements*, starting on its effective provisional approval date. Legal requirements and service-provider obligations remain effective.
3. **Progressive external admission:** External enterprises shall not need a universally complete company-registration, PAYE, UIF, VAT or other documentary pack simply to create an account and request eligible low-risk services. Evidence is asked for in context when necessary.
4. **Separate decisions:** Identity acceptance, admission, canonical reconciliation, evidence verification, legal status, contract authority, subscription classification, entitlement, provisioning, service eligibility and activation remain separate authoritative decisions.
5. **Risk-proportionate workflow:** Low-risk standard paths may eventually be policy-authorised and automated if an independently approved policy and auditable non-applicant authority are in place. Elevated-risk changes, founding-group sponsorship and exceptions retain appropriate human review and separation of duties.
6. **No subscription-pricing redesign:** First-party INTERNAL classification (where independently eligible), metering and explicit entitlements remain. A later ADR will determine external commercial packaging, pricing and revenue policies.

### The central rule

> Baobab shall admit an enterprise when it can establish the minimum trustworthy platform relationship and administer a suitable tenant safely. Further evidence shall be required when a specified service, market activity, contracting relationship or obligation makes it relevant—not automatically for every tenant.

## 3. Non-negotiable boundaries

- **Organisation is not necessarily an incorporated LegalEntity.** A pre-incorporation venture may be represented as an Organisation, but shall never receive a fictitious corporate registration number or a falsely verified LegalEntity profile.
- **An authorised legal/contracting actor is still required wherever a contract or transaction needs one.** This may be an actually eligible natural person, incorporated parent or other valid legal person, subject to documented authority; never silently invent a proxy.
- **No global legal-compliance Boolean.** Claims, evidence, verification results, policy decisions and outstanding requirements remain separate, per ADR-BCP-023.
- **Nabhold affiliation is not ownership proof.** An authorised first-party sponsorship or platform relationship may establish bounded admission eligibility without pretending to verify shareholding or control.
- **Group membership is not access.** Corporate ownership, common sponsorship, INTERNAL classification and PlatformAccount linkage confer no cross-tenant IAM authority.
- **Admission approval is not activation.** The canonical authorisation, desired-state planning, digest-bound approvals, provider provisioning, reconciliation and readiness remain in force.
- **No statutory waiver.** The platform cannot grant an exemption from laws applying to an enterprise or to Baobab, a payment provider, marketplace, employer, regulated intermediary or engine.
- **No fabricated readiness.** Missing evidence is not VERIFIED; unavailable providers are not HEALTHY; a tenant is not production-ready because a showcase works.
- **No blanket suspension by the Regulations engine.** Its findings inform policy; each authorised policy enforcement point acts only within its legitimate remit.
- **No consumer-mandated engine internals.** Applicants select business needs, not Medusa/iDempiere/Payload/provider IDs.

## 4. Operating model: two entry paths, one platform

| Concern | Founding-group path | External enterprise path |
|---|---|---|
| Channel | INTERNAL_GROUP / authorised assisted path | SELF_SERVICE or ASSISTED_ENTERPRISE |
| Minimum identity | Authenticated sponsor and accountable administrator | Authenticated applicant and accountable representative |
| Evidence at admission | Identity, sponsorship and minimum lawful authority; other platform documents may be deferred | Purpose-limited minimum data; requirements evaluated by legal form, activity, market, product/provider |
| Group eligibility | Verified governed relationship OR specifically authorised, expiring founding sponsorship for *admission* | Ordinary external PlatformRelationship |
| Documentary deferral | Explicit one-time 24-month bounded first-party policy | Not inherently a 24-month obligation; applicability determines whether evidence is ever required |
| Subscription | Existing INTERNAL classification only if the current authoritative eligibility policy authorises it | Existing canonical types; pricing decision out of scope |
| Approval | Recorded first-party sponsorship; independent consequential authorisations | Policy-driven low-risk path or reviewed elevated-risk path |
| Activation | Only eligible, provisioned capabilities | Same provider, IAM, tenancy and readiness standards |

A first-party exemption **shall not create a Nabhold-only Tenant schema, IAM realm, bypass API or provisioning algorithm**. Distinctions live in versioned policy, sponsorship/evidence provenance and auditable decisions. Runtime implementation must remain reusable.

## 5. Founding group and affiliation

Initial sponsored roster (group-governance business declarations as of 2026-10-09; not an independent CIPC verification or statutory authorisation):

| Organisation | Intended business context | Declared legal/evidence position |
|---|---|---|
| Nabhold Group Africa | Registered holding company and proposed legal operating principal for named first-party businesses | Group declares South African incorporation; separately reconcile its official registration evidence and applicable authority |
| Thamani Global | Independent incorporated B2C/logistics subsidiary of Nabhold | Group declares CIPC registration. Independently verify company registration and Nabhold's corporate relationship before marking either assertion VERIFIED |
| ZuriBeans | Independently operated B2B/cross-border trade business awaiting incorporation | Group expressly declares not separately incorporated. Nabhold is the proposed legally responsible actor for enumerated authorised activities; never manufacture a corporate registration |
| Equator & Estate Co. | Independently operated property, construction and hospitality business awaiting incorporation | Group expressly declares not separately incorporated. Nabhold is the proposed legally responsible actor for enumerated authorised activities; no implied property, licensing or statutory permissions |

Each independently operated business and incorporated subsidiary shall preserve its business decision-making, justified tenant isolation, market participation, administrator scope and future exit rights (Option B autonomy). **ZuriBeans and Equator & Estate Co. are presently operating businesses, not separately incorporated subsidiaries.** Thamani is the declared incorporated subsidiary. A first-party sponsor attestation establishes neither verified legal incorporation nor corporate control nor the authority to issue a specific commercial document.

### 5.0.1 Responsibility allocation and evidence boundaries

- **Nabhold:** proposed responsible legal actor for scoped ZuriBeans and Equator & Estate transactions, in addition to its own obligations. The phrase 'most responsibility' SHALL be expanded into named approved roles, market/activity scopes, effective dates, issuer/contracting authority, exceptions and source evidence. It SHALL NOT become an unrestricted blanket mandate.
- **ZuriBeans:** separate operating Organisation and, where justified, isolated Tenant; no fictitious incorporated LegalEntityProfile. May transact through Nabhold only when a valid activity-specific mandate and relevant provider/legal eligibility exist.
- **Equator & Estate:** same separate operating identity and tenant principle, with property, construction, hospitality, title and permitting conditions assessed specifically rather than assumed from group sponsorship.
- **Thamani:** separate incorporated subsidiary Organisation and LegalEntityProfile when independently evidenced, with its own legal responsibilities and Tenant. Historical origin as a Nabhold logistics unit does not automatically transfer today's contracts or invoices to Nabhold.
- **Evidence semantics:** group statements are business declarations; official company status, verified ownership, brand/trading rights and operating mandates each require separately adequate proof. Existing verification must be reviewed by assertion and source, not blindly elevated or downgraded.
- **No new pricing decision:** founding-group INTERNAL classification remains governed by its own evidence-based eligibility or an explicitly approved and versioned sponsorship-policy amendment.

The generic Organisation-first and mandate rules remain reusable for all enterprises.

### 5.1 Founding sponsorship authority

An eligible sponsorship SHALL identify: sponsoring first-party organisation, sponsored Organisation, authorised initiating principal, independent approving principal, authority basis, purpose, effective/expiry dates, assertion versus verification standing, relevant evidence reference, current status and audit/correlation references.

Do not use email domain, brand name, self-claimed subsidiary status or fixture position as sole proof of eligibility. A recognised internal governance authority must approve sponsorship; revocation or corporate divestiture triggers review of eligibility and INTERNAL subscription classification.

Where exact Shared constructs already express these facts, reuse them. A new FoundingGroupSponsorship aggregate may be proposed only after a gap analysis, and shall not duplicate CorporateRelationship or PlatformRelationship authority.

### 5.2 Pre-incorporation legal-actor mapping

Current tenant registration requires a legal_entity_id. Implementation SHALL not evade that invariant by manufacturing a legal entity for an unincorporated ZuriBeans or an emerging enterprise.

For a pre-incorporation business:
1. Register or resolve the canonical Organisation and retain incorporation as an unverified or pending claim.
2. Identify its **real** authorised contracting/operating legal actor, if required for the capabilities sought.
3. Where appropriate and authorised, use an existing parent or other legitimate legal actor in the tenant/legal-entity mapping, explicitly identifying the relationship and responsibility. Parent identity is not assumed merely from the group chart.
4. If no legally valid actor exists for a particular activity, retain admission/organisation identity where supported but defer that activity's tenant binding or capability activation; do not invent a substitute.
5. Reconcile later incorporation into LegalEntity, references and mappings without needlessly destroying Organisation/Tenant history.

Shared and CP shall design a generic supportable legal-actor boundary for sole proprietors and other lawful enterprise forms, instead of a founding-group special case.

## 6. Twenty-four-month founding-group evidence grace

The 24-month period SHALL apply to a *named, approved list of deferrable platform evidence requirements*, not to an organisation's legal existence or applicable statutory duties.

**Start:** effective timestamp of first approved provisional onboarding for the organisation.  
**Duration:** 24 calendar months, using an explicit timezone/UTC instant for enforcement.  
**Expiry:** the twenty-fourth calendar-month anniversary of that approval; calculated and persisted deterministically (including month-end and leap-day cases).  
**No reset:** re-application, re-provisioning, renaming, moving markets or changing administrators shall not restart it.  
**No automatic renewal:** extension, if permitted, requires an independent exceptional decision with specific scope and a new time limit.

Suggested review milestones: months 6, 12 and 18, with month 24 as the expiry checkpoint. These are review targets, not a substitute for runtime expiry validation.

The deferral record shall include the governed organisation, requirement IDs, source policy and version, missing/document statuses, admitting actor, distinct authoriser, justification, restriction set, start, expiry, review schedule, evidence/provenance, revocation, remediation and audit history.

A platform documentary requirement may be:
- **Not applicable** to this organisation/role/activity;
- **Satisfied** by a valid official check or other approved evidence;
- **Pending** and expressly deferred by policy;
- **Required before a particular capability or transaction**, regardless of the wider founding-group grace;
- **Non-deferrable** because of applicable law, provider contract, security or other approved risk policy.

On expiry CP SHALL reassess each outstanding requirement and constrain only the operations whose authorised policy actually depends on it. Do not delete tenant data or automatically disable unrelated compliant capabilities. Expired deferral is never a valid positive evidence result. Reconciliation may invoke a governed restriction/suspension Changeset where justified.

### Policy examples (illustrative, non-normative names)

~~~yaml
policy_id: founding-group-progressive-admission
scope: INTERNAL_GROUP
sponsorship:
  independent_authorisation: required
documentary_deferral:
  duration: P24M
  start: first_provisional_approval
  renew_automatically: false
  items: explicitly_approved_requirement_ids
evidence:
  unverifiable_claims_remain_unverified: true
  official_registry_lookup_may_satisfy_relevant_claim: true
restrictions:
  external_payments: provider_and_legal_policy
  statutory_requirements: never_waived
  cross_tenant_access: prohibited
  production_readiness: normal_gates
reviews:
  months: [6, 12, 18, 24]
~~~

This is a policy illustration, NOT an authorised Shared schema or a new enum contract.

## 7. Progressive onboarding for African enterprises

### 7.1 Minimal admission

The applicant journey SHALL be built around:
1. Identify a real applicant and contact channel using IAM.
2. Describe the business, business form, operating country and authorised representative.
3. Accept applicable platform terms and identify a responsible contracting actor where needed.
4. State desired products/services and intended market participation.
5. Evaluate **purpose-specific** minimum evidence, security and provider restrictions.
6. Obtain a recorded admission decision through an authorised human or duly governed policy service.
7. Authorise a TenantOnboardingRequest and proceed with canonical Shared provisioning.
8. Request additional evidence **at the moment it becomes relevant** to an activated capability, provider, regulated activity or risk change.

Minimal data does not mean anonymous access, fabricated verification or no contract.

### 7.2 Applicability before document collection

Requirement evaluation SHALL consider:
- Legal form and contracting actor (incorporated company, natural-person business, cooperative, partnership or other recognised form);
- Country/jurisdiction and actual market activity;
- Employment and remuneration conditions for employer-specific documents;
- Product, capability and third-party provider requirements;
- Whether Baobab is directly obligated in the service arrangement;
- Declared information, verified facts and source freshness;
- The identified versioned policy and the reason for each requested item.

Documents such as PAYE, UIF, VAT or corporate registration are not universally mandatory for platform admission. No fixed tax threshold is embedded in admission source code: current legal rules, exemptions and interpretation belong to specialised governed policy content and qualified domain decisions.

A declaration that an enterprise has no employees is a claim; it may cause a requirement to be assessed as non-applicable under the relevant policy, but it is not proof of universally compliant status.

### 7.3 Graduated capability eligibility

| Capability example | Admission evidence | Additional gates when applicable |
|---|---|---|
| CMS drafts / basic presence | Minimal authenticated business identity | Ownership/domain rights for controlled publication |
| Pulse reports / intelligence | Authenticated account and appropriate entitlement | Data/licence/classification restrictions |
| Trade catalogue drafts | Authenticated business identity | Publishing, sale, supplier or market restrictions as required |
| ERP trial setup / bookkeeping configuration | Authenticated administrator and contracting actor as applicable | Payroll, invoicing, tax configuration or regulated financial actions when activated |
| Payment settlement / regulated services | Cannot infer eligibility from admission | Payment-provider onboarding and applicable legal/contractual controls |
| Cross-border execution | Intent can be captured | Transaction/corridor/product-specific requirements before execution |

This is an example capability taxonomy: actual capability keys and provider eligibility shall be derived from Shared declarations and live providers.

## 8. Responsibilities and decision authority

| Authority | Owns | Does not own |
|---|---|---|
| Government registries | Legal registration records and their official status | Baobab tenant admission |
| Enterprise / legal actor | Its legal obligations and truth of declarations | Platform IAM or other tenants' privileges |
| Baobab Regulations | Sourced requirement intelligence, assessments and evidence reasoning when implemented | Universal mandatory suspension or legal authority |
| Control Plane | Admission policy, canonical organisation linkage, governed tenant desired state, entitlement orchestration and platform restrictions | Statutory incorporation decisions or provider-native transaction rules |
| IAM | Authentication, federation, membership projection and revocation | Company-incorporation adjudication |
| Trade / ERP / CMS / Pulse / providers | Their operational boundaries and valid policy enforcement points | Cross-platform canonical admission authority |
| Subscriptions | Subscription classification, commercial terms and entitlement lifecycle | Admission shortcuts or engine-native provisioning authority |

CP SHALL implement an explicit policy enforcement point for Baobab's own legal/contractual/security rules and enforce valid provider conditions it is required or authorised to honour. It shall not treat every negative regulations assessment as a global tenant block.

## 9. Lifecycle and boundary diagrams

### 9.1 From business need to eligible activation

~~~mermaid
flowchart TD
 A[Authenticated applicant or founding sponsor] --> B[Business profile and requested services]
 B --> C[Requirements applicability decision]
 C --> D{First party?}
 D -->|Yes| E[Governed founding sponsorship and bounded deferral]
 D -->|No| F[Progressive external policy]
 E --> G[Admission decision]
 F --> G
 G --> H[Authorised TenantOnboardingRequest]
 H --> I[Shared desired state and deterministic plan]
 I --> J[Digest bound approval and execution]
 J --> K[Provider and IAM provisioning]
 K --> L[Observed readiness and permitted capabilities]
 C --> M[Evidence claims and verifications]
 M --> G
 M --> L
~~~

### 9.2 Evidence must not become entitlement

~~~mermaid
flowchart LR
 A[Claim] --> B[Evidence and source]
 B --> C[Verification result]
 C --> D[Named policy assessment]
 D --> E[Admission eligibility]
 E --> F[Governed onboarding]
 F --> G[Product subscription and explicit grants]
 G --> H[Provider bindings and readiness]
~~~

Compliance/evidence assessment can supply a prerequisite; it shall never silently create a subscription, IAM grant or binding.

### 9.3 Corporate structure is not access

~~~mermaid
flowchart TB
 N[Nabhold Group Africa] -. sponsored/claimed relationship .-> Z[ZuriBeans]
 N -. sponsored/claimed relationship .-> T[Thamani Global]
 N -. sponsored/claimed relationship .-> E[Equator and Estate Co.]
 Z --> ZT[Isolated tenant]
 T --> TT[Isolated tenant]
 E --> ET[Isolated tenant]
~~~

Actual ownership verification and derived CorporateGroup state remain independent of the sponsored diagram.

## 10. Admission automation and separation of duties

Existing ADR-BCP-017 has explicit admission decisions; ADR-BCP-020/021 require scoped authority and distinct authorisation for consequential changes. This ADR permits a **future controlled low-risk automatic policy-decision route**, not a blanket replacement of those controls.

Requirements for any automatic decision:
- An independently approved, versioned, narrowly scoped policy.
- A registered workload/service principal with the right audience and least privilege.
- A durable assessment and deterministic reason/provenance.
- A reviewer/challenger and override/escalation path.
- Explicit exclusion of high-risk exceptions, disputed ownership, elevated admin grants, regulated provider provisioning and material cross-tenant changes.
- Audit of what policy and evidence were evaluated, without leaking secrets/PII.
- Separately authorised onboarding and plan execution where existing controls demand it.

Until implemented and certified, use the current authorised human maker/checker flow. No single applicant may authorise its own privileged tenant activation.

## 11. User experience: premium African enterprise platform

The CP Console and applicant workspace SHALL be business-language first, progressive and explainable.

- Ask for a business name, legal form, market, contact and intended services before soliciting optional registration documents.
- Show a clear difference between: applicant identity; registered/incorporated entity; unregistered venture; verification; missing items; authorised deferral; capability readiness.
- Ask only relevant questions. A business with no employees should not face a mandatory employer-document step without an applicable policy finding.
- Provide guided growth milestones and the next evidence request triggered by service expansion.
- Preserve drafts and resumability, transparent status, accessible errors and mobile-friendly essential actions.
- Explain blocked *capabilities*, rather than marking the whole enterprise defective.
- Make founders' and subsidiaries' independence visible without accidentally exposing parent access.
- Uphold existing Next.js/BFF server-side security, generated Shared OpenAPI types, WCAG 2.2 AA and the CP backend as sole authority.

Do not require the user to understand EngineInstance, CapabilityBinding, OIDC issuer internals or cloud tenancy to complete routine onboarding.

## 12. Subscription and economic scope

The founding group was the initial purpose of Baobab. It shall not be forced through a commercial sales funnel simply to use its shared platform.

Preserve existing accepted terms:
- INTERNAL ProductSubscription where authoritative eligibility exists.
- R0 platform charge for eligible first-party INTERNAL subscriptions.
- Usage metering, entitlement and audit remain enabled.
- Explicit CapabilityComposition, CapabilityGrant and CapabilityBinding.
- Change-of-control triggers reviewed classification changes; tenant identity remains stable.

This ADR **does not** set prices, billing tiers, trial durations, packaging, monetisation strategy or future customer subscription economics. Those decisions are reserved for a separate ProductSubscription/commercial ADR. The existence of the commercial classification vocabulary does not authorise inventing a revenue product in this work.

For an affiliate sponsored but not yet sufficiently proved to qualify for INTERNAL classification, retain an explicitly authorised transitional position under existing governance; do not fabricate the verified eligibility that the current classification runtime requires. If an amendment to classification policy is needed, specify and approve it at its actual authority boundary before enabling it.

## 13. Contract and runtime gaps to resolve

Implementation SHALL begin with a repo-verified contract-gap inventory, including:

1. **Legal-actor reference:** The current tenant registration contract requires legal_entity_id. Define how lawful sole proprietors, pre-incorporation ventures and sponsored operations map to real legal/contracting actors, without dummy companies or weakening tenant invariants.
2. **Kind-specific attestation:** Preserve ADR-BCP-024. Unverified Organisation and LegalEntity claims cannot bypass the type/role attestation boundary.
3. **Policy decisions:** Add or extend versioned policy/applicability/deferral records only after checking Shared's existing evidence and compliance definitions. Record what requirement applies, why, to whom, to which capability/market and for how long.
4. **First-party sponsorship:** Model audited sponsor/authoriser and scoped admission eligibility independently of verified corporate control and IAM membership.
5. **Admission routes:** Complete authorised INTERNAL_GROUP/assisted intake if missing. Apply a governed low-risk automatic route only after authorisation design and testing.
6. **Onboarding/provisioning:** Continue to use authorised requests, desired-state convergence, plan digest, idempotent execution, topology eligibility, readiness and drift; never reintroduce caller-selected engine manifests.
7. **Readiness:** Separate documented evidence posture, policy eligibility, production operation permission and provider readiness in APIs and UI.
8. **Events:** Use Shared event vocabulary and outbox publication. Never invent conflicting events or send sensitive documents through events.
9. **Frontend:** Use the pinned OpenAPI client, not frontend-only shadow decisions.
10. **Existing data:** Migrate with reversible, additive changes and preserve provenance. No forced Tenant ID replacement to fix legal form.
11. **Regulations seam:** Regulatory data may inform named assessments, but the runtime of the core platform must not depend on a fictitious completed Regulations integration.

No changes shall be described as implemented until tests and code evidence demonstrate them.

## 14. Implementation programme

| Gate | Work | Evidence of completion |
|---|---|---|
| PEO-00 | Audit current Shared/CP/IAM/Subscriptions and unblock legal-actor model | Implementation/contract delta matrix |
| PEO-01 | Accept ADR, specify Shared policy and evidence-applicability contracts | Contract tests, versioning and drift gate |
| PEO-02 | Implement 24-month founding sponsorship/evidence deferral with authority, reviews and expiry | Migrations, API, policy and time-bound tests |
| PEO-03 | Improve self-service admission and legal-form-specific evidence, preserving appropriate SoD | API integration and UI tests |
| PEO-04 | Register Nabhold and all named subsidiaries with truthful verification/relationship state | Idempotent controlled onboarding fixtures, registry evidence |
| PEO-05 | Authorise tenant desired state, IAM and eligible providers per subsidiary | End-to-end operations, isolation and readiness evidence |
| PEO-06 | Expose progressive journey and showcase in CP Console/digital estates | Browser, accessibility and security tests |
| PEO-07 | Production acceptance per permitted capability; gap and governance review | Signed-off acceptance matrix and operator runbook |

Gates may be split into bounded, cross-repository PRs. Do not hard-code the first-party names into production policy rules; configuration/fixtures may name them, while runtime supports generic sponsorship. Do not mark unavailable integrations as complete.

## 15. Acceptance tests

The following tests are minimum exit criteria:

| ID | Scenario | Expected |
|---|---|---|
| PEO-T01 | Authenticated emerging enterprise without PAYE/UIF certificates requests a low-risk product | No universal document gate; purpose-specific evaluation |
| PEO-T02 | No employees declared | Employer evidence may be non-applicable under named policy; declaration retains provenance |
| PEO-T03 | Payroll or regulated provider capability later enabled | New requirements evaluated before restricted operation |
| PEO-T04 | ZuriBeans pre-incorporation application | Organisation claim recorded; no fake verified corporate entity |
| PEO-T05 | First-party sponsor attests affiliated business | Independent authority recorded; not treated as proof of corporate control |
| PEO-T06 | Grace is approved for selected requirements | Exact 24-calendar-month deadline; outstanding claims stay outstanding |
| PEO-T07 | Grace reaches month 24 | Runtime denies deferral reliance; targeted reassessment and governed restriction |
| PEO-T08 | Rename/reapply/re-provision | Original deadline does not reset |
| PEO-T08A | Reach the twelfth month of a valid 24-month deferral | Deferral remains bounded and in force only for explicitly listed requirements; statutory/provider gates still apply |
| PEO-T08B | Existing pre-amendment 12-month deferral, if any | No automatic extension without independent recorded approval and policy migration |
| PEO-T08C | Month-end/leap-day start or expired 24-month record | Anniversary expiry is deterministic in UTC; no calendar rollover or replay extends the deadline |
| PEO-T09 | Statutory/provider check applies to payment settlement | First-party grace cannot waive it |
| PEO-T10 | Parent attempts sibling tenant access | Denied |
| PEO-T11 | First-party sponsor loses authority or subsidiary exits group | Review/reclassification/revocation; preserve historical Tenant ID |
| PEO-T12 | Applicant attempts to authorise own consequential onboarding | Denied |
| PEO-T13 | No eligible provider, stale approval or failed IAM projection | Correct blocked/not-ready state; no force-ready |
| PEO-T14 | Duplicate fixture execution | No duplicate Organisations/Tenants/deferrals |
| PEO-T15 | CIPC/URSB result unavailable | Not verified; non-applicable independent capabilities may remain available |
| PEO-T16 | External applicant falsely claims INTERNAL | Denied |
| PEO-T17 | Source evidence changes/revokes | Reassessment with traceable provenance, appropriate scope |
| PEO-T18 | Existing tenants upgraded to new contracts | Identity, access, audit, grants and status preserved |

| PEO-T19 | Unincorporated ZuriBeans and Equator & Estate founding businesses | Distinct Organisations and appropriately isolated Tenants; no fictitious incorporated LegalEntityProfiles |
| PEO-T20 | Thamani CIPC and subsidiary claim | Registration and parent-control assertions independently verified before either becomes VERIFIED |
| PEO-T21 | Nabhold proposed as responsible actor for two independent tenants | Independently approved per-business, per-role, per-market mandates; no cross-tenant access |
| PEO-T22 | Required property, import/export or settlement licence/authority absent | Affect only dependent capability; no statutory exemption under founding-group grace |

Include Go/PostgreSQL migration tests, contract generation/drift tests, IAM and tenancy isolation, concurrency/idempotency, negative security cases, and frontend journey/accessibility tests. Live registry-provider tests require actual authorised access and may be blocked rather than faked.

## 16. Risks, trade-offs and mitigations

| Risk | Mitigation |
|---|---|
| Premium positioning confused with burdensome KYB | Progressive relevance-based evidence and first-party grace |
| False legal-person assertions | Separate Organisation from LegalEntity; provenance and authoritative verification |
| Group exception becomes a privilege bypass | Scoped sponsorship, independent approval, expiry and audit |
| Missing documents used as general legal waiver | Separate obligations and enforce only applicable service/legal requirements |
| Regulatory decisions wrongly block tenants globally | Named versioned policy; decision/enforcement authority separation |
| Pre-incorporation entity cannot meet current tenant schema | Explicit real legal-actor design; no dummy ID; migrate contracts deliberately |
| Over-automation bypasses approval requirements | Approved low-risk policy/service principal; escalation and SoD |
| Expired grace causes data loss | Targeted capability restrictions, no tenant deletion |
| Revenue work distracts from founding mission | Commercial policy deliberately deferred |
| Complex cross-repo rollout stalls onboarding | First complete vertical, bounded PRs, honest capability readiness |

## 17. Alternatives considered

**A. Require complete registration/tax/employer documents for every applicant.** Rejected: not uniformly applicable, excludes emerging businesses, burdens first-party onboarding, conflicts with data minimisation.

**B. Exempt Nabhold from every governance, IAM and legal requirement.** Rejected: unbounded access and false compliance would undermine platform trust.

**C. Implement a Nabhold-only bypass API or database seeding shortcut.** Rejected: duplicates architecture, weakens testability and becomes an unmaintainable privileged path.

**D. Use a time-limited, evidenced first-party exemption plus generic progressive admission.** Chosen: serves original platform purpose, future enterprise usability and existing safety invariants.

## 18. Consequences

**Positive:** Nabhold can begin controlled onboarding without awaiting every administrative document; subsidiaries retain autonomy; emerging enterprises can access suitable services earlier; evidence collection is proportionate; reuse of Shared contracts and provider-neutral provisioning remains intact.

**Costs:** Requires changes to legal-actor modelling, applicability policy, admin authority, documentary deferral, contracts, UI and tests. A grace model creates timed operational work and auditable governance obligations.

**Not decided:** Commercial subscription packaging, external pricing, payment monetisation, universal incorporation rules, enterprise-specific legal advice, waiver of regulatory obligations, or a production release date.

## 19. Accepted amendment semantics and implementation approvals

As of acceptance on 2026-10-09, the following interpretations apply to future changes; Shared normative contracts and runtime implementations must still be amended and certified:
- ADR-BCP-017 shall no longer be interpreted as requiring a fully verified corporate registration and comprehensive tax/employer evidence **for every type of applicant before any admission**. Its explicit admission, authorisation and provisioning separation otherwise stands.
- ADR-BCP-018 shall be interpreted to allow an Organisation identity and governed first-party platform sponsorship without equating that to registered legal-person identity or verified corporate ownership.
- ADR-BCP-023 shall treat evidence collection and deferral as named-policy, purpose, risk and capability dependent; its claim/evidence/verification invariants remain fully authoritative.
- ADR-BCP-019 shall adopt a progressive, premium, business-language onboarding journey.
- ADR-BCP-020/021 remain the authority for machine policy approval, separation of duties and consequential mutation.
- ADR-SHARED-015 remains the authority for provider-neutral desired-state provisioning and readiness.
- Subscription charging and commercial packaging remain outside this amendment.

**Implementation prerequisites:** Architecture and Security SHALL review the corresponding Shared contract changes, legal-actor boundaries, pre-incorporation representation, maker/checker flows and grace-expiry mechanics before implementation or production activation. Individual legal responsibilities and permits require applicable review. **Acceptance of this ADR alone authorises no production bypass, legal waiver, tenant registration, capability activation or provider provisioning.**

---

**Outcome sought:** Baobab is an enterprise enablement platform built first for Nabhold Group Africa and its subsidiaries, and designed to welcome ambitious African enterprises at many stages of maturity—without sacrificing truthful evidence, legal responsibility, tenant isolation, operational reliability or explainable governance.
