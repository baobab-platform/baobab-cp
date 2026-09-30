# IAM-M1C — Ory provider-neutral canonical identity evidence

**Date:** 2026-09-30  
**Scope:** IAM PR #42 / ADR-IAM-0020 cross-repository evidence  
**Status:** Implemented on this branch; CI is the merge authority

## Finding

Canonical identity resolution is provider-neutral at the Control Plane boundary:

```text
verified issuer + subject
        |
        v
ExternalIdentity
        |
        v
Canonical Principal
```

`identity.ExternalIdentity.provider_type` is optional metadata. The domain
validator does not whitelist Keycloak and repository resolution keys on
`(issuer, subject)`, not on provider type.

The audit found no identity-resolution or authorization branch that grants
authority because `provider_type == "keycloak"`. The tests added by this
change prove an `ory` ExternalIdentity validates and resolves through the same
canonical path as other providers.

## Evidence

- `internal/domain/identity_test.go` accepts `keycloak`, `ory`, `google`
  and an omitted provider type under the same ExternalIdentity invariants.
- `internal/service/identity_service_test.go` links
  `provider_type=ory`, an Ory-shaped issuer and a workload subject, then
  resolves the same canonical workload Principal through issuer + subject.
- `internal/repository/postgres.go` persists provider type as nullable
  descriptive data while the canonical uniqueness/resolution invariant remains
  issuer + subject.

## Non-claims

This evidence does **not** enable production dual-issuer verification. That is
IAM migration M18 / IssuerTrust work and requires the Ory issuer, JWKS and
cutover evidence.

It also does not migrate CP's separate IAM-organisation projection. The current
`IamOrganisationReference` provider vocabulary is still Keycloak-specific.
That is a later human/B2B organisation migration concern and does not block the
machine workload path proven here.

## Exit result

For IAM PR #42 Gate M1-C:

- issuer + subject canonical resolution: **PASS**
- non-Keycloak `provider_type` fixture: **PASS**
- provider-type authorization dependency: **no dependency found in canonical identity path**
- production dual issuer: **OUT OF SCOPE / M18**
