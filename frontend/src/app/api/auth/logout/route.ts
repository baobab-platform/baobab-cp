import { NextRequest, NextResponse } from "next/server";

import {
  SESSION_COOKIE, confirmUnsafeRequest, createAuthStore, readAuthSettings, requireExactOrigin,
} from "@/server/auth/runtime";

export const runtime = "nodejs";

export async function POST(request: NextRequest) {
  try {
    const settings = readAuthSettings();
    requireExactOrigin(request, settings.origin);
    const handle = request.cookies.get(SESSION_COOKIE)?.value;
    if (!handle) throw new Error("missing session");
    const store = createAuthStore(settings);
    const session = await store.readSession(handle);
    if (!session) throw new Error("revoked session");
    confirmUnsafeRequest(request, session.csrfToken, settings.origin);
    await store.revokeSession(handle);
    const response = NextResponse.json({ signedOut: true }, { headers: { "Cache-Control": "no-store" } });
    response.cookies.delete(SESSION_COOKIE);
    return response;
  } catch {
    return NextResponse.json({ code: "LOGOUT_DENIED" }, {
      status: 403, headers: { "Cache-Control": "no-store" },
    });
  }
}
