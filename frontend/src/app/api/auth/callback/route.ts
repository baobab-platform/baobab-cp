import { NextRequest, NextResponse } from "next/server";

import { discoverOidc, validateAuthorizationCallback } from "@/server/auth/oidc-protocol";
import { exchangeAuthorizationCode, verifyOidcIdToken } from "@/server/auth/oidc-token";
import { LOGIN_ATTEMPT_COOKIE, authCookie, createAuthStore, readAuthSettings, requireCurrentAccessToken, requireExactOrigin } from "@/server/auth/runtime";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  try {
    const settings = readAuthSettings();
    requireExactOrigin(request, settings.origin);
    const handle = request.cookies.get(LOGIN_ATTEMPT_COOKIE)?.value;
    if (!handle) throw new Error("missing one-time attempt");
    const store = createAuthStore(settings);
    // Redis GETDEL atomically burns this authorization attempt before any token exchange.
    const pending = await store.consumeAuthorization(handle);
    if (!pending) throw new Error("consumed or expired");
    const { code, codeVerifier, nonce } = validateAuthorizationCallback(
      pending, request.nextUrl.searchParams, settings.config,
    );
    const metadata = await discoverOidc(settings.config);
    const tokens = await exchangeAuthorizationCode(
      metadata, settings.config, code, codeVerifier, settings.clientSecret,
    );
    const id = await verifyOidcIdToken(tokens.id_token, tokens.access_token, nonce,
      settings.config, metadata);
    const now = Date.now();
    const expiresAt = Math.min(id.expiresAt, now + tokens.expires_in * 1000, now + 900_000);
    if (expiresAt - now < 1000) throw new Error("expired credential");
    // Signed ID token proves authentication; current CP-audience introspection
    // separately proves this principal may present its access token now.
    await requireCurrentAccessToken({
      principalId: id.subject, accessToken: tokens.access_token,
      csrfToken: "", expiresAt,
    }, settings);
    const session = await store.createSession(id.subject, tokens.access_token, expiresAt);
    const response = NextResponse.redirect(new URL("/workspace", settings.origin), { status: 303 });
    response.cookies.set(authCookie(session, Math.floor((expiresAt - now) / 1000), "strict"));
    response.cookies.delete(LOGIN_ATTEMPT_COOKIE);
    response.headers.set("Cache-Control", "no-store");
    return response;
  } catch {
    const response = NextResponse.json({ code: "OIDC_CALLBACK_DENIED" }, {
      status: 401, headers: { "Cache-Control": "no-store" },
    });
    response.cookies.delete(LOGIN_ATTEMPT_COOKIE);
    return response;
  }
}
