import "server-only";

import createClient, { type Middleware } from "openapi-fetch";

import type { paths } from "@/generated/control-plane/openapi";

import { ControlPlaneError, toControlPlaneError } from "./errors";
import type { UnimplementedOperation } from "./unimplemented";

type HttpMethod = "get" | "put" | "post" | "delete" | "options" | "head" | "patch" | "trace";

/**
 * The generated paths without the operations the Control Plane does not
 * serve: calling one is a type error.
 */
export type ServedPaths = {
  [Path in keyof paths]: {
    [Key in keyof paths[Path] as Key extends HttpMethod
      ? `${Uppercase<Key & string>} ${Path & string}` extends UnimplementedOperation
        ? never
        : Key
      : Key]: paths[Path][Key];
  };
};

// The only request headers that reach the Control Plane. It derives the
// actor from the bearer token alone (ADR-BCP-022 sections 13-14), so no
// identity or tenant header is ever forwarded; the concurrency, idempotency
// and tracing headers pass unchanged.
const forwardedHeaders = new Set([
  "accept",
  "authorization",
  "content-type",
  "idempotency-key",
  "if-match",
  "traceparent",
  "tracestate",
  "x-causation-id",
  "x-correlation-id",
]);

export interface ControlPlaneClientOptions {
  /** The validated CP_API_BASE_URL (src/server/env.ts). */
  baseUrl: string;
  /** The signed-in user's access token, held server-side by the BFF session. */
  accessToken: () => Promise<string>;
  /** Milliseconds before a request is abandoned. */
  timeoutMs?: number;
  fetch?: typeof globalThis.fetch;
  correlationId?: () => string;
}

/**
 * A server-only Control Plane client typed from the pinned OpenAPI. Each
 * BFF operation calls one explicit route through it; nothing forwards an
 * arbitrary path (ADR-BCP-019 section 42). The browser never receives the
 * token or calls the Control Plane directly.
 */
export function createControlPlaneClient(options: ControlPlaneClientOptions) {
  const base = new URL(options.baseUrl);
  const baseFetch = options.fetch ?? globalThis.fetch;
  const timeoutMs = options.timeoutMs ?? 10_000;
  const mintCorrelationId = options.correlationId ?? (() => crypto.randomUUID());

  const client = createClient<ServedPaths>({
    baseUrl: `${base.origin}${base.pathname.replace(/\/+$/, "")}/v1`,
    // Authenticated responses are never cached, and the Control Plane
    // never redirects: following one could send the token elsewhere.
    cache: "no-store",
    redirect: "error",
    fetch: async (request: Request) => {
      // The signal goes to fetch directly and stays referenced for the whole
      // call. A Request derived from it would follow it only weakly, so a
      // collected composite signal could silently drop the timeout.
      const signal = AbortSignal.any([request.signal, AbortSignal.timeout(timeoutMs)]);
      try {
        return await baseFetch(request, { signal });
      } catch (error) {
        const correlationId = request.headers.get("x-correlation-id") ?? undefined;
        if (error instanceof DOMException && error.name === "TimeoutError") {
          throw new ControlPlaneError(504, "TIMEOUT", "unavailable", true, correlationId, undefined);
        }
        throw new ControlPlaneError(503, "UNREACHABLE", "unavailable", true, correlationId, undefined);
      }
    },
  });

  const boundary: Middleware = {
    async onRequest({ request }) {
      for (const name of [...request.headers.keys()]) {
        if (!forwardedHeaders.has(name)) {
          request.headers.delete(name);
        }
      }
      if (!request.headers.has("x-correlation-id")) {
        request.headers.set("x-correlation-id", mintCorrelationId());
      }
      request.headers.set("authorization", `Bearer ${await options.accessToken()}`);
      return request;
    },
  };
  client.use(boundary);
  return client;
}

export type ControlPlaneClient = ReturnType<typeof createControlPlaneClient>;

/**
 * Returns a call's data, or throws the translated ControlPlaneError.
 */
export async function expectOk<Data>(
  call: Promise<{ data?: Data; error?: unknown; response: Response }>,
): Promise<{ data: Data; response: Response }> {
  const { data, error, response } = await call;
  if (!response.ok) {
    throw toControlPlaneError(response.status, error, response.headers.get("x-correlation-id") ?? undefined);
  }
  return { data: data as Data, response };
}
