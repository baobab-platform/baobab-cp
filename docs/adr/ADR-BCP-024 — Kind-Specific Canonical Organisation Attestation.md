# ADR-BCP-024 — Kind-Specific Canonical Organisation Attestation

**Status:** Accepted
**Date:** 2026-09-20
**Repository:** `baobab-platform/baobab-cp`
**Depends on:** ADR-BCP-004, ADR-BCP-014, ADR-BCP-016, ADR-BCP-017, ADR-BCP-018
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
   `expected_organisation_type` field alongside `organisation_id` or
   `iam_organization` (ADR-BCP-018 gate ORG-10).
2. An expected type is valid only when:
   - an organisation is named, by `organisation_id` or by IAM organisation
     evidence that resolves to one;
   - it names a registered organisation type;
   - the resolved canonical entity's exact `EntityType` matches it.
3. A mismatch fails the complete resolution closed. No Context is persisted.
   The kind is compared only after the organisation is attested for the
   tenant (ADR-BCP-018 gate ORG-14), so a caller cannot learn the kind of an
   organisation its tenant is not attested for.
4. A successful response includes `organisation_id` and
   `organisation_type` as Control Plane attestations.
5. Existing callers remain compatible: omitting
   `expected_organisation_type` preserves ADR-BCP-016's generic
   organisation verification.
6. Downstream engines must request the exact kind when a command grants
   kind-specific authority. ZB-04 approval requests
   `BUYER_ORGANISATION`.
7. This endpoint verifies an existing canonical entity; it creates and
   changes nothing. Organisation authority is settled by ADR-BCP-018 (section
   194), not by this ADR.
8. The exact kind is the canonical entity's `EntityType`. That is the
   bounded ADR-BCP-016 model, which ADR-BCP-018 keeps valid during
   migration. When ADR-BCP-018 gate ORG-13 generalises buyer and supplier
   organisations into Organisation plus counterparty roles, the attestation
   SHALL be extended so that an Organisation holding the corresponding
   active role satisfies the expected kind. Existing callers keep the same
   request and response shape.

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
