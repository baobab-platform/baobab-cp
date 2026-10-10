# PEO-03A — Independent staff review visibility for progressive submissions

**Authority:** ADR-BCP-026/027; existing applicant v2 contract and existing platform-admin role/ `admission:review` scope. **No new foundational ADR.**
**Acceptance status:** staged, review-only building block; NOT the completed admission-decision bridge or authorisation for real onboarding.

The previous PEO-03 increment implemented applicant-owned v2 creation, draft modification and submission. The next necessary step is a separate, authenticated **read-only reviewer projection**, before a maker/checker admission decision can responsibly operate on progressive application evidence.

| Route | Guard | What it returns |
| --- | --- | --- |
| `GET /v2/admin/client-applications?limit=25` | Registered human CP platform admin + `admission:review` | At most 100 *SUBMITTED* applications; skips reviewer’s own applications |
| `GET /v2/admin/client-applications/{applicationID}` | Same | Only another applicant's *SUBMITTED* record; missing/private DRAFT returns 404 |

All routes stay disabled unless `PEO_PROGRESSIVE_ADMISSION_ENABLED=true` and `BAOBAB_ENVIRONMENT` is one of the strict nonproduction allow-list values. Production and unknown environment are refused. Both routes read applicant-provided **claims and evidence references**, never a verified legal identity. Responses use `Cache-Control: no-store`.

The reviewer cannot approve, rewrite evidence, infer an incorporated company, grant INTERNAL entitlement, generate a TenantOnboardingRequest, create a tenant, or activate a legal mandate from these endpoints. No application life-cycle state changes occur. The reviewer is never JIT-registered; applicant self-review is denied even if the identity later acquires a staff role. These reads still need independent operator privacy review and pagination/correlation operational proof before a broad release.

## PEO-03B — required next implementation

A governed admission-decision bridge must be implemented with **specific new v2 persistence and Shared contract convergence**, not by stuffing `business_identity` into the v1 `organisation_profile` with fictitious registration identifiers.

To close PEO-03:

1. Add signed/versioned applicability and real evidence review, independent verified authority, immutable decision provenance, distinct reviewer/decider, and challenge/appeal lifecycle for the submitted v2 application.
2. Establish immutable v2 `AdmissionDecision` and derive scope, classification, product requirements, and per-market participation from independently reviewed evidence — *never from applicant-selected entitlement fields*.
3. Implement a distinct operator-requested, second-principal-authorised **Organisation-bound** `TenantOnboardingRequestV2` conforming to Shared `admission/v2/onboarding.schema.json`. It must reference the exact reviewed decision and pre-tenant Organisation identity.
4. Converge the LA-03B `/v2/tenants` server path, which today requires the older v1 `TenantOnboardingRequest`, on an explicit, accepted v2 request authority. No silent v1 fallback or v1 incorporation prerequisites.
5. Test atomic registration + immutable primary Organisation binding, request fulfilment, enrolment *pending* product status, SoD, revocation and replay, then submit for independent staging acceptance.

**Nothing here confers a legal person, a founding sponsorship, a corporate relationship, an ERP accounting entity or a tenant.** No production route authorisation or real human admission decision has been granted.
