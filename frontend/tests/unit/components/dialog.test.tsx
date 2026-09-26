// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { Button, ConfirmDialog } from "@/components";

import { expectAccessible } from "../axe";

function Harness({ onConfirm, typed, reason }: { onConfirm: (reason: string | undefined) => void; typed?: string; reason?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <main>
      <Button onClick={() => setOpen(true)}>Suspend tenant</Button>
      <ConfirmDialog
        open={open}
        onClose={() => setOpen(false)}
        onConfirm={(value) => {
          onConfirm(value);
          setOpen(false);
        }}
        title="Suspend Example A?"
        consequence="Its workloads stop resolving context."
        confirmLabel="Suspend"
        {...(typed === undefined ? {} : { typedConfirmation: typed })}
        requireReason={reason === true}
      />
    </main>
  );
}

describe("ConfirmDialog", () => {
  it("opens as a labelled modal dialog and returns focus to its opener", async () => {
    const user = userEvent.setup();
    const { container } = render(<Harness onConfirm={() => {}} />);
    const opener = screen.getByRole("button", { name: "Suspend tenant" });
    await user.click(opener);
    const dialog = screen.getByRole("dialog", { name: "Suspend Example A?" });
    expect(dialog.getAttribute("aria-describedby")).toBeTruthy();
    expect(screen.getByText("Its workloads stop resolving context.").id).toBe(dialog.getAttribute("aria-describedby"));
    await expectAccessible(container);

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it("confirms only after the typed phrase matches and a reason is given", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(<Harness onConfirm={onConfirm} typed="Example A" reason />);
    await user.click(screen.getByRole("button", { name: "Suspend tenant" }));

    const confirm = screen.getByRole("button", { name: "Suspend" });
    expect(confirm).toHaveProperty("disabled", true);
    await user.type(screen.getByLabelText(/Type Example A to confirm/), "Example");
    expect(confirm).toHaveProperty("disabled", true);
    await user.type(screen.getByLabelText(/Type Example A to confirm/), " A");
    expect(confirm).toHaveProperty("disabled", true);
    await user.type(screen.getByLabelText(/Reason/), "  Non-payment escalation  ");
    expect(confirm).toHaveProperty("disabled", false);

    await user.click(confirm);
    expect(onConfirm).toHaveBeenCalledWith("Non-payment escalation");
  });

  it("clears what was typed when it is cancelled", async () => {
    const user = userEvent.setup();
    render(<Harness onConfirm={() => {}} typed="Example A" />);
    await user.click(screen.getByRole("button", { name: "Suspend tenant" }));
    await user.type(screen.getByLabelText(/Type Example A to confirm/), "Example A");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Suspend tenant" }));
    expect(screen.getByLabelText(/Type Example A to confirm/)).toHaveProperty("value", "");
    expect(screen.getByRole("button", { name: "Suspend" })).toHaveProperty("disabled", true);
  });
});
