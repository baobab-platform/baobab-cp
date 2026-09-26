import { describe, expect, it } from "vitest";

import { GET } from "@/app/healthz/route";

describe("GET /healthz", () => {
  it("reports liveness without detail and is never cached", async () => {
    const response = GET();
    expect(response.status).toBe(200);
    expect(await response.text()).toBe("ok");
    expect(response.headers.get("Cache-Control")).toBe("no-store");
  });
});
