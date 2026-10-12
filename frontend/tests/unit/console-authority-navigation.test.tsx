// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it, vi } from "vitest";

import { WorkspaceAuthorityProvider } from "@/features/console/authority-context";
import { visibleConsoleRoutes } from "@/features/console/navigation-model";
import { CommandPalette } from "@/app/workspace/CommandPalette";
import { WorkspaceNavigation } from "@/app/workspace/WorkspaceNavigation";
import { summariseEffectiveAuthority } from "@/server/bff/authority";
import { resolveProgressiveCommand } from "@/server/bff/progressive";
import { expectAccessible } from "./axe";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => "/workspace",
  useRouter: () => ({ push }),
}));

beforeAll(() => {
  // jsdom does not implement the native modal API.
  HTMLDialogElement.prototype.showModal = function showModal() { this.setAttribute("open", ""); };
  HTMLDialogElement.prototype.close = function close() { this.removeAttribute("open"); this.dispatchEvent(new Event("close")); };
});

const now = Date.parse("2026-10-12T10:01:00Z");
const grant = (permission: string) => ({
  grant_id: "agr_example1234567890",
  permission,
  scope: { level: "PLATFORM" },
  grant_type: "STANDING", source: "DIRECT", risk_class: "LOW",
});

describe("CPFE-04 caller-bound, accessible Console navigation", () => {
  it("maps only exact validated, fresh caller grants to UI routes", () => {
    const resolved = summariseEffectiveAuthority({
      principal_id: "prn_owned", evaluated_at: "2026-10-12T10:00:00Z",
      grants: [grant("application.review"), grant("operation.view"), grant("organisation.view")],
    }, "prn_owned", now);
    expect(resolved.state).toBe("available");
    expect(visibleConsoleRoutes(resolved).map((route) => route.href)).toEqual([
      "/workspace", "/workspace/apply", "/workspace/organisations", "/workspace/review", "/workspace/operations",
    ]);
    const other = summariseEffectiveAuthority({
      principal_id: "prn_other", evaluated_at: "2026-10-12T10:00:00Z",
      grants: [grant("application.review")],
    }, "prn_owned", now);
    expect(other.state).toBe("unavailable");
    expect(visibleConsoleRoutes(other).map((route) => route.href)).toEqual(["/workspace", "/workspace/apply"]);
  });

  it("does not infer authority from a stale result, arbitrary roles, or fabricated grants", () => {
    const revokedByTime = summariseEffectiveAuthority({
      principal_id: "prn_owned", evaluated_at: "2026-10-12T09:01:00Z",
      grants: [grant("application.decide")], iam_roles: ["platform_admin"],
    }, "prn_owned", now);
    expect(revokedByTime.state).toBe("unavailable");
    const malformed = summariseEffectiveAuthority({
      principal_id: "prn_owned", evaluated_at: "2026-10-12T10:00:00Z",
      grants: [{ permission: "application.review", scope: { level: "PLATFORM" } }],
    }, "prn_owned", now);
    expect(malformed.state).toBe("unavailable");
    const noGrants = summariseEffectiveAuthority({
      principal_id: "prn_owned", evaluated_at: "2026-10-12T10:00:00Z", grants: [],
    }, "prn_owned", now);
    expect(noGrants.state).toBe("available");
    expect(visibleConsoleRoutes(noGrants)).toHaveLength(2);
  });

  it("keeps authority endpoint an exact server-owned read with no caller-supplied selector", () => {
    expect(resolveProgressiveCommand({ action: "effective-authority" })).toEqual({
      method: "GET", path: "/v1/admin/effective-authority",
    });
    for (const attack of [
      { action: "effective-authority", id: "prn_other_00001" },
      { action: "effective-authority", tenantId: "ten_other_00001" },
      { action: "effective-authority", body: { principal_id: "other" } },
    ]) expect(() => resolveProgressiveCommand(attack as never)).toThrow();
  });

  it("hides privileged links on unavailable authority and remains accessible", async () => {
    const { container } = render(
      <WorkspaceAuthorityProvider value={{ state: "unavailable", permissions: [] }}>
        <WorkspaceNavigation />
      </WorkspaceAuthorityProvider>,
    );
    expect(container.querySelector('a[href="/workspace/apply"]')).not.toBeNull();
    expect(container.querySelector('a[href="/workspace/review"]')).toBeNull();
    expect(container.querySelector('a[href="/workspace/operations"]')).toBeNull();
    expect(screen.getAllByText(/administrative navigation is hidden/i).length).toBeGreaterThan(0);
    await expectAccessible(container);
  });

  it("searches only visible routes through a keyboard-accessible native modal", async () => {
    const user = userEvent.setup();
    const { container } = render(
      <CommandPalette authority={{ state: "available", permissions: ["application.review"] }} />,
    );
    await user.keyboard("{Control>}k{/Control}");
    expect(screen.getByRole("dialog")).toBeTruthy();
    const search = screen.getByRole("searchbox", { name: "Search available Console pages" });
    await user.type(search, "readiness");
    expect(screen.getByRole("status").textContent).toContain("No matching");
    expect(container.querySelector('button[data-command]')).toBeNull();
    await user.clear(search);
    await user.type(search, "review");
    expect(container.querySelectorAll('button[data-command]')).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: /independent review/i }));
    expect(push).toHaveBeenCalledWith("/workspace/review");
    expect(container.querySelector("dialog")?.hasAttribute("open")).toBe(false);
  });
});
