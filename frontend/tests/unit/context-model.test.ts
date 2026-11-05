import { describe, expect, it } from "vitest";

import { contextHref, isContextPathActive, type ConsoleContextOption } from "@/features/console/context-model";

const organisation: ConsoleContextOption = {
  kind: "organisation",
  id: "org_123",
  label: "org_123",
};
const tenant: ConsoleContextOption = {
  kind: "tenant",
  id: "tn_abc123",
  label: "tn_abc123",
};

describe("Console context route selection", () => {
  it("keeps an organisation selected on its Organisation 360 and nested records routes", () => {
    expect(isContextPathActive(organisation, contextHref(organisation))).toBe(true);
    expect(isContextPathActive(organisation, "/workspace/organisations/org_123/records")).toBe(true);
    expect(isContextPathActive(organisation, "/workspace/organisations/org_123/records/operations")).toBe(true);
  });

  it("keeps a tenant selected on its nested routes and tolerates a trailing slash", () => {
    expect(isContextPathActive(tenant, "/workspace/tenants/tn_abc123")).toBe(true);
    expect(isContextPathActive(tenant, "/workspace/tenants/tn_abc123/")).toBe(true);
  });

  it("does not select a sibling resource with a shared identifier prefix", () => {
    expect(isContextPathActive(organisation, "/workspace/organisations/org_1234")).toBe(false);
    expect(isContextPathActive(tenant, "/workspace/tenants/tn_abc1234")).toBe(false);
    expect(isContextPathActive(organisation, "/workspace/tenants/tn_abc123")).toBe(false);
  });
});
