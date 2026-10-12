import {describe,expect,it} from "vitest";
import {preview,propose,cpDatasetAction} from "@/features/console/dataset-command-model";
const org="0199d000-1111-7777-8000-111111111111";
describe("CX-05L fixed dataset BFF boundaries",()=>{
 it("refuses untrusted principal and arbitrary request paths",()=>{
  expect(preview.safeParse({action:"preview",kind:"REDACTED_CSV_EXPORT",target_ids:[org],principal_id:"other"}).success).toBe(false);
  expect(preview.safeParse({action:"preview",kind:"REDACTED_CSV_EXPORT",target_ids:[org,org]}).success).toBe(false);
  expect(preview.safeParse({action:"preview",kind:"BULK_METADATA_PROPOSAL",target_ids:[org],changes:[]}).success).toBe(true); // version-only bulk preview needs no export grant
 });
 it("only emits fixed CP paths and allowlisted fields",()=>{
  const p=preview.parse({action:"preview",kind:"REDACTED_CSV_EXPORT",target_ids:[org]});
  expect(cpDatasetAction(p).path).toBe("/v1/admin/datasets/preview");
  expect(Object.keys(cpDatasetAction(p).payload!)).toEqual(["kind","target_ids","changes"]);
  expect(propose.safeParse({action:"propose",kind:"REDACTED_CSV_EXPORT",target_ids:[org],purpose:"review",
   recipient_reference:"case",source_fingerprint:"a".repeat(64),valid_until:new Date(Date.now()+60_000).toISOString(),
   idempotency_key:"idempotency-request-unique"}).success).toBe(true);
 });
});
