// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { ProgressiveShowcase } from "@/features/onboarding/ProgressiveShowcase";
import { journeyStages, serviceIllustrations } from "@/features/onboarding/progressive-model";

import { expectAccessible } from "./axe";

describe("PEO-06 progressive journey showcase", () => {
  it("exposes all seven independent transitions without inventing authority", async () => {
    const { container } = render(<ProgressiveShowcase />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("clearer path");
    expect(screen.getByText(/not a live application/i)).toBeTruthy();
    expect(screen.getByText(/APPROVED does not mean READY/)).toBeTruthy();
    const items = container.querySelectorAll("ol li");
    expect(items).toHaveLength(journeyStages.length);
    expect(journeyStages).toHaveLength(7);
    await expectAccessible(container);
  });

  it("uses non-mutating service examples, not a document checklist or eligibility decision", async () => {
    const user = userEvent.setup();
    render(<ProgressiveShowcase />);
    const settlement = screen.getByRole("button", { name: "Payment settlement" });
    await user.click(settlement);
    expect(settlement.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText(/Statutory and payment-provider restrictions cannot be waived/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /approve|activate|submit application/i })).toBeNull();
    expect(serviceIllustrations).toHaveLength(5);
    expect(screen.getByText(/PAYE, UIF, VAT and corporate certificates are not universal/)).toBeTruthy();
  });
});
