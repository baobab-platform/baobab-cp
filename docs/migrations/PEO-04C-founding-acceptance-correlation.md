# PEO-04C / LA-06 — founding-group acceptance correlation (ADR-BCP-026/027)

**Implementation boundary:** The Control Plane can already recognise first-party Organisations without legal-person fabrication (LA-03), execute human-reviewed Organisation-first four-Tenant registration against PostgreSQL 17 (PEO-04B), and assess independent company registration and corporate-control evidence (PEO-04A).

This increment joins the three streams **without** authorising payment, trade, ERP posting, a legal person, shared parent tenant access or provider activation.

## Four founding identities and authority

| Shared first-party ID | Identity class | Incorporated claim | Tenant ownership | Required evidence for admission |
|---|---|---|---|---|
| NABHOLD | LEGAL_PERSON | REGISTERED_EVIDENCED | Independent PRIMARY Organisation and Tenant | Fresh independently accepted Nabhold OEV-03 CIPC case |
| THAMANI-GLOBAL | LEGAL_PERSON | REGISTERED_EVIDENCED | Independent PRIMARY Organisation and Tenant | Fresh independently accepted Thamani OEV-03 CIPC case |
| ZURIBEANS | OPERATING_BUSINESS | NOT_INCORPORATED | Independent PRIMARY Organisation and Tenant | Human-approved current founding sponsorship; no fabricated CIPC profile; scoped legal-actor mandate when restricted operations are requested |
| EQUATOR-ESTATE | OPERATING_BUSINESS | NOT_INCORPORATED | Independent PRIMARY Organisation and Tenant | Same guard, separate sponsorship and actor mandates; property-specific authorisations remain independent |

The registered Nabhold→Thamani ownership claim is evaluated and returned separately. A lack of independently established shareholding does not prevent a separately qualified Thamani tenant, but it blocks any group-affiliate status **that depends on that claimed control**.

## Acceptance algorithm

1. Accept the exact four source-controlled first-party IDs, forms and incorporation claims. Never derive additional founding eligibility from a brand, arbitrary applicant field or platform group membership.
2. Read current PRIMARY tenant assignment for each Organisation. Duplicate tenants, duplicate Organisations, missing or non-isolated PRIMARY mappings fail closed.
3. Require real company verification *separately* for Nabhold and Thamani via accepted PEO-04A OEV-03 result identifiers.
4. Require current separately approved founding sponsorship and scope-bound legal actor evidence where an unincorporated enterprise operates under another legal person. This gate consumes the existing PEO-04B read-only roster judgement, and does not create the approvals.
5. Emit `IDENTITY_TENANT_ACCEPTED_NOT_PRODUCTION` only when the identity/tenant/evidence controls converge, otherwise `PENDING` with precise unresolved requirements. Group identity readiness and independent control status are separate fields.
6. No business entity receives a positive legal/incorporation, commercial, IAM, PSP, provider-support or go-live claim from this report. Existing human-approved progressive admission/onboarding commands remain the **sole write path**, proven synthetically in the existing PEO-04B PostgreSQL test.

## Still external dependencies

- Current reviewed government records for both registered companies, independently sourced corporate-control attestations for group-affiliate assertions, and company-secretarial sign-off.
- Live founder sponsorship approvals, independently scoped mandate records, and accepted human reviewer decisions.
- A protected staging proof of tenant isolation, market/account entitlements, IAM principal binding, and physical cloud/infrastructure desired-state execution.

**Sequence:** Complete these capability-specific operational proofs before claiming Package 3 fully accepted. LA-05 / Package 4 closes downstream consumer bypasses independently. Never turn this report into a blanket global verifier.
