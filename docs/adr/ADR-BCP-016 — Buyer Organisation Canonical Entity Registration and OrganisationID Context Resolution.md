# ADR-BCP-016 — Buyer Organisation Canonical Entity Registration and OrganisationID Context Resolution

**Status:** Accepted
**Date:** 2026-09-17
**Repository:** `nabhold/baobab-cp`
**Depends on:** ADR-0006, ADR-BCP-004, ADR-BCP-010 §41
**Related:** ADR-BCP-014
**Gate:** ZB-03.2 (Canonical identity and organisation spine)

## Context

`docs/reconciliation/gate-zb03-authority-contract-freeze.md` (ZB-03.0)
identified two related gaps blocking ZB-03.3 (Buyer IAM → CP → Trade
integration):

1. `internal/provisioning.ContextResolutionRequest` and
   `AuthoritativeContextResolver.Resolve` had no `OrganisationID` stage.
   `resolver.ResolutionEvidence.OrganisationID` and `resolver.Context.OrganisationID`
   already existed and are already used by `resolver.MappingScope` matching
   (`internal/resolver/context.go`, `entitlement.go`) — a caller-asserted
   organisation identifier had a place to land in the resolved `Context`, but
   nothing on the CP side verified it before trusting it. Per this
   session's established never-trust-client-supplied-context-headers
   principle (already applied to `MarketID`, `DigitalEstateID` and
   `IsolationProfileID`), that gap had to close before any downstream
   engine could rely on `Context.OrganisationID`.
2. Two accepted ADRs speak to "organisation" without being contradictory,
   but without being reconciled in writing either:
   - ADR-0006 registers `SUPPLIER_ORGANISATION` as a `CanonicalEntity.EntityType`,
     explicitly scoped: *"This does not resolve canonical Organisation
     ownership... a distinct, narrower concept — an estate-scoped supplier
     record, not the platform-wide Organisation concept."*
   - ADR-BCP-014 describes a considerably broader
     `CanonicalEntity → Organisation/Person → CounterpartyProfile →
     CounterpartyRole/CounterpartyRelationship` model, covering every
     shape a counterparty organisation can take (supplier, customer,
     distributor, carrier, etc.) with role and relationship history. None
     of `CounterpartyProfile`, `CounterpartyRole` or
     `CounterpartyRelationship` exist in code as of this ADR.

ZB-03.2's scope, per this session's "basics" precedent (deliberately bounded
slices that unblock the next dependent gate rather than building every
future-facing concept at once), is: register a `BUYER_ORGANISATION`
canonical entity kind, and wire fail-closed CP-side verification of a
caller-asserted `organisation_id` into `AuthoritativeContextResolver`. The
full `CounterpartyProfile`/`CounterpartyRole`/`CounterpartyRelationship`
apparatus ADR-BCP-014 describes remains future work — nothing in ZB-03.3
through ZB-03.6 as scoped by the ZB-03 programme requires a buyer
organisation to hold multiple simultaneous roles or a relationship history
yet, and ADR-BCP-014 itself does not describe an implementation deadline.

## Decision

1. `BUYER_ORGANISATION` is a registered canonical entity kind, named as the
   constant `domain.EntityTypeBuyerOrganisation` (`internal/domain/entity_types.go`),
   following ADR-0006's exact precedent for `SUPPLIER_ORGANISATION`: a plain
   string registered for callers to reference instead of a literal, no
   schema or enum enforcement added, registered through the existing
   `CanonicalEntityService.Create` API. Its canonical key follows the same
   `namespace:type` pattern, e.g. `buyer:zuribeans_ug:buy_01k4p8q2r3s4`.

2. `domain.OrganisationEntityTypes` (`internal/domain/entity_types.go`) is a
   new, explicit registry of every `CanonicalEntity.EntityType` value the
   control plane recognises as "an organisation" for context-resolution
   purposes: today, `BUYER_ORGANISATION` and `SUPPLIER_ORGANISATION`. A
   canonical entity of any other `EntityType` — including `PRODUCT` — can
   never be asserted as a request's `organisation_id`, whatever its ID's
   well-formedness.

3. `ContextResolutionRequest` (`internal/provisioning/context_resolver.go`)
   gains an optional `OrganisationID` field. When supplied,
   `AuthoritativeContextResolver.Resolve` verifies, before trusting it:
   - the identifier names a real `CanonicalEntity` (`ContextAuthority.GetCanonicalEntity`,
     backed by `*repository.PostgresRepository`/`*repository.Repository` via
     the existing `ContextAuthorityRepo`/`ContextAuthorityAdapter` chain —
     no new repository dependency introduced);
   - its `EntityType` is in `domain.OrganisationEntityTypes`;
   - its `Status` is `"ACTIVE"`;
   - its `OwnerTenantID` equals the requesting tenant's ID.

   Any failure of these checks fails the whole `Resolve` call closed (no
   partially-resolved `Context` is ever returned), matching every other
   verification stage in this resolver. On success, `OrganisationID` is
   populated on both `resolver.ResolutionEvidence` and the resulting
   `resolver.Context`, and recorded in `Context.Provenance` with
   `TrustLevel: resolver.TrustSystem` and source
   `"baobab-cp:canonical-registry"` — consistent with how `MarketID` and
   `DigitalEstateID` are already sourced and attributed.

4. This ADR does not implement `CounterpartyProfile`, `CounterpartyRole`,
   or `CounterpartyRelationship`. A `BUYER_ORGANISATION`/`SUPPLIER_ORGANISATION`
   canonical entity today has exactly one implicit role, spelled by its
   `EntityType`; it cannot hold multiple simultaneous roles or an explicit
   relationship history. A canonical entity registered under this ADR is
   forward-compatible with ADR-BCP-014's model: `CanonicalEntity` is already
   that model's root node, so a future
   `CounterpartyProfile`/`CounterpartyRole` implementation attaches beneath
   the same `CanonicalEntity` row rather than requiring a migration of this
   ADR's data.

## Consequences

- ZB-03.3 (Buyer IAM → CP → Trade integration) can now pass a
  Keycloak-Organization-derived `organisation_id` into
  `ContextResolutionRequest` and get back a `Context.OrganisationID` that
  `baobab-trade` can trust was verified by CP, not merely echoed from a
  client-supplied header or JWT claim.
- `resolver.MappingScope.OrganisationID` matching
  (`internal/resolver/context.go` `matchScope`, `internal/resolver/entitlement.go`)
  now has a CP-verified `Context.OrganisationID` to match against for the
  first time — previously that field could only ever be its zero value in
  any `Context` produced by `AuthoritativeContextResolver`.
- `SUPPLIER_ORGANISATION`-kind canonical entities registered under ADR-0006
  are, without any further change, now also valid `organisation_id` values
  for context resolution — a supplier organisation is as much "an
  organisation" as a buyer one for this purpose, and ADR-0006's narrower
  scoping (estate-local supplier record, not platform-wide Organisation
  ownership) is unaffected by this ADR.
- The platform-wide Organisation-ownership gap this ADR's context
  (`gate-zb03-authority-contract-freeze.md`) and ADR-0006 both flag —
  `nabhold/shared`'s `contracts/erp/v1/system-of-record.yaml` still keeps
  `canonical_owner: unassigned` for the broader Organisation concept — is
  not resolved by this ADR. This ADR resolves only enough of that gap for
  CP to fail closed when verifying a caller-asserted `organisation_id`; it
  does not decide who owns Organisation lifecycle authority platform-wide.
  That remains ADR-BCP-014's (or a future ADR's) decision to make.
- A future ADR implementing `CounterpartyProfile`/`CounterpartyRole` per
  ADR-BCP-014 supersedes this ADR's single-implicit-role model without
  needing to reverse anything registered under it.
