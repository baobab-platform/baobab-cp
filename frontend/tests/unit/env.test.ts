import { describe, expect, it } from "vitest";

import { parseServerEnv } from "@/server/env";

const valid = { CONSOLE_ENVIRONMENT: "production", CP_API_BASE_URL: "https://cp.internal.baobab.example" };

describe("parseServerEnv", () => {
  it("accepts a complete production configuration", () => {
    expect(parseServerEnv(valid)).toEqual(valid);
  });

  it("allows plain http only for localhost in development", () => {
    expect(parseServerEnv({ CONSOLE_ENVIRONMENT: "development", CP_API_BASE_URL: "http://localhost:8080" }).CP_API_BASE_URL).toBe(
      "http://localhost:8080",
    );
    expect(() => parseServerEnv({ CONSOLE_ENVIRONMENT: "development", CP_API_BASE_URL: "http://cp.internal:8080" })).toThrow(
      "CP_API_BASE_URL: must use https outside local development",
    );
    expect(() => parseServerEnv({ CONSOLE_ENVIRONMENT: "staging", CP_API_BASE_URL: "http://localhost:8080" })).toThrow(
      "must use https",
    );
  });

  it("reports every missing or invalid variable at once", () => {
    let message = "";
    try {
      parseServerEnv({ CONSOLE_ENVIRONMENT: "prod" });
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).toContain("CONSOLE_ENVIRONMENT");
    expect(message).toContain("CP_API_BASE_URL");
  });

  it("refuses a secret exposed through the NEXT_PUBLIC_ prefix", () => {
    expect(() => parseServerEnv({ ...valid, NEXT_PUBLIC_OIDC_CLIENT_SECRET: "x" })).toThrow(
      "NEXT_PUBLIC_OIDC_CLIENT_SECRET: secrets must never use the NEXT_PUBLIC_ prefix",
    );
  });
});
