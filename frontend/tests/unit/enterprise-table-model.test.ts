import {describe,expect,it} from "vitest";
import {parseEnterpriseTableQuery,projectEnterpriseTable,enterpriseTableHref} from "@/features/console/enterprise-table-model";
import type {OrganisationOverviewProjection} from "@/features/console/OrganisationIntegrations";
import type {OrganisationGovernance} from "@/features/console/OrganisationGovernancePanel";

const org="0199d000-1111-7777-8000-111111111111";
const now=Date.parse("2026-10-12T10:00:40Z");
const base={organisation_id:org,evaluated_at:"2026-10-12T10:00:00Z",
  profile:{canonical_entity_id:org,display_name:"Nabhold",status:"ACTIVE",source_authority:"CP",verification_state:"PENDING_REVIEW"},
  legal_entities:[],corporate_relationships:[],corporate_group_memberships:[],platform_relationships:[],
  tenants:[{tenant_id:"tn_aaa",display_name:"ZuriBeans",mapping_role:"PRIMARY_ORGANISATION",mapping_provenance:"CP",
    residency_region:"af-south-1",isolation_strategy:"row",desired_state:"ACTIVE",observed_state:"PENDING",
    markets_visible:true,subscriptions_visible:false,estates_visible:false,
    market_participations:[{country_code:"UG",registry_market_id:"market_ug",currency_code:"UGX"}],
    subscriptions:[],digital_estates:[]},
    {tenant_id:"tn_bbb",display_name:"Thamani",mapping_role:"PRIMARY_ORGANISATION",mapping_provenance:"CP",
    residency_region:"af-south-1",isolation_strategy:"row",desired_state:"ACTIVE",observed_state:"PENDING",
    markets_visible:false,subscriptions_visible:false,estates_visible:false,
    market_participations:[],subscriptions:[],digital_estates:[]}],
} as OrganisationOverviewProjection;
const gov={organisation_id:org,evaluated_at:"2026-10-12T10:00:10Z",tenants:[
  {tenant_id:"tn_aaa",mandates_visible:true,readiness_visible:true,mandates:[
    {mandate_id:org,tenant_id:"tn_aaa",legal_entity_id:"le_1",status:"PENDING",roles:["SELLER_OF_RECORD"],
      effective_from:"2026-10-10",provenance:"CP",evidence_count:2,verification_reference_present:false}],
    readiness:{tenant_id:"tn_aaa",state:"UNKNOWN",source:"NO_SNAPSHOT",evaluated_at:"2026-10-12",
      overall_ready:false,desired_state_version:0,observed_state_version:0,blocking_reasons:[]}},
]} as OrganisationGovernance;
const query=(domain:string,extra:Record<string,string>={})=>{
  const q=parseEnterpriseTableQuery({domain,...extra});
  if(!q) throw Error("invalid test query");
  return q;
};

describe("CX-04D / CX-05C enterprise table visibility boundaries",()=>{
  it("rejects arbitrary selectors, export and out-of-range pagination",()=>{
    for(const v of [
      {domain:"markets",tenant_id:"tn_other"}, {domain:"markets",principal:"other"},
      {domain:"markets",export:"csv"}, {domain:"markets",page:"-1"},
      {domain:"markets",page:"10000"}, {domain:"markets",sort:"secret"},
      {domain:"readiness",state:["READY","UNKNOWN"]}, {domain:"markets",q:"X".repeat(81)}
    ])expect(parseEnterpriseTableQuery(v)).toBeNull();
    expect(query("tenants").domain).toBe("tenants");
  });
  it("shows only permitted domain rows, never silently expanding subsidiaries",()=>{
    const visible=projectEnterpriseTable(base,null,query("markets"),now);
    expect(visible.state).toBe("available");
    expect(visible.visibleCount).toBe(1);
    expect(visible.suppressedSections).toBe(1);
    expect(visible.items[0]?.subject).toBe("tn_aaa");
    expect(visible.items.map(x=>x.subject)).not.toContain("tn_bbb");
    expect(projectEnterpriseTable(base,null,query("subscriptions"),now).state).toBe("restricted");
  });
  it("keeps separately governed mandate and readiness source unavailable, not empty",()=>{
    expect(projectEnterpriseTable(base,null,query("mandates"),now).state).toBe("unavailable");
    expect(projectEnterpriseTable(base,gov,query("mandates"),now).items[0]?.state).toBe("PENDING");
    const readiness=projectEnterpriseTable(base,gov,query("readiness"),now);
    expect(readiness.items[0]?.state).toBe("UNKNOWN");
    expect(readiness.suppressedSections).toBe(1);
    const swapped={...gov,tenants:[{...gov.tenants[0]!,tenant_id:"tn_foreign"}]};
    expect(projectEnterpriseTable(base,swapped,query("mandates"),now).state).toBe("unavailable");
  });
  it("rejects stale, forged, inconsistent and duplicate data",()=>{
    expect(projectEnterpriseTable(null,gov,query("tenants"),now).state).toBe("unavailable");
    expect(projectEnterpriseTable({...base,evaluated_at:"2026-10-12T09:40:00Z"},gov,query("tenants"),now).state).toBe("unavailable");
    const duplicate={...base,tenants:[base.tenants![0]!,base.tenants![0]!]};
    expect(projectEnterpriseTable(duplicate,gov,query("tenants"),now).state).toBe("unavailable");
  });
  it("uses deterministic sort, exact status and URL-only presentation preferences",()=>{
    const sorted=projectEnterpriseTable(base,null,query("tenants",{sort:"name-desc"}),now);
    expect(sorted.items.map(i=>i.name)).toEqual(["ZuriBeans","Thamani"]);
    expect(projectEnterpriseTable(base,null,query("tenants",{state:"ACTIVE"}),now).visibleCount).toBe(0);
    const url=enterpriseTableHref(org,query("tenants",{q:"Zuri",columns:"compact",page:"2"}));
    expect(url).toContain("/workspace/organisations/"+org+"/records?");
    expect(url).toContain("columns=compact");
    expect(url).not.toContain("token");
  });
});
