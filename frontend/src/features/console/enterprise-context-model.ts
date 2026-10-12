import { parseOrganisationOverview } from "./OrganisationIntegrations";
import { parseTenant } from "./organisation-model";

/**
 * CX-03B: a read-only navigation context, NEVER a Control Plane Context token.
 * Inputs must be independent CP reads obtained with the same current caller.
 * The caller must also pass current exact org+tenant grants server-side.
 */
export type EnterpriseMarket = {
  countryCode: string;
  marketId: string;
  currencyCode: string;
};
export type EnterpriseNavigationContext = {
  organisationId: string;
  organisationName: string;
  tenantId: string;
  tenantName: string;
  mappingProvenance: string;
  desiredState: string;
  observedState: string;
  evaluatedAt: string;
  marketsVisible: boolean;
  markets: EnterpriseMarket[];
  selectedMarket?: EnterpriseMarket;
};

const organisationID = /^[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$/;
const tenantID = /^tn_[a-z0-9]{3,60}$/;
const countryCode = /^[A-Z]{2}$/;
const inactive = /(?:^|_)(?:SUSPENDED|REVOKED|DEACTIVATED|TERMINATED|DISABLED|DELETED)(?:_|$)/;

export function enterpriseContextHref(organisationId: string, tenantId: string, market?: string): string {
  const root = "/workspace/organisations/" + encodeURIComponent(organisationId) +
    "/contexts/" + encodeURIComponent(tenantId);
  return market ? root + "?market=" + encodeURIComponent(market) : root;
}

/** Verify a fresh source-backed PRIMARY mapping and optional current market.
 * Never infer subsidiary access, legal authority or operational activation.
 * An unavailable source, stale projection or missing independent permission
 * fails closed. Market selection is explicit, not silently defaulted.
 */
export function projectEnterpriseNavigationContext(
  organisationId: string,
  tenantId: string,
  requestedMarket: string | undefined,
  organisationSource: unknown,
  tenantSource: unknown,
  now = Date.now(),
): EnterpriseNavigationContext | null {
  if (!organisationID.test(organisationId) || !tenantID.test(tenantId) ||
      (requestedMarket !== undefined && !countryCode.test(requestedMarket))) return null;
  const overview = parseOrganisationOverview(organisationSource, organisationId);
  const tenant = parseTenant(tenantSource, tenantId);
  if (!overview || !tenant || !overview.tenants) return null;
  const at = Date.parse(overview.evaluated_at);
  if (!Number.isFinite(at) || at > now + 60_000 || at < now - 300_000) return null;
  if (inactive.test(overview.profile.status.toUpperCase()) ||
      inactive.test(tenant.desired_state.toUpperCase()) ||
      inactive.test(tenant.observed_state.toUpperCase())) return null;
  const matches = overview.tenants.filter((item) => item.tenant_id === tenantId);
  if (matches.length !== 1) return null;
  const mapping = matches[0]!;
  if (mapping.mapping_role !== "PRIMARY_ORGANISATION" ||
      !mapping.mapping_provenance.trim()) return null;
  // The CP overview has already required an exact market.view grant for
  // markets_visible. An ungranted market cannot enter this selector.
  const markets: EnterpriseMarket[] = [];
  const seen = new Set<string>();
  if (mapping.markets_visible) {
    for (const item of mapping.market_participations) {
      if (!countryCode.test(item.country_code) || !item.registry_market_id ||
          !item.currency_code || seen.has(item.country_code)) return null;
      seen.add(item.country_code);
      markets.push({
        countryCode: item.country_code,
        marketId: item.registry_market_id,
        currencyCode: item.currency_code,
      });
    }
  } else if (mapping.market_participations.length) return null;
  const selectedMarket = requestedMarket === undefined ? undefined :
    markets.find((market) => market.countryCode === requestedMarket);
  if (requestedMarket !== undefined && !selectedMarket) return null;
  return {
    organisationId, organisationName: overview.profile.display_name,
    tenantId, tenantName: tenant.display_name,
    mappingProvenance: mapping.mapping_provenance,
    desiredState: tenant.desired_state, observedState: tenant.observed_state,
    evaluatedAt: overview.evaluated_at, marketsVisible: mapping.markets_visible,
    markets, ...(selectedMarket ? { selectedMarket } : {}),
  };
}
