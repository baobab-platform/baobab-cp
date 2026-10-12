// @vitest-environment jsdom
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { DirectorySearchInput } from "@/features/console/DirectorySearchInput";
import { expectAccessible } from "./axe";

afterEach(() => {
  vi.useRealTimers();
});

describe("CX-05B debounced accessible organisation search", () => {
  it("defers a native GET request until typing settles and never sends one for unchanged terms", () => {
    vi.useFakeTimers();
    const { container } = render(
      <form action="/workspace/organisations" method="GET">
        <label htmlFor="org-query">Organisation name</label>
        <DirectorySearchInput defaultQuery="Nabhold" />
      </form>,
    );
    const form = container.querySelector("form")!;
    const submit = vi.spyOn(form, "requestSubmit").mockImplementation(() => {});
    const input = screen.getByRole("searchbox", { name: "Organisation name" });
    fireEvent.change(input, { target: { value: "Nab" } });
    act(() => vi.advanceTimersByTime(400));
    expect(submit).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "Nabhold Group" } });
    act(() => vi.advanceTimersByTime(450));
    expect(submit).toHaveBeenCalledTimes(1);
    expect(form.getAttribute("method")).toBe("GET");
    expect(form.getAttribute("action")).toBe("/workspace/organisations");
    expect(input.getAttribute("name")).toBe("q");
    expect(container.querySelector('[name="tenant_id"]')).toBeNull();
    fireEvent.change(input, { target: { value: "Nabhold" } });
    act(() => vi.advanceTimersByTime(500));
    expect(submit).toHaveBeenCalledTimes(1);
  });

  it("clears outstanding timers on removal and preserves accessible input help", async () => {
    const { container, unmount } = render(
      <form action="/workspace/organisations" method="GET">
        <label htmlFor="org-query">Organisation name</label>
        <DirectorySearchInput defaultQuery="" />
      </form>,
    );
    await expectAccessible(container);
    vi.useFakeTimers();
    const submit = vi.spyOn(container.querySelector("form")!, "requestSubmit").mockImplementation(() => {});
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "Thamani" } });
    unmount();
    act(() => vi.advanceTimersByTime(600));
    expect(submit).not.toHaveBeenCalled();
  });
});
