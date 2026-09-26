import axe from "axe-core";
import { expect } from "vitest";

/**
 * Asserts that the rendered markup has no axe violations. Colour contrast
 * needs real layout, which jsdom lacks; tokens.test.ts checks the palette
 * instead.
 */
export async function expectAccessible(container: Element): Promise<void> {
  const results = await axe.run(container, { rules: { "color-contrast": { enabled: false } } });
  const violations = results.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(" ")).join(", ")})`);
  expect(violations).toEqual([]);
}
