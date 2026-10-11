# PEO-05A — founding tenant IAM and provider qualification

**Authority:** Accepted ADR-BCP-026 PEO-05 and PEO-T10/12/13, ADR-BCP-027 LA-06/LA-07, ADR-SHARED-015, ADR-IAM-0033.

**Status:** Implemented read-only policy composition and negative unit tests; integration adapters, protected runtime wiring and real attestation **not yet complete**. This file is not a certificate of production readiness.

## Scope

The PEO-04C gate already correlates four distinct governed Organisation/PRIMARY Tenant identities with independently sourced Nabhold/Thamani registration evidence. This increment adds an independent, context-bound check before a tenant can even be reported as a **candidate for protected staging validation**.

Each tenant is assessed separately. The report explicitly sets ProductionEligible=false. It does not create a Tenant, issue IAM access, grant a capability, select a provider, certify an incorporation claim, set READY or bypass legal-actor policy.

Read-only evaluation order:

1. Rerun PEO-04C, including OEV-03 and Package 3 tenant identity. No caller-supplied acceptance flag.
2. Demand exact Organisation/Tenant pair and distinct onboarding requester/authoriser.
3. Require a frozen desired-state digest that matches the plan's source digest and an unexpired independent plan approval over the identical plan digest.
4. Require current canonical provisioning READY and a tenant-bound ACTIVE ProductSubscription.
5. Require exact IAM issuer/audience/Organisation/Tenant membership with fresh authenticated revocation evidence.
6. Resolve all MANDATORY capabilities from the authoritative current composition and require unique, tenant-bound, certified providers with correct residency and fresh HEALTHY observations.
7. Return an explainable BLOCKED report for every missing prerequisite. Even an entirely synthetic positive report is only READY_FOR_PROTECTED_STAGING_VALIDATION_NOT_PRODUCTION.

No corporate ownership or sponsorship directly supplies CP administrative rights, IAM membership or sibling Tenant access.

## Engineering tests

The unit suite covers the complete synthetic four-business scenario and negative cases including: missing CIPC verification, missing IAM authority, cross-tenant/Organisation substitution, maker/checker self-approval, stale plan, desired-state digest tampering, revoked subscription or membership, missing/expired issuer or revocation proof, hidden or duplicate mandatory capability, uncertified provider, missing health, wrong residency, duplicate provider, unbound tenant, missing EngineInstance and production-eligible report tampering.

Run: go test ./internal/service/organisation

## Runtime wiring still required

The PEO05TenantReader is a **trusted server-side port**, not an applicant payload and not a finished PostgreSQL adapter. Implementation work for the next Package 5 tasks must compose it from independently verified sources:

| Evidence | Owner | Required runtime implementation |
|---|---|---|
| Current desired state and approval | CP | Read current immutable desired_state/plan/plan_approval/tenant_provisioning; recheck original onboarding and current revision; reject historical superseded plans |
| Tenant subscription and entitlement | CP/Subscriptions | Read the tenant's actual subscription and classification; prove live revision and no revocation |
| IAM membership and revocation | IAM + CP | Workload-authenticated provider-neutral issuer, subject, tenant/Organisation membership, assurance, expiration and revocation evidence; no email, corporate ownership, or Keycloak-only role shortcut |
| Current provider support | CP registry + each engine | Derive required capabilities from the current composition, enforce approved support/certification/contract major and current binding per tenant |
| Runtime health | CP topology observations | Require fresh independently observed health and residency-compatible engine instance, not a deployed flag or stale test fixture |
| End-to-end proof | Infrastructure | Distinct tokens for four tenants, sibling-denial, TTL/revocation/race tests, recorded operation and signed staging acceptance |

The next PR must implement these readers and prove that a trusted runtime rather than test code produces each field. A current boolean from an applicant or a first-party registry YAML is **never** accepted as IAM/provider readiness.

## Package 5 task order

- **PEO-05A:** Read-only, fail-closed evidence correlation. This PR.
- **PEO-05B:** Bind governed desired state and tenant-scoped IAM membership/revocation to actual CP/IAM sources.
- **PEO-05C:** Wire canonical capability compositions, current provider-support certification and observed health with per-subsidiary isolation.
- **PEO-05D:** Prove protected IAM-authenticated staging end-to-end, negative races and audit, then independently review the acceptance matrix.

Gate exit requires current signed/scoped evidence; passing Go CI alone is insufficient. Production remains disabled until PEO-07/LA-07 and real external legal/PSP/provider approvals are separately complete.

## Operator interpretation

BLOCKED means one or more required sources are missing, unavailable, revoked, stale, inconsistent or not independently certified. It does **not** mean the business is invalid. It also does not delete the tenant or require universal payroll/employer documents: legal/provider restrictions remain capability- and market-specific.

READY_FOR_PROTECTED_STAGING_VALIDATION_NOT_PRODUCTION means the deterministic evaluation has no listed blocker, but a protected end-to-end rehearsal and independent acceptance are still required. It is not permission to make a payment, post ERP documents, complete a Trade checkout, claim legal incorporation or activate production access.
