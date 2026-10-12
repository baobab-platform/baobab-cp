import Link from "next/link";
import type { ReactNode } from "react";
import { z } from "zod";
import { ArrowUpRight, Globe2, Layers3, ShoppingBag, PanelsTopLeft, LockKeyhole } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { enterpriseContextHref } from "./enterprise-context-model";

const text = z.string().min(1);
export const tenantOverviewSchema = z.object({
  tenant_id: z.string().regex(/^tn_[a-z0-9]{3,60}$/),
  display_name: text,
  mapping_role: text,
  mapping_provenance: text,
  residency_region: text,
  isolation_strategy: text,
  desired_state: text,
  observed_state: text,
  markets_visible: z.boolean(),
  subscriptions_visible: z.boolean(),
  estates_visible: z.boolean(),
  market_participations: z.array(z.object({
    country_code: text, registry_market_id: z.string(),
    currency_code: z.string(),
  })).max(100),
  subscriptions: z.array(z.object({
    subscription_id: text, product_id: text, status: text,
    source: text, effective_from: text, effective_to: z.string().optional(),
  })).max(100),
  digital_estates: z.array(z.object({
    id: text, name: text, domain: text, status: text,
  })).max(100),
}).passthrough();
export type TenantOverviewProjection = z.infer<typeof tenantOverviewSchema>;

function SourceList({ visible, empty, children }: {
  visible: boolean; empty: string; children: ReactNode;
}) {
  if (!visible) return (
    <p className="flex items-center gap-2 text-xs leading-relaxed text-muted-foreground">
      <LockKeyhole aria-hidden="true" className="size-4 shrink-0" />
      Not visible with the current independent tenant permission.
    </p>
  );
  return <div className="text-sm">{children || <p className="text-muted-foreground">{empty}</p>}</div>;
}

/** These are only PRIMARY-linked tenants for which CP confirmed a current
 * exact tenant.view grant. Related legal entities alone provide no access.
 */
export function OrganisationTenantIntegrations({ tenants, organisationId }: {
  tenants: readonly TenantOverviewProjection[];
  organisationId?: string;
}) {
  return (
    <section aria-label="Authorised organisation tenants and services" className="space-y-5">
      <div>
        <p className="text-xs font-bold uppercase tracking-wider text-primary">Scoped operational relationships</p>
        <h2 className="mt-2 text-2xl font-bold tracking-tight">Tenants, markets and services</h2>
        <p className="mt-2 max-w-3xl text-sm leading-relaxed text-muted-foreground">
          Only tenants connected by an effective PRIMARY mapping and separately authorised for the current principal appear here. This is not a group-wide or unrestricted tenant directory.
        </p>
      </div>
      {tenants.length === 0 ? (
        <Card><CardContent>
          <p className="text-sm leading-relaxed text-muted-foreground">
            No independently accessible PRIMARY-linked tenants were returned. This does not prove that the organisation has no other tenants.
          </p>
        </CardContent></Card>
      ) : tenants.map((tenant) => (
        <Card key={tenant.tenant_id}>
          <CardHeader>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Layers3 aria-hidden="true" className="size-6 text-primary" />
              <div className="flex flex-wrap gap-4">
                <Link className="inline-flex min-h-10 items-center gap-2 text-xs font-semibold text-primary underline-offset-4 hover:underline"
                  href={"/workspace/tenants/" + encodeURIComponent(tenant.tenant_id)} prefetch={false}>
                  Open authorised tenant <ArrowUpRight aria-hidden="true" className="size-4" />
                </Link>
                {organisationId ? (
                  <Link className="inline-flex min-h-10 items-center gap-2 text-xs font-semibold text-primary underline-offset-4 hover:underline"
                    href={enterpriseContextHref(organisationId, tenant.tenant_id)} prefetch={false}>
                    Open governed context <ArrowUpRight aria-hidden="true" className="size-4" />
                  </Link>
                ) : null}
              </div>
            </div>
            <CardTitle>{tenant.display_name}</CardTitle>
            <CardDescription>{tenant.tenant_id} · {tenant.mapping_role} association · Source: {tenant.mapping_provenance}</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="mb-6 flex flex-wrap gap-2">
              <Badge variant="neutral">Desired · {tenant.desired_state}</Badge>
              <Badge variant="neutral">Observed · {tenant.observed_state}</Badge>
              <Badge variant="neutral">{tenant.residency_region}</Badge>
              <Badge variant="neutral">{tenant.isolation_strategy}</Badge>
            </div>
            <div className="grid gap-5 lg:grid-cols-3">
              <section aria-label={"Market participation for " + tenant.display_name} className="space-y-3 rounded-lg border border-border p-4">
                <h3 className="flex items-center gap-2 text-sm font-semibold"><Globe2 aria-hidden="true" className="size-4 text-primary" />Operational markets</h3>
                <SourceList visible={tenant.markets_visible} empty="No active market participation records were returned.">
                  {tenant.market_participations.length ? <ul className="space-y-3">{tenant.market_participations.map((market) => (
                    <li key={market.registry_market_id + market.country_code} className="text-xs">
                      <strong>{market.country_code}</strong>
                      <p className="break-all text-muted-foreground">{market.registry_market_id} · {market.currency_code}</p>
                    </li>
                  ))}</ul> : null}
                </SourceList>
              </section>
              <section aria-label={"Subscriptions for " + tenant.display_name} className="space-y-3 rounded-lg border border-border p-4">
                <h3 className="flex items-center gap-2 text-sm font-semibold"><ShoppingBag aria-hidden="true" className="size-4 text-primary" />Product subscriptions</h3>
                <SourceList visible={tenant.subscriptions_visible} empty="No product subscriptions were returned.">
                  {tenant.subscriptions.length ? <ul className="space-y-3">{tenant.subscriptions.map((item) => (
                    <li key={item.subscription_id} className="text-xs">
                      <strong>{item.product_id}</strong>
                      <p className="text-muted-foreground">Status: {item.status} · Source: {item.source}</p>
                      <p className="text-muted-foreground">Effective: {item.effective_from}</p>
                    </li>
                  ))}</ul> : null}
                </SourceList>
              </section>
              <section aria-label={"Digital Estates for " + tenant.display_name} className="space-y-3 rounded-lg border border-border p-4">
                <h3 className="flex items-center gap-2 text-sm font-semibold"><PanelsTopLeft aria-hidden="true" className="size-4 text-primary" />Digital Estates</h3>
                <SourceList visible={tenant.estates_visible} empty="No Digital Estates were returned.">
                  {tenant.digital_estates.length ? <ul className="space-y-3">{tenant.digital_estates.map((estate) => (
                    <li key={estate.id} className="text-xs">
                      <strong>{estate.name}</strong>
                      <p className="break-all text-muted-foreground">{estate.domain}</p>
                      <p className="text-muted-foreground">Status: {estate.status}</p>
                    </li>
                  ))}</ul> : null}
                </SourceList>
              </section>
            </div>
            <p className="mt-5 text-xs leading-relaxed text-muted-foreground">
              Commercial subscription status, market participation and Digital Estate status do not demonstrate financial settlement, legal-actor mandate acceptance or provider production readiness.
            </p>
          </CardContent>
        </Card>
      ))}
    </section>
  );
}
