// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { expectAccessible } from "./axe";

describe("Baobab Tailwind/shadcn integration", () => {
  it("keeps semantics in the owned CSS tokens, not shadcn-generated hex values", () => {
    const css = readFileSync(resolve(process.cwd(), "src/styles/globals.css"), "utf8");
    const tokens = readFileSync(resolve(process.cwd(), "src/styles/tokens.css"), "utf8");
    expect(css).toContain('@import "tailwindcss"');
    expect(css).toContain("--color-primary: var(--ui-primary)");
    expect(css).toContain("--color-status-revoked-bg: var(--status-revoked-bg)");
    expect(css).toContain("--color-status-blocked-fg: var(--status-blocked-fg)");
    expect(tokens).toContain("--ui-primary: var(--color-primary)");
    expect(tokens).toContain("--status-revoked-fg:");
    expect(tokens).toContain("--baobab-gold: #fca300");
  });

  it("shadcn buttons do not submit forms without explicit intent", async () => {
    const { container } = render(<form><Button>Open details</Button><Button variant="destructive" disabled>Unavailable</Button></form>);
    expect(screen.getByRole("button", { name: "Open details" }).getAttribute("type")).toBe("button");
    expect(screen.getByRole("button", { name: "Unavailable" }).hasAttribute("disabled")).toBe(true);
    await expectAccessible(container);
  });

  it("keeps shadcn card information readable and status badges labelled", async () => {
    const { container } = render(
      <Card><CardTitle>Plan status</CardTitle><CardDescription>Current readiness is independently evaluated</CardDescription>
        <Badge variant="blocked">Blocked</Badge><Badge variant="revoked">Revoked</Badge></Card>,
    );
    expect(screen.getByText("Blocked").textContent).toBe("Blocked");
    expect(screen.getByText("Revoked").textContent).toBe("Revoked");
    await expectAccessible(container);
  });

  it("deduplicates utility classes without changing semantic CSS sources", () => {
    expect(cn("px-4 bg-primary", "px-6")).toBe("bg-primary px-6");
  });
});
