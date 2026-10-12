import Link from "next/link";
import { notFound } from "next/navigation";
import { ArrowLeft, Database, ShieldAlert } from "lucide-react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { parseOrganisationOverview } from "@/features/console/OrganisationIntegrations";
import { parseOrganisationGovernance, reconcileOrganisationGovernance } from "@/features/console/OrganisationGovernancePanel";
import { enterpriseDomainLabels, enterpriseTableHref, parseEnterpriseTableQuery, projectEnterpriseTable } from "@/features/console/enterprise-table-model";
import { requireScopedContext } from "@/server/console/scoped-context";
import { invokeProgressive } from "@/server/bff/progressive";
import {datasetConsoleEnabled} from "@/server/bff/console-datasets";
import {hasExactScopedPermission} from "@/server/console/scoped-context";

export const dynamic = "force-dynamic";
export const revalidate = 0;

/** Server-side-only, caller-authorised bounded enterprise tables.
 * This never accepts a principal/tenant selector, arbitrary CP path or
 * export command. The current CP overview applies independent permissions.
 */
export default async function EnterpriseRecords({
  params, searchParams,
}: {
  params: Promise<{organisationId:string}>;
  searchParams: Promise<Record<string,string|string[]|undefined>>;
}) {
  const {organisationId} = await params;
  const query = parseEnterpriseTableQuery(await searchParams);
  if (!query) notFound();
  const actor = await requireScopedContext("organisation",organisationId);
  const governanceNeeded = query.domain==="mandates" || query.domain==="readiness";
  const [orgResponse,govResponse] = await Promise.all([
    invokeProgressive({action:"organisation-overview",id:organisationId},actor.accessToken)
      .then(r=>r.data).catch(()=>null),
    governanceNeeded ? invokeProgressive({action:"organisation-governance",id:organisationId},actor.accessToken)
      .then(r=>r.data).catch(()=>null) : Promise.resolve(null),
  ]);
  const overview = parseOrganisationOverview(orgResponse,organisationId);
  const governance = governanceNeeded ? reconcileOrganisationGovernance(
    parseOrganisationGovernance(govResponse,organisationId),
    overview?.tenants?.map(t=>t.tenant_id) ?? null,overview?.evaluated_at,
  ) : null;
  const table = projectEnterpriseTable(overview,governance,query);
  const href = (patch:Partial<typeof query>)=>enterpriseTableHref(organisationId,{...query,...patch});
  const current=datasetConsoleEnabled()?await invokeProgressive({action:"effective-authority"},actor.accessToken)
    .then(x=>x.data).catch(()=>null):null;
  const canDataset=!!current && ["organisation.export","organisation.bulk.propose","organisation.bulk.approve"]
    .some(p=>hasExactScopedPermission(current,actor.principalId,p,"organisation",organisationId));
  const formStyle = "h-11 w-full rounded-lg border border-border bg-card px-3 text-sm text-foreground focus-visible:outline-2 focus-visible:outline-focus";
  return (
    <section aria-labelledby="enterprise-tables-title" className="space-y-6">
      <Link href={"/workspace/organisations/"+encodeURIComponent(organisationId)} prefetch={false}
        className="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-primary underline-offset-4 hover:underline">
        <ArrowLeft aria-hidden="true" className="size-4"/>Organisation 360°
      </Link>
      {canDataset&&(      <Link href={"/workspace/organisations/"+encodeURIComponent(organisationId)+"/records/operations"}
        prefetch={false} className="inline-flex min-h-11 items-center text-sm font-semibold text-primary underline-offset-4 hover:underline">
        Governed export and bulk operations (protected environments)
      </Link>)}
      <div className="rounded-2xl bg-brand-forest p-7 text-white sm:p-10">
        <p className="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-amber-200"><Database aria-hidden="true" className="size-5"/>Caller-filtered administrative records</p>
        <h1 id="enterprise-tables-title" className="mt-3 text-3xl font-bold sm:text-4xl">Enterprise data tables</h1>
        <p className="mt-3 max-w-3xl text-sm leading-relaxed text-emerald-50">
          Read-only Control Plane records for this organisation. Tenant associations and each service domain
          require their own effective permissions. Unavailable data is not a zero count or a readiness approval.
        </p>
      </div>
      <Card>
        <CardHeader><CardTitle>Explore governed records</CardTitle>
          <CardDescription>Search, filter, sort and paginate the currently authorised bounded read model. URL options are presentation preferences only.</CardDescription>
        </CardHeader>
        <CardContent>
          <form method="GET" action={"/workspace/organisations/"+encodeURIComponent(organisationId)+"/records"}
            className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <label className="space-y-2 text-sm font-semibold">
              <span>Resource type</span>
              <select name="domain" defaultValue={query.domain} className={formStyle}>
                {Object.entries(enterpriseDomainLabels).map(([value,label])=><option key={value} value={value}>{label}</option>)}
              </select>
            </label>
            <label className="space-y-2 text-sm font-semibold">
              <span>Search visible records</span>
              <input name="q" type="search" defaultValue={query.q} maxLength={80} placeholder="Name, reference or state" className={formStyle}/>
            </label>
            <label className="space-y-2 text-sm font-semibold">
              <span>Exact state filter</span>
              <input name="state" defaultValue={query.state ?? ""} maxLength={40} placeholder="e.g. ACTIVE" pattern="[A-Z_]+" className={formStyle}/>
            </label>
            <label className="space-y-2 text-sm font-semibold">
              <span>Sort order</span>
              <select name="sort" defaultValue={query.sort} className={formStyle}>
                <option value="name-asc">Name ascending</option>
                <option value="name-desc">Name descending</option>
              </select>
            </label>
            <label className="space-y-2 text-sm font-semibold">
              <span>Visible columns</span>
              <select name="columns" defaultValue={query.columns} className={formStyle}>
                <option value="detailed">Detailed</option>
                <option value="compact">Compact</option>
              </select>
            </label>
            <div className="flex items-end gap-3">
              <button type="submit" className="h-11 flex-1 rounded-lg bg-primary px-4 font-semibold text-primary-foreground">Apply filters</button>
              <Link href={enterpriseTableHref(organisationId,{domain:query.domain,q:"",sort:"name-asc",page:1,columns:"detailed"})}
                className="inline-flex h-11 items-center text-sm font-semibold text-primary underline-offset-4 hover:underline">Reset</Link>
            </div>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{enterpriseDomainLabels[query.domain]}</CardTitle>
          <CardDescription>{table.evaluatedAt ? "Source: Control Plane · Evaluated "+table.evaluatedAt : "Authoritative source unavailable"}
            {" · "}Read-only · No export or bulk mutation permission has been granted</CardDescription>
        </CardHeader>
        <CardContent>
          {table.state==="unavailable" ? <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
            <ShieldAlert aria-hidden="true" className="size-5 shrink-0"/>Sources are unavailable, stale or inconsistent. No records can be displayed.
          </p> : table.state==="restricted" ? <p role="status" className="text-sm text-muted-foreground">
            No independently authorised sections are available for this resource type.
          </p> : <>
            {table.suppressedSections>0 ? <p role="status" className="mb-4 text-sm text-muted-foreground">
              {table.suppressedSections} tenant section(s) excluded because permission or authoritative readback was unavailable.
            </p> : null}
            <p className="mb-4 text-xs text-muted-foreground">
              {table.visibleCount} matching authorised records · Page {query.page} of {Math.max(table.totalPages,1)}
            </p>
            {table.items.length===0 ? <p role="status" className="py-8 text-sm text-muted-foreground">
              No matching records within the currently visible sections{query.page>1?". Return to the first page.":"."}
            </p> :
              <div className="overflow-x-auto rounded-xl border border-border">
                <table className="w-full min-w-[42rem] border-collapse text-left text-sm">
                  <caption className="sr-only">{enterpriseDomainLabels[query.domain]} in the current authorised organisation</caption>
                  <thead className="bg-muted text-xs"><tr>
                    <th scope="col" className="p-3">Name</th>
                    <th scope="col" className="p-3">State</th>
                    <th scope="col" className="p-3">Subject</th>
                    {query.columns==="detailed" ? <><th scope="col" className="p-3">Source</th>
                      <th scope="col" className="p-3">Details</th></> : null}
                    <th scope="col" className="p-3">Record</th>
                  </tr></thead>
                  <tbody>{table.items.map(row=><tr key={row.key} className="border-t border-border">
                    <th scope="row" className="break-words p-3 font-semibold">{row.name}</th>
                    <td className="p-3">{row.state}</td>
                    <td className="break-all p-3 text-xs">{row.subject}</td>
                    {query.columns==="detailed" ? <><td className="break-words p-3 text-xs">{row.source}</td>
                      <td className="break-words p-3 text-xs">{row.detail}</td></> : null}
                    <td className="p-3">{row.href ? <Link href={row.href} prefetch={false} className="inline-flex min-h-10 items-center text-primary underline-offset-4 hover:underline">Open</Link> : "—"}</td>
                  </tr>)}</tbody>
                </table>
              </div>}
            <nav aria-label="Table pagination" className="mt-5 flex flex-wrap items-center gap-4 text-sm">
              {query.page>1 ? <Link href={href({page:query.page-1})} prefetch={false} className="min-h-10 text-primary underline-offset-4 hover:underline">Previous</Link> : null}
              {query.page<table.totalPages ? <Link href={href({page:query.page+1})} prefetch={false} className="min-h-10 text-primary underline-offset-4 hover:underline">Next</Link> : null}
            </nav>
          </>}
        </CardContent>
      </Card>
      <p className="rounded-lg border border-border p-4 text-xs text-muted-foreground">
        Each navigation is re-authorised by CP. This bounded server projection is not a global search index
        or a stable high-volume snapshot. Export, bulk operations and saved preferences require separate
        permission and privacy decisions; none are enabled by this page.
      </p>
    </section>
  );
}
