// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const navigation = vi.hoisted(() => ({
  pathname: "/workspace/organisations/org_123/records",
  push: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  usePathname: () => navigation.pathname,
  useRouter: () => ({ push: navigation.push }),
}));

import { ContextSwitcher } from "@/app/workspace/ContextSwitcher";
import type { ConsoleContextOption } from "@/features/console/context-model";

const options: ConsoleContextOption[] = [
  { kind: "organisation", id: "org_123", label: "Primary organisation" },
  { kind: "organisation", id: "org_1234", label: "Sibling organisation" },
  { kind: "tenant", id: "tn_abc", label: "Tenant" },
];

describe("ContextSwitcher nested-route selection", () => {
  beforeEach(() => {
    navigation.pathname = "/workspace/organisations/org_123/records";
    navigation.push.mockClear();
  });

  it("retains the exact organisation selection on nested Organisation 360 routes", () => {
    render(<ContextSwitcher options={options} verified />);
    const select = screen.getByLabelText("Current governed resource context") as HTMLSelectElement;
    expect(select.value).toBe("/workspace/organisations/org_123");
  });

  it("does not select a sibling whose identifier only shares a prefix", () => {
    navigation.pathname = "/workspace/organisations/org_1234/records";
    render(<ContextSwitcher options={options} verified />);
    const select = screen.getByLabelText("Current governed resource context") as HTMLSelectElement;
    expect(select.value).toBe("/workspace/organisations/org_1234");
    expect(select.value).not.toBe("/workspace/organisations/org_123");
  });

  it("does not infer a context across organisation and tenant route kinds", () => {
    navigation.pathname = "/workspace/tenants/tn_abc/records";
    render(<ContextSwitcher options={options} verified />);
    const select = screen.getByLabelText("Current governed resource context") as HTMLSelectElement;
    expect(select.value).toBe("/workspace/tenants/tn_abc");
  });
});
