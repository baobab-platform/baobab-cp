import "server-only";

import { createHash, createPublicKey, verify as verifySignature, timingSafeEqual } from "node:crypto";

import { OidcBoundaryError, type OidcConfiguration, type OidcMetadata } from "./oidc-protocol";

export type TokenResponse = {
  access_token: string;
  id_token: string;
  token_type: "Bearer";
  expires_in: number;
  refresh_token?: string;
};

export type VerifiedIdentity = {
  issuer: string;
  subject: string;
  audience: string;
  expiresAt: number;
  issuedAt: number;
  nonce: string;
};

type JsonMap = Record<string, unknown>;

function isObject(value: unknown): value is JsonMap {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function safeJSON(bytes: Uint8Array): unknown {
  try {
    return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  } catch {
    throw new OidcBoundaryError();
  }
}

/** A bounded, no-cache JSON parser: rejects oversized responses before buffering. */
export async function boundedJSON(response: Response, maxBytes = 65_536): Promise<unknown> {
  if (!response.ok || !response.headers.get("content-type")?.toLowerCase().startsWith("application/json") ||
      !response.body || maxBytes < 1 || maxBytes > 1_048_576) {
    throw new OidcBoundaryError();
  }
  const advertised = response.headers.get("content-length");
  if (advertised !== null && (!/^\d+$/.test(advertised) || Number(advertised) > maxBytes)) {
    throw new OidcBoundaryError();
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (true) {
      const part = await reader.read();
      if (part.done) break;
      length += part.value.byteLength;
      if (length > maxBytes) throw new OidcBoundaryError();
      chunks.push(part.value);
    }
  } catch {
    throw new OidcBoundaryError();
  } finally {
    await reader.cancel().catch(() => undefined);
  }
  return safeJSON(Buffer.concat(chunks.map((part) => Buffer.from(part)), length));
}

/** Never automatically forward client credentials or an authorization code across redirects. */
export async function exchangeAuthorizationCode(
  metadata: OidcMetadata,
  config: OidcConfiguration,
  code: string,
  verifier: string,
  clientSecret: string,
  fetcher: typeof fetch = fetch,
): Promise<TokenResponse> {
  if (!/^[A-Za-z0-9_-]{43}$/.test(verifier) || !code || code.length > 4096 ||
      !config.clientId || !clientSecret || clientSecret.length > 4096) {
    throw new OidcBoundaryError();
  }
  const url = new URL(metadata.token_endpoint);
  if (url.protocol !== "https:" || url.username || url.password || url.hash ||
      url.search || url.origin !== new URL(config.issuer).origin) {
    throw new OidcBoundaryError();
  }
  const form = new URLSearchParams({
    grant_type: "authorization_code",
    code,
    code_verifier: verifier,
    redirect_uri: config.redirectUri,
  });
  const basic = Buffer.from(`${config.clientId}:${clientSecret}`, "utf8").toString("base64");
  let response: Response;
  try {
    response = await fetcher(url, {
      method: "POST",
      body: form.toString(),
      headers: {
        Authorization: `Basic ${basic}`,
        Accept: "application/json",
        "Content-Type": "application/x-www-form-urlencoded",
        "Cache-Control": "no-store",
      },
      redirect: "error",
      cache: "no-store",
      signal: AbortSignal.timeout(5_000),
    });
  } catch {
    throw new OidcBoundaryError();
  }
  const data = await boundedJSON(response);
  if (!isObject(data) || data.token_type !== "Bearer" ||
      typeof data.access_token !== "string" || data.access_token.length < 16 || data.access_token.length > 16384 ||
      typeof data.id_token !== "string" || data.id_token.length < 32 || data.id_token.length > 16384 ||
      typeof data.expires_in !== "number" || !Number.isSafeInteger(data.expires_in) ||
      data.expires_in < 1 || data.expires_in > 3600 ||
      (data.refresh_token !== undefined &&
        (typeof data.refresh_token !== "string" || data.refresh_token.length > 16384))) {
    throw new OidcBoundaryError();
  }
  return {
    access_token: data.access_token,
    id_token: data.id_token,
    token_type: "Bearer",
    expires_in: data.expires_in,
    ...(typeof data.refresh_token === "string" ? { refresh_token: data.refresh_token } : {}),
  };
}

type OidcJwk = JsonWebKey & { kid?: string; use?: string; alg?: string; key_ops?: string[] };

function jwtSegment(segment: string): JsonMap {
  if (!/^[A-Za-z0-9_-]+$/.test(segment) || segment.length > 8192) throw new OidcBoundaryError();
  const data: unknown = safeJSON(Buffer.from(segment, "base64url"));
  if (!isObject(data)) throw new OidcBoundaryError();
  return data;
}

/**
 * Cryptographically verify an ID token against an exact-issuer HTTPS JWKS.
 * Never accept 'none', HS*, an embedded JWK, jku, x5u, or an unknown kid.
 */
export async function verifyOidcIdToken(
  jwt: string,
  accessToken: string,
  expectedNonce: string,
  config: OidcConfiguration,
  metadata: OidcMetadata,
  options: { fetcher?: typeof fetch; nowSeconds?: number } = {},
): Promise<VerifiedIdentity> {
  const pieces = jwt.split(".");
  if (pieces.length !== 3 || jwt.length > 16384 || !/^[A-Za-z0-9_-]+$/.test(pieces[2] ?? "") ||
      !/^[A-Za-z0-9_-]{43}$/.test(expectedNonce)) {
    throw new OidcBoundaryError();
  }
  const [encodedHeader, encodedClaims, encodedSignature] = pieces as [string, string, string];
  const header = jwtSegment(encodedHeader);
  const claims = jwtSegment(encodedClaims);
  if ((header.alg !== "RS256" && header.alg !== "ES256") ||
      typeof header.kid !== "string" || header.kid.length < 1 || header.kid.length > 128 ||
      (header.typ !== undefined && header.typ !== "JWT") ||
      "jwk" in header || "jku" in header || "x5u" in header || "crit" in header) {
    throw new OidcBoundaryError();
  }
  const jwks = new URL(metadata.jwks_uri);
  if (jwks.protocol !== "https:" || jwks.origin !== new URL(config.issuer).origin ||
      jwks.username || jwks.password || jwks.search || jwks.hash) throw new OidcBoundaryError();
  let response: Response;
  try {
    response = await (options.fetcher ?? fetch)(jwks, {
      method: "GET", headers: { Accept: "application/json" },
      redirect: "error", cache: "no-store", signal: AbortSignal.timeout(5_000),
    });
  } catch {
    throw new OidcBoundaryError();
  }
  const data = await boundedJSON(response);
  if (!isObject(data) || !Array.isArray(data.keys) || data.keys.length > 32) throw new OidcBoundaryError();
  const keys = data.keys.filter((key: unknown): key is OidcJwk =>
    isObject(key) && key.kid === header.kid && (key.use === undefined || key.use === "sig") &&
    (key.alg === undefined || key.alg === header.alg) &&
    (key.key_ops === undefined || Array.isArray(key.key_ops) && key.key_ops.includes("verify")));
  if (keys.length !== 1) throw new OidcBoundaryError();
  const key = keys[0];
  if (!key || header.alg === "RS256" && (key.kty !== "RSA" || !key.n || !key.e) ||
      header.alg === "ES256" && (key.kty !== "EC" || key.crv !== "P-256" || !key.x || !key.y)) {
    throw new OidcBoundaryError();
  }
  let valid = false;
  try {
    // Node's JsonWebKey requires an index signature; project only the
    // already-validated public key fields rather than casting untrusted JSON.
    const nodeJwk: Record<string, string> = header.alg === "RS256"
      ? { kty: "RSA", n: key.n ?? "", e: key.e ?? "" }
      : { kty: "EC", crv: "P-256", x: key.x ?? "", y: key.y ?? "" };
    const publicKey = createPublicKey({ key: nodeJwk, format: "jwk" });
    valid = verifySignature("sha256", Buffer.from(`${encodedHeader}.${encodedClaims}`, "ascii"),
      header.alg === "ES256" ? { key: publicKey, dsaEncoding: "ieee-p1363" } : publicKey,
      Buffer.from(encodedSignature, "base64url"));
  } catch {
    throw new OidcBoundaryError();
  }
  if (!valid) throw new OidcBoundaryError();
  const now = options.nowSeconds ?? Math.floor(Date.now() / 1000);
  const aud = claims.aud;
  const audiences = typeof aud === "string" ? [aud] : Array.isArray(aud) ? aud : [];
  if (claims.iss !== config.issuer || typeof claims.sub !== "string" ||
      claims.sub.length < 1 || claims.sub.length > 256 ||
      !audiences.every((item): item is string => typeof item === "string") ||
      !audiences.includes(config.clientId) ||
      (audiences.length > 1 && claims.azp !== config.clientId) ||
      (claims.azp !== undefined && claims.azp !== config.clientId) ||
      !Number.isSafeInteger(claims.exp) || !Number.isSafeInteger(claims.iat) ||
      !Number.isSafeInteger(now) || Number(claims.exp) <= now - 60 ||
      Number(claims.exp) > now + 3600 || Number(claims.iat) > now + 60 ||
      (claims.nbf !== undefined && (!Number.isSafeInteger(claims.nbf) || Number(claims.nbf) > now + 60)) ||
      typeof claims.nonce !== "string" || claims.nonce.length !== expectedNonce.length ||
      !timingSafeEqual(Buffer.from(claims.nonce), Buffer.from(expectedNonce))) {
    throw new OidcBoundaryError();
  }
  if (claims.at_hash !== undefined) {
    const digest = createHash("sha256").update(accessToken, "ascii").digest();
    const expected = digest.subarray(0, digest.length / 2).toString("base64url");
    if (typeof claims.at_hash !== "string" || claims.at_hash.length !== expected.length ||
        !timingSafeEqual(Buffer.from(claims.at_hash), Buffer.from(expected))) {
      throw new OidcBoundaryError();
    }
  }
  return { issuer: config.issuer, subject: claims.sub, audience: config.clientId,
    expiresAt: Number(claims.exp) * 1000, issuedAt: Number(claims.iat) * 1000, nonce: claims.nonce };
}
