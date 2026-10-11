import { randomBytes } from "node:crypto";

import { afterEach, describe, expect, it, vi } from "vitest";

import { OidcBoundaryError } from "@/server/auth/oidc-protocol";
import {
  confirmUnsafeRequest, publicSession, readAuthSettings, requireCurrentAccessToken,
  requireExactOrigin,
} from "@/server/auth/runtime";

const origin = "https://console.example.test";
function sources(overrides: Record<string, string> = {}) {
  return {
    CONSOLE_ENVIRONMENT: "staging",
    CP_API_BASE_URL: "https://cp.example.test",
    CP_CONSOLE_AUTH_ENABLED: "true",
    CP_OIDC_ISSUER: "https://identity.example.test/realms/baobab",
    CP_OIDC_CLIENT_ID: "cp-console",
    CP_OIDC_CLIENT_SECRET: "test-local-secret",
    CP_OIDC_REDIRECT_URI: origin + "/api/auth/callback",
    CP_OIDC_INTROSPECTION_URL: "https://identity.example.test/realms/baobab/introspect",
    CP_REDIS_TLS_HOST: "staging-redis.example.test",
    CP_REDIS_TLS_PORT: "6380",
    CP_REDIS_USERNAME: "cp-console",
    CP_REDIS_PASSWORD: "test-not-real",
    CP_CONSOLE_SESSION_KEY_B64: randomBytes(32).toString("base64"),
    ...overrides,
  };
}
const now = 1_800_000_000_000;

afterEach(() => { vi.useRealTimers(); });

describe("FE-03C protected server-side runtime", () => {
  it("requires explicit nonproduction activation and complete secret store", () => {
    const settings = readAuthSettings(sources());
    expect(settings.config.redirectUri).toBe(origin + "/api/auth/callback");
    expect(settings.encryptionKey.length).toBe(32);
    for (const changes of [
      { CONSOLE_ENVIRONMENT: "production" }, { CP_CONSOLE_AUTH_ENABLED: "false" },
      { CP_OIDC_REDIRECT_URI: "http://console.example.test/api/auth/callback" },
      { CP_OIDC_CLIENT_SECRET: "" },
      { CP_REDIS_TLS_PORT: "not-a-port" },
      { CP_CONSOLE_SESSION_KEY_B64: "" },
    ]) {
      expect(() => readAuthSettings(sources(changes))).toThrow();
    }
  });

  it("enforces exact origin and same-origin anti-CSRF for mutations", () => {
    const token = randomBytes(32).toString("base64url");
    const request = new Request(origin + "/api/auth/logout", {
      method: "POST",
      headers: { Origin: origin, "x-csrf-token": token },
    });
    expect(() => requireExactOrigin(request, origin)).not.toThrow();
    expect(() => confirmUnsafeRequest(request, token, origin)).not.toThrow();
    for (const config of [
      { Origin: "https://evil.example.test", "x-csrf-token": token },
      { Origin: origin, "x-csrf-token": "0".repeat(43) },
      { "x-csrf-token": token },
    ]) {
      expect(() => confirmUnsafeRequest(new Request(origin, { method: "POST", headers: config }), token, origin))
        .toThrow(OidcBoundaryError);
    }
    expect(() => requireExactOrigin(new Request("https://evil.example.test"), origin)).toThrow(OidcBoundaryError);
  });

  it("revalidates active CP audience, issuer, subject and expiration on every session read", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(now));
    const settings = readAuthSettings(sources());
    const session = {
      principalId: "person-one", csrfToken: randomBytes(32).toString("base64url"),
      accessToken: "opaque-token", expiresAt: now + 60_000,
    };
    const good = async (_url: URL | RequestInfo, init?: RequestInit) => {
      expect(init?.cache).toBe("no-store");
      expect(init?.redirect).toBe("error");
      return new Response(JSON.stringify({
        active: true, iss: settings.config.issuer, sub: "person-one",
        aud: ["baobab-control-plane"], exp: Math.floor(now / 1000) + 30,
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    };
    await expect(requireCurrentAccessToken(session, settings, good)).resolves.toBeUndefined();
    const cases = [
      { active: false, iss: settings.config.issuer, sub: "person-one",
        aud: "baobab-control-plane", exp: Math.floor(now / 1000) + 30 },
      { active: true, iss: "https://lookalike.test", sub: "person-one",
        aud: "baobab-control-plane", exp: Math.floor(now / 1000) + 30 },
      { active: true, iss: settings.config.issuer, sub: "someone-else",
        aud: "baobab-control-plane", exp: Math.floor(now / 1000) + 30 },
      { active: true, iss: settings.config.issuer, sub: "person-one",
        aud: "wrong-api", exp: Math.floor(now / 1000) + 30 },
    ];
    for (const proof of cases) {
      const fake = async () => new Response(JSON.stringify(proof), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
      await expect(requireCurrentAccessToken(session, settings, fake)).rejects.toThrow(OidcBoundaryError);
    }
    expect(publicSession(session)).not.toHaveProperty("accessToken");
  });
});
