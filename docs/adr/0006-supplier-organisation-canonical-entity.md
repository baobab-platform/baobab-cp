# ADR-0006: Supplier Organisation Canonical Entity Registration

**Status:** Accepted
**Date:** 2026-09-07
**Repository:** `baobab-platform/baobab-cp`
**Depends on:** ADR-BCP-001
**Contract:** `baobab-platform/shared/contracts/supplier-onboarding/v1` (see `baobab-platform/shared` ADR-0006)

## Context

`baobab-platform/thamani` is building a supplier registration, vetting and
cross-border sourcing capability (see `baobab-platform/shared` ADR-0006 for the
cross-repository context, including why `baobab-platform/zuribeans`' own
independently-built supplier-registration slice does not resolve this
program's discovery). That capability needs a canonical entity kind so a
supplier organisation can eventually be resolved and mapped the same way a
`PRODUCT` already is.

Per ADR-BCP-001, `CanonicalEntity`, `Mapping`, `MappingScope`, `Market`,
`DigitalEstate`, `Engine`, `EngineInstance`, `Capability`,
`CapabilityBinding`, `Context` and `IsolationProfile` are the frozen
vocabulary — this ADR must reuse them, not redefine them. Investigation of
`internal/domain/canonical.go` confirms `CanonicalEntity.EntityType` is
already a plain, unconstrained string with no enum or switch anywhere in
the domain, service, or repository layers: registering a new kind is a
data-driven decision, not a code change.

## Decision

`SUPPLIER_ORGANISATION` is a registered canonical entity kind, named as the
constant `domain.EntityTypeSupplierOrganisation` (`internal/domain/entity_types.go`)
for callers to reference instead of a repeated string literal. Its
canonical key follows the existing `namespace:type` pattern, e.g.
`supplier:thamani_global:sup_01k4p8q2r3s4`.

No other code changes: `CanonicalEntityService.Create`, `Validate`,
`Activate`, `Suspend`, and `Retire` already operate generically over
`EntityType` and require no modification to accept this kind. This ADR
does not add a Mapping, MappingScope, HTTP endpoint, or migration —
`docs/reconciliation/canonical-contract-matrix.md` already documents that
`Mapping`/`MappingScope` are thinner than their own accepted schema, and a
supplier-onboarding consumer registering entities today inherits that
existing limitation rather than this ADR introducing a new one.

## Consequences

- `baobab-platform/thamani` (or any hosting estate) may call the existing
  `POST /v1/canonical-entities` endpoint with
  `entity_type: "SUPPLIER_ORGANISATION"` once it is ready to register a
  supplier organisation's canonical identity — this ADR does not itself
  perform that registration.
- This does not resolve canonical Organisation ownership.
  `baobab-platform/shared`'s `contracts/erp/v1/system-of-record.yaml` keeps that
  concept's `canonical_owner` `unassigned` (CI-enforced there), and a
  `SUPPLIER_ORGANISATION` canonical entity here is a distinct, narrower
  concept — an estate-scoped supplier record, not the platform-wide
  Organisation concept.
- A future ADR resolving the `Mapping`/`MappingScope` schema gaps recorded
  in `docs/reconciliation/canonical-contract-matrix.md` benefits this kind
  the same way it benefits `PRODUCT` today; nothing here is a workaround
  that a later fix would need to unwind.
