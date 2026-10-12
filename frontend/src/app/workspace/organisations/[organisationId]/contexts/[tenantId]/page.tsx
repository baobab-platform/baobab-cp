import Link from "next/link";
import { notFound } from "next/navigation";
import { ArrowLeft, Building2, Globe2, LockKeyhole, ShieldCheck } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { enterpriseContextHref } from "@/features/console/enterprise-context-model";
import { requireEnterpriseNavigationContext } from "@/server/console/enterprise-context";

export const dynamic = "force-dynamic";
export const revalidate = 0;

/** A server-validated navigation view; never an authorisation context for
 * mutations, tenant activation or calls to downstream engines.
 */
export default async function EnterpriseContextPage({
  params, searchParams,
}: {
  params: Promise<{ organisationId: string; tenantId: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { organisationId, tenantId } = await params;
  const query = await searchParams;
  if (Object.keys(query).some((key) => key !== "market") ||
      Array.isArray(query.market)) notFound();
  const context = await requireEnterpriseNavigationContext(
    organisationId, tenantId, query.market,
  );
  return (
    <section aria-labelledby="enterprise-context-title" className="space-y-7">
      <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Link href={"/workspace/organisations/" + encodeURIComponent(organisationId)}
          prefetch={false} className="inline-flex min-h-10 items-center gap-1 text-primary underline-offset-4 hover:underline">
          <ArrowLeft aria-hidden="true" className="size-4" />Organisation 360°
        </Link>
        <span aria-hidden="true">/</span><span>Governed tenant context</span>
      </nav>
      <div className="rounded-2xl bg-brand-forest p-7 text-white sm:p-10">
        <p className="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-amber-200">
          <ShieldCheck aria-hidden="true" className="size-5" />Current authorised relationship
        </p>
        <h1 id="enterprise-context-title" className="mt-4 break-words text-3xl font-bold sm:text-4xl">
          {context.organisationName} / {context.tenantName}
        </h1>
        <p className="mt-4 max-w-3xl text-sm leading-relaxed text-emerald-50">
          Read-only enterprise navigation. Both resource grants and the effective PRIMARY relationship
          are independently checked by Control Plane on every request. This view does not create
          a reusable tenant identity, trading authority or workload Context.
        </p>
      </div>
      <div className="grid gap-5 lg:grid-cols-2">
        <Card>
          <CardHeader><Building2 aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Governed tenant association</CardTitle>
            <CardDescription>Source: CP Organisation overview + canonical tenant registry</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <p className="break-all"><strong>Organisation:</strong> {context.organisationId}</p>
            <p className="break-all"><strong>Tenant:</strong> {context.tenantId}</p>
            <p><strong>Mapping:</strong> PRIMARY_ORGANISATION</p>
            <p><strong>Provenance:</strong> {context.mappingProvenance}</p>
            <p className="text-xs text-muted-foreground">Evaluated by CP: <time>{context.evaluatedAt}</time></p>
            <div className="flex flex-wrap gap-2">
              <Badge variant="neutral">Desired · {context.desiredState}</Badge>
              <Badge variant="neutral">Observed · {context.observedState}</Badge>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader><Globe2 aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Select an authorised market</CardTitle>
            <CardDescription>Market choice is validated against fresh tenant-scoped CP market participation</CardDescription>
          </CardHeader>
          <CardContent>
            {!context.marketsVisible ? (
              <p role="status" className="flex gap-2 text-sm text-muted-foreground">
                <LockKeyhole aria-hidden="true" className="size-4 shrink-0" />
                An independent market.view grant is required to inspect or select markets.
              </p>
            ) : context.markets.length === 0 ? (
              <p role="status" className="text-sm text-muted-foreground">
                No current authorised market participation was returned.
              </p>
            ) : (
              <div className="space-y-3">
                <p className="text-xs text-muted-foreground">
                  No market is selected automatically. Changing market triggers a fresh CP authority and relationship check.
                </p>
                <nav aria-label="Authorised tenant markets" className="flex flex-wrap gap-2">
                  {context.markets.map((market) => (
                    <Link key={market.marketId} prefetch={false}
                      href={enterpriseContextHref(organisationId, tenantId, market.countryCode)}
                      aria-current={context.selectedMarket?.countryCode === market.countryCode ? "page" : undefined}
                      className={"inline-flex min-h-11 items-center gap-2 rounded-lg border px-4 py-2 text-sm font-semibold underline-offset-4 focus-visible:outline-2 focus-visible:outline-focus " +
                        (context.selectedMarket?.countryCode === market.countryCode
                          ? "border-brand-forest bg-brand-forest text-white" : "border-border text-primary hover:bg-muted")}>
                      {market.countryCode} <span className="text-xs">({market.currencyCode})</span>
                    </Link>
                  ))}
                </nav>
                {context.selectedMarket && (
                  <div role="status" className="rounded-lg border border-border bg-muted p-4 text-xs">
                    Selected current market: <strong>{context.selectedMarket.countryCode}</strong>
                    <p className="mt-1 break-all text-muted-foreground">Registry reference: {context.selectedMarket.marketId}</p>
                  </div>
                )}
                <Link href={enterpriseContextHref(organisationId, tenantId)}
                  prefetch={false} className="inline-flex min-h-11 items-center text-xs font-semibold text-primary underline-offset-4 hover:underline">
                  Clear market selection
                </Link>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
      <p className="rounded-lg border border-border bg-card p-4 text-xs leading-relaxed text-muted-foreground">
        A selected market is a navigation filter, not a permission grant. CP must independently
        authorise every command and resolve the real execution Context. Revocation, suspension,
        stale evidence or relationship changes fail closed rather than retaining a browser-selected scope.
      </p>
    </section>
  );
}
