import { NextRequest, NextResponse } from "next/server";

import { beginAuthorization, discoverOidc } from "@/server/auth/oidc-protocol";
import { authCookie, createAuthStore, readAuthSettings, requireExactOrigin } from "@/server/auth/runtime";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  try {
    const settings = readAuthSettings();
    requireExactOrigin(request, settings.origin);
    const metadata = await discoverOidc(settings.config);
    const { authorizationUrl, pending } = beginAuthorization(settings.config, metadata);
    const handle = await createAuthStore(settings).issueAuthorization(pending);
    const response = NextResponse.redirect(authorizationUrl, { status: 303 });
    response.cookies.set(authCookie(handle, 300, "lax"));
    response.headers.set("Cache-Control", "no-store");
    return response;
  } catch {
    return NextResponse.json({ code: "AUTH_UNAVAILABLE" }, {
      status: 503, headers: { "Cache-Control": "no-store" },
    });
  }
}
