#!/usr/bin/env node
// CX-05F: real, fail-closed, read-only staging acceptance.
// No mocks, no token/PII in artifacts, no database writes.
import { createHash } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";

function required(name) {
  const value=process.env[name]?.trim();
  if (!value || /<[^>]+>|example\.com|localhost|127\.0\.0\.1/.test(value)) {
    throw new Error("missing or non-live protected setting: "+name);
  }
  return value;
}
function requireCondition(ok, code) {if (!ok) throw new Error(code);}
const problems=[];
const evidence={kind:"CX05F_PROTECTED_STAGING",revision:process.env.GITHUB_SHA??"local",
  executed_at:new Date().toISOString(),result:"BLOCKED",checks:[],production_eligible:false};
function verified(name, ok, detail) {
  evidence.checks.push({name,result:ok?"PASS":"FAIL",...detail});
  if (!ok) problems.push(name);
}
async function run() {
  const base=required("CX05_STAGING_CP_URL");
  const u=new URL(base);
  requireCondition(u.protocol==="https:" && !u.username && !u.password && !u.search && !u.hash,"INVALID_TLS_CP_ORIGIN");
  const allowed=required("CX05_STAGING_GRANTED_TOKEN");
  const denied=required("CX05_STAGING_DENIED_TOKEN");
  const revoked=required("CX05_STAGING_REVOKED_TOKEN");
  const foreignOrg=required("CX05_STAGING_FOREIGN_ORG_UUID");
  const accessibleOrg=required("CX05_STAGING_OWN_ORG_UUID");
  requireCondition(/^[0-9a-f-]{36}$/i.test(foreignOrg) && /^[0-9a-f-]{36}$/i.test(accessibleOrg) && foreignOrg!==accessibleOrg,"INVALID_STAGING_ORGANISATIONS");
  requireCondition(new Set([allowed,denied,revoked]).size===3,"TEST_IDENTITIES_NOT_INDEPENDENT");
  const min=Number(process.env.CX05_STAGING_MIN_VISIBLE??5001);
  requireCondition(Number.isSafeInteger(min) && min>500 && min<=20000,"INVALID_SCALE_THRESHOLD");
  let calls=0;
  async function get(path, token) {
    const url=new URL(path,u);
    requireCondition(url.origin===u.origin && url.protocol==="https:","CROSS_ORIGIN_TEST_REQUEST");
    const controller=new AbortController();
    const timeout=setTimeout(()=>controller.abort(),12000);
    try {
      const r=await fetch(url,{method:"GET",redirect:"error",
        headers:{Authorization:"Bearer "+token,Accept:"application/json","Cache-Control":"no-store"},
        signal:controller.signal,cache:"no-store"});
      calls++;
      const body=(await r.text()).slice(0,1024*256);
      let json=null;try{json=JSON.parse(body)}catch{}
      return {status:r.status,contentType:r.headers.get("content-type")??"",json};
    } finally {clearTimeout(timeout)}
  }
  const page=await get("/v1/organisations/directory/page?limit=50&sort=name-asc",allowed);
  const validPage=page.status===200 && Array.isArray(page.json?.items) &&
    Number.isInteger(page.json.total_visible) && page.json.total_visible>=min &&
    page.json.items.length>0 && page.json.items.length<=50;
  verified("LIVE_HIGH_VOLUME_EXACT_GRANTS",validPage,{visible_count:page.json?.total_visible??null});
  if (!validPage) return;
  const observed=new Set(page.json.items.map(x=>x.organisation_id));
  const cursor=page.json.next_after;
  const next=cursor?await get("/v1/organisations/directory/page?sort=name-asc&limit=50&after="+encodeURIComponent(cursor),allowed):null;
  verified("KEYSET_PAGINATION_NO_DUPLICATES",Boolean(next&&next.status===200&&next.json?.items?.length &&
    next.json.items.every(x=>!observed.has(x.organisation_id))&&next.json.total_visible===page.json.total_visible));
  verified("NO_FOREIGN_ORGANISATION_LEAK",!observed.has(foreignOrg)&&
    !next?.json?.items?.some(x=>x.organisation_id===foreignOrg));
  const foreign=await get("/v1/organisations/"+encodeURIComponent(foreignOrg)+"/overview",allowed);
  verified("FOREIGN_DETAIL_DENIED",[403,404].includes(foreign.status));
  const own=await get("/v1/organisations/"+encodeURIComponent(accessibleOrg)+"/overview",allowed);
  verified("OWN_DETAIL_AUTHORITATIVE",own.status===200 && own.json?.organisation_id===accessibleOrg);
  const noGrant=await get("/v1/organisations/directory/page?limit=50",denied);
  verified("SEPARATE_PRINCIPAL_DENIED_OR_EMPTY",noGrant.status===401||noGrant.status===403||
    noGrant.status===404||(noGrant.status===200&&noGrant.json?.total_visible===0));
  const withdrawn=await get("/v1/organisations/"+encodeURIComponent(accessibleOrg)+"/overview",revoked);
  verified("REVOKED_TOKEN_REJECTED",[401,403,404].includes(withdrawn.status));
  const injection=await get("/v1/organisations/directory/page?principal_id=someone",allowed);
  verified("USER_PRINCIPAL_INJECTION_DENIED",injection.status===400);
  const stale=await get("/v1/organisations/directory/page?limit=50&after="+encodeURIComponent(foreignOrg),allowed);
  verified("FOREIGN_CURSOR_REJECTED",stale.status===409);
  const forged=await get("/v1/organisations/directory/page?limit=5001",allowed);
  verified("QUERY_BUDGET_ENFORCED",forged.status===400);
  evidence.calls=calls;
}
try{
  await run();
  evidence.result=problems.length===0 && evidence.checks.length>=10?"PASS":"BLOCKED";
}catch(err) {
  evidence.result="BLOCKED";
  evidence.checks.push({name:"EXECUTION_PREREQUISITE",result:"FAIL",reason:String(err?.message??err).slice(0,160)});
}
evidence.signed_off=false;
const encoded=JSON.stringify(evidence,null,2)+"\n";
evidence.report_digest=createHash("sha256").update(encoded).digest("hex");
await mkdir("cx05-evidence",{recursive:true});
await writeFile("cx05-evidence/cx05-staging-report.json",JSON.stringify(evidence,null,2)+"\n",{mode:0o600});
console.log("CX05 staging acceptance:",evidence.result,
  "checks",evidence.checks.filter(x=>x.result==="PASS").length+"/"+evidence.checks.length,
  "production eligible: false");
if (evidence.result!=="PASS") process.exitCode=2;
