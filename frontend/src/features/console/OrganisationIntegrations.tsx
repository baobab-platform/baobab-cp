import { z } from "zod";
import { Badge } from "@/components/ui/badge";
import { OrganisationTenantIntegrations, tenantOverviewSchema } from "./OrganisationTenantIntegrations";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Building2, GitBranch, Landmark, Network, ShieldCheck } from "lucide-react";

const state = z.string().min(1);
const link = z.object({
  id: state, source_organisation_id: state, target_organisation_id: state,
  relationship_type: state, verification_state: state, status: state,
  direct_or_derived: state, source_authority: state, effective_from: state,
}).passthrough();
const legal = z.object({
  legal_entity_id: state, legal_name: state, legal_status: state,
  verification_state: state, source_authority: state,
  jurisdiction_of_incorporation: z.string().optional(),
  verified_at: z.string().optional(), evidence_count: z.number().int().nonnegative(),
}).passthrough();
const group = z.object({
  id: state, corporate_group_id: state, status: state, derived_at: state,
  group_role: z.string().optional(),
}).passthrough();
const platform = z.object({
  id: state, platform_id: state, relationship_type: state, status: state,
  verification_state: state, source_authority: state, effective_from: state,
}).passthrough();
const overview = z.object({
  organisation_id: state,
  evaluated_at: state,
  profile: z.object({
    canonical_entity_id: state, display_name: state,
    verification_state: state, source_authority: state, status: state,
  }).passthrough(),
  legal_entities: z.array(legal).max(100),
  corporate_relationships: z.array(link).max(100),
  corporate_group_memberships: z.array(group).max(100),
  platform_relationships: z.array(platform).max(100),
  tenants: z.array(tenantOverviewSchema).max(100).optional(),
}).passthrough();

export type OrganisationOverviewProjection = z.infer<typeof overview>;

/** No foreign subject substitutions and no endpoint-dependent optimistic data. */
export function parseOrganisationOverview(input: unknown, expectedId: string): OrganisationOverviewProjection | null {
  const result = overview.safeParse(input);
  if (!result.success || result.data.organisation_id !== expectedId ||
      result.data.profile.canonical_entity_id !== expectedId ||
      result.data.legal_entities.some((record) => !record.legal_entity_id) ||
      result.data.corporate_relationships.some((record) =>
        record.source_organisation_id !== expectedId && record.target_organisation_id !== expectedId) ||
      (result.data.tenants ?? []).some((tenant) =>
        tenant.mapping_role !== "PRIMARY_ORGANISATION" ||
        (!tenant.markets_visible && tenant.market_participations.length > 0) ||
        (!tenant.subscriptions_visible && tenant.subscriptions.length > 0) ||
        (!tenant.estates_visible && tenant.digital_estates.length > 0))) {
    return null;
  }
  return result.data;
}

function VerificationTag({ value }: { value: string }) {
  const variant = value === "VERIFIED" ? "success"
    : value === "CONFLICTED" || value === "REJECTED" ? "failure"
    : value === "EXPIRED" ? "expired"
    : value === "PENDING_REVIEW" ? "warning" : "neutral";
  return <Badge variant={variant}>{value.replaceAll("_", " ")}</Badge>;
}
function Empty({ label }: { label: string }) {
  return <p className="text-sm leading-relaxed text-muted-foreground">{label}</p>;
}

export function OrganisationIntegrations({
  record, available,
}: { record: OrganisationOverviewProjection | null; available: boolean }) {
  if (!available || !record) return (
    <Card>
      <CardHeader>
        <CardTitle>Governed organisation relationships</CardTitle>
        <CardDescription>Read-only Control Plane source projection</CardDescription>
      </CardHeader>
      <CardContent>
        <p role="status" className="text-sm leading-relaxed text-muted-foreground">
          The scoped organisation overview is unavailable or access has been denied.
          No legal status, corporate control or operational readiness is assumed.
        </p>
      </CardContent>
    </Card>
  );
  return (
    <section aria-label="Governed organisation relationships" className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs font-bold uppercase tracking-wider text-primary">Source-backed enterprise records</p>
          <h2 className="mt-2 text-2xl font-bold tracking-tight">Relationships and legal identity</h2>
        </div>
        <span className="text-xs text-muted-foreground">CP evaluated: <time>{record.evaluated_at}</time></span>
      </div>
      <div className="grid gap-5 xl:grid-cols-2">
        <Card>
          <CardHeader><Landmark aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Legal entities and incorporation evidence</CardTitle>
            <CardDescription>Authoritative profile status; independently verified only if marked VERIFIED</CardDescription>
          </CardHeader>
          <CardContent>
            {record.legal_entities.length === 0 ? <Empty label="No legal-entity profiles associated with this organisation." /> :
              <ul className="divide-y divide-border">{record.legal_entities.map((item) => (
                <li className="space-y-2 py-4 first:pt-0" key={item.legal_entity_id}>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <strong className="min-w-0 break-words text-sm">{item.legal_name}</strong>
                    <VerificationTag value={item.verification_state} />
                  </div>
                  <p className="break-all text-xs text-muted-foreground">{item.legal_entity_id}</p>
                  <p className="text-xs text-muted-foreground">Jurisdiction: {item.jurisdiction_of_incorporation || "Not recorded"} · Registry status: {item.legal_status}</p>
                  <p className="text-xs text-muted-foreground">Source: {item.source_authority} · Evidence references: {item.evidence_count} · Verified: {item.verified_at ?? "Not verified"}</p>
                </li>
              ))}</ul>}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><GitBranch aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Corporate ownership and control</CardTitle>
            <CardDescription>Directional relationships; claims, derived facts and verified evidence remain distinct</CardDescription>
          </CardHeader>
          <CardContent>
            {record.corporate_relationships.length === 0 ? <Empty label="No corporate relationship records within the evaluated period." /> :
              <ul className="divide-y divide-border">{record.corporate_relationships.map((item) => (
                <li className="space-y-2 py-4 first:pt-0" key={item.id}>
                  <div className="flex flex-wrap items-center gap-2">
                    <strong className="text-sm">{item.relationship_type.replaceAll("_", " ")}</strong>
                    <VerificationTag value={item.verification_state} />
                  </div>
                  <p className="break-all text-xs text-muted-foreground">{item.source_organisation_id} → {item.target_organisation_id}</p>
                  <p className="text-xs text-muted-foreground">{item.direct_or_derived} · {item.status} · Source: {item.source_authority}</p>
                  <p className="text-xs text-muted-foreground">Effective from: {item.effective_from}</p>
                </li>
              ))}</ul>}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><Building2 aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Corporate group membership</CardTitle>
            <CardDescription>Membership is not a permission grant or proof of subsidiary operational control</CardDescription>
          </CardHeader>
          <CardContent>
            {record.corporate_group_memberships.length === 0 ? <Empty label="No current corporate-group membership records." /> :
              <ul className="divide-y divide-border">{record.corporate_group_memberships.map((item) => (
                <li className="space-y-2 py-4 first:pt-0" key={item.id}>
                  <strong className="block break-all text-sm">{item.corporate_group_id}</strong>
                  <p className="text-xs text-muted-foreground">Role: {item.group_role || "Not assigned"} · Status: {item.status}</p>
                  <p className="text-xs text-muted-foreground">Derived at: {item.derived_at}</p>
                </li>
              ))}</ul>}
          </CardContent>
        </Card>
        <Card>
          <CardHeader><Network aria-hidden="true" className="size-6 text-primary" />
            <CardTitle>Baobab platform affiliation</CardTitle>
            <CardDescription>Platform relationship is independent of tenant admission and activation</CardDescription>
          </CardHeader>
          <CardContent>
            {record.platform_relationships.length === 0 ? <Empty label="No platform affiliation records within the evaluated period." /> :
              <ul className="divide-y divide-border">{record.platform_relationships.map((item) => (
                <li className="space-y-2 py-4 first:pt-0" key={item.id}>
                  <div className="flex flex-wrap items-center gap-2">
                    <strong className="text-sm">{item.relationship_type.replaceAll("_", " ")}</strong>
                    <VerificationTag value={item.verification_state} />
                  </div>
                  <p className="break-all text-xs text-muted-foreground">Platform: {item.platform_id}</p>
                  <p className="text-xs text-muted-foreground">{item.status} · Source: {item.source_authority}</p>
                </li>
              ))}</ul>}
          </CardContent>
        </Card>
      </div>
      <OrganisationTenantIntegrations tenants={record.tenants ?? []} />
      <p className="flex gap-2 rounded-lg border border-border bg-card p-4 text-xs leading-relaxed text-muted-foreground">
        <ShieldCheck aria-hidden="true" className="size-4 shrink-0 text-primary" />
        No organisation, legal person, corporate group, mandate, platform affiliation, tenant,
        market or provider becomes authorised merely by appearing in this read-only projection.
      </p>
    </section>
  );
}
