"use client";

import { useState, type FormEvent } from "react";

import { readWorkspace, sendWorkspaceCommand, type ApplicationView } from "./workspace-client";

import styles from "@/app/workspace/workspace.module.css";

type Requirement = "operates_b2b" | "operates_b2c" | "requires_accounting" |
  "trades_cross_border" | "requires_content_management" | "requires_intelligence";

const requirements = [
  ["operates_b2b", "Sell to other businesses"],
  ["operates_b2c", "Sell to consumers"],
  ["requires_accounting", "Manage accounting"],
  ["trades_cross_border", "Trade across national borders"],
  ["requires_content_management", "Publish business content"],
  ["requires_intelligence", "Use market intelligence"],
] as const;

export function ApplicantWorkspace() {
  const [operatingName, setOperatingName] = useState("");
  const [form, setForm] = useState("UNINCORPORATED_ORGANISATION");
  const [claim, setClaim] = useState("NOT_INCORPORATED");
  const [legalName, setLegalName] = useState("");
  const [registration, setRegistration] = useState("");
  const [market, setMarket] = useState("ZA");
  const [representative, setRepresentative] = useState("");
  const [representativeRole, setRepresentativeRole] = useState("Founder");
  const [requirementsSelected, setRequirementsSelected] = useState<Requirement[]>([]);
  const [draft, setDraft] = useState<ApplicationView | null>(null);
  const [retrieveID, setRetrieveID] = useState("");
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState("");

  function toggleRequirement(value: Requirement, enabled: boolean) {
    setRequirementsSelected((old) => enabled ? [...new Set([...old, value])] : old.filter((item) => item !== value));
  }

  const businessIdentity: Record<string, unknown> = {
    operating_name: operatingName.trim(),
    organisation_form: form,
    operating_country: market,
    incorporation_claim: claim,
    authorised_representative: {
      full_name: representative.trim(), role: representativeRole.trim(),
    },
    ...(claim === "INCORPORATED_CLAIMED" ? {
      legal_name: legalName.trim(),
      jurisdiction_of_incorporation: market,
      registration_identifiers: [{
        type: "COMPANY_REGISTRATION", value: registration.trim(), issuing_jurisdiction: market,
      }],
    } : {}),
  };

  function loadForm(app: ApplicationView) {
    setDraft(app);
    const b = app.business_identity ?? {};
    if (typeof b.operating_name === "string") setOperatingName(b.operating_name);
    if (typeof b.organisation_form === "string") setForm(b.organisation_form);
    if (typeof b.incorporation_claim === "string") setClaim(b.incorporation_claim);
    if (typeof b.legal_name === "string") setLegalName(b.legal_name);
    if (app.requested_markets?.[0]?.country_code) setMarket(app.requested_markets[0].country_code);
    const rep = b.authorised_representative as Record<string, unknown> | undefined;
    if (typeof rep?.full_name === "string") setRepresentative(rep.full_name);
    if (typeof rep?.role === "string") setRepresentativeRole(rep.role);
  }

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSaving(true);
    setNotice("");
    try {
      if (claim === "INCORPORATED_CLAIMED" && (!legalName.trim() || !registration.trim())) {
        throw new Error("An incorporated-company claim needs its actual legal name and declared registration number.");
      }
      const body = {
        business_identity: businessIdentity,
        requirements: Object.fromEntries(requirementsSelected.map((key) => [key, true])),
        requested_markets: [{ country_code: market }],
        ...(draft ? { version: draft.version } : {}),
      };
      const app = await sendWorkspaceCommand<ApplicationView>({
        action: draft ? "update" : "create",
        ...(draft ? { id: draft.client_application_id } : {}),
        body,
      });
      loadForm(app);
      setNotice(`Application ${app.reference} saved as ${app.status}. Saving is not admission approval.`);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Saving was not permitted.");
    } finally {
      setSaving(false);
    }
  }

  async function retrieve() {
    setSaving(true);
    setNotice("");
    try {
      const result = await readWorkspace<ApplicationView>("application", retrieveID);
      loadForm(result);
      setNotice(`Loaded application ${result.reference} with current status ${result.status}.`);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Application unavailable.");
    } finally {
      setSaving(false);
    }
  }

  async function submit() {
    if (!draft || draft.status !== "DRAFT") return;
    setSaving(true);
    setNotice("");
    try {
      const updated = await sendWorkspaceCommand<ApplicationView>({
        action: "submit", id: draft.client_application_id,
      });
      setDraft(updated);
      setNotice(`Submitted ${updated.reference}. An independent reviewer must assess it before admission.`);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "Submission was not authorised.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <section>
      <p className={styles.eyebrow}>Applicant · Self-service</p>
      <h1>Tell us about your business</h1>
      <p>Describe your operating business truthfully. A trading name and a company are not automatically the same legal person.</p>
      <div className={styles.notice}>
        We will not require company-registration, PAYE, UIF or VAT documents universally.
        Particular services, laws, markets and payment providers may require independently verified evidence later.
      </div>

      <section className={styles.panel} aria-label="Retrieve an existing application">
        <h2>Continue an application</h2>
        <div className={styles.formGrid}>
          <label>Control Plane application ID
            <input value={retrieveID} onChange={(e) => setRetrieveID(e.target.value)}
              placeholder="capp_..." autoComplete="off" maxLength={63} />
          </label>
        </div>
        <div className={styles.actions}>
          <button type="button" disabled={saving || !/^capp_[a-z0-9]+$/.test(retrieveID)} onClick={retrieve}>Load my draft</button>
        </div>
        <p className={styles.note}>Only applications owned by your authenticated principal can be loaded.</p>
      </section>

      <form onSubmit={(e) => void save(e)} className={styles.panel}>
        <h2>{draft ? "Update business details" : "Start a new application"}</h2>
        <div className={styles.formGrid}>
          <label>Operating business name
            <input required minLength={1} maxLength={300} value={operatingName}
              onChange={(e) => setOperatingName(e.target.value)} autoComplete="organization" />
          </label>
          <label>Organisation form
            <select value={form} onChange={(e) => setForm(e.target.value)}>
              <option value="UNINCORPORATED_ORGANISATION">Unincorporated operating business</option>
              <option value="SOLE_PROPRIETOR">Sole proprietor</option>
              <option value="COMPANY">Registered company</option>
              <option value="PARTNERSHIP">Partnership</option>
              <option value="COOPERATIVE">Cooperative</option>
              <option value="OTHER">Other recognised form</option>
            </select>
          </label>
          <label>Operating country / requested market
            <select value={market} onChange={(e) => setMarket(e.target.value)}>
              <option value="ZA">South Africa</option>
              <option value="UG">Uganda</option>
            </select>
          </label>
          <label>Incorporation declaration
            <select value={claim} onChange={(e) => setClaim(e.target.value)}>
              <option value="NOT_INCORPORATED">Not separately incorporated</option>
              <option value="INCORPORATED_CLAIMED">Registered company — evidence pending verification</option>
              <option value="UNKNOWN">Not yet established</option>
            </select>
          </label>
          {claim === "INCORPORATED_CLAIMED" ? (
            <>
              <label>Declared legal company name
                <input required maxLength={300} value={legalName} onChange={(e) => setLegalName(e.target.value)} />
              </label>
              <label>Declared company registration number
                <input required maxLength={128} value={registration} onChange={(e) => setRegistration(e.target.value)} />
              </label>
            </>
          ) : null}
          <label>Authorised representative’s full name
            <input required maxLength={200} value={representative}
              onChange={(e) => setRepresentative(e.target.value)} autoComplete="name" />
          </label>
          <label>Representative’s role
            <input required maxLength={100} value={representativeRole}
              onChange={(e) => setRepresentativeRole(e.target.value)} />
          </label>
          <fieldset className={styles.wide}>
            <legend>Business goals — request only what you need</legend>
            <div className={styles.checks}>
              {requirements.map(([key, label]) => (
                <label key={key}>
                  <input type="checkbox" checked={requirementsSelected.includes(key)}
                    onChange={(e) => toggleRequirement(key, e.target.checked)} />
                  {label}
                </label>
              ))}
            </div>
          </fieldset>
        </div>
        <div className={styles.actions}>
          <button type="submit" disabled={saving || !!draft && draft.status !== "DRAFT"}>
            {draft ? "Save draft changes" : "Create application draft"}
          </button>
          <button type="button" disabled={!draft || draft.status !== "DRAFT" || saving}
            onClick={() => void submit()}>Submit for independent review</button>
        </div>
        {draft ? <p><strong>Current status:</strong> {draft.status} · {draft.reference} · Revision {draft.version}</p> : null}
        <p className={styles.note}>Submission does not create a tenant or authorise trading. A different, qualified reviewer must assess admission.</p>
      </form>
      {notice ? <p role="status" aria-live="polite" className={styles.notice}>{notice}</p> : null}
    </section>
  );
}
