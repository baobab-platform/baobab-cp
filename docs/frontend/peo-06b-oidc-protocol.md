# PEO-06B / FE-03A — OIDC confidential-client protocol boundary

**Authority:** ADR-BCP-019 FE-03; ADR-IAM-0033 provider-neutral identities; RFC 10017 (browser BFF), RFC 9700 (OAuth security), RFC 7636 (PKCE).

**Status:** Security protocol component only. Not a sign-in route, validated ID token, credential registry, distributed session store, IAM client deployment or live Console. This is a separately reviewable coding increment on the critical path to PEO-06C–E.

`frontend/src/server/auth/oidc-protocol.ts` now implements:
- Strict exact-issuer discovery with HTTPS endpoints, bounded JSON and no redirects;
- Explicit authorization-code-only OIDC metadata and required `S256` PKCE;
- Cryptographically random 32-byte state, nonce, code verifier and SHA256 PKCE challenge;
- Exactly one state/code, callback issuer fencing when present, five-minute expiry, constant-time state verification and mismatch denials;
- A typed **server-only** session interface requiring atomic one-time authorization consumption, opaque browser session handles, revocation and server-side token custody, with no implicit in-memory fallback.

`beginAuthorization` and `validateAuthorizationCallback` cannot be wired to public login until:
1. A protected, durable atomic multi-instance authorization/session store exists and is tested against replay, expiry and revocation.
2. An exact approved OIDC confidential client, client credentials, audience and redirect URI exist in Baobab IAM, and an authenticated TLS token exchange is implemented.
3. Signed OIDC ID-token validation against the exact issuer's JWKS checks signature, `iss`, `aud`, `nonce`, `exp`, `nbf`, `iat` and `azp` when required.
4. Correctly protected `Secure`, `HttpOnly`, `SameSite=Strict`, path-only cookie and origin/CSRF defenses, logout and step-up hooks are implemented.
5. The approved authenticated CP BFF client uses **only** the server-held session/token and CP independently authorises requests.

Do not use a browser-local bearer, localStorage, arbitrary reverse proxy, static mock or an in-memory fallback to unlock administrative functions. The existing `/onboarding` preview remains production-disabled until full FE-03 closure.

Unit tests assert PKCE derivation, callback mismatch and expiration, duplicate parameters, issuer drift, insecure and cross-host metadata, no discovery redirects and no session fallback.
