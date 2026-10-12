"use client";

import {useState} from "react";
import type {FormEvent} from "react";

type Preview={source_fingerprint:string;rows:{organisation_id:string;revision:number;display_name:string}[]};
type Receipt={digest:string;state:string;kind:string};
const download=(digest:string)=>"/api/workspace/datasets?action=download&digest="+encodeURIComponent(digest);
export function DatasetOperationsWorkbench({organisationId,csrfToken,canExport,canBulk,canApprove}:{
 organisationId:string;csrfToken:string;canExport:boolean;canBulk:boolean;canApprove:boolean;
}){
 const [mode,setMode]=useState<"REDACTED_CSV_EXPORT"|"BULK_METADATA_PROPOSAL">(canExport?"REDACTED_CSV_EXPORT":"BULK_METADATA_PROPOSAL");
 const [purpose,setPurpose]=useState("");
 const [recipient,setRecipient]=useState("");
 const [label,setLabel]=useState("");
 const [evidence,setEvidence]=useState("");
 const [digest,setDigest]=useState("");
 const [preview,setPreview]=useState<Preview|null>(null);
 const [state,setState]=useState("");
 const [busy,setBusy]=useState(false);
 const [message,setMessage]=useState("");
 const [receipt,setReceipt]=useState<Receipt|null>(null);
 async function send(command:Record<string,unknown>){
  setBusy(true);setMessage("");
  try{
   const response=await fetch("/api/workspace/datasets",{method:"POST",credentials:"same-origin",
     headers:{"Content-Type":"application/json","X-CSRF-Token":csrfToken},body:JSON.stringify(command)});
   if(!response.ok){setMessage("Request refused or authoritative sources unavailable ("+response.status+").");return null}
   return await response.json();
  }catch{setMessage("The governed operation could not be completed.");return null}
  finally{setBusy(false)}
 }
 const changes=mode==="BULK_METADATA_PROPOSAL"&&preview?.rows[0]
  ? [{organisation_id:organisationId,expected_version:preview.rows[0].revision,internal_label:label}]:[];
 async function doPreview(event:FormEvent){
  event.preventDefault();
  if(mode==="BULK_METADATA_PROPOSAL"&&!label.trim()){setMessage("Specify a versioned internal label.");return}
  // A bulk-preview requires the current version. First query a read-only export
  // preview to obtain it, then request the governed bulk source fingerprint.
  let result=await send({action:"preview",kind:"REDACTED_CSV_EXPORT",target_ids:[organisationId],changes:[]});
  if(!result)return;
  const ch=mode==="BULK_METADATA_PROPOSAL"
    ? [{organisation_id:organisationId,expected_version:result.rows?.[0]?.revision,internal_label:label}]:[];
  if(mode==="BULK_METADATA_PROPOSAL"){
   result=await send({action:"preview",kind:mode,target_ids:[organisationId],changes:ch});
  }
  if(result){setPreview(result);setReceipt(null);setMessage("Source preview retrieved. Review it before submitting.");}
 }
 async function propose(){
  if(!preview||!purpose.trim()||!recipient.trim()){setMessage("Preview, purpose and recipient reference are required.");return}
  const now=Date.now();
  const result=await send({action:"propose",kind:mode,target_ids:[organisationId],
   ...(mode==="BULK_METADATA_PROPOSAL"?{changes}:{}),
   source_fingerprint:preview.source_fingerprint,purpose,recipient_reference:recipient,
   valid_until:new Date(now+60*60*1000).toISOString(),idempotency_key:crypto.randomUUID()});
  if(result){setDigest(result.digest);setState(result.state);setReceipt(result);setMessage("Proposal created. An independent reviewer must approve before execution.");}
 }
 async function reviewer(action:"approve"|"execute"){
  if(!/^[0-9a-f]{64}$/.test(digest))return setMessage("Paste the immutable proposal digest.");
  const result=await send({action,digest,...(action==="approve"?{evidence_reference:evidence}:{}),
    idempotency_key:crypto.randomUUID()});
  if(result){setReceipt(result);setState(result.state);setMessage("Operation "+result.state+". Current permissions are checked by CP.");}
 }
 const input="w-full rounded-lg border border-border bg-card p-3 text-sm";
 return <section aria-label="Governed dataset operations" className="space-y-4 rounded-2xl border border-border bg-card p-6">
  <h2 className="text-xl font-bold">Governed export and bulk operations</h2>
  <p className="text-sm text-muted-foreground">Every operation is subject to independently evaluated CP grants, source revisions, immutable proposal digests and separate human approval. This workbench is enabled only in approved nonproduction environments.</p>
  {(canExport||canBulk)&&<form onSubmit={doPreview} className="space-y-3">
   <label className="block text-sm font-semibold">Operation
    <select className={input} value={mode} onChange={e=>{setMode(e.target.value as typeof mode);setPreview(null)}}>
     {canExport&&<option value="REDACTED_CSV_EXPORT">Redacted CSV export</option>}
     {canBulk&&<option value="BULK_METADATA_PROPOSAL">Versioned internal label update</option>}
    </select>
   </label>
   {mode==="BULK_METADATA_PROPOSAL"&&<label className="block text-sm font-semibold">Internal label<input className={input} value={label} maxLength={120} onChange={e=>setLabel(e.target.value)}/></label>}
   <label className="block text-sm font-semibold">Purpose<input className={input} value={purpose} maxLength={250} onChange={e=>setPurpose(e.target.value)} required/></label>
   <label className="block text-sm font-semibold">Recipient/case reference<input className={input} value={recipient} maxLength={128} onChange={e=>setRecipient(e.target.value)} required/></label>
   <button type="submit" disabled={busy} className="rounded-lg bg-primary px-4 py-3 font-semibold text-primary-foreground">Preview current source</button>
   {preview&&<p className="text-sm">Preview: {preview.rows.map(r=>r.display_name).join(", ")} · {preview.rows.length} authorised organisation(s)</p>}
   {preview&&<button type="button" disabled={busy} onClick={propose} className="ml-3 rounded-lg border border-border px-4 py-3 font-semibold">Submit governed proposal</button>}
  </form>}
  <div className="space-y-3 border-t border-border pt-4">
   <label className="block text-sm font-semibold">Immutable proposal digest<input className={input} value={digest} onChange={e=>setDigest(e.target.value)} maxLength={64} placeholder="64-character digest"/></label>
   {canApprove&&<><label className="block text-sm font-semibold">Independent approval evidence reference<input className={input} value={evidence} onChange={e=>setEvidence(e.target.value)} maxLength={200}/></label>
    <button disabled={busy||!evidence} onClick={()=>reviewer("approve")} className="rounded-lg border border-border px-4 py-3 font-semibold">Approve as independent reviewer</button></>}
   {(canExport||canBulk||canApprove)&&<button disabled={busy} onClick={()=>reviewer("execute")} className="ml-3 rounded-lg border border-border px-4 py-3 font-semibold">Execute approved operation</button>}
   {receipt?.kind==="REDACTED_CSV_EXPORT"&&state==="COMPLETED"&&<a href={download(digest)} className="inline-flex min-h-11 items-center px-4 text-primary underline">Download authorised redacted CSV</a>}
   {message&&<p role="status" className="text-sm">{message}</p>}
  </div>
 </section>
}
