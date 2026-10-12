// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import WorkspaceIndex from "@/app/workspace/page";
import { WorkspaceNavigation } from "@/app/workspace/WorkspaceNavigation";
import { expectAccessible } from "./axe";

vi.mock("next/navigation", () => ({
  usePathname: () => "/workspace",
}));

describe("secure Console presentation foundation", () => {
  it("uses authorised routes and reports no fake metrics or activation state", async () => {
    const { container } = render(<main><WorkspaceIndex /></main>);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Where businesses become operational");
    for (const href of ["/workspace/apply", "/workspace/review", "/workspace/operations"]) {
      expect(container.querySelector('a[href="' + href + '"]')).not.toBeNull();
    }
    expect(container.textContent).toContain("Navigation does not confer privileges");
    expect(container.textContent).toContain("does not report live status");
    await expectAccessible(container);
  });

  it("marks current route and clearly labels restricted access", async () => {
    const { container } = render(<WorkspaceNavigation />);
    expect(screen.getByRole("navigation", { name: "Console workspace" })).toBeTruthy();
    expect(container.querySelector('a[href="/workspace"][aria-current="page"]')).not.toBeNull();
    expect(container.textContent).toContain("Restricted");
    expect(container.textContent).toContain("authorised independently");
    await expectAccessible(container);
  });
});
