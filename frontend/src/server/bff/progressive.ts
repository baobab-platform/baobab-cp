import "server-only";

import { randomUUID } from "node:crypto";

import { serverEnv } from "@/server/env";

export type ProgressiveReadAction = "application" | "review-queue" | "review-application" | "operation";
export type ProgressiveWriteAction =
  | "create" | "update" | "submit" | "review" | "decide" | "request-onboarding"
  | "authorise-onboarding" | "register-tenant";

export type ProgressiveAction = ProgressiveReadAction | ProgressiveWriteAction;

export type ProgressiveCommand = {
  action: ProgressiveAction;
  id?: string;
  body?: Record<string, unknown>;
  idempotencyKey?: string;
};

export class ProgressiveBffError extends Error {
  constructor(readonly status: number, readonly code: string) {
    super("Control Plane request denied");
  }
}

function validIdentifier(value: string | undefined): value is string {
  return typeof value === "string" && /^[A-Za-z][A-Za-z0-9_-]{5,127}$/.test(value);
}

const bodyFields: Partial<Record<ProgressiveWriteAction, readonly string[]>> = {
  create: ["business_identity", "requirements", "requested_markets"],
  update: ["version", "business_identity", "requirements", "requested_markets"],
  review: [
    "organisation_id", "evidence_reference", "identity_resolution_policy_reference",
    "subscription_type", "market_scope", "product_requirements", "isolation_strategy",
  ],
  decide: ["decision", "reason", "evidence_reference"],
  "request-onboarding": ["display_name", "residency_region", "market_participation", "reason"],
  "authorise-onboarding": ["policy_reference", "evidence_reference"],
  "register-tenant": [
    "tenant_onboarding_request_id", "organisation_id", "display_name",
    "isolation_strategy", "residency_region", "requested_products", "metadata",
  ],
};

/** No arbitrary path, method, selector, headers or tenant ID can be supplied. */
export function resolveProgressiveCommand(command: ProgressiveCommand): { method: "GET" | "POST" | "PATCH"; path: string; body?: Record<string, unknown> } {
  const { action, id, body } = command;
  let method: "GET" | "POST" | "PATCH";
  let path: string;
  switch (action) {
    case "create": method = "POST"; path = "/v2/client-applications"; break;
    case "application": method = "GET"; path = "/v2/client-applications"; break;
    case "update": method = "PATCH"; path = "/v2/client-applications"; break;
    case "submit": method = "POST"; path = "/v2/client-applications"; break;
    case "review-queue": method = "GET"; path = "/v2/admin/client-applications?limit=25"; break;
    case "review-application": method = "GET"; path = "/v2/admin/client-applications"; break;
    case "review": method = "POST"; path = "/v2/admission/applications"; break;
    case "decide": method = "POST"; path = "/v2/admission/reviews"; break;
    case "request-onboarding": method = "POST"; path = "/v2/admission/decisions"; break;
    case "authorise-onboarding": method = "POST"; path = "/v2/admission/onboarding-requests"; break;
    case "register-tenant": method = "POST"; path = "/v2/admission/onboarding-requests"; break;
    case "operation": method = "GET"; path = "/v1/admin/operations"; break;
    default: throw new ProgressiveBffError(400, "UNKNOWN_ACTION");
  }
  const needsId = !["create", "review-queue"].includes(action);
  if (needsId && !validIdentifier(id) || !needsId && id !== undefined) {
    throw new ProgressiveBffError(400, "INVALID_RESOURCE_ID");
  }
  if (needsId) path += `/${encodeURIComponent(id!)}`;
  switch (action) {
    case "submit": path += "/submit"; break;
    case "review": path += "/review"; break;
    case "decide": path += "/decision"; break;
    case "request-onboarding": path += "/onboarding-requests"; break;
    case "authorise-onboarding": path += "/authorisation"; break;
    case "register-tenant": path += "/register-tenant"; break;
  }

  if (method === "GET" || action === "submit") {
    if (body !== undefined) throw new ProgressiveBffError(400, "BODY_NOT_PERMITTED");
    return { method, path };
  }
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new ProgressiveBffError(400, "INVALID_COMMAND_BODY");
  }
  const allowed = bodyFields[action as ProgressiveWriteAction] ?? [];
  if (action === "register-tenant" && body.tenant_onboarding_request_id !== id) {
    throw new ProgressiveBffError(409, "ONBOARDING_REQUEST_MISMATCH");
  }
  if (Object.keys(body).some((name) => !allowed.includes(name)) ||
      action === "update" && (!Number.isSafeInteger(body.version) || Number(body.version) < 1)) {
    throw new ProgressiveBffError(400, "UNAUTHORISED_COMMAND_FIELD");
  }
  // CP's exact, pinned admission/v2 and organisation/v2 schemas remain the
  // final validation authority. This is only the BFF field-minimisation gate.
  return { method, path, body };
}

export async function boundedBody(request: Request, maxBytes = 65_536): Promise<unknown> {
  const header = request.headers.get("content-length");
  if (header && (!/^\d+$/.test(header) || Number(header) > maxBytes)) throw new ProgressiveBffError(413, "BODY_TOO_LARGE");
  if (!request.body) throw new ProgressiveBffError(400, "BODY_REQUIRED");
  const stream = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const part = await stream.read();
      if (part.done) break;
      total += part.value.byteLength;
      if (total > maxBytes) throw new ProgressiveBffError(413, "BODY_TOO_LARGE");
      chunks.push(part.value);
    }
  } finally {
    await stream.cancel().catch(() => undefined);
  }
  try {
    return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(
      Buffer.concat(chunks.map((part) => Buffer.from(part)), total),
    ));
  } catch {
    throw new ProgressiveBffError(400, "INVALID_JSON");
  }
}

/** Explicit v2 CP route only. No frontend-supplied bearer or tenant context. */
export async function invokeProgressive(
  command: ProgressiveCommand,
  accessToken: string,
  options: { fetcher?: typeof fetch; baseUrl?: string } = {},
): Promise<{ status: number; data: unknown }> {
  const target = resolveProgressiveCommand(command);
  if (!accessToken || accessToken.length > 16384) throw new ProgressiveBffError(401, "AUTH_REQUIRED");
  if (target.method !== "GET" && (typeof command.idempotencyKey !== "string" ||
      !/^[A-Za-z0-9._:-]{16,128}$/.test(command.idempotencyKey))) {
    throw new ProgressiveBffError(400, "IDEMPOTENCY_KEY_REQUIRED");
  }
  const base = new URL(options.baseUrl ?? serverEnv().CP_API_BASE_URL);
  if (base.search || base.hash || base.username || base.password ||
      base.protocol !== "https:" && !(serverEnv().CONSOLE_ENVIRONMENT === "development" && base.hostname === "localhost")) {
    throw new ProgressiveBffError(503, "CP_CONFIG_DENIED");
  }
  const url = new URL(`${base.pathname.replace(/\/$/, "")}${target.path}`, base.origin);
  let result: Response;
  try {
    result = await (options.fetcher ?? fetch)(url, {
      method: target.method,
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${accessToken}`,
        "Cache-Control": "no-store",
        "X-Correlation-ID": randomUUID(),
        ...(target.method !== "GET" ? {
          "Idempotency-Key": command.idempotencyKey!,
          ...(target.body ? { "Content-Type": "application/json" } : {}),
        } : {}),
      },
      ...(target.body ? { body: JSON.stringify(target.body) } : {}),
      cache: "no-store", redirect: "error", signal: AbortSignal.timeout(10_000),
    });
  } catch {
    throw new ProgressiveBffError(503, "CP_UNAVAILABLE");
  }
  if (!result.ok) {
    // Do not buffer or forward upstream error bodies or privileged policy details.
    const status = [400, 401, 403, 404, 409, 412, 422, 429].includes(result.status) ? result.status : 503;
    await result.body?.cancel().catch(() => undefined);
    throw new ProgressiveBffError(status, "CP_REJECTED");
  }
  if (!result.headers.get("content-type")?.toLowerCase().startsWith("application/json") || !result.body) {
    throw new ProgressiveBffError(502, "INVALID_UPSTREAM_RESPONSE");
  }
  const reader = result.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (true) {
      const part = await reader.read();
      if (part.done) break;
      length += part.value.byteLength;
      if (length > 131_072) throw new ProgressiveBffError(502, "INVALID_UPSTREAM_RESPONSE");
      chunks.push(part.value);
    }
  } finally {
    await reader.cancel().catch(() => undefined);
  }
  let decoded: unknown;
  try {
    decoded = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(
      Buffer.concat(chunks.map((part) => Buffer.from(part)), length),
    ));
  } catch {
    throw new ProgressiveBffError(502, "INVALID_UPSTREAM_RESPONSE");
  }
  return { status: result.status, data: decoded };
}
