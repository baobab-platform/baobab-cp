// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApplicantWorkspace } from "@/features/onboarding/ApplicantWorkspace";
import { ReviewerWorkspace } from "@/features/onboarding/ReviewerWorkspace";
import { OperationWorkspace } from "@/features/onboarding/OperationWorkspace";

import { expectAccessible } from "./axe";

vi.mock("@/features/onboarding/workspace-client", () => ({
  readWorkspace: vi.fn().mockRejectedValue(new Error("synthetic denied")),
  sendWorkspaceCommand: vi.fn().mockRejectedValue(new Error("synthetic denied")),
}));

describe("PEO-06F accessible governed Console forms", () => {
  it("keeps applicant form fields and dynamically relevant incorporation evidence accessible", async () => {
    const user = userEvent.setup();
    const { container } = render(<main><ApplicantWorkspace /></main>);
    await expectAccessible(container);
    expect(screen.getByRole("heading", { level: 1 }).textContent)
      .toContain("Tell us about your business");
    expect(screen.queryByLabelText("Declared company registration number")).toBeNull();
    await user.selectOptions(screen.getByLabelText("Incorporation declaration"), "INCORPORATED_CLAIMED");
    expect(screen.getByLabelText("Declared company registration number")).toBeTruthy();
    await expectAccessible(container);
    await user.selectOptions(screen.getByLabelText("Incorporation declaration"), "NOT_INCORPORATED");
    expect(screen.queryByLabelText("Declared company registration number")).toBeNull();
  });

  it("maintains accessible fields through distinct reviewer, decider and onboarding states", async () => {
    const user = userEvent.setup();
    const { container } = render(<main><ReviewerWorkspace /></main>);
    await expectAccessible(container);
    const stage = screen.getByLabelText("Independent authority stage");
    await user.selectOptions(stage, "decide");
    expect(screen.getByLabelText("Independent decision")).toBeTruthy();
    await expectAccessible(container);
    await user.selectOptions(stage, "request-onboarding");
    expect(screen.getByLabelText("Reviewed operating display name")).toBeTruthy();
    await expectAccessible(container);
    await user.selectOptions(stage, "authorise-onboarding");
    expect(screen.getByLabelText("Current approved policy reference")).toBeTruthy();
    await expectAccessible(container);
  });

  it("provides a read-only operation status control accessible by keyboard", async () => {
    const user = userEvent.setup();
    const { container } = render(<main><OperationWorkspace /></main>);
    await expectAccessible(container);
    const input = screen.getByLabelText("Control Plane operation ID");
    // Exercise actual sequential keyboard navigation: pointer focus would
    // incorrectly pass even if tabIndex=-1 removed the input from tab order.
    await user.tab();
    expect(document.activeElement).toBe(input);
    await user.keyboard("op_test123");
    const button = screen.getByRole("button", { name: "Refresh operation state" });
    expect(button.hasAttribute("disabled")).toBe(false);
    await user.tab();
    expect(document.activeElement).toBe(button);
    await user.keyboard("{Enter}");
    expect(screen.getByRole("status")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /activate tenant/i })).toBeNull();
  });
});
