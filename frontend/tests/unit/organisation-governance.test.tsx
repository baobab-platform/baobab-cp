// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { OrganisationGovernancePanel, parseOrganisationGovernance, reconcileOrganisationGovernance } from "@/features/console/OrganisationGovernancePanel";
import { resolveProgressiveCommand } from "@/server/bff/progressive";
import { expectAccessible } from "./axe";

const org="0199d000-1111-7777-8000-111111111111";
const tenant="tn_audit_demo";
const allowed={
  organisation_id:org,evaluated_at:"2026-10-12T10:00:00Z",
  tenants:[{
    tenant_id:tenant,mandates_visible:true,readiness_visible:true,
    mandates:[{
      mandate_id:"0199d000-1111-7777-8000-111111111111",tenant_id:tenant,
      legal_entity_id:"le_redacted",status:"PENDING",roles:["SELLER_OF_RECORD"],
      effective_from:"2026-10-12T10:00:00Z",provenance:"independent-review",
      evidence_count:2,verification_reference_present:false,
    }],
    readiness:{tenant_id:tenant,state:"STALE",source:"IMMUTABLE_PROVISIONING_SNAPSHOT",
      evaluated_at:"2026-10-10T10:00:00Z",overall_ready:true,
      desired_state_version:2,observed_state_version:2,blocking_reasons:[]},
  }],
};
describe("CX-04C independent mandate and readiness boundary",()=>{
  it("uses only the server-owned literal organisation source, rejecting selector injection",()=>{
    expect(resolveProgressiveCommand({action:"organisation-governance",id:org})).toEqual({
      method:"GET",path:"/v1/organisations/"+org+"/governance",
    });
    for(const attack of [
      {action:"organisation-governance",id:"../someone"},
      {action:"organisation-governance",id:org,tenantId:"tn_sibling"},
      {action:"organisation-governance",id:org,body:{include_all:true}},
      {action:"organisation-governance",id:org,idempotencyKey:"test"},
    ])expect(()=>resolveProgressiveCommand(attack as never)).toThrow();
  });
  it("refuses wrong organisations, foreign mandates and unpermitted data",()=>{
    expect(parseOrganisationGovernance(allowed,org)).not.toBeNull();
    expect(parseOrganisationGovernance(allowed,"0199d000-1111-7777-8000-222222222222")).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!,mandates_visible:false,
    }]},org)).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!,readiness_visible:false,
    }]},org)).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!,mandates:[{...allowed.tenants[0]!.mandates[0]!,tenant_id:"tn_sibling"}],
    }]},org)).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!,readiness:{...allowed.tenants[0]!.readiness,source:"NO_SNAPSHOT",state:"READY"},
    }]},org)).toBeNull();
  });
  it("rejects duplicate tenant or mandate sources and contradictory READY claims", () => {
    expect(parseOrganisationGovernance({...allowed, tenants:[allowed.tenants[0],allowed.tenants[0]]}, org)).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!, mandates: [
        allowed.tenants[0]!.mandates[0]!,allowed.tenants[0]!.mandates[0]!,
      ],
    }]},org)).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!, readiness: {
        ...allowed.tenants[0]!.readiness, state:"READY", overall_ready:false,
      },
    }]},org)).toBeNull();
    expect(parseOrganisationGovernance({...allowed,tenants:[{
      ...allowed.tenants[0]!, readiness: {
        ...allowed.tenants[0]!.readiness, state:"UNKNOWN",
      },
    }]},org)).toBeNull();
  });

  it("reconciles fresh governance only to the independently visible PRIMARY tenant set", () => {
    const now = Date.parse("2026-10-12T10:00:30Z");
    const projected = parseOrganisationGovernance(allowed, org);
    expect(reconcileOrganisationGovernance(projected,[tenant],"2026-10-12T10:00:05Z",now)).not.toBeNull();
    expect(reconcileOrganisationGovernance(projected,[],"2026-10-12T10:00:05Z",now)).toBeNull();
    expect(reconcileOrganisationGovernance(projected,["tn_sibling"],"2026-10-12T10:00:05Z",now)).toBeNull();
    expect(reconcileOrganisationGovernance(projected,[tenant,tenant],"2026-10-12T10:00:05Z",now)).toBeNull();
    expect(reconcileOrganisationGovernance(projected,null,"2026-10-12T10:00:05Z",now)).toBeNull();
    expect(reconcileOrganisationGovernance(projected,[tenant],"2026-10-12T09:45:00Z",now)).toBeNull();
    expect(reconcileOrganisationGovernance(projected,[tenant],"2026-10-12T10:00:05Z",now + 600_000)).toBeNull();
  });

  it("renders stale evidence and pending, never invented provider certification",async()=>{
    const {container}=render(<OrganisationGovernancePanel record={parseOrganisationGovernance(allowed,org)}/>);
    expect(screen.getByText("STALE")).toBeTruthy();
    expect(screen.getByText("PENDING")).toBeTruthy();
    expect(container.textContent).toContain("Provisioning snapshots are immutable observations");
    await expectAccessible(container);
  });
  it("renders clear denied and absent states",async()=>{
    const {container}=render(<OrganisationGovernancePanel record={null}/>);
    expect(screen.getByRole("status").textContent).toContain("unavailable");
    await expectAccessible(container);
  });
});
