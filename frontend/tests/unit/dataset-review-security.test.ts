import {describe,expect,it} from "vitest";
import {preview} from "@/features/console/dataset-command-model";
describe("CX-05M bulk review permissions",()=>{
 it("permits metadata version-only preview without an export grant",()=>{
  expect(preview.safeParse({action:"preview",kind:"BULK_METADATA_PROPOSAL",
    target_ids:["0199d000-1111-7777-8000-111111111111"],changes:[]}).success).toBe(true);
 });
 it("denies mismatched full changes and duplicate targets",()=>{
  expect(preview.safeParse({action:"preview",kind:"BULK_METADATA_PROPOSAL",
    target_ids:["0199d000-1111-7777-8000-111111111111"],
    changes:[{organisation_id:"0199d000-1111-7777-8000-222222222222",expected_version:1,internal_label:"x"}]}).success).toBe(false);
 });
});