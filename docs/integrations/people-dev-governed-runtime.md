# People development: governed BYOP selection

**Status: engineering candidate, not activated or payroll certified.**

## Runtime boundaries

Accepted Shared #457 (commit c0d3dd935e4b635fd38f113b280a220e552bc908) admits a standing, least-privilege development People reader. IAM #190 implements governed Ory client creation after independently signed maker/checker approval; no live issuance has been demonstrated.

CP now proposes an employer-specific provider choice as DRAFT and approves it through a separate current human administrator with a different Principal. The opt-in PEOPLE_DEV_CHOICE_MUTATION_ENABLED flag mounts the routes only in development/integration, never staging/production. They require changeset:write and changeset:approve plus current admin roles respectively.

Proposals are closed JSON with tenant, legal employer, jurisdiction, capability payroll.run.read, binding UUID, certification UUID, evidence reference and bounded effective time. Provider ID, approvals, status, secret and employee data cannot be supplied. CP derives provider and revision from its ACTIVE PRIMARY binding. Before mutation and again at commit, PostgreSQL requires **separate ACTIVE workforce memberships for both maker and checker**, scoped to the exact tenant and legal employer, each with `cp:tenant-admin`. An unrelated tenant administrator or a disabled/reassigned membership cannot approve a provider choice, even if a general admin bearer remains valid. PostgreSQL additionally checks a current human principal, ACTIVE grant, provider, release, supported contract, engine instance, and independent EA-09 CERTIFIED record with qualification profile people/external-dev-v1. Serialisable transactions and one-active-source exclusion fence replay and concurrent checker decisions. This CP code **does not** create EA-09 certifications, promote canonical DRAFT People capabilities, establish vendor credentials or sign source evidence.

ERP #156 adds one read-only external Frappe HR adapter. It uses current CP selection and employer-private HTTPS credentials to query Payroll Entry status metadata: name, company, docstatus and modified only. It does not read salaries or employee data, import or calculate payroll, file tax, post GL or release payments. Vendor account and independent source-signed qualification evidence have not been supplied and a live adapter call has not been witnessed.

## Proposed endpoints (all disabled by default)

- POST /v1/admin/people/provider-choices — requires current human changeset:write; creates DRAFT.
- POST /v1/admin/people/provider-choices/{choiceID}/approve — requires a *different* current human changeset:approve and exact expected_version; rechecks authority, then activates only when all native gates pass.
- POST /v1/admin/people/provider-choices/{choiceID}/revoke — requires a current exact-employer `cp:tenant-admin` human distinct from the original maker and exact `expected_version`; atomically transitions ACTIVE → REVOKED. A replay or stale version fails, and the original choice remains auditable. CP reads refuse REVOKED immediately, independently of bearer expiry.

## Guardrails

No default People provider, tenant sibling inheritance, automatic provider activation or protected deployment is added. A source-only test is not EA-09, statutory, or vendor qualification. ZA PAYE/UIF/SDL/ETI and UG URA/NSSF expert-reviewed vectors, employer data rights, source-signed receipts, incident replay and actual licensed vendor account remain separate HOLD conditions. No protected staging/production or real employer activation is authorised by merging code.
