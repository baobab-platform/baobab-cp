// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import WorkspaceIndex from "@/app/workspace/page";
import { WorkspaceNavigation } from "@/app/workspace/WorkspaceNavigation";
import { expectAccessible } from "./axe";
import { WorkspaceAuthorityProvider } from "@/features/console/authority-context";

vi.mock("next/navigation", () => ({
  usePathname: () => "/workspace",
}));

describe("secure Console presentation foundation", () => {
  it("uses authorised routes and reports no fake metrics or activation state", async () => {
    const { container } = render(<WorkspaceAuthorityProvider value={{ state: "available", permissions: ["application.review", "operation.view"] }}><main><WorkspaceIndex /></main></WorkspaceAuthorityProvider>);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Where businesses become operational");
    for (const href of ["/workspace/apply", "/workspace/review", "/workspace/operations"]) {
      expect(container.querySelector('a[href="' + href + '"]')).not.toBeNull();
    }
    expect(container.textContent).toContain("Every action is authorised again by the Control Plane");
    expect(container.textContent).toContain("does not report live status");
    await expectAccessible(container);
  });

  it("marks current route and clearly labels restricted access", async () => {
    const { container } = render(<WorkspaceAuthorityProvider value={{ state: "available", permissions: ["application.review", "operation.view"] }}><WorkspaceNavigation /></WorkspaceAuthorityProvider>);
    expect(screen.getByRole("navigation", { name: "Console workspace" })).toBeTruthy();
    expect(container.querySelector('a[href="/workspace"][aria-current="page"]')).not.toBeNull();
    expect(container.textContent).toContain("Independent review");
    expect(container.textContent).toContain("independently authorised");
    await expectAccessible(container);
  });
});
