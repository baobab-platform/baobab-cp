import { describe, expect, it } from "vitest";

import { consoleRoutes, visibleConsoleRoutes } from "@/features/console/navigation-model";
import { routePresentation } from "@/app/workspace/page";

describe("Console route presentation catalogue", () => {
  it("provides valid presentation metadata for every non-overview route", () => {
    const routes = consoleRoutes.filter((route) => route.href !== "/workspace");
    expect(Object.keys(routePresentation).sort()).toEqual(routes.map((route) => route.href).sort());
    for (const route of routes) {
      const presentation = routePresentation[route.href];
      expect(presentation.icon).toBeDefined();
      expect(presentation.action.trim().length).toBeGreaterThan(0);
      expect(presentation.detail.trim().length).toBeGreaterThan(0);
    }
  });

  it("keeps administrative routes hidden when their grants are absent or unavailable", () => {
    expect(visibleConsoleRoutes({ state: "available", permissions: [] }).map((route) => route.href))
      .toEqual(["/workspace", "/workspace/apply"]);
    expect(visibleConsoleRoutes({ state: "unavailable", permissions: ["organisation.view", "operation.view"] })
      .map((route) => route.href)).toEqual(["/workspace", "/workspace/apply"]);
  });
});
