# Decision Log

Durable record of platform-architecture decisions that are not themselves full ADRs but
that later work relies on having been settled. Each entry cites the evidence (migration,
source file, or ADR) it is drawn from; nothing here is asserted from prose alone.

---

## 2026-09-27 — ADR status reconciliation (proposed)

ADR-BCP-001 to ADR-BCP-010 and the Tenant Onboarding & Provisioning Technical Specification were still marked Proposed, although ADR-BCP-011 to ADR-BCP-024, Shared ADR-SHARED-008 to ADR-SHARED-015 and the implementation all treat them as normative. Each is classified from what later documents actually decide about it, not marked Accepted wholesale. ADR-0003 is superseded. Merging this entry is the acceptance.

| Document | Status | Evidence |
|---|---|---|
| ADR-0003 | Superseded | Its tenancy hierarchy and tiered entitlements are replaced by ADR-BCP-002, -003, -005 and -018; its context endpoint by ADR-BCP-004 and -007; its contract location by Shared `control-plane/v1` |
| ADR-BCP-001 | Accepted, amended in part | Technical Specification CR-002 (`binding_mode`), CR-004 (migration-count erratum), CR-005 (bindings through providers) |
| ADR-BCP-002 | Accepted, amended in part | CR-001 replaces its Gate 0; CR-002 rules out `READ_ONLY` as a binding mode |
| ADR-BCP-003 to ADR-BCP-009 | Accepted, amended in part | CR-001 replaces each one's Gate 0 with the single Phase-0 classification (`docs/reconciliation/phase-0-architecture-inventory-and-lock.md`) |
| ADR-BCP-010 | Accepted | No later document amends it |
| Technical Specification | Accepted, amended in part and refined | ADR-SHARED-008 supersedes CR-003 and §§47–48 (event types are `com.baobab-platform.*`); ADR-SHARED-015 refines §§21–27 and 42–44 |

The Specification's own erratum named `com.nabhold.*`, which ADR-SHARED-008 has since replaced with `com.baobab-platform.*`, and its §§47–48 showed the superseded `baobab.*` names without a marker; both are corrected. `docs/adr/index.md` becomes the complete register: it listed 7 of 29 documents.

Not changed by this entry: the implementation gaps the ADRs describe, for example ADR-BCP-006 health observation, ADR-BCP-020 AdministrativeGrant and ADR-BCP-023 evidence. An accepted ADR records the target; the gaps are tracked as work, not as reasons to leave the decision Proposed.

---

## 2026-09-25 — ADR-BCP-018 accepted

ADR-BCP-018 moves from Proposed to **Accepted — Normative Platform Architecture**. Its migrations, APIs, contracts, security policy, runbooks and readiness gates were already on `main`, and a normative ADR still marked Proposed was governance drift.

Evidence, gate by gate (migration numbers refer to `internal/store/postgres/migrations`; Shared paths are under `baobab-platform/shared` `contracts/`):

| Gate | Evidence |
|---|---|
| ORG-01 Shared contracts | `organisation/v1`, validated with Draft 2020-12 and negative fixtures; this repository pins it in `contracts.lock.yaml`, guarded by `TestSourceContractReferencesAreDeclared` |
| ORG-02 Organisation | 000045; `internal/repository/postgres_organisation*.go` |
| ORG-03 / ORG-12 LegalEntity and first-party reconciliation | 000045; `cmd/reconcile-first-party`; `legal-entity/registry.yaml` |
| ORG-04 CorporateRelationship | 000045; directed, evidence-backed, effective-dated lifecycle and divestiture review |
| ORG-05 CorporateGroup | 000045; `CorporateGroupDeriver` derives membership from verified relationships; triggers (000055) request it on every graph change and a worker plus scheduled sweep keep it current (runbook §13) |
| ORG-06 PlatformRelationship | 000045; verification, conflict and end transitions |
| ORG-07 PlatformAccount | 000045 and 000053: explicit tenant binding and the §83 lifecycle (#164) |
| ORG-08 Tenant mappings | 000045; explicit mappings and fail-closed context attestation |
| ORG-09 Admission | 000050 and 000054: ClientApplication, AdmissionDecision, organisation admission, and the TenantOnboardingRequest handoff (#165) |
| ORG-10 IAM Organisation | 000046; explicit Keycloak → ExternalReference → Organisation path |
| ORG-11 Subscription classification | 000051 and 000052; classification, provenance, drift and billing projection (#160, #161) |
| ORG-13 Buyer/supplier reconciliation | 000047; quarantine, never auto-merge |
| ORG-14 Isolation/security | production-path isolation tests |
| ORG-15 Drift/audit/metrics | 000048; drift rules, audit lineage, §130 metrics |
| ORG-16 Production readiness | 000049; indexes, append-only audit, post-restore verifier, runbook |

Known, recorded gaps that do not reopen the decision:

- The engine-side items in the ORG-11 certification record remain open: the Kill Bill and HyperSwitch providers and the outbox relays.

---

## 2026-09-13 — Programme Gate P0 checklist (BCP-TS-ONBOARDING-001 §87)

See `docs/reconciliation/phase-0-architecture-inventory-and-lock.md` for the full
inventory and evidence. Summary of the five locked/decided items:

1. **Binding enum lock.** `capability.capability_binding.binding_mode` is constrained to
   `PRIMARY`, `FALLBACK`, `SHADOW`, `MIGRATION`, `DISABLED` (migration `000028`). The
   superseded `SECONDARY`/`READ_ONLY`/`MIGRATION_SOURCE`/`MIGRATION_TARGET` values are
   defensively remapped, not assumed absent.
2. **Binding model lock.** `capability_binding.provider_id` exists (migration `000028`)
   alongside `engine_instance_id`, giving CR-005's shape a real column on both sides.
   Nullable pending a provider-registration backfill workflow (`#76`, `#82`).
3. **Event vocabulary lock.** Canonical event-type format is
   `com.nabhold.<context>.<...>.v<N>` (`baobab-platform/shared`'s
   `contracts/events/v1/envelope.schema.json`), already implemented by `internal/events`
   and verified end-to-end against real PostgreSQL and the real shared schema. This
   repository's own Technical Specification (CR-003) and `docs/adr/ADR-BCP-015` had
   independently specified a different, incompatible `baobab.*` format that no code here
   ever implemented; both documents are corrected by reference to `shared` ADR-SHARED-008.
   **Erratum (event namespace migration):** after the GitHub organisation rename,
   `baobab-platform/shared`'s envelope accepts only `com.baobab-platform.<context>.<...>.v<N>`
   (ADR-SHARED-008), and every event this repository emits now uses that namespace and is
   registered in Shared. `com.nabhold.*` is no longer emitted or accepted.
4. **ADR-BCP-001 migration-count erratum.** CR-004 in the Technical Specification
   documents the 17-vs-18 discrepancy. Moot in practice: the canonical migration set has
   grown to 32 files (`000001`–`000032`), all registered in `canonicalMigrationNames`.
   Implementation tooling relies on the actual migration files, not any prose count.
5. **Migration strategy decision.** `baobab-cp` extends its migration sequence forward
   (migrations `000019`–`000032` since the `docs/reconciliation/shared-control-plane-audit.md`
   baseline) rather than rewriting already-applied canonical migrations `000001`–`000018`,
   for the remainder of the pre-production period (Technical Specification §55).

**Gate status: PASSED.** Programme Gate P1 and beyond may proceed.
