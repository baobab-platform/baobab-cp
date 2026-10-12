import Link from "next/link";
import { ArrowRight, Building2, ChevronRight, Filter, FolderSearch, Search, ShieldCheck, XCircle } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { directoryPageHref, parseDirectoryPageRequest, parseDirectoryPageResult } from "@/features/console/directory-page-model";
import { invokeProgressive } from "@/server/bff/progressive";
import { authenticatedConsoleActor } from "@/server/console/scoped-context";

export const dynamic = "force-dynamic";

type SearchParams = Record<string, string | string[] | undefined>;

/** Server-side paging and search over the caller's CURRENT exact grants.
 * Sorting, cursor and free text never authorise or widen the CP read.
 */
export default async function OrganisationsDirectory({
  searchParams,
}: { searchParams: Promise<SearchParams> }) {
  const raw = await searchParams;
  const criteria = parseDirectoryPageRequest(raw);
  const actor = await authenticatedConsoleActor();
  const data = criteria ? await invokeProgressive(
    { action: "organisation-directory-page", page: criteria }, actor.accessToken,
  ).then((response) => response.data).catch(() => null) : null;
  const result = criteria ? parseDirectoryPageResult(data) : null;
  const hasFilters = Boolean(criteria?.q || criteria?.verification || criteria?.status || criteria?.after);
  const baseline = criteria ? { ...criteria, after: undefined } : {};
  return (
    <section aria-labelledby="directory-title" className="space-y-7">
      <div className="rounded-2xl bg-brand-forest p-7 text-white sm:p-10">
        <div className="mb-4 flex items-center gap-2 text-amber-200">
          <FolderSearch aria-hidden="true" className="size-6" />
          <span className="text-xs font-bold uppercase tracking-wider">Governed enterprise directory</span>
        </div>
        <h1 id="directory-title" className="text-3xl font-bold tracking-tight sm:text-4xl">
          Organisation directory
        </h1>
        <p className="mt-3 max-w-2xl text-sm leading-relaxed text-emerald-50">
          Search and compare authorised organisational records. Every search and page is evaluated
          against your current Control Plane permissions—not a global corporate or tenant index.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><Filter aria-hidden="true" className="size-5 text-primary" />Explore authorised organisations</CardTitle>
          <CardDescription>Server-side search, exact-scope filtering and name sorting</CardDescription>
        </CardHeader>
        <CardContent>
          <form method="GET" action="/workspace/organisations" className="grid items-end gap-4 md:grid-cols-2 xl:grid-cols-[minmax(14rem,2fr)_1fr_1fr_1fr_1fr_auto]">
            <div className="space-y-2">
              <label htmlFor="org-query" className="block text-xs font-bold uppercase tracking-wide text-muted-foreground">Organisation name</label>
              <input id="org-query" name="q" type="search" defaultValue={criteria?.q ?? ""} maxLength={80}
                placeholder="Search current access…" className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm text-foreground focus-visible:outline-2 focus-visible:outline-focus"/>
            </div>
            <div className="space-y-2">
              <label htmlFor="org-verification" className="block text-xs font-bold uppercase tracking-wide text-muted-foreground">Verification</label>
              <select id="org-verification" name="verification" defaultValue={criteria?.verification ?? ""}
                className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm focus-visible:outline-2 focus-visible:outline-focus">
                <option value="">All states</option>
                {["UNVERIFIED", "PENDING_REVIEW", "VERIFIED", "CONFLICTED", "REJECTED", "EXPIRED"].map((value) =>
                  <option key={value} value={value}>{value.replaceAll("_", " ")}</option>)}
              </select>
            </div>
            <div className="space-y-2">
              <label htmlFor="org-status" className="block text-xs font-bold uppercase tracking-wide text-muted-foreground">Record status</label>
              <select id="org-status" name="status" defaultValue={criteria?.status ?? ""}
                className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm focus-visible:outline-2 focus-visible:outline-focus">
                <option value="">All statuses</option>
                {["ACTIVE", "PENDING", "SUSPENDED", "DRAFT", "ENDED"].map((value) =>
                  <option key={value} value={value}>{value}</option>)}
              </select>
            </div>
            <div className="space-y-2">
              <label htmlFor="org-sort" className="block text-xs font-bold uppercase tracking-wide text-muted-foreground">Sort by</label>
              <select id="org-sort" name="sort" defaultValue={criteria?.sort ?? "name-asc"}
                className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm focus-visible:outline-2 focus-visible:outline-focus">
                <option value="name-asc">Name A → Z</option>
                <option value="name-desc">Name Z → A</option>
              </select>
            </div>
            <div className="space-y-2">
              <label htmlFor="org-page-size" className="block text-xs font-bold uppercase tracking-wide text-muted-foreground">Rows per page</label>
              <select id="org-page-size" name="limit" defaultValue={String(criteria?.limit ?? 25)}
                className="h-11 w-full rounded-lg border border-border bg-card px-3 text-sm focus-visible:outline-2 focus-visible:outline-focus">
                {[10,25,50].map((value) => <option key={value} value={value}>{value}</option>)}
              </select>
            </div>
            <button type="submit" className="inline-flex h-11 items-center justify-center gap-2 rounded-lg bg-primary px-5 text-sm font-semibold text-primary-foreground hover:opacity-90 focus-visible:outline-2 focus-visible:outline-focus">
              <Search aria-hidden="true" className="size-4" />Search
            </button>
          </form>
          {hasFilters ? <Link href="/workspace/organisations" className="mt-4 inline-flex items-center gap-2 text-xs font-semibold text-primary underline-offset-4 hover:underline">
            <XCircle aria-hidden="true" className="size-4" />Clear filters
          </Link> : null}
        </CardContent>
      </Card>

      {!criteria ? (
        <Card><CardHeader><CardTitle>Invalid directory filters</CardTitle></CardHeader>
          <CardContent><p role="alert" className="text-sm text-muted-foreground">The requested filters or page cursor are invalid. <Link className="text-primary underline" href="/workspace/organisations">Start a new search</Link>.</p></CardContent></Card>
      ) : !result ? (
        <Card><CardHeader><CardTitle>Directory read unavailable</CardTitle></CardHeader>
          <CardContent><p role="status" className="text-sm text-muted-foreground">
            The search could not be authorised or verified, or its cursor has expired. No unverified entries are displayed.
            <Link className="ml-2 font-semibold text-primary underline" href={directoryPageHref(baseline)}>Start from the first page</Link>.
          </p></CardContent></Card>
      ) : (
        <Card>
          <CardHeader>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2">
                <ShieldCheck aria-hidden="true" className="size-5 text-primary" />
                Results from current authority
              </CardTitle>
              <Badge variant="neutral">{result.total_visible} matching records</Badge>
            </div>
            <CardDescription>Source: Control Plane · Evaluated {result.evaluated_at}. Results may change when grants or records change between pages.</CardDescription>
          </CardHeader>
          <CardContent>
            {result.items.length === 0 ? (
              <p role="status" className="py-10 text-center text-sm leading-relaxed text-muted-foreground">
                No matching organisations were returned within your exact permissions.
                A corporate relationship alone does not grant access to another organisation.
              </p>
            ) : (
              <div className="overflow-x-auto rounded-xl border border-border">
                <table className="w-full min-w-[46rem] border-collapse text-left text-sm">
                  <caption className="sr-only">Authorised organisation search results, ordered by {result.sort.replace("-", " ")}</caption>
                  <thead className="bg-muted/60 text-xs font-bold uppercase tracking-wide text-muted-foreground">
                    <tr>
                      <th scope="col" className="px-4 py-3">Organisation</th>
                      <th scope="col" className="px-4 py-3">Record state</th>
                      <th scope="col" className="px-4 py-3">Verification</th>
                      <th scope="col" className="px-4 py-3">Jurisdiction</th>
                      <th scope="col" className="px-4 py-3">Source authority</th>
                      <th scope="col" className="px-4 py-3 text-right">View</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {result.items.map((org) => (
                      <tr key={org.organisation_id} className="transition-colors hover:bg-muted/50">
                        <th scope="row" className="px-4 py-4 text-left font-semibold">
                          <div className="flex items-start gap-3">
                            <Building2 aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />
                            <div className="space-y-1">
                              <p>{org.display_name}</p>
                              <p className="break-all font-mono text-xs font-normal text-muted-foreground">{org.organisation_id}</p>
                            </div>
                          </div>
                        </th>
                        <td className="px-4 py-4"><Badge variant="neutral">{org.status}</Badge></td>
                        <td className="px-4 py-4">
                          <Badge variant="neutral">{org.verification_state.replaceAll("_", " ")}</Badge>
                        </td>
                        <td className="px-4 py-4 text-muted-foreground">{org.jurisdiction || "Not recorded"}</td>
                        <td className="px-4 py-4 text-muted-foreground">{org.source_authority}</td>
                        <td className="px-4 py-4 text-right">
                          <Link aria-label={"View Organisation 360 for " + org.display_name}
                            className="inline-flex min-h-10 items-center gap-1 font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-focus"
                            href={"/workspace/organisations/" + encodeURIComponent(org.organisation_id)}>
                            Open <ChevronRight aria-hidden="true" className="size-4" />
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="mt-5 flex flex-wrap items-center justify-between gap-4">
              <p className="text-xs text-muted-foreground">
                Showing {result.items.length} on this page · {result.total_visible} matching current grants · Page size {result.page_size}
              </p>
              <div className="flex flex-wrap items-center gap-4">
                {criteria.after ? <Link href={directoryPageHref(baseline)} className="text-sm font-semibold text-primary underline-offset-4 hover:underline">
                  First page
                </Link> : null}
                {result.next_after ? (
                  <Link href={directoryPageHref({ ...baseline, after: result.next_after })}
                    className="inline-flex min-h-11 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-primary-foreground hover:opacity-90 focus-visible:outline-2 focus-visible:outline-focus">
                    Next page <ArrowRight aria-hidden="true" className="size-4" />
                  </Link>
                ) : <span className="text-xs text-muted-foreground">End of authorised results</span>}
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </section>
  );
}
