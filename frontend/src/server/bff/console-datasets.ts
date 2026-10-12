import "server-only";

import { authConfigSource,serverEnv } from "@/server/env";

export function datasetConsoleEnabled(): boolean {
 return authConfigSource().CX05_DATASET_CONSOLE_ENABLED==="true" &&
   ["development","integration","staging"].includes(serverEnv().CONSOLE_ENVIRONMENT);
}
export function datasetEndpoint(path:string):URL {
 if(!/^\/v1\/admin\/datasets(?:\/(?:preview|[0-9a-f]{64}(?:\/(?:approve|execute|download))?))?$/.test(path))
   throw new Error("UNAPPROVED_DATASET_PATH");
 const base=new URL(serverEnv().CP_API_BASE_URL);
 // URL(string, origin) treats //host as a new authority, so forbid
 // ambiguous path bases before any request carries a bearer token.
 if(base.username||base.password||base.search||base.hash||
   base.pathname.startsWith("//")||base.pathname.includes("\\")||
   (base.protocol!=="https:" && !(serverEnv().CONSOLE_ENVIRONMENT==="development"&&base.hostname==="localhost")))throw new Error("INVALID_CP_ORIGIN");
 const target=new URL(base.pathname.replace(/\/$/,"")+path,base.origin);
 if(target.origin!==base.origin)throw new Error("INVALID_CP_ORIGIN");
 return target;
}
export async function datasetCPRequest(path:string,accessToken:string,method:"GET"|"POST",body?:unknown,key?:string):Promise<Response>{
 if(!datasetConsoleEnabled()||!accessToken||accessToken.length>16384)throw new Error("DATASET_API_DISABLED");
 return fetch(datasetEndpoint(path),{method,redirect:"error",cache:"no-store",signal:AbortSignal.timeout(15_000),
  headers:{Authorization:"Bearer "+accessToken,Accept:method==="GET"?"text/csv, application/json":"application/json",
   "Cache-Control":"no-store",...(method==="POST"?{"Content-Type":"application/json",...(key?{"Idempotency-Key":key}:{})}:{})},
  ...(body!==undefined?{body:JSON.stringify(body)}:{})});
}
