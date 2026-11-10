# ERP finance, People and WMS — CP governed enablement boundary
**Date:** 2026-11-10. **Authority:** Shared ADR-SHARED-017, accepted ADR-SHARED-033/045; ERP ADR-ERP-023/024; CP ADR-BCP-003/006/020/021.

## Registered is not enabled

CP pins Shared `584d7f3cd8426ffd3d1dc9f068f0b444dc74346e` and projects **42** canonical capability definitions through the existing `capabilitycatalogue.SyncEmbedded`. F1, People and WMS form nine additional ERP-owned DRAFT catalogue entries. No extra CP-local definitions, simulated instances, tenant entitlements, grants, bindings or operational routes are created by the projection. Finance F2/F3 are also canonical DRAFT after merged Shared #453, without implemented support, grants or binding.

## Required operational pathway

1. ERP (or qualifying incumbent for People/WMS) implements one canonical operation and publishes an evidence-backed engine provider declaration: `IMPLEMENTED` support only. No status promotion from the vendor's assertion alone.
2. CP imports the independently verified exact provider registration, preserving `DRAFT` lifecycle, current owner, contract major, logical service reference and legal-actor provenance. Never synthesize support from the catalogue or a draft plan.
3. Enterprise-specific capability grants and provider/instance bindings require authorised onboarding, product subscription/entitlement, market, employer/warehouse/operator or finance book scope and current IAM principal; default deny when missing.
4. EA-09 certification, approved engine release, independent health/contract readback and provider-neutral selection are recorded. `PROVIDER_ACTIVATION` changeset with maker/checker separation is required; newly added provider activation check refuses **any** support whose canonical lifecycle is not ACTIVE.
5. Re-resolve current provider support, Active catalogue lifecycle and current grant at invocation. No stale scope, revoked provider, unsupported contract, foreign legal entity, unknown health, WMS owner/operator confusion or unapproved journal authority may execute.

## Qualification holds

- F1: Finance F1-P01–P11 against native iDempiere/PostgreSQL, balanced journal, SoD, closed period, reversal, crash/replay, accountant sign-off and no unauthorized money movement.
- F2/F3: nine canonical DRAFT keys now exist under Shared #453; separate native implementation, Finance policy, integration evidence and provider activation remain required.
- People: BYOP legal employer/provider choice; workforce, statutory golden vectors, recipient safety and revocation; no payroll GL or payment release inferred.
- WMS: BYOP operator/depositor/service-customer separation, inventory ownership/custody, 3PL SLA, effective market rules, WMS-00–WMS-12, and separate native ERP material accounting/Trade reservation/TMS custody authority.

**Runtime activation status: HELD.** This CP change updates canonical catalogue projection and fail-closed admission. It neither certifies any of the nine keys nor grants tenants an operable finance/People/WMS service.
