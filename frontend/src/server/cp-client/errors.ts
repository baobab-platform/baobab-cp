import "server-only";

import type { components } from "@/generated/control-plane/openapi";

import type { ControlPlaneMessageKey } from "./messages";

export type ProblemDetails = components["schemas"]["ProblemDetails"];

/**
 * A Control Plane refusal or failure, translated for the Console. `message`
 * is the key of what the user may be told; `detail` is operator text for
 * server logs only and must never be rendered. `correlationId` is the
 * reference a user quotes to support.
 */
export class ControlPlaneError extends Error {
  override readonly name = "ControlPlaneError";

  constructor(
    readonly status: number,
    readonly code: string,
    readonly userMessage: ControlPlaneMessageKey,
    readonly retryable: boolean,
    readonly correlationId: string | undefined,
    readonly detail: string | undefined,
  ) {
    super(`Control Plane ${status} ${code}${correlationId ? ` (${correlationId})` : ""}`);
  }
}

function isProblem(body: unknown): body is ProblemDetails {
  if (typeof body !== "object" || body === null) {
    return false;
  }
  const candidate = body as Record<string, unknown>;
  return typeof candidate.code === "string" && typeof candidate.status === "number" && typeof candidate.retryable === "boolean";
}

function messageFor(status: number): ControlPlaneMessageKey {
  switch (status) {
    case 400:
    case 413:
    case 422:
      return "invalid";
    case 401:
      return "unauthenticated";
    case 403:
      return "forbidden";
    case 404:
      return "notFound";
    case 409:
      return "conflict";
    case 412:
      return "stale";
    case 428:
      return "preconditionRequired";
    case 429:
      return "rateLimited";
    default:
      return "unavailable";
  }
}

/**
 * Translates a failed response. A body that is not RFC 9457 problem details
 * (a proxy's HTML page, say) yields UPSTREAM_ERROR; its content is dropped.
 */
export function toControlPlaneError(status: number, body: unknown, correlationId: string | undefined): ControlPlaneError {
  if (isProblem(body)) {
    return new ControlPlaneError(status, body.code, messageFor(status), body.retryable, body.correlation_id || correlationId, body.detail);
  }
  return new ControlPlaneError(status, "UPSTREAM_ERROR", messageFor(status), status >= 500, correlationId, undefined);
}
