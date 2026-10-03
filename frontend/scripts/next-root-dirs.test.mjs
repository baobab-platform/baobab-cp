import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const require = createRequire(import.meta.url);
const nextRequire = createRequire(require.resolve("eslint-config-next"));
const { getRootDirs } = nextRequire("@next/eslint-plugin-next/dist/utils/get-root-dirs.js");

test("Next root directory discovery retains directory-only glob behavior", () => {
  const root = mkdtempSync(join(tmpdir(), "cp-next-roots-"));
  try {
    const first = join(root, "app-a");
    const second = join(root, "app-b");
    mkdirSync(first);
    mkdirSync(second);
    writeFileSync(join(root, "app-file"), "not a directory");
    // Next passes these paths to path.join and fs, which accept absolute and
    // relative directory paths equally. Compare the directories they name.
    const roots = (rootDir) => getRootDirs({ cwd: root, settings: { next: { rootDir } } }).map((path) => resolve(path)).sort();
    assert.deepEqual(roots(undefined), [root]);
    assert.deepEqual(roots(join(root, "app-*")), [first, second]);
    assert.deepEqual(roots([first, second]), [first, second]);
    assert.deepEqual(roots(join(root, "app-{a,b}")), [first, second]);
    assert.deepEqual(roots(join(root, "app-*").replaceAll("/", "\\")), [first, second]);
    assert.deepEqual(roots(join(root, "missing-*")), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
