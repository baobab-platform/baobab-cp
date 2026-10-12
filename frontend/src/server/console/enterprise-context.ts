import "server-only";

import { notFound } from "next/navigation";

import { projectEnterpriseNavigationContext } from "@/features/console/enterprise-context-model";
import { invokeProgressive } from "@/server/bff/progressive";
import {
  authenticatedConsoleActor, loadConsoleAuthority, matchesScopedContext,
} from "./scoped-context";

/** Read-only human Console context. Each navigation rechecks the CURRENT
 * session, fresh effective-grant set, source-backed PRIMARY mapping and
 * separately permitted market participation. No browser-owned authority,
 * no workload Context redemption, no sticky selection or stale fallback.
 */
export async function requireEnterpriseNavigationContext(
  organisationId: string, tenantId: string, market?: string,
) {
  const actor = await authenticatedConsoleActor();
  const authority = await loadConsoleAuthority(actor.accessToken, actor.principalId).catch(() => null);
  if (!authority?.verified ||
      !matchesScopedContext(authority.candidates, "organisation", organisationId) ||
      !matchesScopedContext(authority.candidates, "tenant", tenantId)) notFound();

  const [organisationSource, tenantSource] = await Promise.all([
    invokeProgressive({ action: "organisation-overview", id: organisationId }, actor.accessToken)
      .then((result) => result.data).catch(() => null),
    invokeProgressive({ action: "tenant-record", id: tenantId }, actor.accessToken)
      .then((result) => result.data).catch(() => null),
  ]);
  const context = projectEnterpriseNavigationContext(
    organisationId, tenantId, market, organisationSource, tenantSource,
  );
  // Revoked/suspended scope, changed association, missing market grant,
  // expired source or unavailable CP reads cannot reuse an old selection.
  if (!context) notFound();
  return context;
}
