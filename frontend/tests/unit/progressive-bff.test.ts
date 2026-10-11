import { describe, expect, it } from "vitest";

import {
  ProgressiveBffError, boundedBody, invokeProgressive, resolveProgressiveCommand,
} from "@/server/bff/progressive";

const id = "capp_testresource123";
const body = { business_identity: { operating_name: "Sample" }, requested_markets: [{ country_code: "ZA" }] };

describe("PEO-06C/D/E explicit Control Plane BFF", () => {
  it("routes each operation to a literal server-owned API template", () => {
    expect(resolveProgressiveCommand({ action: "create", body }).path).toBe("/v2/client-applications");
    expect(resolveProgressiveCommand({ action: "application", id }).path)
      .toBe(`/v2/client-applications/${id}`);
    expect(resolveProgressiveCommand({ action: "review-queue" }).path)
      .toBe("/v2/admin/client-applications?limit=25");
    expect(resolveProgressiveCommand({ action: "submit", id }).path)
      .toBe(`/v2/client-applications/${id}/submit`);
    expect(resolveProgressiveCommand({ action: "operation", id: "op_example1" }).path)
      .toBe("/v1/admin/operations/op_example1");
  });

  it("refuses privilege-escalating body fields, path traversal and direct actor selectors", () => {
    const attacks = [
      { action: "create", body: { ...body, applicant_principal_id: "admin" } },
      { action: "create", body: { ...body, subscription_type: "INTERNAL" } },
      { action: "update", id, body: { version: 1, legal_entity_id: "fake" } },
      { action: "submit", id, body: {} },
      { action: "application", id: "../etc/evil" },
      { action: "review", id, body: { tenant_id: "sibling" } },
      { action: "register-tenant", id: "tor_own", body: { tenant_onboarding_request_id: "tor_other" } },
    ];
    for (const payload of attacks) {
      expect(() => resolveProgressiveCommand(payload as never)).toThrow(ProgressiveBffError);
    }
  });

  it("forwards only server-provided access token and response, without following redirects", async () => {
    let called = false;
    const fetcher = async (url: URL | RequestInfo, init?: RequestInit) => {
      called = true;
      expect(String(url)).toBe("https://cp.example.test/v2/client-applications");
      expect(init?.redirect).toBe("error");
      expect(init?.cache).toBe("no-store");
      const headers = init?.headers as Record<string, string>;
      expect(headers.Authorization).toBe("Bearer server-access-token");
      expect(headers["Idempotency-Key"]).toBe("stable-operation-123");
      return new Response(JSON.stringify({ client_application_id: id, status: "DRAFT" }), {
        status: 201, headers: { "Content-Type": "application/json" },
      });
    };
    const response = await invokeProgressive({
      action: "create", body, idempotencyKey: "stable-operation-123",
    }, "server-access-token", { baseUrl: "https://cp.example.test", fetcher });
    expect(called).toBe(true);
    expect(response.status).toBe(201);
    expect(response.data).toHaveProperty("status", "DRAFT");
  });

  it("rejects CP error bodies without leaking upstream decision detail", async () => {
    const fetcher = async () => new Response(JSON.stringify({ detail: "SECRET-PRIVATE-CASE", code: "DENIED" }), {
      status: 403, headers: { "Content-Type": "application/json" },
    });
    await expect(invokeProgressive({ action: "application", id }, "server-token",
      { baseUrl: "https://cp.example.test", fetcher }))
      .rejects.toMatchObject({ status: 403, code: "CP_REJECTED" });
  });

  it("enforces idempotency and byte-bounded request bodies", async () => {
    await expect(invokeProgressive({ action: "create", body }, "server-token",
      { baseUrl: "https://cp.example.test", fetcher: async () => new Response("{}") }))
      .rejects.toMatchObject({ code: "IDEMPOTENCY_KEY_REQUIRED" });
    const oversized = new Request("https://console.example.test/api/workspace/v2", {
      method: "POST", body: "x".repeat(65537), headers: { "Content-Type": "application/json" },
    });
    await expect(boundedBody(oversized)).rejects.toMatchObject({ status: 413 });
  });
});
