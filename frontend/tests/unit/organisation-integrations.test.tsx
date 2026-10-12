// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { parseOrganisationOverview, OrganisationIntegrations } from "@/features/console/OrganisationIntegrations";
import { resolveProgressiveCommand } from "@/server/bff/progressive";
import { expectAccessible } from "./axe";

const id = "0199d000-1111-7777-8000-111111111111";
const entity = {
  organisation_id: id, evaluated_at: "2026-10-12T10:00:00Z",
  profile: {
    canonical_entity_id: id, display_name: "Example organisation",
    verification_state: "UNVERIFIED", source_authority: "business-onboarding", status: "ACTIVE",
  },
  legal_entities: [{
    legal_entity_id: "le_001", legal_name: "Example Legal Ltd", legal_status: "ACTIVE",
    verification_state: "PENDING_REVIEW", source_authority: "documentary-claim", evidence_count: 0,
  }],
  corporate_relationships: [{
    id: "cor_001", source_organisation_id: id, target_organisation_id: "other",
    relationship_type: "OWNS", verification_state: "UNVERIFIED",
    status: "PENDING", direct_or_derived: "DIRECT", source_authority: "applicant",
    effective_from: "2026-10-12T10:00:00Z",
  }],
  corporate_group_memberships: [], platform_relationships: [],
};

describe("CX-04 source-authoritative organisation integration", () => {
  it("routes the overview through an exact server-owned GET path", () => {
    expect(resolveProgressiveCommand({ action: "organisation-overview", id })).toEqual({
      method: "GET", path: "/v1/organisations/" + id + "/overview",
    });
    for (const attack of [
      { action: "organisation-overview", id: "../other" },
      { action: "organisation-overview", id, tenantId: "tn_other" },
      { action: "organisation-overview", id, body: { legal_status: "VERIFIED" } },
      { action: "organisation-overview", id, idempotencyKey: "unsafe" },
    ]) expect(() => resolveProgressiveCommand(attack as never)).toThrow();
  });
  it("refuses substituted cross-organisation projections", () => {
    expect(parseOrganisationOverview(entity, id)).not.toBeNull();
    expect(parseOrganisationOverview(entity, "foreign")).toBeNull();
    expect(parseOrganisationOverview({ ...entity, profile: { ...entity.profile, canonical_entity_id: "other" } }, id)).toBeNull();
    expect(parseOrganisationOverview({
      ...entity, corporate_relationships: [{ ...entity.corporate_relationships[0],
        source_organisation_id: "other", target_organisation_id: "another" }],
    }, id)).toBeNull();
  });
  it("displays unverified, pending and unavailable explicitly, never green as a default", async () => {
    const { container } = render(<OrganisationIntegrations record={parseOrganisationOverview(entity, id)} available />);
    expect(screen.getByText("PENDING REVIEW")).toBeTruthy();
    expect(screen.getByText("UNVERIFIED")).toBeTruthy();
    expect(screen.queryByText("VERIFIED")).toBeNull();
    expect(container.textContent).toContain("Source: documentary-claim");
    await expectAccessible(container);
    const { container: denied } = render(<OrganisationIntegrations record={null} available={false} />);
    expect(denied.textContent).toContain("unavailable or access has been denied");
    await expectAccessible(denied);
  });
});
