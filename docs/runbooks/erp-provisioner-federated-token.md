# ERP provisioner: federated token exchange (FB-05)

How the Control Plane's provisioning worker gets the bearer token it presents to ERP when its identity is a **federated workload**
(Shared `workload-registry.yaml`: `credential_type: federated_workload_token`, no static secret). Two modes; exactly one is configured.

| Mode | Settings | Use |
|---|---|---|
| Ready token | `ERP_PROVISIONER_TOKEN_FILE` | A token another component already obtained and keeps fresh in the file. Unchanged. |
| Federated exchange | `ERP_PROVISIONER_ASSERTION_FILE`, `ERP_PROVISIONER_TOKEN_URL`, `ERP_PROVISIONER_CLIENT_ID`, `ERP_PROVISIONER_SUBJECT` (+ optional `ERP_PROVISIONER_SCOPE`, default `erp:provision`; `ERP_PROVISIONER_AUDIENCE`) | The platform projects a short-lived assertion into `ERP_PROVISIONER_ASSERTION_FILE`; the Control Plane exchanges it at the provider's token endpoint (RFC 7523 `jwt-bearer`, Ory Hydra, ADR-IAM-0033) and caches the access token. |

`ERP_PROVISIONER_ISSUER` (the provider's issuer, as it appears in the access token) is required in both modes.

## Federated exchange

- The provider-side client is a **public** client with the `jwt-bearer` grant and no secret; IAM registers it together with a trust grant that
  binds the assertion's exact issuer and subject to the Shared scopes (baobab-iam `ProvisionFederatedWorkload`). The request therefore carries
  `grant_type`, `assertion`, `client_id` and `scope` and nothing secret.
- **`ERP_PROVISIONER_SUBJECT` has no default in this mode.** Under `jwt-bearer` the access token's `sub` is the *assertion subject* the trust
  grant binds, not the logical client id. Set it to exactly that subject; the Control Plane looks its principal up by (issuer, subject).
- The assertion file is re-read on every exchange, so the platform can rotate it without a restart. The access token is cached until 80% of
  its lifetime has passed (never closer than 30 seconds to expiry) and capped at the governed 15 minutes. A refresh that fails while the cached
  token is still in date keeps serving it; an expired token is never served.
- The exchange refuses: an answer that is not 200, not a bearer token, without a positive lifetime, with a scope other than the one requested,
  or larger than 64 KiB; a redirect (never followed, so the assertion cannot be carried to another host); an assertion file that is not a compact
  JWT or is larger than 16 KiB. Errors carry the HTTP status and the OAuth error *code* only, never the response body, the assertion or a token.
- `ERP_PROVISIONER_TOKEN_URL` must be HTTPS (loopback HTTP only for local development), with no credentials, query or fragment.

## What this does and does not establish

It supplies the missing client half of the exchange. It does **not** make the identity usable: that still needs a provider that issues
`aud=baobab-erp` on the `jwt-bearer` path, an infrastructure-projected assertion with a governed issuer and key lifecycle, and the provider
trust registered by IAM. The audience is verified by ERP, not trusted from here; `ERP_PROVISIONER_AUDIENCE` is sent only when set, and whether
the provider honours it on `jwt-bearer` must be proven live. See the FB-05 acceptance matrix in the infrastructure runbook.
