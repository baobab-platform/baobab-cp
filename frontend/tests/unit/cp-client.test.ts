import { describe, expect, it } from "vitest";

import { ControlPlaneError, createControlPlaneClient, expectOk, toControlPlaneError } from "@/server/cp-client";
import { controlPlaneMessages } from "@/server/cp-client/messages";

const tenant = {
  tenant_id: "tn_01k4zuribeans",
  legal_entity_id: "NABHOLD-GROUP-AFRICA",
  display_name: "Zuri Beans",
  isolation_strategy: "schema_per_tenant",
  residency_region: "af-east-1",
  desired_state: "active",
  observed_state: "active",
  revision: 3,
};

function recorder(respond: (request: Request, signal: AbortSignal) => Response | Promise<Response>) {
  const requests: Request[] = [];
  const fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input as Request;
    requests.push(request);
    return respond(request, init?.signal ?? request.signal);
  };
  return { requests, fetch: fetch as typeof globalThis.fetch };
}

function json(status: number, body: unknown, contentType = "application/json") {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": contentType } });
}

function client(fetch: typeof globalThis.fetch, timeoutMs?: number) {
  return createControlPlaneClient({
    baseUrl: "https://cp.internal.example/",
    accessToken: async () => "access-token",
    fetch,
    correlationId: () => "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b",
    ...(timeoutMs === undefined ? {} : { timeoutMs }),
  });
}

describe("Control Plane client", () => {
  it("calls the pinned route with the bearer token and a correlation id", async () => {
    const { requests, fetch } = recorder(() => json(200, tenant));
    const { data } = await expectOk(client(fetch).GET("/tenants/{tenant_id}", { params: { path: { tenant_id: tenant.tenant_id } } }));

    expect(data.tenant_id).toBe(tenant.tenant_id);
    const [request] = requests;
    expect(request?.url).toBe("https://cp.internal.example/v1/tenants/tn_01k4zuribeans");
    expect(request?.headers.get("authorization")).toBe("Bearer access-token");
    expect(request?.headers.get("x-correlation-id")).toBe("0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b");
    expect(request?.redirect).toBe("error");
    expect(request?.cache).toBe("no-store");
  });

  it("never forwards identity or tenant headers, but keeps concurrency and idempotency headers", async () => {
    const { requests, fetch } = recorder(() => json(200, { id: "x" }));
    await client(fetch).POST("/canonical-entities/{entity_id}/activate", {
      params: {
        path: { entity_id: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b" },
        header: { "If-Match": '"2"' },
      },
      headers: { "X-Actor-Id": "someone-else", "X-Tenant-ID": "tn_other", "Idempotency-Key": "k-1", Authorization: "Bearer forged" },
    });
    const headers = requests[0]?.headers;
    expect(headers?.get("x-actor-id")).toBeNull();
    expect(headers?.get("x-tenant-id")).toBeNull();
    expect(headers?.get("if-match")).toBe('"2"');
    expect(headers?.get("idempotency-key")).toBe("k-1");
    expect(headers?.get("authorization")).toBe("Bearer access-token");
  });

  it("encodes path parameters so they cannot leave their segment", async () => {
    const { requests, fetch } = recorder(() => json(404, {}));
    await client(fetch).GET("/tenants/{tenant_id}", { params: { path: { tenant_id: "../admin?x=1" } } });
    expect(new URL(requests[0]?.url ?? "").pathname).toBe("/v1/tenants/..%2Fadmin%3Fx%3D1");
  });

  it("translates problem details without exposing their detail", async () => {
    const problem = {
      type: "https://docs.nabhold.com/problems/canonical_entity_version_mismatch",
      title: "Precondition Failed",
      status: 412,
      code: "CANONICAL_ENTITY_VERSION_MISMATCH",
      detail: "internal: row version 7",
      correlation_id: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a70",
      retryable: false,
    };
    const { fetch } = recorder(() => json(412, problem, "application/problem+json"));
    const call = client(fetch).POST("/canonical-entities/{entity_id}/validate", {
      params: { path: { entity_id: "0199a1b2-c3d4-7e8f-9a0b-1c2d3e4f5a6b" }, header: { "If-Match": '"1"' } },
    });
    const error = await expectOk(call).catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ControlPlaneError);
    const translated = error as ControlPlaneError;
    expect(translated.code).toBe("CANONICAL_ENTITY_VERSION_MISMATCH");
    expect(translated.correlationId).toBe(problem.correlation_id);
    expect(controlPlaneMessages[translated.userMessage]).toBe(
      "This resource changed after you opened it. Reload the latest version before applying your changes.",
    );
    expect(translated.message).not.toContain("row version");
  });

  it("drops a body that is not problem details", () => {
    const error = toControlPlaneError(502, "<html>bad gateway</html>", "ref-1");
    expect(error.code).toBe("UPSTREAM_ERROR");
    expect(error.retryable).toBe(true);
    expect(error.detail).toBeUndefined();
    expect(error.userMessage).toBe("unavailable");
  });

  it("abandons a request that exceeds its time budget", async () => {
    const { fetch } = recorder(
      (_, signal) =>
        new Promise<Response>((_resolve, reject) => {
          signal.addEventListener("abort", () => reject(signal.reason));
        }),
    );
    const error = await client(fetch, 20)
      .GET("/tenants/{tenant_id}", { params: { path: { tenant_id: tenant.tenant_id } } })
      .catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ControlPlaneError);
    expect((error as ControlPlaneError).code).toBe("TIMEOUT");
    expect((error as ControlPlaneError).retryable).toBe(true);
  });

  it("cannot name an operation the Control Plane does not serve", () => {
    const { fetch } = recorder(() => json(200, {}));
    const cp = client(fetch);
    // Declared by Shared, not served (FE-00 G2): each is a type error.
    // @ts-expect-error GET /markets/{market_id} is not served
    void cp.GET("/markets/{market_id}", { params: { path: { market_id: "m" } } });
    // @ts-expect-error POST /mappings is not served
    void cp.POST("/mappings", { body: {} });
    // @ts-expect-error POST /resolution/mappings is not served
    void cp.POST("/resolution/mappings", { body: {} });
  });
});
