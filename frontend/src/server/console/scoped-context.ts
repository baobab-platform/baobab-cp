import "server-only";

import { cookies } from "next/headers";
import { notFound, redirect } from "next/navigation";
import { z } from "zod";

import { invokeProgressive } from "@/server/bff/progressive";
import { summariseEffectiveAuthority } from "@/server/bff/authority";
import { SESSION_COOKIE, createAuthStore, currentSession, readAuthSettings } from "@/server/auth/runtime";
import type { ConsoleContextOption } from "@/features/console/context-model";

const id = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/);
const tenantId = z.string().regex(/^tn_[a-z0-9]{3,60}$/);

/** Mirrors minimum required Shared EffectiveAuthority fields and strict
 * scope-anchor checks for selectable EXACT organisation and tenant grants.
 * Broad platform / group grants do not silently enumerate businesses.
 */
const record = z.object({
  principal_id: z.string().min(1),
  evaluated_at: z.iso.datetime({ offset: true }),
  grants: z.array(z.object({
    grant_id: z.string().min(1),
    permission: z.string(),
    scope: z.object({
      level: z.string(),
      mode: z.string().optional(),
      organisation_id: z.string().optional(),
      tenant_id: z.string().optional(),
    }).passthrough(),
    grant_type: z.string(),
    source: z.string(),
    risk_class: z.string(),
    valid_until: z.iso.datetime({ offset: true }).optional(),
  }).passthrough()).max(500),
}).passthrough();

export function scopedContextCandidates(input: unknown, principal: string, now = Date.now()): ConsoleContextOption[] {
  if (summariseEffectiveAuthority(input, principal, now).state !== "available") return [];
  const parsed = record.safeParse(input);
  if (!parsed.success) return [];
  const known = new Set<string>();
  const out: ConsoleContextOption[] = [];
  for (const grant of parsed.data.grants) {
    if (grant.valid_until && Date.parse(grant.valid_until) <= now) continue;
    if (grant.scope.mode && grant.scope.mode !== "EXACT") continue;
    let option: ConsoleContextOption | undefined;
    if (grant.permission === "organisation.view" && grant.scope.level === "ORGANISATION" &&
        id.safeParse(grant.scope.organisation_id).success) {
      option = { kind: "organisation", id: grant.scope.organisation_id!, label: grant.scope.organisation_id! };
    }
    if (grant.permission === "tenant.view" && grant.scope.level === "TENANT" &&
        tenantId.safeParse(grant.scope.tenant_id).success) {
      option = { kind: "tenant", id: grant.scope.tenant_id!, label: grant.scope.tenant_id! };
    }
    if (option && !known.has(option.kind + ":" + option.id)) {
      known.add(option.kind + ":" + option.id);
      out.push(option);
    }
  }
  return out.sort((a, b) => a.kind.localeCompare(b.kind) || a.id.localeCompare(b.id));
}

/** A grant-scoped view hint is separate from a privilege to invoke CP. */
export function hasExactScopedPermission(
  input: unknown,
  principal: string,
  permission: string,
  kind: ConsoleContextOption["kind"],
  selectedId: string,
  now = Date.now(),
): boolean {
  if (summariseEffectiveAuthority(input, principal, now).state !== "available") return false;
  const parsed = record.safeParse(input);
  if (!parsed.success) return false;
  return parsed.data.grants.some((grant) =>
    grant.permission === permission &&
    (!grant.valid_until || Date.parse(grant.valid_until) > now) &&
    (!grant.scope.mode || grant.scope.mode === "EXACT") &&
    (kind === "organisation"
      ? grant.scope.level === "ORGANISATION" && grant.scope.organisation_id === selectedId
      : grant.scope.level === "TENANT" && grant.scope.tenant_id === selectedId));
}

export function matchesScopedContext(options: readonly ConsoleContextOption[], kind: ConsoleContextOption["kind"], selectedId: string): boolean {
  return options.some((option) => option.kind === kind && option.id === selectedId);
}

/** The server owns identity and the CP access token. Unavailable auth is
 * fail-closed; no remembered browser-selected context is considered authority.
 */
export async function authenticatedConsoleActor() {
  let settings;
  try {
    settings = readAuthSettings();
  } catch {
    notFound();
  }
  const store = createAuthStore(settings);
  const cookieStore = await cookies();
  const session = await currentSession(store, cookieStore.get(SESSION_COOKIE)?.value, settings).catch(() => null);
  if (!session) redirect("/api/auth/login");
  return session;
}

export async function loadConsoleAuthority(accessToken: string, principalId: string) {
  const data = await invokeProgressive({ action: "effective-authority" }, accessToken);
  if (summariseEffectiveAuthority(data.data, principalId).state !== "available") {
    return { candidates: [] as ConsoleContextOption[], verified: false };
  }
  return { candidates: scopedContextCandidates(data.data, principalId), verified: true };
}

export async function requireScopedContext(kind: ConsoleContextOption["kind"], requestedId: string) {
  const actor = await authenticatedConsoleActor();
  const authority = await loadConsoleAuthority(actor.accessToken, actor.principalId).catch(() => null);
  if (!authority?.verified || !matchesScopedContext(authority.candidates, kind, requestedId)) {
    notFound();
  }
  return actor;
}
