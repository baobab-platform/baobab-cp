// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { Disclosure, StatusLabel } from "@baobab-platform/experience-ui";
import { expectAccessible } from "./axe";

describe("BXS-05E published experience components", () => {
  it("preserves semantic warning and native keyboard disclosure", async () => {
    const { container } = render(
      <section data-bx-theme="baobab-platform">
        <StatusLabel tone="warning">Not a readiness authorisation</StatusLabel>
        <Disclosure summary="What does this mean?">
          Illustration only. CP grants and approvals remain authoritative.
        </Disclosure>
      </section>,
    );
    expect(screen.getByText("Not a readiness authorisation")).toBeTruthy();
    const summary = screen.getByText("What does this mean?");
    summary.focus();
    await userEvent.keyboard("{Enter}");
    expect(summary.closest("details")?.hasAttribute("open")).toBe(true);
    await expectAccessible(container);
  });
});
