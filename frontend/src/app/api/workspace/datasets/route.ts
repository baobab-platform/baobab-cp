import { NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE,confirmUnsafeRequest,createAuthStore,currentSession,readAuthSettings,requireExactOrigin } from "@/server/auth/runtime";
import {boundedBody} from "@/server/bff/progressive";
import {datasetCPRequest,datasetConsoleEnabled} from "@/server/bff/console-datasets";
import {writeSchema,cpDatasetAction,digest} from "@/features/console/dataset-command-model";

export const runtime="nodejs";
export const dynamic="force-dynamic";
const denied=(status=404)=>NextResponse.json({code:"DATASET_UNAVAILABLE"},{status,headers:{"Cache-Control":"no-store"}});
export async function POST(request:NextRequest){
 try{
  if(!datasetConsoleEnabled())return denied();
  const settings=readAuthSettings();
  requireExactOrigin(request,settings.origin);
  const session=await currentSession(createAuthStore(settings),request.cookies.get(SESSION_COOKIE)?.value,settings);
  confirmUnsafeRequest(request,session.csrfToken,settings.origin);
  if(!request.headers.get("content-type")?.startsWith("application/json"))return denied(415);
  const command=writeSchema.safeParse(await boundedBody(request,65_536));
  if(!command.success)return denied(400);
  const {path,payload}=cpDatasetAction(command.data);
  const key="idempotency_key" in command.data?command.data.idempotency_key:undefined;
  const cp=await datasetCPRequest(path,session.accessToken,"POST",payload,key);
  if(![200,201].includes(cp.status))return denied([400,401,403,404,409,422].includes(cp.status)?cp.status:503);
  const response=await cp.json();
  return NextResponse.json(response,{status:cp.status,headers:{"Cache-Control":"private, no-store"}});
 }catch{return denied(503)}
}
export async function GET(request:NextRequest){
 try{
  if(!datasetConsoleEnabled())return denied();
  const settings=readAuthSettings();
  requireExactOrigin(request,settings.origin);
  const session=await currentSession(createAuthStore(settings),request.cookies.get(SESSION_COOKIE)?.value,settings);
  const params=request.nextUrl.searchParams;
  if([...params.keys()].some(k=>!["action","digest"].includes(k))||
     params.getAll("action").length!==1||params.getAll("digest").length!==1 ||
     !digest.safeParse(params.get("digest")).success)return denied(400);
  const action=params.get("action");
  if(action!=="status"&&action!=="download")return denied(400);
  const cp=await datasetCPRequest("/v1/admin/datasets/"+params.get("digest")+(action==="download"?"/download":""),session.accessToken,"GET");
  if(cp.status!==200)return denied([400,401,403,404,409].includes(cp.status)?cp.status:503);
  if(action==="download"){
   if(!cp.headers.get("content-type")?.startsWith("text/csv"))return denied(503);
   const bytes=await cp.arrayBuffer();
   if(bytes.byteLength===0||bytes.byteLength>2_000_000)return denied(503);
   return new NextResponse(bytes,{status:200,headers:{"Content-Type":"text/csv; charset=utf-8",
     "Content-Disposition":"attachment; filename=\"redacted-organisations.csv\"",
     "Cache-Control":"private, no-store","X-Content-Type-Options":"nosniff"}});
  }
  return NextResponse.json(await cp.json(),{status:200,headers:{"Cache-Control":"private, no-store"}});
 }catch{return denied(503)}
}
