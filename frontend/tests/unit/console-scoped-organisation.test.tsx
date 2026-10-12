// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ContextSwitcher } from "@/app/workspace/ContextSwitcher";
import { contextHref } from "@/features/console/context-model";
import { parseOrganisation, parseOrganisationIamLinks, parseOrganisationAudit, parseTenant } from "@/features/console/organisation-model";
import { hasExactScopedPermission, matchesScopedContext, scopedContextCandidates } from "@/server/console/scoped-context";
import { resolveProgressiveCommand } from "@/server/bff/progressive";
import { expectAccessible } from "./axe";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  usePathname: () => "/workspace",
  useRouter: () => ({ push }),
}));

const now = Date.parse("2026-10-12T08:00:30.000Z");
const grant = (permission: string, scope: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({
  grant_id: "agr_000001", permission, scope, grant_type: "STANDING",
  source: "DIRECT", risk_class: "LOW", ...extra,
});
const fixture = (grants: unknown[], principalId = "prn_owner") => ({
  principal_id: principalId, evaluated_at: "2026-10-12T08:00:00.000Z", grants,
});
const org = { kind: "organisation", id: "org_canonical_001", label: "org_canonical_001" } as const;
const tenant = { kind: "tenant", id: "tn_abc123", label: "tn_abc123" } as const;

describe("CX-03 exact-scope context and CX-04 Organisation 360", () => {
  it("only offers exact caller-bound grants and removes duplicates", () => {
    const authority = fixture([
      grant("organisation.view", { level: "ORGANISATION", organisation_id: org.id }),
      grant("organisation.view", { level: "ORGANISATION", organisation_id: org.id }),
      grant("tenant.view", { level: "TENANT", tenant_id: tenant.id }),
      grant("organisation.view", { level: "PLATFORM" }),
      grant("organisation.view", { level: "CORPORATE_GROUP", corporate_group_id: "group_corp" }),
      grant("organisation.view", { level: "ORGANISATION", organisation_id: "org_sibling",
        mode: "DYNAMIC_GROUP_DESCENDANTS" }),
      grant("tenant.activate", { level: "TENANT", tenant_id: "tn_unowned" }),
      grant("tenant.view", { level: "TENANT", tenant_id: "tn_expired" },
        { valid_until: "2026-10-12T07:59:00.000Z" }),
    ]);
    expect(scopedContextCandidates(authority, "prn_owner", now)).toEqual([org, tenant]);
    expect(scopedContextCandidates(authority, "prn_other", now)).toEqual([]);
    expect(matchesScopedContext([org], "organisation", "org_sibling")).toBe(false);
    expect(contextHref(org)).toBe("/workspace/organisations/org_canonical_001");
    expect(contextHref(tenant)).toBe("/workspace/tenants/tn_abc123");
  });

  it("rejects stale grants, forged scope identifiers and audit privilege substitution", () => {
    const auditOnly = fixture([
      grant("audit.view", { level: "ORGANISATION", organisation_id: org.id }),
    ]);
    expect(scopedContextCandidates(auditOnly, "prn_owner", now)).toEqual([]);
    expect(hasExactScopedPermission(auditOnly, "prn_owner", "audit.view", "organisation", org.id, now)).toBe(true);
    expect(hasExactScopedPermission(auditOnly, "prn_owner", "organisation.view", "organisation", org.id, now)).toBe(false);
    expect(hasExactScopedPermission(auditOnly, "prn_other", "audit.view", "organisation", org.id, now)).toBe(false);
    expect(hasExactScopedPermission(auditOnly, "prn_owner", "audit.view", "organisation", "org_sibling", now)).toBe(false);
    expect(scopedContextCandidates(fixture([grant("organisation.view", {
      level: "ORGANISATION", organisation_id: "../other",
    })]), "prn_owner", now)).toEqual([]);
    expect(scopedContextCandidates({
      principal_id: "prn_owner", evaluated_at: "2026-10-12T07:40:00.000Z",
      grants: [grant("organisation.view", { level: "ORGANISATION", organisation_id: org.id })],
    }, "prn_owner", now)).toEqual([]);
  });

  it("accepts only exact GET-only upstream paths and escapes opaque IDs", () => {
    expect(resolveProgressiveCommand({ action: "organisation-entity", id: org.id })).toEqual({
      method: "GET", path: "/v1/canonical-entities/org_canonical_001",
    });
    expect(resolveProgressiveCommand({ action: "organisation-iam-links", id: org.id }).path)
      .toBe("/v1/canonical-entities/org_canonical_001/iam-organisations");
    expect(resolveProgressiveCommand({ action: "organisation-audit", id: org.id }).path)
      .toBe("/v1/organisations/org_canonical_001/audit?limit=25");
    expect(resolveProgressiveCommand({ action: "tenant-record", id: tenant.id }).path)
      .toBe("/v1/tenants/tn_abc123");
    expect(resolveProgressiveCommand({ action: "organisation-entity", id: "org:canonical-123" }).path)
      .toBe("/v1/canonical-entities/org%3Acanonical-123");
    for (const payload of [
      { action: "organisation-entity", id: "../admin" },
      { action: "organisation-entity", id: org.id, tenantId: "tn_sibling" },
      { action: "organisation-audit", id: org.id, body: { limit: 5000 } },
      { action: "tenant-record", id: "org_wrong_00001" },
      { action: "tenant-record", id: tenant.id, idempotencyKey: "unsafe" },
      { action: "organisation-iam-links", id: org.id + "?limit=1000" },
    ]) expect(() => resolveProgressiveCommand(payload as never)).toThrow();
  });

  it("rejects cross-organisation and cross-tenant response substitution", () => {
    const record = { id: org.id, entity_type: "ORGANISATION", status: "ACTIVE",
      version: 2, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-10-12T00:00:00Z" };
    expect(parseOrganisation(record, org.id)?.status).toBe("ACTIVE");
    expect(parseOrganisation(record, "org_other")).toBeNull();
    expect(parseOrganisation({ ...record, entity_type: "LEGAL_ENTITY" }, org.id)).toBeNull();
    expect(parseOrganisationIamLinks({ items: [{
      id: "ref_1", organisation_id: "org_other", provider: "keycloak",
      status: "ACTIVE", effective_from: "2026-01-01T00:00:00Z", source_authority: "CP",
    }] }, org.id)).toBeNull();
    expect(parseTenant({ tenant_id: tenant.id, legal_entity_id: "le_x", display_name: "Sample",
      isolation_strategy: "row_level_security", residency_region: "af-south-1",
      desired_state: "active", observed_state: "pending", revision: 1 }, "tn_sibling")).toBeNull();
    expect(parseOrganisationAudit({ items: [{ audit_id: "id", occurred_at: "now", action: "audit.read" }] })).toBeNull();
  });

  it("accepts a tenant without fictitious incorporation while preserving exact tenant authority", () => {
    const base = {
      tenant_id: tenant.id, display_name: "Unincorporated operating business",
      isolation_strategy: "row_level_security", residency_region: "af-south-1",
      desired_state: "provisioning", observed_state: "pending", revision: 1,
    };
    expect(parseTenant({ ...base, legal_entity_id: "" }, tenant.id)?.legal_entity_id).toBe("");
    expect(parseTenant({ ...base, legal_entity_id: null }, tenant.id)?.legal_entity_id).toBeNull();
    expect(parseTenant(base, tenant.id)?.legal_entity_id).toBeUndefined();
    // Optional DEFAULT legal entity never relaxes tenant identity matching.
    expect(parseTenant({ ...base, legal_entity_id: null }, "tn_sibling")).toBeNull();
    expect(parseTenant({ ...base, legal_entity_id: { id: "le_fabricated" } }, tenant.id)).toBeNull();
  });

  it("renders an accessible selector and never navigates to an unauthorised typed ID", async () => {
    push.mockClear();
    const user = userEvent.setup();
    const { container } = render(<ContextSwitcher verified options={[org, tenant]} />);
    expect(screen.getByRole("combobox", { name: "Current governed resource context" })).toBeTruthy();
    await user.selectOptions(screen.getByRole("combobox"), contextHref(tenant));
    expect(push).toHaveBeenCalledWith("/workspace/tenants/tn_abc123");
    await expectAccessible(container);
    const { container: blocked } = render(<ContextSwitcher verified={false} options={[]} />);
    expect(blocked.querySelector("select")?.disabled).toBe(true);
    expect(blocked.querySelector('option[value="/workspace/tenants/tn_abc123"]')).toBeNull();
  });
});
