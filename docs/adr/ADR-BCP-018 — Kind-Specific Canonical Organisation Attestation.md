# ADR-BCP-018 — Kind-Specific Canonical Organisation Attestation

**Status:** Accepted
**Date:** 2026-09-20
**Repository:** `baobab-platform/baobab-cp`
**Depends on:** ADR-BCP-004, ADR-BCP-014, ADR-BCP-016, ADR-BCP-017
**Gate:** ZB-04 (Buyer Onboarding)

## Context

ADR-BCP-016 deliberately permits both `BUYER_ORGANISATION` and
`SUPPLIER_ORGANISATION` in the generic `organisation_id` context dimension.
That is correct for general context resolution but insufficient for a
domain command whose safety depends on the organisation's exact canonical
kind.

ZB-04 admission must prove that the supplied canonical entity is an active
buyer organisation owned by the ZuriBeans tenant. A successful generic
organisation resolution cannot prove that: an active supplier organisation
would also resolve successfully. Trade must not infer the kind from an ID,
JWT, header, naming convention, or local copy.

## Decision

1. `POST /v1/platform-context/resolve` accepts the optional
   `expected_organisation_type` field alongside `organisation_id`.
2. An expected type is valid only when:
   - `organisation_id` is present;
   - it names a registered organisation type;
   - the resolved canonical entity's exact `EntityType` matches it.
3. A mismatch fails the complete resolution closed. No Context is persisted.
4. A successful response includes `organisation_id` and
   `organisation_type` as Control Plane attestations.
5. Existing callers remain compatible: omitting
   `expected_organisation_type` preserves ADR-BCP-016's generic
   organisation verification.
6. Downstream engines must request the exact kind when a command grants
   kind-specific authority. ZB-04 approval requests
   `BUYER_ORGANISATION`.
7. This endpoint verifies an existing canonical entity. It does not transfer
   organisation lifecycle ownership to Control Plane and does not implement
   the broader counterparty model deferred by ADR-BCP-014.

## Security properties

- A supplier canonical entity cannot be used to activate a buyer account.
- Cross-tenant, inactive, missing, non-organisation, and wrong-kind entities
  all fail closed.
- The workload token still requires `context:resolve`.
- Tenant reconciliation remains authoritative and unchanged.
- The response is an online attestation and is not cached by Trade for an
  admission decision.

## Consequences

The ZB-04 Trade admission coordinator can consume one authoritative
attestation instead of combining a generic context result with an
unverified local assumption. Generic supplier and buyer context resolution
continues to work for callers that do not request a specific kind.
