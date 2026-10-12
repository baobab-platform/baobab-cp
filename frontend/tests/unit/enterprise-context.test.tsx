// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { OrganisationTenantIntegrations } from "@/features/console/OrganisationTenantIntegrations";
import {
  enterpriseContextHref, projectEnterpriseNavigationContext,
} from "@/features/console/enterprise-context-model";
import { expectAccessible } from "./axe";

const now = Date.parse("2026-10-12T11:00:00Z");
const org = "0199d000-1111-7777-8000-111111111111";
const tenant = "tn_abc123";
const tenantRecord = {
  tenant_id: tenant, legal_entity_id: "le_valid", display_name: "Thamani",
  isolation_strategy: "row_level_security", residency_region: "af-south-1",
  desired_state: "active", observed_state: "pending", revision: 2,
};
const mapping = {
  tenant_id: tenant, display_name: "Thamani", mapping_role: "PRIMARY_ORGANISATION",
  mapping_provenance: "CP registry", residency_region: "af-south-1",
  isolation_strategy: "row_level_security", desired_state: "active", observed_state: "pending",
  markets_visible: true, subscriptions_visible: false, estates_visible: false,
  market_participations: [
    { country_code: "ZA", registry_market_id: "market_za", currency_code: "ZAR" },
    { country_code: "UG", registry_market_id: "market_ug", currency_code: "UGX" },
  ], subscriptions: [], digital_estates: [],
};
const overview = {
  organisation_id: org, evaluated_at: "2026-10-12T10:59:30Z",
  profile: { canonical_entity_id: org, display_name: "Nabhold", verification_state: "PENDING_REVIEW", source_authority: "CP", status: "ACTIVE" },
  legal_entities: [], corporate_relationships: [], corporate_group_memberships: [],
  platform_relationships: [], tenants: [mapping],
};
const project = (source: unknown = overview, record: unknown = tenantRecord,
  market: string | undefined = "ZA") =>
  projectEnterpriseNavigationContext(org, tenant, market, source, record, now);

describe("CX-03B caller-bound enterprise navigation scope", () => {
  it("accepts only an independently authorised current PRIMARY mapping and explicit CP market", () => {
    const context = project();
    expect(context?.selectedMarket?.marketId).toBe("market_za");
    expect(context?.tenantName).toBe("Thamani");
    expect(project(overview, tenantRecord, undefined)?.selectedMarket).toBeUndefined();
    expect(enterpriseContextHref(org, tenant, "ZA"))
      .toBe("/workspace/organisations/" + org + "/contexts/" + tenant + "?market=ZA");
  });

  it("fails closed for foreign, duplicate, nonprimary and missing associations", () => {
    for (const mappings of [
      [], [{ ...mapping, tenant_id: "tn_sibling" }],
      [{ ...mapping, mapping_role: "SECONDARY_ORGANISATION" }],
      [mapping, mapping],
    ]) {
      expect(project({ ...overview, tenants: mappings })).toBeNull();
    }
    expect(project({ ...overview, organisation_id: "0199d000-1111-7777-8000-222222222222" })).toBeNull();
    expect(project(overview, { ...tenantRecord, tenant_id: "tn_sibling" })).toBeNull();
  });

  it("never selects an ungranted, ambiguous, foreign or injected market", () => {
    expect(project(overview, tenantRecord, "KE")).toBeNull();
    expect(project(overview, tenantRecord, "../ZA")).toBeNull();
    expect(project({ ...overview, tenants: [{ ...mapping, markets_visible: false, market_participations: [] }] })).toBeNull();
    expect(project({ ...overview, tenants: [{
      ...mapping, market_participations: [...mapping.market_participations,
        { country_code: "ZA", registry_market_id: "other", currency_code: "ZAR" }],
    }] })).toBeNull();
    expect(project({ ...overview, tenants: [{
      ...mapping, markets_visible: false, market_participations: mapping.market_participations,
    }] })).toBeNull();
  });

  it("invalidates stale evidence and suspended or revoked identities without persisting a context", () => {
    expect(project({ ...overview, evaluated_at: "2026-10-12T10:40:00Z" })).toBeNull();
    expect(project({ ...overview, evaluated_at: "2026-10-12T11:10:00Z" })).toBeNull();
    expect(project({ ...overview, profile: { ...overview.profile, status: "SUSPENDED" } })).toBeNull();
    expect(project(overview, { ...tenantRecord, observed_state: "revoked" })).toBeNull();
    expect(project(overview, { ...tenantRecord, desired_state: "suspended" })).toBeNull();
    expect(project(null)).toBeNull();
  });

  it("offers governed navigation only for CP-returned tenant rows, with accessible links", async () => {
    const { container } = render(<OrganisationTenantIntegrations tenants={[mapping]} organisationId={org} />);
    const link = screen.getByRole("link", { name: /open governed context/i });
    expect(link.getAttribute("href")).toBe(enterpriseContextHref(org, tenant));
    await expectAccessible(container);
  });
});
