// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { navigation, pages, legalPages } from "@/features/public-site/content";
import { Landing } from "@/features/public-site/Landing";
import { SiteShell } from "@/features/public-site/SiteShell";
import { expectAccessible } from "./axe";

describe("public Baobab website", () => {
  it("keeps navigation internal links resolved and external links explicit", () => {
    const known = new Set(["/", "/login", "/signup", "/workspace", "/workspace/apply", "/workspace/review", "/workspace/operations"]);
    for (const slug of Object.keys(pages)) known.add("/" + slug);
    for (const slug of Object.keys(legalPages)) known.add("/legal/" + slug);
    for (const group of navigation) for (const item of group.links) expect(known.has(item.href)).toBe(true);
    for (const page of Object.values(pages)) {
      expect(page.title.length).toBeGreaterThan(12);
      expect(page.lead.length).toBeGreaterThan(30);
      expect(page.sections.length).toBeGreaterThan(1);
      expect(page.href.startsWith("https://github.com/") || known.has(page.href)).toBe(true);
    }
  });

  it("provides a real entry to the protected Console without pretending to activate tenants", async () => {
    const { container } = render(<Landing />);
    expect(screen.getByRole("navigation", { name: "Primary navigation" })).toBeTruthy();
    expect(screen.getByRole("main")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1, name: /one platform/i })).toBeTruthy();
    expect(screen.getAllByRole("link", { name: /console/i }).some((a) => a.getAttribute("href") === "/login")).toBe(true);
    expect(screen.getAllByRole("link", { name: /log in/i }).some((a) => a.getAttribute("href") === "/login")).toBe(true);
    expect(screen.getAllByRole("link", { name: /sign up/i }).some((a) => a.getAttribute("href") === "/signup")).toBe(true);
    expect(screen.getAllByRole("link", { name: /get started/i }).some((a) => a.getAttribute("href") === "/start")).toBe(true);
    expect(screen.getByText(/authorised access and an enabled environment/i)).toBeTruthy();
    expect(container.textContent).toContain("before activation");
    await expectAccessible(container);
  });

  it("keeps public and protected experiences separate", () => {
    const { container } = render(<SiteShell><p>Read-only platform information</p></SiteShell>);
    expect(container.querySelector("main")?.textContent).toContain("Read-only");
    expect(container.querySelector("form")).toBeNull();
    expect(container.querySelector('a[href="/login"]')).not.toBeNull();
    expect(container.querySelector('a[href="/signup"]')).not.toBeNull();
    expect(container.querySelector('a[href="/legal/privacy"]')).not.toBeNull();
  });

  it("does not fake legal and health certification", () => {
    expect(pages.trust?.note).toContain("No ISO");
    expect(pages.status?.note).toContain("No public uptime");
    expect(legalPages.privacy?.introduction).toContain("await approval");
    expect(pages.contact?.note).toContain("does not collect");
  });
});
