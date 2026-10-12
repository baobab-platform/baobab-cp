import "server-only";

import { z } from "zod";

import type { AuthoritySummary } from "@/features/console/navigation-model";

/** Minimum of Shared administration/v1/grant.schema.json EffectiveAuthority.
 * Extra canonical fields are not interpreted as permissions or roles.
 */
const scopeSchema = z.object({
  level: z.enum(["PLATFORM", "PLATFORM_ACCOUNT", "CORPORATE_GROUP", "ORGANISATION",
    "TENANT", "LEGAL_ENTITY", "MARKET", "DIGITAL_ESTATE", "RESOURCE"]),
}).passthrough();
const grantSchema = z.object({
  grant_id: z.string().min(1),
  permission: z.string().regex(/^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+$/),
  scope: scopeSchema,
  grant_type: z.enum(["STANDING", "TIME_BOUND", "JUST_IN_TIME"]),
  source: z.string().min(1),
  risk_class: z.string().min(1),
}).passthrough();
const effectiveAuthoritySchema = z.object({
  principal_id: z.string().min(1),
  evaluated_at: z.iso.datetime({ offset: true }),
  grants: z.array(grantSchema).max(500),
  elevated_until: z.iso.datetime({ offset: true }).optional(),
}).passthrough();

export const unavailableAuthority: AuthoritySummary = { state: "unavailable", permissions: [] };

/** Fail closed on malformed or stale grant sets; this is never an authz engine.
 * The caller's own grant snapshot can only *hide/show* known navigation.
 */
export function summariseEffectiveAuthority(
  input: unknown,
  principalId: string,
  now = Date.now(),
): AuthoritySummary {
  const result = effectiveAuthoritySchema.safeParse(input);
  if (!result.success || !principalId || result.data.principal_id !== principalId) {
    return unavailableAuthority;
  }
  const evaluated = Date.parse(result.data.evaluated_at);
  if (!Number.isFinite(evaluated) || evaluated > now + 60_000 || evaluated < now - 300_000) {
    return unavailableAuthority;
  }
  return {
    state: "available",
    permissions: [...new Set(result.data.grants.map((grant) => grant.permission))].sort(),
    evaluatedAt: result.data.evaluated_at,
    ...(result.data.elevated_until ? { elevatedUntil: result.data.elevated_until } : {}),
  };
}
