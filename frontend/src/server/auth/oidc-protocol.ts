import "server-only";

import { createHash, randomBytes, timingSafeEqual } from "node:crypto";

/**
 * FE-03 protocol boundary for a provider-neutral BFF (RFC 10017, 9700, 7636).
 * No browser token, OAuth callback route or session is activated here.
 * A durable atomic state/session store and signed ID-token verification are
 * mandatory before this module is wired to live routes.
 */

export interface OidcConfiguration {
  issuer: string;
  clientId: string;
  redirectUri: string;
}

export interface OidcMetadata {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  jwks_uri: string;
  code_challenge_methods_supported: readonly string[];
  response_types_supported: readonly string[];
}

export interface PendingAuthorization {
  state: string;
  nonce: string;
  codeVerifier: string;
  redirectUri: string;
  issuer: string;
  clientId: string;
  createdAt: number;
  expiresAt: number;
}

export class OidcBoundaryError extends Error {
  constructor() {
    super("OIDC protocol validation denied");
    this.name = "OidcBoundaryError";
  }
}

const MAX_PENDING_MS = 5 * 60_000;
const MAX_CODE_LENGTH = 4096;
const allowedLoopback = new Set(["localhost", "127.0.0.1"]);

function isTrustedHttps(value: string, allowLocal = false): URL {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new OidcBoundaryError();
  }
  const local = allowLocal && url.protocol === "http:" && allowedLoopback.has(url.hostname);
  if ((url.protocol !== "https:" && !local) || !url.hostname || url.username || url.password || url.hash ||
    url.search || url.pathname.includes("/../")) {
    throw new OidcBoundaryError();
  }
  return url;
}

/** Strictly compares metadata against an operator-configured issuer. */
export function validateOidcMetadata(config: OidcConfiguration, metadata: OidcMetadata, allowLocal = false): void {
  const issuer = isTrustedHttps(config.issuer, allowLocal);
  const redirect = isTrustedHttps(config.redirectUri, allowLocal);
  if (issuer.href !== metadata.issuer || config.clientId.length < 1 || config.clientId.length > 256 ||
    redirect.pathname === "/" || !metadata.code_challenge_methods_supported?.includes("S256") ||
    !metadata.response_types_supported?.includes("code")) {
    throw new OidcBoundaryError();
  }
  for (const value of [metadata.authorization_endpoint, metadata.token_endpoint, metadata.jwks_uri]) {
    const endpoint = isTrustedHttps(value, allowLocal);
    // A single trusted issuer front door: no cross-host token/callback SSRF.
    if (endpoint.origin !== issuer.origin) {
      throw new OidcBoundaryError();
    }
  }
}

/** Provider-neutral OIDC metadata, no redirects, caching or unbounded body. */
export async function discoverOidc(
  config: OidcConfiguration,
  options: { fetcher?: typeof fetch; allowLocal?: boolean } = {},
): Promise<OidcMetadata> {
  const issuer = isTrustedHttps(config.issuer, options.allowLocal);
  const url = new URL(issuer.href.replace(/\/+$/, "") + "/.well-known/openid-configuration");
  let result: Response;
  try {
    result = await (options.fetcher ?? fetch)(url, {
      method: "GET",
      headers: { Accept: "application/json" },
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(5_000),
    });
  } catch {
    throw new OidcBoundaryError();
  }
  if (!result.ok || !result.headers.get("content-type")?.toLowerCase().startsWith("application/json")) {
    throw new OidcBoundaryError();
  }
  let data: unknown;
  try {
    const content = await result.text();
    if (content.length > 65_536) throw new OidcBoundaryError();
    data = JSON.parse(content);
  } catch {
    throw new OidcBoundaryError();
  }
  if (typeof data !== "object" || data === null) throw new OidcBoundaryError();
  const row = data as Record<string, unknown>;
  for (const key of ["issuer", "authorization_endpoint", "token_endpoint", "jwks_uri"]) {
    if (typeof row[key] !== "string") throw new OidcBoundaryError();
  }
  if (!Array.isArray(row.code_challenge_methods_supported) || !Array.isArray(row.response_types_supported)) {
    throw new OidcBoundaryError();
  }
  const metadata = data as OidcMetadata;
  validateOidcMetadata(config, metadata, options.allowLocal);
  return metadata;
}

/** No state or code-verifier value is ever emitted to browser JavaScript. */
export function beginAuthorization(
  config: OidcConfiguration,
  metadata: OidcMetadata,
  now: number = Date.now(),
): { authorizationUrl: string; pending: PendingAuthorization } {
  validateOidcMetadata(config, metadata);
  if (!Number.isSafeInteger(now) || now <= 0) throw new OidcBoundaryError();
  const state = randomBytes(32).toString("base64url");
  const nonce = randomBytes(32).toString("base64url");
  const codeVerifier = randomBytes(32).toString("base64url");
  const challenge = createHash("sha256").update(codeVerifier, "ascii").digest("base64url");
  const target = new URL(metadata.authorization_endpoint);
  target.searchParams.set("client_id", config.clientId);
  target.searchParams.set("response_type", "code");
  target.searchParams.set("redirect_uri", config.redirectUri);
  target.searchParams.set("scope", "openid");
  target.searchParams.set("state", state);
  target.searchParams.set("nonce", nonce);
  target.searchParams.set("code_challenge", challenge);
  target.searchParams.set("code_challenge_method", "S256");
  return {
    authorizationUrl: target.href,
    pending: { state, nonce, codeVerifier, redirectUri: config.redirectUri,
      issuer: config.issuer, clientId: config.clientId, createdAt: now, expiresAt: now + MAX_PENDING_MS },
  };
}

/**
 * Called only AFTER the server-side pending authorization has been consumed
 * atomically. This function alone cannot prevent callback replay; callers
 * must ensure a one-time durable consume before contacting the token endpoint.
 */
export function validateAuthorizationCallback(
  pending: PendingAuthorization,
  callback: URLSearchParams,
  config: OidcConfiguration,
  now: number = Date.now(),
): { code: string; codeVerifier: string; nonce: string } {
  if (!Number.isSafeInteger(now) || !pending || now < pending.createdAt ||
    now >= pending.expiresAt || pending.expiresAt - pending.createdAt > MAX_PENDING_MS ||
    pending.redirectUri !== config.redirectUri || pending.issuer !== config.issuer ||
    pending.clientId !== config.clientId || callback.has("error") ||
    callback.getAll("state").length !== 1 || callback.getAll("code").length !== 1 ||
    callback.getAll("iss").length > 1) {
    throw new OidcBoundaryError();
  }
  const received = callback.get("state") ?? "";
  const code = callback.get("code") ?? "";
  if (!/^[A-Za-z0-9_-]{43}$/.test(pending.state) || !/^[A-Za-z0-9_-]{43}$/.test(pending.nonce) ||
    !/^[A-Za-z0-9_-]{43}$/.test(pending.codeVerifier) ||
    received.length !== pending.state.length || code.length === 0 || code.length > MAX_CODE_LENGTH ||
    callback.has("iss") && callback.get("iss") !== config.issuer ||
    !timingSafeEqual(Buffer.from(received), Buffer.from(pending.state))) {
    throw new OidcBoundaryError();
  }
  return { code, codeVerifier: pending.codeVerifier, nonce: pending.nonce };
}

export interface ConfidentialSessionStore {
  /** Create a server-only, expires-at-bound authorization attempt. */
  issueAuthorization(pending: PendingAuthorization): Promise<string>;
  /** An atomic delete-and-read: the returned attempt can never be reused. */
  consumeAuthorization(handle: string): Promise<PendingAuthorization | null>;
  /** Server-side token custody; returns an opaque browser-session handle. */
  createSession(principalId: string, opaqueTokenHandle: string, expiresAt: number): Promise<string>;
  /** Revokes browser session and its server-side token binding. */
  revokeSession(handle: string): Promise<void>;
}

/** Refuse to enable login without a verified durable cross-instance store. */
export function requireSessionStore(store?: ConfidentialSessionStore): ConfidentialSessionStore {
  if (!store) throw new OidcBoundaryError();
  return store;
}
