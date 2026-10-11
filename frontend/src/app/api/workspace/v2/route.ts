import { NextRequest, NextResponse } from "next/server";

import { SESSION_COOKIE, confirmUnsafeRequest, createAuthStore, currentSession, readAuthSettings, requireExactOrigin } from "@/server/auth/runtime";
import {
  ProgressiveBffError, boundedBody, invokeProgressive,
  type ProgressiveCommand, type ProgressiveReadAction, type ProgressiveWriteAction,
} from "@/server/bff/progressive";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

function failure(error: unknown) {
  const status = error instanceof ProgressiveBffError ? error.status : 503;
  const code = error instanceof ProgressiveBffError ? error.code : "WORKSPACE_UNAVAILABLE";
  return NextResponse.json({ code }, { status, headers: { "Cache-Control": "no-store" } });
}

export async function GET(request: NextRequest) {
  try {
    const settings = readAuthSettings();
    requireExactOrigin(request, settings.origin);
    const session = await currentSession(createAuthStore(settings),
      request.cookies.get(SESSION_COOKIE)?.value, settings);
    const action = request.nextUrl.searchParams.get("action");
    if (!["application", "review-application", "review-queue", "operation"].includes(action ?? "") ||
        Array.from(request.nextUrl.searchParams.keys()).some((key) => key !== "action" && key !== "id")) {
      throw new ProgressiveBffError(400, "INVALID_READ_ACTION");
    }
    const id = request.nextUrl.searchParams.get("id") ?? undefined;
    const command: ProgressiveCommand = {
      action: action as ProgressiveReadAction,
      ...(id !== undefined ? { id } : {}),
    };
    const data = await invokeProgressive(command, session.accessToken);
    return NextResponse.json(data.data, { status: data.status, headers: { "Cache-Control": "no-store" } });
  } catch (error) {
    return failure(error);
  }
}

export async function POST(request: NextRequest) {
  try {
    const settings = readAuthSettings();
    requireExactOrigin(request, settings.origin);
    const session = await currentSession(createAuthStore(settings),
      request.cookies.get(SESSION_COOKIE)?.value, settings);
    confirmUnsafeRequest(request, session.csrfToken, settings.origin);
    if (!request.headers.get("content-type")?.startsWith("application/json")) {
      throw new ProgressiveBffError(415, "JSON_REQUIRED");
    }
    const raw = await boundedBody(request);
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
      throw new ProgressiveBffError(400, "INVALID_COMMAND");
    }
    const command = raw as Record<string, unknown>;
    if (Object.keys(command).some((key) => !["action", "id", "body", "idempotencyKey"].includes(key)) ||
        typeof command.action !== "string" ||
        !["create", "update", "submit", "review", "decide", "request-onboarding", "authorise-onboarding",
          "register-tenant"].includes(command.action)) {
      throw new ProgressiveBffError(400, "INVALID_COMMAND");
    }
    const input: ProgressiveCommand = {
      action: command.action as ProgressiveWriteAction,
      ...(typeof command.id === "string" ? { id: command.id } : {}),
      ...(command.body && typeof command.body === "object" && !Array.isArray(command.body)
        ? { body: command.body as Record<string, unknown> } : {}),
      ...(typeof command.idempotencyKey === "string" ? { idempotencyKey: command.idempotencyKey } : {}),
    };
    const data = await invokeProgressive(input, session.accessToken);
    return NextResponse.json(data.data, { status: data.status, headers: { "Cache-Control": "no-store" } });
  } catch (error) {
    return failure(error);
  }
}
