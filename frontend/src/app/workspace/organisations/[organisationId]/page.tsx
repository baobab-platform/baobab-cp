import Link from "next/link";
import { notFound } from "next/navigation";
import {
  ArrowLeft, Building2, FileCheck2,
  Fingerprint, History, Info, Network, ShieldCheck,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { parseOrganisation, parseOrganisationIamLinks, parseOrganisationAudit } from "@/features/console/organisation-model";
import { OrganisationIntegrations, parseOrganisationOverview } from "@/features/console/OrganisationIntegrations";
import { OrganisationGovernancePanel, parseOrganisationGovernance, reconcileOrganisationGovernance } from "@/features/console/OrganisationGovernancePanel";
import { hasExactScopedPermission, requireScopedContext } from "@/server/console/scoped-context";
import { invokeProgressive } from "@/server/bff/progressive";

export const dynamic = "force-dynamic";

function Detail({ label, value }: { label: string; value: string | number | undefined }) {
  return <div className="min-w-0 border-b border-border py-3 last:border-b-0">
    <dt className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{label}</dt>
    <dd className="mt-1 break-all text-sm font-semibold text-foreground">{value ?? "Not recorded"}</dd>
  </div>;
}

/** Organisation 360: no aggregated invented relationships or verified legal
 * claims. Caller-bound exact grant and CP canonical read are both required.
 */
export default async function Organisation360({
  params,
}: { params: Promise<{ organisationId: string }> }) {
  const { organisationId } = await params;
  const actor = await requireScopedContext("organisation", organisationId);
  const record = await invokeProgressive(
    { action: "organisation-entity", id: organisationId }, actor.accessToken,
  ).catch(() => null);
  const organisation = parseOrganisation(record?.data, organisationId);
  if (!organisation) notFound();

  // Audit and IAM references are independent reads; never infer their state
  // from an organisation's status or from an applicant declaration.
  const auditGrants = await invokeProgressive({ action: "effective-authority" }, actor.accessToken)
    .then((result) => result.data).catch(() => null);
  const canAudit = hasExactScopedPermission(auditGrants, actor.principalId,
    "audit.view", "organisation", organisationId);
  const [iamRead, auditRead] = await Promise.all([
    invokeProgressive({ action: "organisation-iam-links", id: organisationId }, actor.accessToken)
      .then((response) => response.data).catch(() => null),
    canAudit ? invokeProgressive({ action: "organisation-audit", id: organisationId }, actor.accessToken)
      .then((response) => response.data).catch(() => null) : Promise.resolve(null),
  ]);
  const overviewRead = await invokeProgressive({ action: "organisation-overview", id: organisationId }, actor.accessToken)
    .then((response) => response.data).catch(() => null);
  const overview = parseOrganisationOverview(overviewRead, organisationId);
  const governanceRead = await invokeProgressive({ action: "organisation-governance", id: organisationId }, actor.accessToken)
    .then((response) => response.data).catch(() => null);
  const governance = reconcileOrganisationGovernance(
    parseOrganisationGovernance(governanceRead, organisationId),
    overview?.tenants?.map((tenant) => tenant.tenant_id) ?? null,
    overview?.evaluated_at,
  );
  const iam = parseOrganisationIamLinks(iamRead, organisationId);
  const audit = canAudit ? parseOrganisationAudit(auditRead) : null;

  return (
    <section aria-labelledby="organisation-title" className="space-y-7">
      <nav aria-label="Breadcrumb" className="flex items-center gap-2 text-xs text-muted-foreground">
        <Link href="/workspace" className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline"><ArrowLeft aria-hidden="true" className="size-4" />Workspace</Link>
        <span aria-hidden="true">/</span>Organisation 360°
      </nav>
      <div className="overflow-hidden rounded-2xl bg-brand-forest p-6 text-white sm:p-9">
        <div className="mb-4 flex flex-wrap items-center gap-3 text-amber-200">
          <Building2 aria-hidden="true" className="size-6" />
          <span className="text-xs font-bold uppercase tracking-widest">Organisation 360° · Canonical record</span>
        </div>
        <h1 id="organisation-title" className="break-words text-3xl font-bold tracking-tight sm:text-4xl">{organisation.display_name ?? "Organisation record"}</h1>
        <p className="mt-3 max-w-2xl text-sm leading-relaxed text-emerald-50">
          Governed identity and supporting platform references, read directly from the Control Plane. Corporate affiliation, legal incorporation, mandate acceptance and tenant activation are separate decisions.
        </p>
        <div className="mt-5 flex flex-wrap items-center gap-3">
          <Badge variant="neutral">{organisation.status}</Badge>
          <span className="rounded-full border border-emerald-300 px-3 py-1 text-xs">Revision {organisation.version}</span>
        </div>
      </div>
      <div className="grid gap-5 lg:grid-cols-[1.35fr_1fr]">
        <Card><CardHeader><Fingerprint aria-hidden="true" className="size-6 text-primary" />
          <CardTitle>Canonical identity</CardTitle>
          <CardDescription>Source: CP canonical entity registry · not a live corporate registry verification</CardDescription>
        </CardHeader><CardContent>
          <dl><Detail label="Organisation ID" value={organisation.id} />
            <Detail label="Canonical key" value={organisation.canonical_key} />
            <Detail label="Registered entity type" value={organisation.entity_type} />
            <Detail label="Classification" value={organisation.classification} />
            <Detail label="Record authority" value={organisation.authority} />
            <Detail label="Record updated" value={organisation.updated_at} /></dl>
        </CardContent></Card>
        <div className="space-y-5">
          <Card><CardHeader><Network aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Ownership references</CardTitle>
            <CardDescription>Identifiers are references, not proof of current legal control</CardDescription>
          </CardHeader><CardContent>
            <dl><Detail label="Owner legal entity" value={organisation.owner_legal_entity_id} />
              <Detail label="Owner tenant" value={organisation.owner_tenant_id} /></dl>
          </CardContent></Card>
          <Card><CardHeader><ShieldCheck aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Legal and mandate evidence</CardTitle>
          </CardHeader><CardContent>
            <p className="text-sm leading-relaxed text-muted-foreground">Incorporation evidence states are shown in the linked legal-entity panel below when the CP overview is available. Operating legal-actor mandates still require separate governed acceptance and source evidence: no applicant claim or IAM link is represented as an accepted mandate.</p>
          </CardContent></Card>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border bg-card p-5">
        <div><h2 className="text-lg font-bold">Enterprise resource tables</h2>
          <p className="mt-1 text-sm text-muted-foreground">Search authorised tenants, markets, subscriptions, Digital Estates, legal and governance summaries.</p>
        </div>
        <Link href={"/workspace/organisations/"+encodeURIComponent(organisationId)+"/records"}
          prefetch={false} className="inline-flex min-h-11 items-center rounded-lg bg-primary px-5 text-sm font-semibold text-primary-foreground">
          Open data tables
        </Link>
      </div>
      <OrganisationIntegrations record={overview} available={overview !== null} />
      <OrganisationGovernancePanel record={governance} />
      <Card><CardHeader><FileCheck2 aria-hidden="true" className="size-6 text-primary" />
        <CardTitle>Identity federation references</CardTitle>
        <CardDescription>IAM organisation links do not themselves grant Console authority</CardDescription>
      </CardHeader><CardContent>
        {iam === null ? <p role="status" className="text-sm text-muted-foreground">IAM reference records unavailable or access denied.</p>
          : iam.length === 0 ? <p className="text-sm text-muted-foreground">No IAM organisation references recorded.</p>
          : <div className="overflow-x-auto"><table className="w-full min-w-[32rem] border-collapse text-left text-sm">
            <thead><tr className="border-b border-border text-xs text-muted-foreground"><th className="py-3">Provider</th><th className="py-3">Link status</th><th className="py-3">Source authority</th><th className="py-3">Effective</th></tr></thead>
            <tbody>{iam.map((item) => <tr key={item.id} className="border-b border-border">
              <td className="py-3 font-semibold">{item.provider}</td><td className="py-3">{item.status}</td>
              <td className="py-3">{item.source_authority}</td><td className="py-3">{item.effective_from}</td>
            </tr>)}</tbody></table></div>}
      </CardContent></Card>
      <Card><CardHeader><History aria-hidden="true" className="size-6 text-primary" />
        <CardTitle>Recent organisation audit</CardTitle>
        <CardDescription>CP audit lineage · up to 25 records · read-only</CardDescription>
      </CardHeader><CardContent>
        {!canAudit ? <p className="text-sm text-muted-foreground">An exact, effective organisation audit grant is required to view this section.</p>
          : audit === null ? <p role="status" className="text-sm text-muted-foreground">Audit readback is currently unavailable or access denied.</p>
          : audit.items.length === 0 ? <p className="text-sm text-muted-foreground">No audit entries returned for this organisation.</p>
          : <ol className="divide-y divide-border">{audit.items.map((event) => <li key={event.audit_id} className="grid gap-1 py-3 text-sm sm:grid-cols-[1fr_auto]">
            <span className="font-semibold">{event.action}</span><time className="text-xs text-muted-foreground">{event.occurred_at}</time>
            <span className="text-xs text-muted-foreground">Actor: {event.actor.actor_type}</span>
          </li>)}</ol>}
      </CardContent></Card>
      <div className="flex gap-3 rounded-lg border border-border bg-card p-4 text-sm leading-relaxed text-muted-foreground">
        <Info aria-hidden="true" className="size-5 shrink-0 text-primary" />
        <p>The corporate and legal panels display Control Plane source-backed facts and their verification states, not automatically verified status. Linked tenants, markets, subscriptions, Digital Estates, recorded legal-actor mandates and immutable provisioning readiness are independently scoped. Any unavailable source is not evidence of absence, and provider certification still requires independent evidence.</p>
      </div>
    </section>
  );
}
