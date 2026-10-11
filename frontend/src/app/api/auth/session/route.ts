import { NextRequest, NextResponse } from "next/server";

import { SESSION_COOKIE, createAuthStore, currentSession, publicSession, readAuthSettings, requireExactOrigin } from "@/server/auth/runtime";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  try {
    const settings = readAuthSettings();
    requireExactOrigin(request, settings.origin);
    const handle = request.cookies.get(SESSION_COOKIE)?.value;
    const session = await currentSession(createAuthStore(settings), handle, settings);
    return NextResponse.json(publicSession(session), { headers: { "Cache-Control": "no-store" } });
  } catch {
    const response = NextResponse.json({ authenticated: false }, {
      status: 401, headers: { "Cache-Control": "no-store" },
    });
    response.cookies.delete(SESSION_COOKIE);
    return response;
  }
}
