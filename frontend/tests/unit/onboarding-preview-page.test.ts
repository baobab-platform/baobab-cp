import { beforeEach, describe, expect, it, vi } from "vitest";

const env = { CONSOLE_ENVIRONMENT: "development" };

vi.mock("@/server/env", () => ({ serverEnv: () => env }));
vi.mock("next/navigation", () => ({
  notFound: () => {
    throw new Error("NEXT_NOT_FOUND");
  },
}));
vi.mock("@/features/onboarding/ProgressiveShowcase", () => ({
  ProgressiveShowcase: () => null,
}));

describe("PEO-06 public preview boundary", () => {
  beforeEach(() => {
    env.CONSOLE_ENVIRONMENT = "development";
  });

  it("serves only illustrative material in controlled environments", async () => {
    const { default: Page } = await import("@/app/onboarding/page");
    expect(() => Page()).not.toThrow();
  });

  it("refuses production without FE-03 authenticated Console session", async () => {
    env.CONSOLE_ENVIRONMENT = "production";
    const { default: Page } = await import("@/app/onboarding/page");
    expect(() => Page()).toThrow("NEXT_NOT_FOUND");
  });
});
