# PEO-05C — authenticated IAM and certified current providers

**Authority:** ADR-BCP-026 PEO-05, ADR-BCP-027 LA-06/LA-07, ADR-IAM-0033, ADR-SHARED-015, EA-09.

**Implementation class:** Production-code readers and synthetic negative tests, not a production certificate. PEO-05A and PEO-05B were merged in CP #342/#343.

## Authority composition

1. `PEO05AuthenticatedIAM` requires a verified human OIDC token from the trusted IAM issuer with configured CP audience. It separately resolves the canonical CP Principal from the verified issuer/subject and reads an ACTIVE, tenant-specific WorkforceMembership with tenant-admin authority. A realm role, first-party group relation or bare tenant claim alone cannot create membership.
2. `PEO05OAuthIntrospection` is a concrete, provider-neutral HTTPS RFC 7662 introspection client. It requires authenticated OAuth credentials, exact issuer, subject, jti and audience, `active=true`, and bounded expiration. Revoked, expired, unavailable, non-HTTPS, cross-audience or cross-tenant evidence fails closed. The operator bearer is not written to reports or logs.
3. `PEO05CertifiedProviders` checks current tenant-specific ACTIVE CapabilityGrant and governed CapabilityScope for each mandatory capability in the approved plan, followed by a current PRIMARY CapabilityBinding scoped to that tenant. The binding's provider must be ACTIVE and explicitly support the contract major, at a current ACTIVE EngineInstance in the correct region.
4. A current observed immutable EngineRelease must be APPROVED and declare the provider/capability/contract-major tuple. The exact release-bound EA-09 certification must still be CERTIFIED and unexpired; release drift, a second certification, a missing release, or provider mismatch is a denial.
5. Three independent HealthObservation levels (EngineInstance, Provider, ProviderCapability) must be HEALTHY and fresh. A missing level is not silently promoted to health. Every response is bounded to a one-minute maximum expiry and cannot itself grant an entitlement.

## Runtime authority requirements

The concrete CP PostgreSQL adapter and generic OAuth introspection transport are implemented. Runtime construction must use a properly configured CP OIDC verifier with audience `baobab-control-plane`, a protected per-operator token, nonempty HTTPS introspection endpoint and secret from a trusted secret store, and authoritative `PostgresRepository`. These are deliberately not defaulted to synthetic values.

**The PEO05A gate is still not mounted as a production HTTP endpoint.** PEO-05D will provide a protected operator command, integration/negative rehearsal and signed staging evidence. Until verified staging IAM issuer, OAuth client secret, appropriate member identity, and AWS connectivity exist, evaluation MUST report not accepted. IAM provider-neutral client does not imply provider federation session revocation if the IdP cannot issue a compliant RFC 7662 response: this is an explicit dependency requiring adapter conformance/certification.

## Security testing

`go test ./internal/service/organisation ./internal/repository`

Test cases include tenant substitution, revoked or absent membership, insufficient roles, disabled principal, revoked token, invalid audience, stale verification, TLS-only introspection, foreign tenant grant/scope, missing/altered approved binding, observed release drift, revoked certification, unsupported major and expired/missing multilevel health.

## Non-goals and acceptance

Passing unit/CI tests does not prove AWS staging, real CIPC incorporation verification, PSP certification or PEO-07 full go-live. The read-only report remains `READY_FOR_PROTECTED_STAGING_VALIDATION_NOT_PRODUCTION` only when every live authority is current. Operator/e2e validation and independent signoff are separate required work.
