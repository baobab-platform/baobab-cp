import "server-only";

import { randomBytes, timingSafeEqual } from "node:crypto";

import { parseServerEnv } from "@/server/env";

import { OidcBoundaryError, type OidcConfiguration } from "./oidc-protocol";
import { RedisConfidentialSessionStore, TlsRedisCommands } from "./redis-sessions";
import { boundedJSON } from "./oidc-token";

export const LOGIN_ATTEMPT_COOKIE = "__Host-cp-oauth-attempt";
export const SESSION_COOKIE = "__Host-cp-session";

export interface AuthSettings {
  config: OidcConfiguration;
  clientSecret: string;
  introspectionURL: string;
  redisHost: string;
  redisPort: number;
  redisUsername: string;
  redisPassword: string;
  encryptionKey: Buffer;
  origin: string;
}

function protectedURL(value: string, origin?: string): URL {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new OidcBoundaryError();
  }
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash ||
      !url.hostname || origin && url.origin !== origin) throw new OidcBoundaryError();
  return url;
}

/** Explicit runtime approval; no public or production enablement by default. */
export function readAuthSettings(source: NodeJS.ProcessEnv = process.env): AuthSettings {
  if (source.CP_CONSOLE_AUTH_ENABLED !== "true" ||
      !["integration", "staging", "development"].includes(source.CONSOLE_ENVIRONMENT ?? "")) {
    throw new OidcBoundaryError();
  }
  const config = {
    issuer: source.CP_OIDC_ISSUER ?? "",
    clientId: source.CP_OIDC_CLIENT_ID ?? "",
    redirectUri: source.CP_OIDC_REDIRECT_URI ?? "",
  };
  const issuer = protectedURL(config.issuer);
  const callback = protectedURL(config.redirectUri);
  if (callback.pathname !== "/api/auth/callback" || config.clientId.length < 1 ||
      config.clientId.length > 256) throw new OidcBoundaryError();
  const introspect = protectedURL(source.CP_OIDC_INTROSPECTION_URL ?? "");
  const secret = source.CP_OIDC_CLIENT_SECRET ?? "";
  const redisHost = source.CP_REDIS_TLS_HOST ?? "";
  const redisUsername = source.CP_REDIS_USERNAME ?? "";
  const redisPassword = source.CP_REDIS_PASSWORD ?? "";
  const redisPort = Number(source.CP_REDIS_TLS_PORT);
  const rawKey = source.CP_CONSOLE_SESSION_KEY_B64 ?? "";
  if (!secret || secret.length > 4096 || !redisHost || !redisUsername || !redisPassword ||
      !Number.isSafeInteger(redisPort) || redisPort < 1 || redisPort > 65535 ||
      !/^[A-Za-z0-9+/]{43}=$/.test(rawKey)) {
    throw new OidcBoundaryError();
  }
  const encryptionKey = Buffer.from(rawKey, "base64");
  if (encryptionKey.length !== 32 || encryptionKey.toString("base64") !== rawKey) throw new OidcBoundaryError();
  // Production always remains disabled by this increment, regardless of a flag.
  if (parseServerEnv(source).CONSOLE_ENVIRONMENT === "production") throw new OidcBoundaryError();
  if (issuer.hostname === callback.hostname && issuer.pathname === callback.pathname) throw new OidcBoundaryError();
  return { config, clientSecret: secret, introspectionURL: introspect.href,
    redisHost, redisPort, redisUsername, redisPassword, encryptionKey, origin: callback.origin };
}

export function createAuthStore(settings: AuthSettings): RedisConfidentialSessionStore {
  return new RedisConfidentialSessionStore(new TlsRedisCommands({
    host: settings.redisHost, port: settings.redisPort,
    user: settings.redisUsername, password: settings.redisPassword,
  }), settings.encryptionKey);
}

export interface AuthenticatedSession {
  principalId: string;
  accessToken: string;
  csrfToken: string;
  expiresAt: number;
}

function equalConstantTime(a: string, b: string): boolean {
  return a.length === b.length &&
    timingSafeEqual(Buffer.from(a, "utf8"), Buffer.from(b, "utf8"));
}

export function confirmUnsafeRequest(request: Request, csrfToken: string, origin: string): void {
  if (request.method === "GET" || request.method === "HEAD" || request.method === "OPTIONS" ||
      request.headers.get("origin") !== origin || !/^[A-Za-z0-9_-]{43}$/.test(csrfToken) ||
      !equalConstantTime(csrfToken, request.headers.get("x-csrf-token") ?? "")) {
    throw new OidcBoundaryError();
  }
}

export function requireExactOrigin(request: Request, origin: string): void {
  if (new URL(request.url).origin !== origin) throw new OidcBoundaryError();
}

/** Always re-check access-token revocation, issuer, subject and CP audience. */
export async function requireCurrentAccessToken(
  session: AuthenticatedSession,
  settings: AuthSettings,
  fetcher: typeof fetch = fetch,
): Promise<void> {
  if (!session.accessToken || !session.principalId ||
      !Number.isSafeInteger(session.expiresAt) || session.expiresAt <= Date.now()) throw new OidcBoundaryError();
  let response: Response;
  try {
    response = await fetcher(settings.introspectionURL, {
      method: "POST",
      headers: {
        Authorization: `Basic ${Buffer.from(`${encodeURIComponent(settings.config.clientId)}:${encodeURIComponent(settings.clientSecret)}`, "utf8").toString("base64")}`,
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
        "Cache-Control": "no-store",
      },
      body: new URLSearchParams({ token: session.accessToken, token_type_hint: "access_token" }),
      cache: "no-store", redirect: "error", signal: AbortSignal.timeout(5_000),
    });
  } catch {
    throw new OidcBoundaryError();
  }
  const evidence = await boundedJSON(response);
  if (typeof evidence !== "object" || evidence === null || Array.isArray(evidence)) throw new OidcBoundaryError();
  const verified = evidence as Record<string, unknown>;
  const aud = verified.aud;
  const audiences = typeof aud === "string" ? [aud] : Array.isArray(aud) ? aud : [];
  if (verified.active !== true || verified.iss !== settings.config.issuer ||
      verified.sub !== session.principalId || !audiences.includes("baobab-control-plane") ||
      typeof verified.exp !== "number" || !Number.isSafeInteger(verified.exp) ||
      verified.exp <= Math.floor(Date.now() / 1000)) {
    throw new OidcBoundaryError();
  }
}

/** Caller provides the opaque Secure/HttpOnly cookie value. A bad handle fails closed. */
export async function currentSession(
  store: RedisConfidentialSessionStore,
  handle: string | undefined,
  settings: AuthSettings,
  fetcher: typeof fetch = fetch,
): Promise<AuthenticatedSession> {
  if (!handle) throw new OidcBoundaryError();
  const session = await store.readSession(handle);
  if (!session) throw new OidcBoundaryError();
  await requireCurrentAccessToken(session, settings, fetcher);
  return session;
}

export function authCookie(value: string, seconds: number, sameSite: "lax" | "strict") {
  if (!/^[A-Za-z0-9_-]{43}$/.test(value) || !Number.isSafeInteger(seconds) ||
      seconds < 1 || seconds > 3600) throw new OidcBoundaryError();
  return {
    name: sameSite === "lax" ? LOGIN_ATTEMPT_COOKIE : SESSION_COOKIE,
    value, maxAge: seconds, path: "/", httpOnly: true, secure: true, sameSite,
  } as const;
}

/** Token-free public output; the CSRF token is only returned same-origin. */
export function publicSession(session: AuthenticatedSession) {
  return { authenticated: true, principalId: session.principalId, csrfToken: session.csrfToken,
    expiresAt: session.expiresAt };
}

export function newCorrelationId() {
  return randomBytes(16).toString("hex");
}
