"use client";

import { useState } from "react";

import { readWorkspace } from "./workspace-client";
import styles from "@/app/workspace/workspace.module.css";

type OperationView = Record<string, unknown> & {
  operation_id?: string;
  status?: string;
  kind?: string;
  state?: string;
  tenant_id?: string;
};

export function OperationWorkspace() {
  const [id, setId] = useState("");
  const [operation, setOperation] = useState<OperationView | null>(null);
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);

  async function refresh() {
    setLoading(true);
    setNotice("");
    try {
      const record = await readWorkspace<OperationView>("operation", id);
      setOperation(record);
      setNotice("This is the current Control Plane operation state, not an inferred entitlement.");
    } catch {
      setOperation(null);
      setNotice("Operation not found or unavailable to this authenticated principal.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <section>
      <p className={styles.eyebrow}>Provisioning · Authoritative operation</p>
      <h1>Track tenant provisioning and readiness</h1>
      <p>Admission approval, tenant authorisation, provisioning completion, provider readiness
        and production activation are different transitions. A client never sets their statuses.</p>
      <div className={styles.panel}>
        <div className={styles.formGrid}>
          <label>Control Plane operation ID
            <input value={id} onChange={(e) => setId(e.target.value)} required
              maxLength={128} placeholder="op_..." autoComplete="off" />
          </label>
        </div>
        <div className={styles.actions}>
          <button disabled={loading || !/^[A-Za-z][A-Za-z0-9_-]{5,127}$/.test(id)}
            type="button" onClick={() => void refresh()}>
            {loading ? "Checking current authority…" : "Refresh operation state"}
          </button>
        </div>
        {operation ? (
          <dl>
            <div><dt>Operation</dt><dd>{String(operation.operation_id ?? id)}</dd></div>
            <div><dt>Kind</dt><dd>{String(operation.kind ?? "Unspecified")}</dd></div>
            <div><dt>Current state</dt><dd><strong>{String(operation.status ?? operation.state ?? "Unknown")}</strong></dd></div>
          </dl>
        ) : null}
        {notice ? <p role="status" aria-live="polite" className={styles.notice}>{notice}</p> : null}
      </div>
      <p className={styles.note}>Read-only status; the authoritative Control Plane and each owning engine continue to govern activation, retry, compensation and rollback.</p>
    </section>
  );
}
