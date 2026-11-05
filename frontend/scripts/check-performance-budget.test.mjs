import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it } from "node:test";

import { DEFAULT_LIMITS, runBudgetCheck } from "./check-performance-budget.mjs";

const scriptPath = fileURLToPath(new URL("./check-performance-budget.mjs", import.meta.url));

async function fixture({ javascript = 1024, css = 512, image = 256 } = {}) {
  const root = await mkdtemp(join(tmpdir(), "cp-console-budget-"));
  await mkdir(join(root, ".next", "static", "chunks"), { recursive: true });
  await mkdir(join(root, "public", "images"), { recursive: true });
  await writeFile(join(root, ".next", "static", "chunks", "app.js"), Buffer.alloc(javascript, 1));
  await writeFile(join(root, ".next", "static", "chunks", "app.css"), Buffer.alloc(css, 1));
  await writeFile(join(root, "public", "images", "brand.png"), Buffer.alloc(image, 1));
  return root;
}

describe("CP Console static performance budgets", () => {
  it("keeps the shared estate budget profile explicit and unchanged", () => {
    assert.deepEqual(DEFAULT_LIMITS, {
      allClientJavaScript: 800 * 1024,
      largestClientChunk: 250 * 1024,
      allCss: 64 * 1024,
      largestPublicImage: 250 * 1024,
    });
  });

  it("measures nested emitted files and passes a normal production fixture", async () => {
    const root = await fixture();
    try {
      const report = await runBudgetCheck(root);
      assert.deepEqual(report.oversized, []);
      assert.equal(report.measured.allClientJavaScript, 1024);
      assert.equal(report.measured.allCss, 512);
      assert.equal(report.measured.largestPublicImage, 256);
      const saved = JSON.parse(await readFile(join(root, "reports", "static-budget.json"), "utf8"));
      assert.deepEqual(saved, report);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("fails on oversized JavaScript, CSS and public images while retaining all failure evidence", async () => {
    const root = await fixture({
      javascript: DEFAULT_LIMITS.largestClientChunk + 1,
      css: DEFAULT_LIMITS.allCss + 1,
      image: DEFAULT_LIMITS.largestPublicImage + 1,
    });
    try {
      for (let index = 0; index < 3; index += 1) {
        await writeFile(
          join(root, ".next", "static", "chunks", "shared-" + index + ".js"),
          Buffer.alloc(DEFAULT_LIMITS.largestClientChunk, 1),
        );
      }
      const result = spawnSync(process.execPath, [scriptPath], { cwd: root, encoding: "utf8" });
      assert.equal(result.status, 1, result.stdout + result.stderr);
      assert.match(result.stderr, /Frontend asset budget exceeded/);
      const report = JSON.parse(await readFile(join(root, "reports", "static-budget.json"), "utf8"));
      assert.deepEqual(report.oversized.map((item) => item.metric), [
        "allClientJavaScript",
        "largestClientChunk",
        "allCss",
        "largestPublicImage",
      ]);
      assert.ok(report.measured.allClientJavaScript > DEFAULT_LIMITS.allClientJavaScript);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("fails when production build output is missing", async () => {
    const root = await mkdtemp(join(tmpdir(), "cp-console-budget-missing-"));
    try {
      const result = spawnSync(process.execPath, [scriptPath], { cwd: root, encoding: "utf8" });
      assert.equal(result.status, 1);
      assert.match(result.stderr, /Missing production client JavaScript or CSS/);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  it("fails when the production output contains JavaScript but no CSS", async () => {
    const root = await mkdtemp(join(tmpdir(), "cp-console-budget-no-css-"));
    try {
      await mkdir(join(root, ".next", "static"), { recursive: true });
      await writeFile(join(root, ".next", "static", "app.js"), "window.app = true;");
      const result = spawnSync(process.execPath, [scriptPath], { cwd: root, encoding: "utf8" });
      assert.equal(result.status, 1);
      assert.match(result.stderr, /Missing production client JavaScript or CSS/);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
