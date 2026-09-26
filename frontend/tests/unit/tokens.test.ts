import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { statusTones } from "@/components/feedback/status";

const css = readFileSync(fileURLToPath(new URL("../../src/styles/tokens.css", import.meta.url)), "utf8");

function token(name: string): string {
  const match = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`).exec(css);
  if (!match?.[1]) throw new Error(`token --${name} is not a hex colour`);
  return match[1];
}

// WCAG 2.2 relative luminance and contrast ratio.
function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const [r, g, b] = channels.map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (light + 0.05) / (dark + 0.05);
}

describe("design tokens meet WCAG 2.2 AA contrast", () => {
  const text: Array<[string, string]> = [
    ["color-text", "color-surface"],
    ["color-text", "color-canvas"],
    ["color-text-muted", "color-surface"],
    ["color-text-muted", "color-canvas"],
    ["color-on-primary", "color-primary"],
    ["color-on-primary", "color-primary-hover"],
    ["color-on-danger", "color-danger"],
    ["color-on-danger", "color-danger-hover"],
    ["color-link", "color-surface"],
    ["color-primary", "status-success-bg"],
  ];

  it.each(text)("%s on %s is at least 4.5:1", (fg, bg) => {
    expect(contrast(token(fg), token(bg))).toBeGreaterThanOrEqual(4.5);
  });

  it.each(statusTones.map((tone) => [tone]))("status %s text is at least 4.5:1 on its background", (tone) => {
    expect(contrast(token(`status-${tone}-fg`), token(`status-${tone}-bg`))).toBeGreaterThanOrEqual(4.5);
  });

  it.each([["color-focus"], ["color-border-strong"]])("%s is at least 3:1 against the surface and canvas", (name) => {
    expect(contrast(token(name), token("color-surface"))).toBeGreaterThanOrEqual(3);
    expect(contrast(token(name), token("color-canvas"))).toBeGreaterThanOrEqual(3);
  });
});
