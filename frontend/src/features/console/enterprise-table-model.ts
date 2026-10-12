import { z } from "zod";

import type { OrganisationOverviewProjection } from "./OrganisationIntegrations";
import type { OrganisationGovernance } from "./OrganisationGovernancePanel";

/**
 * CX-05C: bounded read-only table projection over independently authorised CP
 * read models. This is never a cross-tenant index, an authority cache, or
 * permission to export or mutate records.
 */
const domains = [
  "tenants", "markets", "subscriptions", "digital-estates",
  "legal-entities", "corporate-relations", "mandates", "readiness",
] as const;
export type EnterpriseDomain = typeof domains[number];
const querySchema = z.object({
  domain: z.enum(domains).default("tenants"),
  q: z.string().trim().max(80).default(""),
  state: z.string().regex(/^[A-Z_]{1,40}$/).optional(),
  sort: z.enum(["name-asc", "name-desc"]).default("name-asc"),
  page: z.coerce.number().int().min(1).max(400).default(1),
  columns: z.enum(["compact", "detailed"]).default("detailed"),
}).strict();
export type EnterpriseTableQuery = z.infer<typeof querySchema>;
export type EnterpriseTableRow = {
  key: string; name: string; state: string; source: string;
  subject: string; detail: string; href?: string;
};
export type EnterpriseTableResult = {
  state: "available" | "restricted" | "unavailable";
  items: EnterpriseTableRow[];
  visibleCount: number;
  totalPages: number;
  suppressedSections: number;
  evaluatedAt?: string;
};
const perPage = 25;
const maxBoundedRows = 10_000;

export function parseEnterpriseTableQuery(raw: Record<string, string | string[] | undefined>): EnterpriseTableQuery | null {
  if (Object.values(raw).some((v) => Array.isArray(v))) return null;
  const nonempty = Object.fromEntries(Object.entries(raw).filter(([, value]) => value !== "" && value !== undefined));
  const parsed = querySchema.safeParse(nonempty);
  return parsed.success ? parsed.data : null;
}

export function enterpriseTableHref(organisationId: string, criteria: EnterpriseTableQuery): string {
  const params = new URLSearchParams();
  params.set("domain", criteria.domain);
  if (criteria.q) params.set("q", criteria.q);
  if (criteria.state) params.set("state", criteria.state);
  params.set("sort", criteria.sort);
  params.set("page", String(criteria.page));
  params.set("columns", criteria.columns);
  return "/workspace/organisations/" + encodeURIComponent(organisationId) + "/records?" + params.toString();
}

export const enterpriseDomainLabels: Record<EnterpriseDomain, string> = {
  tenants: "Tenants", markets: "Markets", subscriptions: "Subscriptions",
  "digital-estates": "Digital Estates", "legal-entities": "Legal entities",
  "corporate-relations": "Corporate relationships", mandates: "Legal-actor mandates",
  readiness: "Provisioning readiness",
};
function tenantHref(id: string): string {
  return "/workspace/tenants/" + encodeURIComponent(id);
}

/** Every row comes from an already CP-authorised source section; visibility
 * flags and absent source MUST NOT be interpreted as empty data.
 */
export function projectEnterpriseTable(
  overview: OrganisationOverviewProjection | null,
  governance: OrganisationGovernance | null,
  query: EnterpriseTableQuery,
  now = Date.now(),
): EnterpriseTableResult {
  const unavailable: EnterpriseTableResult = { state: "unavailable", items: [], visibleCount: 0, totalPages: 0, suppressedSections: 0 };
  if (!overview) return unavailable;
  const evaluated = Date.parse(overview.evaluated_at);
  if (!Number.isFinite(evaluated) || evaluated > now + 60_000 || evaluated < now - 300_000) return unavailable;
  const rows: EnterpriseTableRow[] = [];
  let suppressed = 0;
  let authorisedSections = 0;
  const tenants = overview.tenants;
  const scoped = (value: boolean) => {
    if (value) authorisedSections++;
    else suppressed++;
    return value;
  };
  const add = (row: EnterpriseTableRow) => {
    if (rows.length >= maxBoundedRows || !row.key || !row.name || !row.source) throw new Error("INCOMPLETE_ENTERPRISE_TABLE");
    rows.push(row);
  };
  try {
    if (query.domain === "tenants") {
      if (!tenants) return unavailable;
      authorisedSections++;
      for (const t of tenants) add({ key:t.tenant_id, name:t.display_name, state:t.observed_state,
        source:t.mapping_provenance, subject:t.tenant_id, detail:"Desired: " + t.desired_state,
        href:tenantHref(t.tenant_id) });
    } else if (query.domain === "legal-entities") {
      authorisedSections++;
      for (const e of overview.legal_entities) add({key:e.legal_entity_id,name:e.legal_name,state:e.verification_state,
        source:e.source_authority,subject:e.legal_entity_id,detail:"Legal status: " + e.legal_status + " · Evidence references: " + e.evidence_count });
    } else if (query.domain === "corporate-relations") {
      authorisedSections++;
      for (const r of overview.corporate_relationships) add({key:r.id,name:r.relationship_type,state:r.verification_state,
        source:r.source_authority,subject:r.target_organisation_id,detail:r.direct_or_derived + " · " + r.status });
    } else if (query.domain === "markets" || query.domain === "subscriptions" || query.domain === "digital-estates") {
      if (!tenants) return unavailable;
      for (const t of tenants) {
        if (query.domain === "markets" && scoped(t.markets_visible)) {
          for (const m of t.market_participations) add({
            key:t.tenant_id+":"+m.registry_market_id, name:m.country_code, state:"PARTICIPATING",
            source:"CP market participation", subject:t.tenant_id,detail:m.registry_market_id + " · " + m.currency_code,
            href:tenantHref(t.tenant_id),
          });
        } else if (query.domain === "subscriptions" && scoped(t.subscriptions_visible)) {
          for (const s of t.subscriptions) add({
            key:t.tenant_id+":"+s.subscription_id,name:s.product_id,state:s.status,
            source:s.source,subject:t.tenant_id,detail:"Effective: "+s.effective_from,href:tenantHref(t.tenant_id),
          });
        } else if (query.domain === "digital-estates" && scoped(t.estates_visible)) {
          for (const e of t.digital_estates) add({
            key:t.tenant_id+":"+e.id,name:e.name,state:e.status,
            source:"CP Digital Estate",subject:t.tenant_id,detail:e.domain,href:tenantHref(t.tenant_id),
          });
        }
      }
    } else if (query.domain === "mandates" || query.domain === "readiness") {
      // A governance source must have passed CX-04D reconciliation to the
      // current PRIMARY-linked set before it enters this projector.
      if (!governance || !tenants) return unavailable;
      const permitted = new Set(tenants.map((t) => t.tenant_id));
      if (governance.organisation_id !== overview.organisation_id ||
          governance.tenants.some((t) => !permitted.has(t.tenant_id))) return unavailable;
      const govTime = Date.parse(governance.evaluated_at);
      if (!Number.isFinite(govTime) || govTime > now+60_000 || govTime < now-300_000) return unavailable;
      for (const t of governance.tenants) {
        if (query.domain === "mandates" && scoped(t.mandates_visible)) {
          for (const m of t.mandates) add({
            key:t.tenant_id+":"+m.mandate_id,name:m.roles.join(", "),state:m.status,
            source:m.provenance,subject:t.tenant_id,detail:"Decision: "+(m.decision??"not recorded")+" · Evidence references: "+m.evidence_count,
            href:tenantHref(t.tenant_id),
          });
        } else if (query.domain === "readiness" && scoped(t.readiness_visible) && t.readiness) {
          add({key:t.tenant_id,name:t.tenant_id,state:t.readiness.state,
            source:t.readiness.source,subject:t.tenant_id,
            detail:"Desired revision: "+t.readiness.desired_state_version+" · Observed revision: "+t.readiness.observed_state_version,
            href:tenantHref(t.tenant_id)});
        }
      }
      suppressed += tenants.length - governance.tenants.length;
    }
    const keys = new Set(rows.map((row) => row.key));
    if (keys.size !== rows.length) return unavailable;
    const needle = query.q.toLocaleLowerCase();
    const matched = rows.filter((row) =>
      (!query.state || row.state === query.state) &&
      (!needle || [row.name,row.subject,row.state,row.detail].some((value)=>value.toLocaleLowerCase().includes(needle))));
    matched.sort((a,b) => {
      const byName = a.name.localeCompare(b.name,undefined,{sensitivity:"base"}) || a.key.localeCompare(b.key);
      return query.sort === "name-desc" ? -byName : byName;
    });
    const offset = (query.page - 1) * perPage;
    return {state: authorisedSections ? "available" : suppressed ? "restricted" : "available",
      items:matched.slice(offset,offset+perPage),visibleCount:matched.length,
      totalPages:Math.ceil(matched.length/perPage),suppressedSections:suppressed,
      evaluatedAt:overview.evaluated_at};
  } catch {
    return unavailable;
  }
}
