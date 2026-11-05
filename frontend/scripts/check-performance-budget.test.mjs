import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { DEFAULT_LIMITS, evaluateBudget } from "./check-performance-budget.mjs";

describe("CP Console static performance budgets", () => {
  it("keeps the shared estate budget profile explicit and unchanged", () => {
    assert.deepEqual(DEFAULT_LIMITS, {
      allClientJavaScript: 800 * 1024,
      largestClientChunk: 250 * 1024,
      allCss: 64 * 1024,
      largestPublicImage: 250 * 1024,
    });
  });

  it("accepts values at the limit and below it", () => {
    assert.deepEqual(evaluateBudget({
      allClientJavaScript: DEFAULT_LIMITS.allClientJavaScript,
      largestClientChunk: DEFAULT_LIMITS.largestClientChunk - 1,
      allCss: DEFAULT_LIMITS.allCss,
      largestPublicImage: 0,
    }), []);
  });

  it("reports every exceeded budget without changing thresholds", () => {
    assert.deepEqual(evaluateBudget({
      allClientJavaScript: DEFAULT_LIMITS.allClientJavaScript + 1,
      largestClientChunk: DEFAULT_LIMITS.largestClientChunk + 2,
      allCss: DEFAULT_LIMITS.allCss,
      largestPublicImage: DEFAULT_LIMITS.largestPublicImage + 3,
    }), [
      { metric: "allClientJavaScript", actualBytes: 819201, limitBytes: 819200 },
      { metric: "largestClientChunk", actualBytes: 256002, limitBytes: 256000 },
      { metric: "largestPublicImage", actualBytes: 256003, limitBytes: 256000 },
    ]);
  });
});
