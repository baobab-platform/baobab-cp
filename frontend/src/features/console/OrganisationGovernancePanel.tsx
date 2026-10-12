import { z } from "zod";
import { Activity, Clock3, FileCheck2, Fingerprint, LockKeyhole, ShieldCheck, ShieldAlert } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

const mandate = z.object({
  mandate_id:z.string().uuid(), tenant_id:z.string(), legal_entity_id:z.string(),
  status:z.enum(["PENDING","ACTIVE","SUSPENDED","REVOKED","EXPIRED"]),
  roles:z.array(z.string()).min(1), effective_from:z.string(),
  effective_to:z.string().optional(), provenance:z.string(),
  evidence_count:z.number().int().nonnegative(),
  decision:z.enum(["APPROVE","REJECT"]).optional(),
  decided_at:z.string().optional(),approved_at:z.string().optional(),
  verification_reference_present:z.boolean(),
});
const readiness=z.object({
  tenant_id:z.string(),state:z.enum(["READY","BLOCKED","STALE","UNKNOWN"]),
  source:z.enum(["IMMUTABLE_PROVISIONING_SNAPSHOT","NO_SNAPSHOT"]),
  evaluated_at:z.string(),overall_ready:z.boolean(),
  desired_state_version:z.number().int().nonnegative(),
  observed_state_version:z.number().int().nonnegative(),
  blocking_reasons:z.array(z.string()).max(30),
});
const governance=z.object({
  organisation_id:z.string().uuid(), evaluated_at:z.string(),
  tenants:z.array(z.object({
    tenant_id:z.string(),mandates_visible:z.boolean(),
    readiness_visible:z.boolean(),mandates:z.array(mandate).max(100),
    readiness:readiness.optional(),
  })).max(100),
});
export type OrganisationGovernance=z.infer<typeof governance>;

export function parseOrganisationGovernance(input: unknown, id: string):OrganisationGovernance|null {
  const parsed=governance.safeParse(input);
  if(!parsed.success || parsed.data.organisation_id!==id ||
     parsed.data.tenants.some(t=>
       (!t.mandates_visible && t.mandates.length>0) ||
       (!t.readiness_visible && t.readiness!==undefined) ||
       (t.readiness_visible && t.readiness===undefined) ||
       t.mandates.some(m=>m.tenant_id!==t.tenant_id) ||
       (t.readiness!==undefined && t.readiness.tenant_id!==t.tenant_id) ||
       (t.readiness?.source==="NO_SNAPSHOT" && t.readiness.state!=="UNKNOWN")))return null;
  return parsed.data;
}
const labels:Record<string,"neutral"|"success"|"warning"|"failure">={
  READY:"success",BLOCKED:"failure",STALE:"warning",UNKNOWN:"neutral",
  ACTIVE:"success",SUSPENDED:"warning",REVOKED:"failure",EXPIRED:"failure",
};

export function OrganisationGovernancePanel({record}: {record:OrganisationGovernance|null}) {
  return <section aria-label="Independent mandate and readiness evidence" className="space-y-5">
    <div>
      <p className="text-xs font-bold uppercase tracking-wider text-primary">Independent operating authority</p>
      <h2 className="mt-2 text-2xl font-bold">Mandates and evidence-backed readiness</h2>
      <p className="mt-2 max-w-3xl text-sm leading-relaxed text-muted-foreground">
        Legal registration, mandate approval, active mandate scope and service readiness are distinct decisions.
        This panel only displays records independently authorised by Control Plane.
      </p>
    </div>
    {!record ? <Card><CardContent>
      <p role="status" className="text-sm text-muted-foreground">
        Governance readback is unavailable, not configured or access was denied.
        Mandates and readiness are not inferred from organisation status.
      </p>
    </CardContent></Card> : record.tenants.length===0 ?
      <Card><CardContent><p className="text-sm text-muted-foreground">
        No accessible PRIMARY-linked tenant governance records were returned. This does not establish that no other mandates exist.
      </p></CardContent></Card> :
      <div className="space-y-5">{record.tenants.map(t=><Card key={t.tenant_id}>
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><Fingerprint aria-hidden="true" className="size-5 text-primary" />Tenant governance</CardTitle>
          <CardDescription className="break-all">{t.tenant_id} · Evaluated {record.evaluated_at}</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-5 lg:grid-cols-2">
            <section aria-label={"Legal mandates for "+t.tenant_id} className="space-y-3 rounded-xl border border-border p-4">
              <h3 className="flex items-center gap-2 font-semibold"><FileCheck2 aria-hidden="true" className="size-5 text-primary"/>Operating legal-actor mandates</h3>
              {!t.mandates_visible ? <p className="flex gap-2 text-sm text-muted-foreground">
                <LockKeyhole aria-hidden="true" className="size-4 shrink-0"/>Independent mandate.view permission is required.
              </p> : t.mandates.length===0 ?
                <p className="text-sm text-muted-foreground">No mandates returned for this authorised tenant.</p> :
                <ol className="divide-y divide-border">{t.mandates.map(m=><li key={m.mandate_id} className="space-y-2 py-4 first:pt-0">
                  <div className="flex flex-wrap gap-2">
                    <Badge variant={labels[m.status]??"neutral"}>{m.status}</Badge>
                    <Badge variant="neutral">{m.decision??"NO DECISION RECORDED"}</Badge>
                  </div>
                  <p className="break-all text-xs font-semibold">{m.legal_entity_id}</p>
                  <p className="text-xs text-muted-foreground">Roles: {m.roles.join(", ")}</p>
                  <p className="text-xs text-muted-foreground">Provenance: {m.provenance} · Evidence references: {m.evidence_count}</p>
                  <p className="text-xs text-muted-foreground">Effective from {m.effective_from}{m.effective_to?" until "+m.effective_to:""}</p>
                  <p className="text-xs text-muted-foreground">Verification reference recorded: {m.verification_reference_present?"Yes":"No"} · Decision at: {m.decided_at??"Not recorded"}</p>
                </li>)}</ol>}
              <p className="text-xs text-muted-foreground">Approval does not activate a mandate. Effective roles remain subject to separate current-time Control Plane policy evaluation.</p>
            </section>
            <section aria-label={"Readiness for "+t.tenant_id} className="space-y-3 rounded-xl border border-border p-4">
              <h3 className="flex items-center gap-2 font-semibold"><Activity aria-hidden="true" className="size-5 text-primary"/>Provisioning readiness evidence</h3>
              {!t.readiness_visible ? <p className="flex gap-2 text-sm text-muted-foreground">
                <LockKeyhole aria-hidden="true" className="size-4 shrink-0"/>Independent readiness.view permission is required.
              </p> : !t.readiness ? <p className="text-sm text-muted-foreground">Readiness evidence unavailable.</p> :
                <>
                  <Badge variant={labels[t.readiness.state]??"neutral"}>{t.readiness.state}</Badge>
                  <p className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Clock3 aria-hidden="true" className="size-4"/> Snapshot: {t.readiness.evaluated_at}
                  </p>
                  <p className="text-xs text-muted-foreground">Source: {t.readiness.source.replaceAll("_"," ")} · Desired revision {t.readiness.desired_state_version} · Observed revision {t.readiness.observed_state_version}</p>
                  {t.readiness.blocking_reasons.length ? <ul className="list-disc space-y-1 pl-5 text-xs text-muted-foreground">{t.readiness.blocking_reasons.map((reason,i)=><li key={i}>{reason}</li>)}</ul> : <p className="text-xs text-muted-foreground">No blocking reasons in the selected snapshot.</p>}
                  {t.readiness.state==="STALE"||t.readiness.state==="UNKNOWN" ? <p role="status" className="flex gap-2 text-xs text-muted-foreground"><ShieldAlert aria-hidden="true" className="size-4 shrink-0"/>Current readiness cannot be established from this evidence.</p>:null}
                </>}
              <p className="text-xs text-muted-foreground">Provisioning snapshots are immutable observations, not independent certification of PSP, cloud or provider production readiness.</p>
            </section>
          </div>
        </CardContent>
      </Card>)}</div>}
    <p className="flex gap-2 rounded-lg border border-border bg-card p-4 text-xs leading-relaxed text-muted-foreground">
      <ShieldCheck aria-hidden="true" className="size-4 shrink-0 text-primary"/>
      No mandate, verified company record or readiness badge confers access to another tenant, payment provider or digital estate.
      Control Plane must authorise every operation again.
    </p>
  </section>;
}
