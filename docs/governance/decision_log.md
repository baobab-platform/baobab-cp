# Decision Log

Durable record of platform-architecture decisions that are not themselves full ADRs but
that later work relies on having been settled. Each entry cites the evidence (migration,
source file, or ADR) it is drawn from; nothing here is asserted from prose alone.

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
   `com.nabhold.<context>.<...>.v<N>` (`nabhold/shared`'s
   `contracts/events/v1/envelope.schema.json`), already implemented by `internal/events`
   and verified end-to-end against real PostgreSQL and the real shared schema. This
   repository's own Technical Specification (CR-003) and `docs/adr/ADR-BCP-015` had
   independently specified a different, incompatible `baobab.*` format that no code here
   ever implemented; both documents are corrected by reference to `shared` ADR-SHARED-008.
4. **ADR-BCP-001 migration-count erratum.** CR-004 in the Technical Specification
   documents the 17-vs-18 discrepancy. Moot in practice: the canonical migration set has
   grown to 32 files (`000001`–`000032`), all registered in `canonicalMigrationNames`.
   Implementation tooling relies on the actual migration files, not any prose count.
5. **Migration strategy decision.** `baobab-cp` extends its migration sequence forward
   (migrations `000019`–`000032` since the `docs/reconciliation/shared-control-plane-audit.md`
   baseline) rather than rewriting already-applied canonical migrations `000001`–`000018`,
   for the remainder of the pre-production period (Technical Specification §55).

**Gate status: PASSED.** Programme Gate P1 and beyond may proceed.
