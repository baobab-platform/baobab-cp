# Control-plane security model

The canonical trust-boundary contract is
`baobab-platform/shared/docs/security/control-plane-trust-boundaries.md`. This document
records the runtime obligations of `baobab-cp`.

## Authentication and authorisation

- Validate OIDC issuer, audience, expiry, not-before time, signature algorithm,
  and current JWKS key.
- Accept only RS256 or ES256 asymmetric signatures, a maximum token lifetime of
  15 minutes, and 30 seconds of clock skew, per the Shared security policy.
- Authorise every management operation using explicit scopes and tenant or
  platform policy; authentication alone never grants administration authority.
- Use workload identity between deployed services. Static shared API keys are
  not the production default.
- APISIX and `baobab-platform/infrastructure` own workload mTLS termination. The
  application still requires a verified workload token and never treats
  caller-supplied identity headers as authoritative.
- Context resolution additionally requires a workload actor, `azp` service
  identity, canonical `tenant_id`, and `context:resolve` scope. Tenant and
  product policy denials are deliberately indistinguishable to the caller.

## Data handling

- Encrypt transport across every trust boundary.
- Store only opaque secret references in control-plane metadata.
- Parameterise SQL and restrict migration/provisioning privileges by role.
- Redact authorisation headers, tokens, connection strings, credentials, and
  personal information from logs and traces.
- Keep audit events append-only under a separately controlled retention policy.
- Propagate or mint a UUID correlation identifier and attach the verified actor,
  actor type, idempotency key, result, and policy decision to privileged audit
  entries.
- Persist authenticated context-resolution decisions with the workload client,
  token identifier and tenant/product target. A read-only resolution has no
  idempotency key; its audit field remains null rather than inventing one.

## Provisioner credentials

Each adapter uses a distinct identity with only its required operations. A
database provisioner cannot edit APISIX; a gateway reconciler cannot create
database schemas; a digital estate has neither permission.

Failures never trigger fallback to a broader identity.
