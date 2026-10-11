# PEO-06 / FE-03B — cryptographic identity and durable session custody

**Decision basis:** Accepted ADR-BCP-019 and ADR-IAM-0033, read alongside ADR-BCP-026/027. This is backend security code, not yet an enabled browser login flow or completed Package 6.

This increment closes two concrete FE-03A follow-ups without introducing a new browser bearer token:

- `oidc-token.ts` exchanges an authorization code using S256 PKCE and an authenticated confidential client at the issuer's exact HTTPS origin. Requests disallow redirects and caching and cap response bytes. `verifyOidcIdToken` checks the JWS against a bounded, live, exact-origin JWKS with RS256 or ES256, strict kid, issuer, audience, azp, nonce, iat, nbf, exp and optional access-token hash. An unsigned or attacker-key JWT fails closed.
- `redis-sessions.ts` implements TLS-authenticated Redis 6.2+ commands without adding a mutable Node dependency or requiring database ownership in the Console. It uses Redis `SET NX PX`, `GETDEL`, `GET`, `DEL`. A per-session 256-bit AES-GCM nonce, key-bound AAD and per-handle SHA-256 Redis keys protect state, secrets, one-time callback consumption and revocation. Cross-instance state comes from the operator-configured Redis service, never an in-process map. Redis ACL should allow only these keys and commands.
- Both modules are `server-only`; tokens and encryption keys never pass to React client components.
- Synthetic unit tests use locally signed JWTs (not spoofed success booleans) and a recording in-process fake Redis *only as a dependency of tests*. There is no fallback fake Redis in deployed production code.

**Still required before enabling sign-in:**

1. Bounded credential configuration from a protected secret provider; issuer/registered callback approval and HTTPS Redis with trustworthy certificate, strong ACL and monitored HA.
2. Secure Next.js `/api/auth/login`, callback, logout, anti-CSRF and session-cookie routes wired to these primitives. A browser may hold only opaque `__Host-` cookies.
3. Current issuer access-token introspection or CP authority checks on every privileged call, and session invalidation on revocation. No fabricated membership.
4. Narrow server-only BFF for applicant/staff v2 endpoints; CP must continue to check principal ownership, independent reviewer/decider/authoriser and provisioning.
5. Protected staging and browser E2E including replay, role change, issuer outage, expiry, stale approvals and two concurrent replicas.

**No completion or provider-readiness claim** is made by this PR. The key `RedisConfidentialSessionStore` is the real executable adapter; it is not a deployed/accepted Redis environment, and no real business application can yet be submitted through the Console.
