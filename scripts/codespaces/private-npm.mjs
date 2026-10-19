#!/usr/bin/env node
/**
 * Codespaces npm consumer-only auth: no token values are ever written to
 * disk. pnpm expands the literal environment reference from *user* npmrc.
 * Prebuild lifecycle MUST NOT run this program (user secrets unavailable).
 */
import { existsSync, lstatSync, readFileSync, writeFileSync, renameSync, chmodSync, rmSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
const key = "//npm.pkg.github.com/:_authToken";
const registry = "@baobab-platform:registry=https://npm.pkg.github.com";
const variable = process.env.BAOBAB_PACKAGES_READ_TOKEN ? "BAOBAB_PACKAGES_READ_TOKEN" : process.env.GITHUB_TOKEN ? "GITHUB_TOKEN" : null;
if (!variable) {
  // Shared workspace needs no package token for workspace-local packages;
  // consumer bootstrap separately fails closed if external packages exist.
  console.log("GitHub Packages token unavailable; no registry auth configured.");
} else {
  const file = join(process.env.HOME || homedir(), ".npmrc");
  if (existsSync(file) && !lstatSync(file).isFile() && !lstatSync(file).isSymbolicLink()) {
    throw new Error("unsafe npm user configuration");
  }
  const current = existsSync(file) && lstatSync(file).isFile() ? readFileSync(file, "utf8") : "";
  const lines = current.split(/\r?\n/).filter(line => line.trim() &&
    !/^\/\/npm\.pkg\.github\.com\/:_authToken\s*=/i.test(line.trim()) &&
    line.trim() !== registry);
  lines.push(registry, key + "=\${" + variable + "}");
  const tmp = join(process.env.HOME || homedir(), ".npmrc.baobab-" + process.pid);
  try {
    writeFileSync(tmp, lines.join("\n") + "\n", {mode:0o600, flag:"wx"});
    renameSync(tmp, file); chmodSync(file, 0o600);
  } finally { rmSync(tmp, {force:true}); }
  console.log("GitHub Packages user npmrc configured via environment reference (no token persisted).");
}
