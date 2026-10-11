import { createHash, createSign, generateKeyPairSync, randomBytes } from "node:crypto";

import { describe, expect, it } from "vitest";

import { OidcBoundaryError, type OidcConfiguration, type OidcMetadata } from "@/server/auth/oidc-protocol";
import { exchangeAuthorizationCode, verifyOidcIdToken } from "@/server/auth/oidc-token";

const cfg: OidcConfiguration = {
  issuer: "https://iam.example.test/realms/baobab",
  clientId: "cp-console",
  redirectUri: "https://console.example.test/api/auth/callback",
};
const metadata: OidcMetadata = {
  issuer: cfg.issuer,
  authorization_endpoint: "https://iam.example.test/realms/baobab/auth",
  token_endpoint: "https://iam.example.test/realms/baobab/token",
  jwks_uri: "https://iam.example.test/realms/baobab/jwks",
  code_challenge_methods_supported: ["S256"],
  response_types_supported: ["code"],
};

const nonce = randomBytes(32).toString("base64url");
const accessToken = "opaque-access-token-do-not-expose-to-browser";

function signFixture(overrides: Record<string, unknown> = {}) {
  const { publicKey, privateKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const publicJwk = publicKey.export({ format: "jwk" });
  const now = 1_800_000_000;
  const atHash = createHash("sha256").update(accessToken, "ascii").digest()
    .subarray(0, 16).toString("base64url");
  const header = { alg: "RS256", kid: "key-1", typ: "JWT" };
  const claims = {
    iss: cfg.issuer, sub: "principal-auth-subject", aud: cfg.clientId, nonce,
    iat: now - 1, exp: now + 300, at_hash: atHash,
    ...overrides,
  };
  const encoded = [header, claims].map((value) => Buffer.from(JSON.stringify(value)).toString("base64url")).join(".");
  const signer = createSign("RSA-SHA256");
  signer.update(encoded);
  const jwt = `${encoded}.${signer.sign(privateKey).toString("base64url")}`;
  const fetcher = async () =>
    new Response(JSON.stringify({ keys: [{ ...publicJwk, kid: "key-1", alg: "RS256", use: "sig" }] }), {
      status: 200, headers: { "Content-Type": "application/json" },
    });
  return { jwt, fetcher, now };
}

describe("FE-03B confidential OIDC exchange and verification", () => {
  it("accepts a verified issuer, client audience, nonce, signed JWT and access-token hash", async () => {
    const { jwt, fetcher, now } = signFixture();
    const id = await verifyOidcIdToken(jwt, accessToken, nonce, cfg, metadata, { fetcher, nowSeconds: now });
    expect(id.subject).toBe("principal-auth-subject");
    expect(id.issuer).toBe(cfg.issuer);
    expect(id.audience).toBe(cfg.clientId);
  });

  it.each([
    ["issuer substitution", { iss: "https://evil.example.test" }],
    ["audience substitution", { aud: "other-client" }],
    ["nonce substitution", { nonce: "0".repeat(43) }],
    ["expired JWT", { exp: 1_799_999_800 }],
    ["future issued at", { iat: 1_800_009_000 }],
    ["foreign azp", { azp: "wrong-client" }],
    ["wrong access hash", { at_hash: "invalid-hash" }],
  ])("rejects %s after verifying a correctly signed token", async (_name, overrides) => {
    const { jwt, fetcher, now } = signFixture(overrides);
    await expect(verifyOidcIdToken(jwt, accessToken, nonce, cfg, metadata, { fetcher, nowSeconds: now }))
      .rejects.toThrow(OidcBoundaryError);
  });

  it("rejects unsigned JWTs, signature tampering, unknown JWKS keys and cross-origin JWKS", async () => {
    const { jwt, fetcher, now } = signFixture();
    const pieces = jwt.split(".");
    const tampered = `${pieces[0]}.${Buffer.from(JSON.stringify({
      iss: cfg.issuer, sub: "attacker", aud: cfg.clientId, nonce, exp: now + 300, iat: now,
    })).toString("base64url")}.${pieces[2]}`;
    await expect(verifyOidcIdToken(tampered, accessToken, nonce, cfg, metadata, { fetcher, nowSeconds: now }))
      .rejects.toThrow(OidcBoundaryError);
    const unknown = async () => new Response(JSON.stringify({ keys: [] }), {
      status: 200, headers: { "Content-Type": "application/json" },
    });
    await expect(verifyOidcIdToken(jwt, accessToken, nonce, cfg, metadata, { fetcher: unknown, nowSeconds: now }))
      .rejects.toThrow(OidcBoundaryError);
    await expect(verifyOidcIdToken(jwt, accessToken, nonce, cfg,
      { ...metadata, jwks_uri: "https://evil.example.test/jwks" }, { fetcher, nowSeconds: now }))
      .rejects.toThrow(OidcBoundaryError);
  });

  it("form-encodes unusual confidential-client credentials in OAuth Basic authentication", async () => {
    const special = { ...cfg, clientId: "cli:ent+Ü" };
    const fetcher = async (_url: URL | RequestInfo, init?: RequestInit) => {
      const encoded = (init?.headers as Record<string, string>).Authorization ?? "";
      expect(Buffer.from(encoded.slice("Basic ".length), "base64").toString("utf8"))
        .toBe(`${encodeURIComponent(special.clientId)}:${encodeURIComponent("sec:+% Ü")}`);
      return new Response(JSON.stringify({
        access_token: accessToken, id_token: "long-signed-id-token-placeholder-for-verification",
        token_type: "bearer", expires_in: 120,
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    };
    const result = await exchangeAuthorizationCode(metadata, special, "code",
      randomBytes(32).toString("base64url"), "sec:+% Ü", fetcher);
    expect(result.token_type).toBe("Bearer");
  });

  it("uses HTTPS authenticated code exchange without redirect or cache and bounds returned credentials", async () => {
    let called = false;
    const fetcher = async (url: URL | RequestInfo, init?: RequestInit) => {
      called = true;
      expect(String(url)).toBe(metadata.token_endpoint);
      expect(init?.redirect).toBe("error");
      expect(init?.cache).toBe("no-store");
      expect(init?.method).toBe("POST");
      expect((init?.headers as Record<string, string>).Authorization?.startsWith("Basic ")).toBe(true);
      expect(String(init?.body)).toContain("code_verifier=");
      return new Response(JSON.stringify({
        access_token: accessToken,
        id_token: "signed-id-token-format-placeholder-replaced-by-verifier",
        token_type: "Bearer",
        expires_in: 120,
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    };
    const tokens = await exchangeAuthorizationCode(metadata, cfg, "one-time-code", randomBytes(32).toString("base64url"),
      "protected-confidential-client-secret", fetcher);
    expect(called).toBe(true);
    expect(tokens.access_token).toBe(accessToken);
    expect(tokens.token_type).toBe("Bearer");
    await expect(exchangeAuthorizationCode({ ...metadata, token_endpoint: "https://evil.example.test/token" },
      cfg, "code", randomBytes(32).toString("base64url"), "secret", fetcher)).rejects.toThrow(OidcBoundaryError);
    await expect(exchangeAuthorizationCode(metadata, cfg, "code", "not-a-pkce-verifier", "secret", fetcher))
      .rejects.toThrow(OidcBoundaryError);
  });
});
