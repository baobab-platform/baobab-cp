#!/usr/bin/env node
/**
 * Post-create ONLY: verifies published read-only organization package access,
 * then uses the exact committed pnpm lockfile. No secrets in argv, source,
 * Dockerfile, .env, logs or npmrc; no fallback to public npm for private scope.
 */
import { spawnSync } from "node:child_process";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
export function externalPackages(manifests) {
  const own=new Set(manifests.map(m=>m.name));
  const result=new Map();
  for(const m of manifests)for(const [name,version] of Object.entries({...m.dependencies,...m.devDependencies,...m.optionalDependencies})){
    if(!name.startsWith("@baobab-platform/") || (version.startsWith("workspace:") && own.has(name)))continue;
    if(!/^@baobab-platform\/[a-z0-9._-]+$/.test(name) || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version))throw Error("non-immutable private package declaration: "+name);
    if(result.has(name) && result.get(name)!==version)throw Error("private package version conflict: "+name);
    result.set(name,version);
  }
  return [...result.entries()].sort((a,b)=>a[0].localeCompare(b[0]));
}
export function failureHint(raw) {
  const s=String(raw);
  if(/EAI_AGAIN|ENOTFOUND|ECONNRESET|ETIMEDOUT|CERT_/i.test(s)) return "network, DNS or verified TLS failure";
  if(/(?:^|\D)401(?:\D|$)|E401|NO AUTHORIZATION HEADER/i.test(s))return "401: missing or rejected package credential";
  if(/(?:^|\D)403(?:\D|$)|E403/i.test(s))return "403: token lacks package or organization SSO access";
  if(/(?:^|\D)404(?:\D|$)|E404/i.test(s))return "404: package/version absent or private access denied";
  return "unclassified registry response (inspect trusted registry and package permissions)";
}
const exec=(bin,args,timeout=60000)=>spawnSync(bin,args,{cwd:root,env:{...process.env,CI:"true",COREPACK_ENABLE_DOWNLOAD_PROMPT:"0"},timeout,encoding:"utf8",maxBuffer:131072});
export function bootstrap({run=exec, manifests=null, runtime=process.versions.node,log=console.log,error=console.error}={}){
  try{
    const rootManifest=JSON.parse(readFileSync(join(root,"package.json"),"utf8"));
    if(rootManifest.packageManager!=="pnpm@11.24.0")throw Error("required pnpm 11.24.0 lock");
    if(!/^24\.(?:19|2\d|[3-9]\d)\./.test(runtime))throw Error("Node 24.19+ runtime required");
    let source=manifests;
    if(source===null){
      source=[rootManifest];
      const frontend=join(root,"frontend/package.json");
      if(existsSync(frontend))source.push(JSON.parse(readFileSync(frontend,"utf8")));
      const pkgs=join(root,"packages");
      if(existsSync(pkgs))for(const d of readdirSync(pkgs)){const file=join(pkgs,d,"package.json");if(existsSync(file))source.push(JSON.parse(readFileSync(file,"utf8")));}
    }
    const packages=externalPackages(source);
    if(packages.length && !(process.env.BAOBAB_PACKAGES_READ_TOKEN || process.env.GITHUB_TOKEN))throw Error("Codespaces token missing; use read-only BAOBAB_PACKAGES_READ_TOKEN or repository-approved GITHUB_TOKEN");
    const a=run(process.execPath,[join(root,"scripts/codespaces/private-npm.mjs")]);
    if(a.error || a.status!==0)throw Error("private npm user-level configuration denied");
    const v=run("pnpm",["--version"]);
    if(v.error || v.status!==0 || v.stdout?.trim()!=="11.24.0")throw Error("incorrect pnpm toolchain");
    for(const [name,version] of packages){
      const probe=run("pnpm",["view",name+"@"+version,"version","--registry","https://npm.pkg.github.com"]);
      if(probe.error || probe.status!==0 || probe.stdout?.trim().replace(/^"|"$/g,"")!==version){
        throw Error(name+"@"+version+" — "+failureHint(probe.stderr||probe.error?.code));
      }
      log("Private package read verified: "+name+"@"+version);
    }
    const install=run("pnpm",["install","--frozen-lockfile"],15*60*1000);
    if(install.error || install.status!==0)throw Error("frozen dependency installation failed: "+failureHint(install.stderr));
    log("Pinned Codespaces dependencies installed successfully.");
    return 0;
  }catch(e){error("Codespaces bootstrap DENIED: "+e.message);return 2;}
}
if(process.argv[1] && resolve(process.argv[1])===fileURLToPath(import.meta.url))process.exitCode=bootstrap();
