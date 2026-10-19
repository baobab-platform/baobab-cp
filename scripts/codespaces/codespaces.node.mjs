import assert from "node:assert/strict";
import {test} from "node:test";
import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {externalPackages,failureHint,bootstrap} from "./bootstrap.mjs";
const root=resolve(fileURLToPath(new URL("../..",import.meta.url)));
const config=JSON.parse(readFileSync(resolve(root,".devcontainer/devcontainer.json"),"utf8"));
const manifest=JSON.parse(readFileSync(resolve(root,"package.json"),"utf8"));
const frontend=JSON.parse(readFileSync(resolve(root,"frontend/package.json"),"utf8"));
test("private image pinned to publisher v1.4.4 digest and prebuild contains no registry auth",()=>{
  assert.match(config.image,/^ghcr\.io\/baobab-platform\/baobab-dev:1\.4\.4(?:-frontend)?@sha256:[a-f0-9]{64}$/);
  assert.equal(config.remoteUser,"vscode");assert.equal(config.waitFor,"updateContentCommand");
  assert.match(config.updateContentCommand,/node --test/);
  assert.doesNotMatch(config.updateContentCommand,/install|private-npm|bootstrap/);
  assert.match(config.postCreateCommand,/bootstrap\.mjs/);
  assert.equal(config.portsAttributes["3000"]?.visibility,"private");
  assert.equal(config.otherPortsAttributes.visibility,"private");
  assert.equal(config.containerEnv.GITHUB_TOKEN,undefined);
  assert.equal(config.containerEnv.BAOBAB_PACKAGES_READ_TOKEN,undefined);
  assert.deepEqual(Object.keys(config.customizations.codespaces.repositories).sort(),["baobab-platform/baobab-dev","baobab-platform/shared"]);
  for(const x of Object.values(config.customizations.codespaces.repositories))assert.deepEqual(x.permissions,{contents:"read",packages:"read"});
});
test("all off-workspace organisation versions immutable",()=>{
  const packages=externalPackages([manifest,frontend]);
  assert.ok(packages.length>=2);
  assert.ok(packages.every(([n,v])=>n.startsWith("@baobab-platform/") && /^\d+\.\d+\.\d+/.test(v)));
  assert.throws(()=>externalPackages([{name:"foo",dependencies:{"@baobab-platform/x":"^0.3.1"}}]),/non-immutable/);
  assert.deepEqual(externalPackages([{name:"@baobab-platform/x",dependencies:{"@baobab-platform/x":"workspace:^"}}]),[]);
});
test("registry errors redacted",()=>{
  for(const [input,expected] of [["ERR_PNPM_FETCH_403 hidden-CANARY","403"],["E401 hidden-CANARY","401"],["HTTP 404 hidden-CANARY","404"],["ENOTFOUND hidden-CANARY","network"]]){
    assert.ok(failureHint(input).includes(expected));assert.ok(!failureHint(input).includes("CANARY"));
  }
});
test("refuse tokenless external package or nonmatching runtime before install",()=>{
  const manifests=[manifest,frontend];let calls=0;
  const options={manifests,runtime:"24.21.0",run:()=>{calls++;return {status:0,stdout:"11.24.0"};},log:()=>{},error:()=>{}};
  const prev=process.env.GITHUB_TOKEN,secret=process.env.BAOBAB_PACKAGES_READ_TOKEN;
  delete process.env.GITHUB_TOKEN;delete process.env.BAOBAB_PACKAGES_READ_TOKEN;
  try{assert.equal(bootstrap(options),2);assert.equal(calls,0);assert.equal(bootstrap({...options,runtime:"22.10.0"}),2);}finally{
    if(prev===undefined)delete process.env.GITHUB_TOKEN;else process.env.GITHUB_TOKEN=prev;
    if(secret===undefined)delete process.env.BAOBAB_PACKAGES_READ_TOKEN;else process.env.BAOBAB_PACKAGES_READ_TOKEN=secret;
  }
});

test("CP Go and frontend profile keeps independent private ports",()=>{assert.deepEqual(config.forwardPorts,[8080,3000]);assert.equal(config.portsAttributes["8080"].visibility,"private");assert.equal(config.features["ghcr.io/devcontainers/features/go:1"].version,"1.27");assert.equal(config.features["ghcr.io/devcontainers/features/docker-outside-of-docker:1"].moby,false);});
