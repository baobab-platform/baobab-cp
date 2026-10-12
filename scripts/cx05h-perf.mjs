#!/usr/bin/env node
// CX-05H protected production-like directory load acceptance.
// This script contains NO test doubles, session storage or synthetic passes.
import { performance } from "node:perf_hooks";
import { createHash } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";

export function percentile(values, p) {
  if (!values.length || p <= 0 || p > 1) throw new Error("invalid latency sample");
  const sorted = [...values].sort((a, b) => a - b);
  return Math.round(sorted[Math.ceil(p * sorted.length) - 1] * 10) / 10;
}

function setting(name) {
  const v = process.env[name]?.trim();
  if (!v || /example\.com|localhost|127\.0\.0\.1|<.*>/.test(v)) {
    throw new Error("MISSING_LIVE_STAGING_SETTING:" + name);
  }
  return v;
}

async function execute() {
  const origin = new URL(setting("CX05_STAGING_CP_URL"));
  if (origin.protocol !== "https:" || origin.username || origin.password || origin.hash || origin.search) {
    throw new Error("PERFORMANCE_TARGET_NOT_HTTPS_ORIGIN");
  }
  const token = setting("CX05_STAGING_GRANTED_TOKEN");
  const minimum = Number(setting("CX05_STAGING_MIN_VISIBLE"));
  const limit = Number(setting("CX05_STAGING_PERF_P95_MS"));
  const count = Number(process.env.CX05_STAGING_PERF_SAMPLES ?? "128");
  if (!Number.isSafeInteger(minimum) || minimum < 5001 ||
      !Number.isSafeInteger(limit) || limit < 50 || limit > 2500 ||
      !Number.isSafeInteger(count) || count < 64 || count > 1024) {
    throw new Error("INVALID_EXPLICIT_PERFORMANCE_ACCEPTANCE_BUDGET");
  }
  const endpoint = new URL("/v1/organisations/directory/page?limit=50&sort=name-asc", origin);
  const latencies = [];
  let rejected = 0;
  let visible = 0;
  let cursorFailures = 0;
  async function sample() {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 12000);
    const start = performance.now();
    try {
      const response = await fetch(endpoint, {
        method: "GET", redirect: "error", cache: "no-store",
        headers: { Authorization: "Bearer " + token,
          Accept: "application/json", "Cache-Control": "no-store" },
        signal: controller.signal,
      });
      const elapsed = performance.now() - start;
      if (response.status !== 200) { rejected++; return; }
      const json = await response.json();
      if (!Number.isInteger(json.total_visible) || json.total_visible < minimum ||
          !Array.isArray(json.items) || json.items.length < 1 || json.items.length > 50 ||
          !json.items.every(row => typeof row.organisation_id === "string") ||
          new Set(json.items.map(row => row.organisation_id)).size !== json.items.length) {
        cursorFailures++; return;
      }
      visible = json.total_visible;
      latencies.push(elapsed);
    } catch {
      rejected++;
    } finally {
      clearTimeout(timer);
    }
  }
  // Bounded concurrency protects the target and makes the run reproducible.
  let next = 0;
  async function worker() {
    while (true) {
      const n = next++;
      if (n >= count) return;
      await sample();
    }
  }
  await Promise.all(Array.from({ length: 4 }, () => worker()));
  if (latencies.length === 0) throw new Error("NO_VALID_LATENCY_SAMPLES");
  const metrics = {
    sample_count: count, success_count: latencies.length,
    rejected_count: rejected, inconsistent_count: cursorFailures,
    visible_count: visible, concurrency: 4, p50_ms: percentile(latencies, 0.50),
    p95_ms: percentile(latencies, 0.95), p99_ms: percentile(latencies, 0.99),
    max_ms: Math.round(Math.max(...latencies) * 10) / 10, p95_budget_ms: limit,
  };
  return { metrics, pass: rejected === 0 && cursorFailures === 0 &&
    latencies.length === count && visible >= minimum && metrics.p95_ms <= limit };
}

const direct = process.argv[1] && new URL(import.meta.url).pathname === process.argv[1];
if (direct) {
  const report = {
    kind: "CX05H_PROTECTED_HIGH_VOLUME_PERFORMANCE",
    revision: process.env.GITHUB_SHA ?? "local",
    executed_at: new Date().toISOString(),
    result: "BLOCKED", production_eligible: false, independently_signed_off: false,
  };
  try {
    const outcome = await execute();
    report.result = outcome.pass ? "PASS" : "BLOCKED";
    report.metrics = outcome.metrics;
  } catch (error) {
    report.failure_code = String(error?.message ?? error).slice(0, 130);
  }
  // No PII, organisation IDs, URL, tokens, browser session or response bodies.
  const raw = JSON.stringify(report);
  report.report_sha256 = createHash("sha256").update(raw).digest("hex");
  await mkdir("cx05-evidence", { recursive: true });
  await writeFile("cx05-evidence/cx05h-perf.json", JSON.stringify(report, null, 2) + "\n", { mode: 0o600 });
  console.log("CX-05H live performance:", report.result, "eligible:false");
  if (report.result !== "PASS") process.exitCode = 2;
}
