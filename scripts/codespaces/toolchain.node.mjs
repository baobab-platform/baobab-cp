import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root=resolve(fileURLToPath(new URL("../..",import.meta.url)));
const text=path=>readFileSync(resolve(root,path),"utf8");

test("DEV-10 exact Node 26.13 and pnpm 12 manifest identity",()=>{
  const base=JSON.parse(text("package.json"));
  const frontend=JSON.parse(text("frontend/package.json"));
  assert.equal(base.packageManager,"pnpm@12.16.1");
  assert.equal(base.engines.node,">=26.13.0 <27");
  assert.equal(frontend.engines.node,base.engines.node);
  assert.equal(frontend.devDependencies["@types/node"],"26.6.6");
  assert.equal(text("frontend/.node-version").trim(),"26.13.0");
});

test("pnpm 12 env and project locks are independently present and non-empty",()=>{
  const lock=text("pnpm-lock.yaml");
  const docs=lock.split("\n---\n");
  assert.equal(docs.length,2,"pnpm12 requires separate pinned manager and project dependency documents");
  const [manager,project]=docs;
  assert.match(manager,/^---\nlockfileVersion: '9\.0'/);
  assert.match(manager,/packageManagerDependencies:\n\s+pnpm:\n\s+specifier: 12\.16\.1\n\s+version: 12\.16\.1/);
  assert.match(manager,/@pnpm\/exe\.linux-x64@12\.16\.1/);
  assert.match(manager,/resolution: \{integrity: sha512-[A-Za-z0-9+/]+={0,2}\}/);
  assert.match(project,/^lockfileVersion: '9\.0'/);
  assert.match(project,/^  frontend:\n/m);
  assert.match(project,/@types\/node@26\.6\.6/);
  assert.match(project,/undici-types@8\.9\.0/);
  assert.match(project,/@baobab-platform\/experience-ui/);
});

test("native and Codespaces setups must pin rather than infer Corepack 0.36 and pnpm 12",()=>{
  const docker=text("frontend/Dockerfile");
  assert.match(docker,/FROM node:26\.13\.0-trixie-slim@sha256:[a-f0-9]{64}/);
  assert.match(docker,/FROM gcr\.io\/distroless\/nodejs26-debian13:nonroot@sha256:[a-f0-9]{64}/);
  assert.match(docker,/corepack@0\.36\.0/);
  assert.match(docker,/pnpm --version/);
  assert.match(docker,/12\.16\.1/);
  const ci=text(".github/workflows/frontend.yml");
  assert.match(ci,/corepack@0\.36\.0/);
  assert.match(ci,/node-version-file: frontend\/\.node-version/);
  const dc=JSON.parse(text(".devcontainer/devcontainer.json"));
  assert.equal(dc.features["ghcr.io/devcontainers/features/node:1"]?.version,"26.13.0");
  assert.match(dc.postCreateCommand,/corepack@0\.36\.0/);
  assert.match(dc.postCreateCommand,/pnpm --version/);
  assert.match(dc.postCreateCommand,/12\.16\.1/);
  assert.equal(dc.otherPortsAttributes.visibility,"private");
});
