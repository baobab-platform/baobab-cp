import { mkdir, readdir, stat, writeFile } from "node:fs/promises";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

export const DEFAULT_LIMITS = Object.freeze({
  allClientJavaScript: 800 * 1024,
  largestClientChunk: 250 * 1024,
  allCss: 64 * 1024,
  largestPublicImage: 250 * 1024,
});

async function filesBelow(directory) {
  let entries;
  try {
    entries = await readdir(directory, { withFileTypes: true });
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return [];
    throw error;
  }
  const nested = await Promise.all(entries.map((entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(path) : [path];
  }));
  return nested.flat();
}

async function measure(paths, root) {
  return Promise.all(paths.map(async (path) => ({
    path: relative(root, path),
    bytes: (await stat(path)).size,
  })));
}

function largest(items) {
  return items.reduce((best, item) => item.bytes > best.bytes ? item : best, { path: "none", bytes: 0 });
}

export function evaluateBudget(measured, limits = DEFAULT_LIMITS) {
  return Object.entries(limits)
    .filter(([metric, maximum]) => measured[metric] > maximum)
    .map(([metric, maximum]) => ({ metric, actualBytes: measured[metric], limitBytes: maximum }));
}

export async function createBudgetReport(root = process.cwd(), limits = DEFAULT_LIMITS) {
  const staticFiles = await filesBelow(join(root, ".next", "static"));
  const js = await measure(staticFiles.filter((path) => path.endsWith(".js")), root);
  const css = await measure(staticFiles.filter((path) => path.endsWith(".css")), root);
  if (js.length === 0 || css.length === 0) {
    throw new Error("Missing production client JavaScript or CSS: run pnpm build first");
  }

  const images = await measure(
    (await filesBelow(join(root, "public"))).filter((path) => /\\.(?:avif|jpe?g|png|webp)$/i.test(path)),
    root,
  );
  const largestChunk = largest(js);
  const largestImage = largest(images);
  const measured = {
    allClientJavaScript: js.reduce((sum, item) => sum + item.bytes, 0),
    largestClientChunk: largestChunk.bytes,
    allCss: css.reduce((sum, item) => sum + item.bytes, 0),
    largestPublicImage: largestImage.bytes,
  };
  return {
    profile: "CI_PRODUCTION_BUILD",
    limits,
    measured,
    oversized: evaluateBudget(measured, limits),
    largestClientChunkPath: largestChunk.path,
    largestPublicImagePath: largestImage.path,
  };
}

async function main() {
  const root = process.cwd();
  const report = await createBudgetReport(root);
  const reportDirectory = join(root, "reports");
  await mkdir(reportDirectory, { recursive: true });
  const reportPath = join(reportDirectory, "static-budget.json");
  await writeFile(reportPath, JSON.stringify(report, null, 2) + "\\n");

  for (const [metric, actual] of Object.entries(report.measured)) {
    const maximum = report.limits[metric];
    console.log(metric + ": " + (actual / 1024).toFixed(1) + " KiB / " + (maximum / 1024).toFixed(1) + " KiB");
  }
  if (report.oversized.length) {
    throw new Error("Frontend asset budget exceeded: " + report.oversized.map((item) =>
      item.metric + " (" + item.actualBytes + " / " + item.limitBytes + " bytes)",
    ).join(", "));
  }
  console.log("Static asset budgets passed; evidence: " + relative(root, reportPath));
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : "Static performance budget failed");
    process.exitCode = 1;
  });
}
