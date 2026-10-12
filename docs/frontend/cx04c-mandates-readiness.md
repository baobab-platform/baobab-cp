# CX-04C — governed legal-actor mandate and attested readiness readback

**Source:** Shared `e875e738d2dd3022964ee539327ff647712dcd18` (merged Shared #297).
**Production acceptance:** NOT IMPLIED by passing GitHub checks.

This increment adds a separate, nonproduction-gated, read-only
`GET /v1/organisations/{organisation_id}/governance`. It does not add
mandate approval, tenant activation or provider certification paths.

## Independent authorisation decisions

1. A current authenticated ACTIVE human platform administrator with a
   `canonical:read` bearer and exact `organisation.view` for the requested
   organisation must pass before any child resource can be read.
2. The organisation's in-effect, registry PRIMARY association is read from
   `registry.tenant_organisation_mapping`, and an independently effective
   EXACT `tenant.view` is required for each child tenant.
3. Mandate readback requires the **new** independent
   `legal-actor-mandate.view` at the exact tenant scope (not inferred from
   `organisation.view`, `mandate.decide`, `tenant.view`, or group
   membership). It reads `registry.operating_legal_actor_mandate` and
   immutable `registry.operating_legal_actor_mandate_decision` records.
4. Readiness readback requires separate exact `readiness.view` for that
   tenant. The latest immutable `provisioning.readiness_snapshot` is the
   source, not tenant desired state or marketing certification.
5. Individual permissions are reflected as `mandates_visible` and
   `readiness_visible`, differentiating denied from an authorised empty
   result. No privileged response is cached.

## Projection semantics

- Mandates display source-backed activity, market and optional capability
  restrictions, as well as status, roles, legal-entity reference, window,
  provenance, decision status/time, evidence **count** and presence of
  recorded verification reference. No evidence text, actor identity,
  unrestricted addresses, basis document or reviewer personal ID is shared.
  `APPROVE` is explicitly different from `ACTIVE`; `ACTIVE` remains
  subject to current-time policy evaluation before each action.
- Readiness uses `UNKNOWN` when no snapshot exists; `STALE` if the
  last immutable snapshot is more than 24 hours old or future-dated;
  `BLOCKED` if the snapshot fails, contains blockers or desired/observed
  revisions differ; `READY` only with a fresh, internally converged
  snapshot. Even READY **does not** certify external PSP/cloud/provider
  compliance or continuous operational health.
- The resource and each permitted subsection are bounded to 100 records.
  Exceeding the bound fails closed rather than truncating. Security is
  source-side, never delegated to frontend navigation.
- The protected Organisation 360 view shows distinct mandate and
  readiness cards using existing Tailwind v4, shadcn/ui tokens, Lucide and
  consistent stale/empty/error states.

## LA-07 scope integration and reproducible client

Readback now includes the immutable mandate `activity_scope`, `market_scope`
and `capability_scope` columns, with bounded arrays, duplicate checks and
uppercase two-letter market codes. Missing mandatory scopes or invalid stored
values fail closed. Activity and capability strings retain their exact
contract-valid whitespace and Unicode; projection must not normalize an
immutable scope or reject a value accepted by the proposal contract. An empty capability filter does not grant a capability;
current grants, legal-actor resolution and provider policy still apply.

Regenerate the entire OpenAPI client after changing the Shared pin or any
embedded referenced schema. Updating its header alone misses declarations
expanded from schema references. Console CI verifies exact generated output;
Go CI and resolution readiness independently require formatted source.

## Remaining independent external acceptance

Before declaring end-to-end acceptance, prove source-trusted CIPC or
provider registry verification, immutable attestation validity and
revocation, IAM-authenticated protected staging, actual tenant/provider
readiness probes, AWS configuration, access revocation latency, and
representative real-tenant data from approved onboarding.

A CP-internal readiness snapshot cannot be represented as external
provider certification merely because it is fresh and internally READY.
