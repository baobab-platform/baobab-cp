import { z } from "zod";

const canonical = z.object({
  id: z.string().min(3).max(128),
  entity_type: z.string(),
  status: z.string(),
  version: z.number().int().positive(),
  created_at: z.string(),
  updated_at: z.string(),
  display_name: z.string().optional(),
  canonical_key: z.string().optional(),
  subtype: z.string().optional(),
  owner_tenant_id: z.string().optional(),
  owner_legal_entity_id: z.string().optional(),
  classification: z.string().optional(),
  authority: z.string().optional(),
}).passthrough();

const tenant = z.object({
  tenant_id: z.string(),
  legal_entity_id: z.string(),
  display_name: z.string(),
  isolation_strategy: z.string(),
  residency_region: z.string(),
  desired_state: z.string(),
  observed_state: z.string(),
  revision: z.number().int().positive(),
}).passthrough();

const linkPage = z.object({
  items: z.array(z.object({
    id: z.string(), organisation_id: z.string(), provider: z.string(),
    status: z.string(), effective_from: z.string(), source_authority: z.string(),
  }).passthrough()).max(1000),
}).passthrough();

const auditPage = z.object({
  items: z.array(z.object({
    audit_id: z.string(), occurred_at: z.string(), action: z.string(),
    target: z.string(), actor: z.object({ actor_id: z.string(), actor_type: z.string() }).passthrough(),
    correlation_id: z.string().optional(),
  }).passthrough()).max(25),
  next: z.string().optional(),
}).passthrough();

export type OrganisationDetail = z.infer<typeof canonical>;
export type TenantDetail = z.infer<typeof tenant>;
export type OrganisationIamLink = z.infer<typeof linkPage>["items"][number];
export type OrganisationAuditEntry = z.infer<typeof auditPage>["items"][number];

export function parseOrganisation(input: unknown, expectedId: string): OrganisationDetail | null {
  const parsed = canonical.safeParse(input);
  if (!parsed.success || parsed.data.id !== expectedId || parsed.data.entity_type !== "ORGANISATION") return null;
  return parsed.data;
}
export function parseTenant(input: unknown, expectedId: string): TenantDetail | null {
  const parsed = tenant.safeParse(input);
  if (!parsed.success || parsed.data.tenant_id !== expectedId) return null;
  return parsed.data;
}
export function parseOrganisationIamLinks(input: unknown, expectedId: string): OrganisationIamLink[] | null {
  const parsed = linkPage.safeParse(input);
  if (!parsed.success || parsed.data.items.some((item) => item.organisation_id !== expectedId)) return null;
  return parsed.data.items;
}
export function parseOrganisationAudit(input: unknown): { items: OrganisationAuditEntry[]; next?: string } | null {
  const parsed = auditPage.safeParse(input);
  if (!parsed.success) return null;
  return {
    items: parsed.data.items,
    ...(parsed.data.next ? { next: parsed.data.next } : {}),
  };
}
