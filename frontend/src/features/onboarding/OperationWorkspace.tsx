"use client";

import { useState } from "react";

import { readWorkspace } from "./workspace-client";
import styles from "@/app/workspace/workspace.module.css";

type OperationView = {
  operation_id?: string;
  status?: string;
  operation_type?: string;
  current_phase?: string;
  revision?: number;
  plan_digest?: string;
  progress?: { completed_steps?: number; total_steps?: number };
  result?: { resource_state?: string; resource_type?: string };
};
type ProvisioningView = {
  tenant_id: string;
  tenant_provisioning_id: string;
  revision: number;
  state: string;
  readiness_status: string;
  operation_id?: string;
  blocking_reasons?: unknown[];
  current_plan?: { plan_digest: string; plan_version: number; stale: boolean };
  approval?: { decision: string; plan_digest: string };
};
type PlanView = { plan_digest: string; desired_state_digest?: string; desired_state_version?: number };
type ReadinessView = { status: string; evaluated_at?: string; snapshots?: unknown[] };

export function OperationWorkspace() {
  const [id, setId] = useState("");
  const [operation, setOperation] = useState<OperationView | null>(null);
  const [tenantId, setTenantId] = useState("");
  const [provisioningId, setProvisioningId] = useState("");
  const [provisioning, setProvisioning] = useState<ProvisioningView | null>(null);
  const [plan, setPlan] = useState<PlanView | null>(null);
  const [readiness, setReadiness] = useState<ReadinessView | null>(null);
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);

  async function refresh() {
    setLoading(true);
    setNotice("");
    setOperation(null);
    try {
      const record = await readWorkspace<OperationView>("operation", id);
      setOperation(record);
      setNotice("Current operation state was retrieved from the authoritative Control Plane.");
    } catch {
      setNotice("Operation not found or unavailable to this authenticated principal.");
    } finally {
      setLoading(false);
    }
  }

  async function refreshProvisioning() {
    setLoading(true);
    setNotice("");
    setProvisioning(null);
    setPlan(null);
    setReadiness(null);
    try {
      // No synthetic READY evaluation: display only the three independently
      // evaluated current CP resources for the same tenant/provisioning IDs.
      const record = await readWorkspace<ProvisioningView>("provisioning", provisioningId, tenantId);
      if (record.tenant_id !== tenantId || record.tenant_provisioning_id !== provisioningId) {
        throw new Error("Cross-tenant provenance mismatch");
      }
      const [currentPlan, currentReadiness] = await Promise.all([
        readWorkspace<PlanView>("provisioning-plan", provisioningId, tenantId),
        readWorkspace<ReadinessView>("provisioning-readiness", provisioningId, tenantId),
      ]);
      if (currentPlan.plan_digest !== record.current_plan?.plan_digest) {
        // A plan changing between reads is a stale snapshot, never an
        // opportunity to display an approval bound to the older digest.
        throw new Error("Stale plan digest");
      }
      setProvisioning(record);
      setPlan(currentPlan);
      setReadiness(currentReadiness);
      setNotice("Current lifecycle, plan and readiness retrieved. A READY operation is not tenant activation.");
    } catch {
      setNotice("The exact tenant, immutable plan and readiness could not be verified together. Refresh to retry.");
    } finally {
      setLoading(false);
    }
  }

  const validOperation = /^[A-Za-z0-9][A-Za-z0-9_-]{5,127}$/.test(id);
  const validProvisioning = /^[A-Za-z0-9][A-Za-z0-9_-]{5,127}$/.test(tenantId) &&
    /^[A-Za-z0-9][A-Za-z0-9_-]{5,127}$/.test(provisioningId);
  const approvalMatchesPlan = !!provisioning?.current_plan && !!provisioning.approval &&
    provisioning.current_plan.plan_digest === provisioning.approval.plan_digest &&
    provisioning.approval.decision === "APPROVED" && !provisioning.current_plan.stale;

  return (
    <section>
      <p className={styles.eyebrow}>Provisioning · Authoritative operation</p>
      <h1>Track tenant provisioning and readiness</h1>
      <p>Admission approval, tenant authorisation, provisioning completion, provider readiness
        and production activation are different transitions. A client never sets their statuses.</p>
      <div className={styles.panel}>
        <h2>Execution operation</h2>
        <div className={styles.formGrid}>
          <label>Control Plane operation ID
            <input value={id} onChange={(e) => setId(e.target.value)} required
              maxLength={128} placeholder="op_..." autoComplete="off" />
          </label>
        </div>
        <div className={styles.actions}>
          <button disabled={loading || !validOperation}
            type="button" onClick={() => void refresh()}>
            {loading ? "Checking current authority…" : "Refresh operation state"}
          </button>
        </div>
        {operation ? (
          <dl>
            <div><dt>Operation</dt><dd>{String(operation.operation_id ?? id)}</dd></div>
            <div><dt>Type</dt><dd>{String(operation.operation_type ?? "Unspecified")}</dd></div>
            <div><dt>Current state</dt><dd><strong>{String(operation.status ?? "Unknown")}</strong></dd></div>
            <div><dt>Revision</dt><dd>{operation.revision ?? "Not reported"}</dd></div>
            {operation.plan_digest ? <div><dt>Plan digest</dt><dd>{operation.plan_digest}</dd></div> : null}
            {operation.progress ? <div><dt>Completed steps</dt>
              <dd>{operation.progress.completed_steps ?? 0} / {operation.progress.total_steps ?? 0}</dd>
            </div> : null}
            {operation.result ? <div><dt>Resulting resource state</dt><dd>{operation.result.resource_state ?? "Unknown"}</dd></div> : null}
          </dl>
        ) : null}
      </div>
      <section className={styles.panel} aria-labelledby="provisioning-heading">
        <h2 id="provisioning-heading">Exact tenant provisioning evidence</h2>
        <p className={styles.note}>Requires the same approved tenant and provisioning identifiers on all three CP reads.
          A stale plan is never displayed as authorised.</p>
        <div className={styles.formGrid}>
          <label>Tenant ID
            <input value={tenantId} onChange={(e) => setTenantId(e.target.value)}
              placeholder="ten_..." maxLength={128} autoComplete="off" />
          </label>
          <label>Tenant provisioning ID
            <input value={provisioningId} onChange={(e) => setProvisioningId(e.target.value)}
              placeholder="tp_..." maxLength={128} autoComplete="off" />
          </label>
        </div>
        <div className={styles.actions}>
          <button type="button" disabled={loading || !validProvisioning}
            onClick={() => void refreshProvisioning()}>
            Refresh lifecycle, immutable plan and readiness
          </button>
        </div>
        {provisioning && plan && readiness ? (
          <dl>
            <div><dt>Provisioning state</dt><dd><strong>{provisioning.state}</strong></dd></div>
            <div><dt>Current revision</dt><dd>{provisioning.revision}</dd></div>
            <div><dt>Readiness verdict</dt><dd><strong>{readiness.status}</strong></dd></div>
            <div><dt>Approved plan aligned</dt><dd>{approvalMatchesPlan ? "Yes — current digest" : "No — absent, stale or mismatched"}</dd></div>
            <div><dt>Plan digest</dt><dd>{plan.plan_digest}</dd></div>
            <div><dt>Plan version</dt><dd>{provisioning.current_plan?.plan_version ?? "Not reported"}</dd></div>
            <div><dt>Blocking reasons</dt><dd>{provisioning.blocking_reasons?.length ?? 0}</dd></div>
            <div><dt>Readiness snapshots</dt><dd>{readiness.snapshots?.length ?? 0}</dd></div>
            {provisioning.operation_id ?
              <div><dt>Applying operation</dt><dd>{provisioning.operation_id}</dd></div> : null}
          </dl>
        ) : null}
      </section>
      {notice ? <p role="status" aria-live="polite" className={styles.notice}>{notice}</p> : null}
      <p className={styles.note}>Read-only status; the authoritative Control Plane and each owning engine continue to
        govern activation, retry, compensation, rollback and capability-specific restrictions.</p>
    </section>
  );
}
