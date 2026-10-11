"use client";

import { useState, type FormEvent } from "react";

import { readWorkspace, sendWorkspaceCommand, type ApplicationView } from "./workspace-client";
import styles from "@/app/workspace/workspace.module.css";

type GovernedStage = "review" | "decide" | "request-onboarding" | "authorise-onboarding" | "register-tenant";

const stages: { action: GovernedStage; title: string; identity: string }[] = [
  { action: "review", title: "Record independent review", identity: "Submitted application ID" },
  { action: "decide", title: "Record a separate decision", identity: "Review UUID" },
  { action: "request-onboarding", title: "Request tenant onboarding", identity: "Approved decision UUID" },
  { action: "authorise-onboarding", title: "Authorise an independent request", identity: "Onboarding request UUID" },
  { action: "register-tenant", title: "Register approved Organisation's tenant", identity: "Authorised request ID" },
];

type GovernanceReceipt = { resource_id?: string; status?: string; operation_id?: string };

export function ReviewerWorkspace() {
  const [queue, setQueue] = useState<ApplicationView[]>([]);
  const [selected, setSelected] = useState<ApplicationView | null>(null);
  const [stage, setStage] = useState<GovernedStage>("review");
  const [id, setId] = useState("");
  const [organisation, setOrganisation] = useState("");
  const [evidence, setEvidence] = useState("");
  const [policy, setPolicy] = useState("");
  const [subscription, setSubscription] = useState("COMMERCIAL");
  const [market, setMarket] = useState("ZA");
  const [isolation, setIsolation] = useState("row_level_security");
  const [product, setProduct] = useState("");
  const [decision, setDecision] = useState("APPROVED");
  const [reason, setReason] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [region, setRegion] = useState("af-south-1");
  const [activities, setActivities] = useState("TRADE");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");

  async function loadQueue() {
    setBusy(true);
    setNotice("");
    try {
      const rows = await readWorkspace<ApplicationView[]>("review-queue");
      setQueue(rows);
      setNotice(`Found ${rows.length} submitted applications eligible for this authorised reviewer's queue.`);
    } catch {
      setQueue([]);
      setNotice("The reviewer queue is unavailable or your principal lacks independent admission authority.");
    } finally {
      setBusy(false);
    }
  }

  async function readApplication(appID: string) {
    setBusy(true);
    setNotice("");
    try {
      const app = await readWorkspace<ApplicationView>("review-application", appID);
      setSelected(app);
      setId(app.client_application_id);
    } catch {
      setSelected(null);
      setNotice("This submitted application is not accessible with your current reviewer authority.");
    } finally {
      setBusy(false);
    }
  }

  function commandBody(): Record<string, unknown> {
    switch (stage) {
      case "review":
        return {
          organisation_id: organisation,
          evidence_reference: evidence, identity_resolution_policy_reference: policy,
          subscription_type: subscription, market_scope: [market],
          product_requirements: product.trim() ? product.split(",").map((part) => part.trim()) : [],
          isolation_strategy: isolation,
        };
      case "decide": return { decision, reason, evidence_reference: evidence };
      case "request-onboarding":
        return {
          display_name: displayName, residency_region: region,
          market_participation: [{ market, activities: activities.split(",").map((part) => part.trim()) }],
          reason,
        };
      case "authorise-onboarding": return { policy_reference: policy, evidence_reference: evidence };
      case "register-tenant":
        return {
          tenant_onboarding_request_id: id, organisation_id: organisation,
          display_name: displayName, isolation_strategy: isolation,
          residency_region: region, requested_products: [],
        };
    }
  }

  async function submitCommand(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setNotice("");
    try {
      const output = await sendWorkspaceCommand<GovernanceReceipt>({ action: stage, id, body: commandBody() });
      setNotice(`Control Plane recorded ${output.status ?? "an authorised stage"}.
        Reference: ${output.resource_id ?? output.operation_id ?? "see current operations"}.
        This does not imply service activation.`);
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "Independent authority is missing.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section>
      <p className={styles.eyebrow}>Governance · Independent actors only</p>
      <h1>Review and authorise each transition separately</h1>
      <p>Review, decision, onboarding request, authorisation and registration are distinct Control Plane operations.
        The applicant, reviewer, decider and authoriser must satisfy their own current identity and separation-of-duties rules.</p>
      <p className={styles.notice}>
        This Console cannot override Control Plane role, scope, documentary evidence, sponsorship,
        legal-actor, product subscription or provider-readiness requirements.
      </p>

      <section className={styles.panel} aria-labelledby="queue-heading">
        <h2 id="queue-heading">Submitted applications</h2>
        <div className={styles.actions}>
          <button type="button" disabled={busy} onClick={() => void loadQueue()}>Load my authorised review queue</button>
        </div>
        {queue.length > 0 ? (
          <table>
            <thead><tr><th scope="col">Reference</th><th scope="col">Status</th><th scope="col">Action</th></tr></thead>
            <tbody>{queue.map((item) => (
              <tr key={item.client_application_id}>
                <th scope="row">{item.reference}</th>
                <td>{item.status}</td>
                <td><button type="button" disabled={busy}
                  onClick={() => void readApplication(item.client_application_id)}>Review details</button></td>
              </tr>
            ))}</tbody>
          </table>
        ) : <p className={styles.note}>No applications loaded. This does not imply a global empty queue.</p>}
        {selected ? <div className={styles.notice} aria-live="polite">
          <strong>{selected.reference}</strong> — {selected.status}. Operating business:{" "}
          {typeof selected.business_identity?.operating_name === "string"
            ? selected.business_identity.operating_name : "unconfirmed declaration"}.
          Current data is applicant evidence, not legal verification.
        </div> : null}
      </section>

      <form onSubmit={(e) => void submitCommand(e)} className={styles.panel}>
        <h2>Record the next governed step</h2>
        <div className={styles.formGrid}>
          <label>Independent authority stage
            <select value={stage} onChange={(e) => setStage(e.target.value as GovernedStage)}>
              {stages.map((item) => <option key={item.action} value={item.action}>{item.title}</option>)}
            </select>
          </label>
          <label>{stages.find((item) => item.action === stage)?.identity}
            <input required maxLength={128} value={id} onChange={(e) => setId(e.target.value)} />
          </label>
          {["review", "register-tenant"].includes(stage) ? (
            <label>Canonical Organisation UUID (from current CP authority)
              <input required maxLength={36} value={organisation} onChange={(e) => setOrganisation(e.target.value)} />
            </label>
          ) : null}
          {["review", "decide", "authorise-onboarding"].includes(stage) ? (
            <label>Independently reviewed evidence reference
              <input required maxLength={500} value={evidence} onChange={(e) => setEvidence(e.target.value)} />
            </label>
          ) : null}
          {["review", "authorise-onboarding"].includes(stage) ? (
            <label>Current approved policy reference
              <input required maxLength={500} value={policy} onChange={(e) => setPolicy(e.target.value)} />
            </label>
          ) : null}
          {stage === "review" ? (
            <>
              <label>Subscription classification
                <select value={subscription} onChange={(e) => setSubscription(e.target.value)}>
                  <option value="COMMERCIAL">Commercial (provider and entitlement checks still apply)</option>
                  <option value="INTERNAL">Internal (requires independent current sponsorship)</option>
                </select>
              </label>
              <label>Approved product keys, comma-separated
                <input maxLength={500} value={product} onChange={(e) => setProduct(e.target.value)} />
              </label>
            </>
          ) : null}
          {["review", "register-tenant"].includes(stage) ? (
            <label>Isolation strategy
              <select value={isolation} onChange={(e) => setIsolation(e.target.value)}>
                <option value="row_level_security">Row-level security</option>
                <option value="schema_per_tenant">Schema per tenant</option>
              </select>
            </label>
          ) : null}
          {["review", "request-onboarding"].includes(stage) ? (
            <label>Reviewed market
              <select value={market} onChange={(e) => setMarket(e.target.value)}>
                <option value="ZA">South Africa</option>
                <option value="UG">Uganda</option>
              </select>
            </label>
          ) : null}
          {stage === "decide" ? (
            <label>Independent decision
              <select value={decision} onChange={(e) => setDecision(e.target.value)}>
                <option value="APPROVED">Approve within evidence and scope</option>
                <option value="REJECTED">Reject with documented reasons</option>
              </select>
            </label>
          ) : null}
          {["decide", "request-onboarding"].includes(stage) ? (
            <label className={styles.wide}>Recorded reason (minimum 10 characters)
              <textarea required minLength={10} maxLength={2000} value={reason} onChange={(e) => setReason(e.target.value)} />
            </label>
          ) : null}
          {["request-onboarding", "register-tenant"].includes(stage) ? (
            <>
              <label>Reviewed operating display name
                <input required maxLength={255} value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
              </label>
              <label>Approved data residency region
                <input required pattern="[a-z]{2}-[a-z]+-[0-9]+" maxLength={30}
                  value={region} onChange={(e) => setRegion(e.target.value)} />
              </label>
            </>
          ) : null}
          {stage === "request-onboarding" ? (
            <label>Reviewed activities (comma-separated)
              <input required maxLength={500} value={activities} onChange={(e) => setActivities(e.target.value)} />
            </label>
          ) : null}
        </div>
        <div className={styles.actions}>
          <button type="submit" disabled={busy}>Send exact {stage} command to Control Plane</button>
        </div>
        <p className={styles.note}>A successful stage does not authorise the next stage for the same person. Control Plane enforces independent principals and records immutable decisions.</p>
      </form>
      {notice ? <p role="status" aria-live="polite" className={styles.notice}>{notice}</p> : null}
    </section>
  );
}
