import { beforeEach, describe, expect, it, vi } from "vitest";

const env = { CONSOLE_ENVIRONMENT: "development", CP_API_BASE_URL: "http://localhost:8080" };

vi.mock("@/server/env", () => ({ serverEnv: () => env }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
}));
vi.mock("@/app/design-system/showcase", () => ({ DesignSystemShowcase: () => null }));

describe("the design-system catalogue", () => {
  beforeEach(() => {
    env.CONSOLE_ENVIRONMENT = "development";
  });

  it("is served outside production", async () => {
    const { default: Page } = await import("@/app/design-system/page");
    expect(() => Page()).not.toThrow();
  });

  it("is not found in production, because its data is illustrative", async () => {
    env.CONSOLE_ENVIRONMENT = "production";
    const { default: Page } = await import("@/app/design-system/page");
    expect(() => Page()).toThrow("NEXT_NOT_FOUND");
  });
});
