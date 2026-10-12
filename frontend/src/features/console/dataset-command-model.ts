import { z } from "zod";

export const orgID = z.string().uuid();
export const digest = z.string().regex(/^[0-9a-f]{64}$/);
const change = z.strictObject({
  organisation_id: orgID,
  expected_version: z.number().int().min(1),
  internal_label: z.string().trim().min(1).max(120).refine(s=>!/[\r\n\x00]/.test(s)),
});
const kind = z.enum(["REDACTED_CSV_EXPORT","BULK_METADATA_PROPOSAL"]);
const targetIds = z.array(orgID).min(1).max(1000).refine(
  items => new Set(items).size===items.length, "duplicate organisation targets");
export const preview = z.strictObject({
  action: z.literal("preview"), kind, target_ids: targetIds,
  changes: z.array(change).max(50).default([]),
}).superRefine((v,ctx) => {
  if(v.kind==="BULK_METADATA_PROPOSAL" && (v.target_ids.length>50 ||
     v.changes.length!==v.target_ids.length || new Set(v.changes.map(x=>x.organisation_id)).size!==v.target_ids.length ||
     !v.changes.every(x=>v.target_ids.includes(x.organisation_id)))) {
    ctx.addIssue({code:"custom",message:"Each bulk target requires one versioned change"});
  }
  if(v.kind==="REDACTED_CSV_EXPORT" && v.changes.length)ctx.addIssue({code:"custom",message:"Export cannot mutate"});
});
export const propose = z.strictObject({
  action: z.literal("propose"),kind,target_ids:targetIds,changes:z.array(change).max(50).default([]),
  source_fingerprint:digest,purpose:z.string().trim().min(1).max(250),
  recipient_reference:z.string().trim().min(1).max(128),
  valid_until:z.iso.datetime({offset:true}),idempotency_key:z.string().min(16).max(128),
}).superRefine((v,ctx)=>{
 if(v.kind==="BULK_METADATA_PROPOSAL" &&
   (v.target_ids.length>50 || v.changes.length!==v.target_ids.length ||
    new Set(v.changes.map(x=>x.organisation_id)).size!==v.target_ids.length ||
    !v.changes.every(x=>v.target_ids.includes(x.organisation_id))))ctx.addIssue({code:"custom",message:"Missing or extra bulk target"});
 if(v.kind==="REDACTED_CSV_EXPORT"&&v.changes.length)ctx.addIssue({code:"custom",message:"Export must be read only"});
 if(Date.parse(v.valid_until)<=Date.now()||Date.parse(v.valid_until)>Date.now()+86_400_000)ctx.addIssue({code:"custom",message:"Expiry must be within 24h"});
});
export const approve = z.strictObject({
 action:z.literal("approve"),digest,evidence_reference:z.string().trim().min(1).max(200),
 idempotency_key:z.string().min(16).max(128),
});
export const execute = z.strictObject({
 action:z.literal("execute"),digest,idempotency_key:z.string().min(16).max(128),
});
export const writeSchema = z.union([preview,propose,approve,execute]);
export type DatasetCommand = z.infer<typeof writeSchema>;
export function cpDatasetAction(command:DatasetCommand){
 const {action}=command;
 if(action==="preview")return {path:"/v1/admin/datasets/preview",payload:{kind:command.kind,target_ids:command.target_ids,changes:command.changes}};
 if(action==="propose")return {path:"/v1/admin/datasets",payload:{kind:command.kind,target_ids:command.target_ids,
   changes:command.changes,source_fingerprint:command.source_fingerprint,purpose:command.purpose,
   recipient_reference:command.recipient_reference,valid_until:command.valid_until}};
 if(action==="approve")return {path:"/v1/admin/datasets/"+command.digest+"/approve",payload:{evidence_reference:command.evidence_reference}};
 return {path:"/v1/admin/datasets/"+command.digest+"/execute",payload:undefined};
}
