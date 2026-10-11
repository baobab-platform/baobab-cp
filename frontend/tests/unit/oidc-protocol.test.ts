import { createHash } from "node:crypto";

import { describe, expect, it } from "vitest";

import {
  beginAuthorization,
  discoverOidc,
  OidcBoundaryError,
  requireSessionStore,
  validateAuthorizationCallback,
  validateOidcMetadata,
  type OidcConfiguration,
  type OidcMetadata,
} from "@/server/auth/oidc-protocol";

const config: OidcConfiguration = {
  issuer: "https://identity.example.test/realms/baobab",
  clientId: "baobab-cp-console",
  redirectUri: "https://console.example.test/api/auth/callback",
};
const metadata: OidcMetadata = {
  issuer: config.issuer,
  authorization_endpoint: "https://identity.example.test/realms/baobab/protocol/openid-connect/auth",
  token_endpoint: "https://identity.example.test/realms/baobab/protocol/openid-connect/token",
  jwks_uri: "https://identity.example.test/realms/baobab/protocol/openid-connect/certs",
  code_challenge_methods_supported: ["S256"],
  response_types_supported: ["code"],
};

describe("FE-03 provider-neutral OIDC protocol gate", () => {
  it("creates an opaque state, nonce and RFC7636 S256 PKCE challenge", () => {
    const { authorizationUrl, pending } = beginAuthorization(config, metadata, 1_800_000_000_000);
    const parsed = new URL(authorizationUrl);
    expect(parsed.searchParams.get("response_type")).toBe("code");
    expect(parsed.searchParams.get("scope")).toBe("openid");
    expect(parsed.searchParams.get("client_id")).toBe(config.clientId);
    expect(parsed.searchParams.get("redirect_uri")).toBe(config.redirectUri);
    expect(parsed.searchParams.get("code_challenge_method")).toBe("S256");
    expect(parsed.searchParams.get("code_challenge")).toBe(
      createHash("sha256").update(pending.codeVerifier, "ascii").digest("base64url"),
    );
    expect(pending.nonce).not.toBe(pending.state);
    expect(pending.expiresAt - pending.createdAt).toBe(300_000);
    const params = new URLSearchParams({ state: pending.state, code: "server-only-code", iss: config.issuer });
    expect(validateAuthorizationCallback(pending, params, config, pending.createdAt + 1_000)).toEqual({
      code: "server-only-code", codeVerifier: pending.codeVerifier, nonce: pending.nonce,
    });
  });

  it.each([
    "mismatched state", "expired", "duplicated state", "duplicate code",
    "issuer substitution", "remote error", "missing code", "changed client",
  ])("fails closed on %s", (scenario) => {
    const now = 1_800_000_000_000;
    const { pending } = beginAuthorization(config, metadata, now);
    const query = new URLSearchParams({ state: pending.state, code: "code" });
    let current = config;
    let at = now + 1;
    switch (scenario) {
      case "mismatched state": query.set("state", "x".repeat(43)); break;
      case "expired": at = now + 300_000; break;
      case "duplicated state": query.append("state", pending.state); break;
      case "duplicate code": query.append("code", "second"); break;
      case "issuer substitution": query.set("iss", "https://evil.example.test"); break;
      case "remote error": query.set("error", "access_denied"); break;
      case "missing code": query.delete("code"); break;
      case "changed client": current = { ...config, clientId: "other" }; break;
    }
    expect(() => validateAuthorizationCallback(pending, query, current, at)).toThrow(OidcBoundaryError);
  });

  it("rejects metadata issuer drift, HTTP endpoint, cross-host token URL and PKCE downgrade", () => {
    const variants = [
      { ...metadata, issuer: "https://lookalike.example.test/" },
      { ...metadata, token_endpoint: "http://identity.example.test/token" },
      { ...metadata, token_endpoint: "https://evil.example.test/token" },
      { ...metadata, code_challenge_methods_supported: ["plain"] },
      { ...metadata, response_types_supported: ["token"] },
    ];
    for (const item of variants) {
      expect(() => validateOidcMetadata(config, item)).toThrow(OidcBoundaryError);
    }
  });

  it("never follows discovery redirects and validates issuer-bound JSON", async () => {
    const goodFetcher = async (_url: URL | RequestInfo, init?: RequestInit): Promise<Response> => {
      expect(init?.redirect).toBe("error");
      expect(init?.cache).toBe("no-store");
      return new Response(JSON.stringify(metadata), { status: 200, headers: { "Content-Type": "application/json" } });
    };
    expect((await discoverOidc(config, { fetcher: goodFetcher })).issuer).toBe(config.issuer);
    const rejectFetcher = async (): Promise<Response> =>
      new Response(JSON.stringify({ ...metadata, jwks_uri: "https://evil.example.test/jwks" }), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    await expect(discoverOidc(config, { fetcher: rejectFetcher })).rejects.toThrow(OidcBoundaryError);
  });

  it("provides no in-memory or implicit production session fallback", () => {
    expect(() => requireSessionStore()).toThrow(OidcBoundaryError);
  });
});
