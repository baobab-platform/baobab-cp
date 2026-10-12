import Link from "next/link";
import { notFound } from "next/navigation";
import { ArrowLeft, CircleAlert, Database, Globe2, Layers3, ShieldCheck } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { parseTenant } from "@/features/console/organisation-model";
import { requireScopedContext } from "@/server/console/scoped-context";
import { invokeProgressive } from "@/server/bff/progressive";

export const dynamic = "force-dynamic";

/** Exact tenant grant PLUS fresh CP tenant read. No implied organisation
 * association or activation is derived from the selector.
 */
export default async function TenantContext({ params }: { params: Promise<{ tenantId: string }> }) {
  const { tenantId } = await params;
  const actor = await requireScopedContext("tenant", tenantId);
  const result = await invokeProgressive({ action: "tenant-record", id: tenantId }, actor.accessToken)
    .catch(() => null);
  const tenant = parseTenant(result?.data, tenantId);
  if (!tenant) notFound();
  const details = [
    ["Tenant ID", tenant.tenant_id], ["Legal entity reference", tenant.legal_entity_id],
    ["Data residency", tenant.residency_region], ["Isolation strategy", tenant.isolation_strategy],
    ["Desired state", tenant.desired_state], ["Observed state", tenant.observed_state],
    ["Revision", String(tenant.revision)],
  ];
  return (
    <section aria-labelledby="tenant-title" className="space-y-7">
      <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-xs text-muted-foreground">
        <Link href="/workspace" className="inline-flex items-center gap-1 text-primary"><ArrowLeft aria-hidden="true" className="size-4" />Workspace</Link>
        <span aria-hidden="true">/</span>Tenant context
      </nav>
      <div className="rounded-2xl bg-brand-forest p-6 text-white sm:p-9">
        <p className="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-amber-200"><Layers3 aria-hidden="true" className="size-5" />Tenant · Governed context</p>
        <h1 id="tenant-title" className="mt-4 text-3xl font-bold tracking-tight sm:text-4xl">{tenant.display_name}</h1>
        <p className="mt-3 max-w-2xl text-sm leading-relaxed text-emerald-50">Live Control Plane tenant registry details. This page does not redeem a workload PlatformContext or activate a tenant.</p>
      </div>
      <div className="grid gap-5 lg:grid-cols-[1.2fr_1fr]">
        <Card><CardHeader><Database aria-hidden="true" className="size-6 text-primary" /><CardTitle>Tenant registry</CardTitle>
          <CardDescription>Source: CP tenant record. State values are reported, not inferred.</CardDescription>
        </CardHeader><CardContent>
          <dl>{details.map(([label, value]) => <div key={label} className="border-b border-border py-3 last:border-0">
            <dt className="text-xs font-bold uppercase tracking-wider text-muted-foreground">{label}</dt>
            <dd className="mt-1 break-all text-sm font-semibold">{value}</dd>
          </div>)}</dl>
        </CardContent></Card>
        <div className="space-y-5">
          <Card><CardHeader><Globe2 aria-hidden="true" className="size-6 text-primary" /><CardTitle>Lifecycle comparison</CardTitle></CardHeader>
            <CardContent><div className="flex flex-wrap gap-3">
              <Badge variant="neutral">Desired · {tenant.desired_state}</Badge>
              <Badge variant="neutral">Observed · {tenant.observed_state}</Badge>
            </div>
            <p className="mt-4 text-sm leading-relaxed text-muted-foreground">Desired and observed state are separate. Neither label certifies provisioning readiness or commercial activation.</p></CardContent>
          </Card>
          <Card><CardHeader><ShieldCheck aria-hidden="true" className="size-6 text-primary" /><CardTitle>Enterprise isolation</CardTitle></CardHeader>
            <CardContent><p className="text-sm text-muted-foreground">Selecting a tenant does not confer access to its corporate parent, subsidiaries, markets or other tenants.</p></CardContent>
          </Card>
        </div>
      </div>
      <p className="flex gap-2 text-xs text-muted-foreground"><CircleAlert aria-hidden="true" className="size-4 shrink-0" />This read-only view is not a tenancy authority or a green production-readiness claim.</p>
    </section>
  );
}
