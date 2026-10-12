import test from "node:test";
import assert from "node:assert/strict";
import { percentile } from "./cx05h-perf.mjs";

test("latencies use nearest-rank percentile and sorted observations", () => {
  assert.equal(percentile([30, 10, 20, 40, 50], 0.95), 50);
  assert.equal(percentile([30, 10, 20, 40, 50], 0.50), 30);
  assert.equal(percentile([10], 0.99), 10);
});
test("empty and invalid sample calculations fail closed", () => {
  assert.throws(() => percentile([], 0.95));
  assert.throws(() => percentile([10], 0));
  assert.throws(() => percentile([10], 1.1));
});
