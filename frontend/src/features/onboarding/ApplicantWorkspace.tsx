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

const marketChoices = [["ZA", "South Africa"], ["UG", "Uganda"]] as const;

export function ApplicantWorkspace() {
  const [operatingName, setOperatingName] = useState("");
  const [form, setForm] = useState("UNINCORPORATED_ORGANISATION");
  const [claim, setClaim] = useState("NOT_INCORPORATED");
  const [legalName, setLegalName] = useState("");
  const [incorporationJurisdiction, setIncorporationJurisdiction] = useState("ZA");
  const [registration, setRegistration] = useState("");
  const [market, setMarket] = useState("ZA");
  const [requestedMarkets, setRequestedMarkets] = useState<string[]>(["ZA"]);
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

  // Preserve applicant declarations absent from this form without copying
  // authoritative verification or legal-entity/grant decisions into a draft.
  const previousIdentity = draft?.business_identity ?? {};
  const retainedBusinessClaims = Object.fromEntries(
    ["trading_names", "proposed_responsible_organisation_id", "declared_relationship_reference"]
      .filter((key) => previousIdentity[key] !== undefined)
      .map((key) => [key, previousIdentity[key]]),
  );
  const previousRepresentative = previousIdentity.authorised_representative;
  const retainedRepresentative = previousRepresentative && typeof previousRepresentative === "object" &&
    !Array.isArray(previousRepresentative) ? previousRepresentative : {};
  const originalRegistrationIdentifiers = Array.isArray(previousIdentity.registration_identifiers)
    ? previousIdentity.registration_identifiers : [];
  const companyIndex = originalRegistrationIdentifiers.findIndex((item: unknown) =>
    !!item && typeof item === "object" &&
    (item as Record<string, unknown>).type === "COMPANY_REGISTRATION");
  const companyIdentifier = {
    type: "COMPANY_REGISTRATION", value: registration.trim(),
    issuing_jurisdiction: incorporationJurisdiction,
  };
  // Replacing the editable company identifier at its original index retains
  // other applicant-provided identifiers and order; no verifier is inferred.
  const updatedRegistrationIdentifiers = companyIndex < 0
    ? [...originalRegistrationIdentifiers, companyIdentifier]
    : originalRegistrationIdentifiers.map((item: unknown, index: number) =>
      index === companyIndex ? { ...(item as object), ...companyIdentifier } : item);
  const businessIdentity: Record<string, unknown> = {
    ...retainedBusinessClaims,
    operating_name: operatingName.trim(),
    organisation_form: form,
    operating_country: market,
    incorporation_claim: claim,
    authorised_representative: {
      ...retainedRepresentative, full_name: representative.trim(), role: representativeRole.trim(),
    },
    ...(claim === "INCORPORATED_CLAIMED" ? {
      legal_name: legalName.trim(),
      jurisdiction_of_incorporation: incorporationJurisdiction,
      registration_identifiers: updatedRegistrationIdentifiers,
    } : {}),
  };

  function loadForm(app: ApplicationView) {
    setDraft(app);
    const b = app.business_identity ?? {};
    setOperatingName(typeof b.operating_name === "string" ? b.operating_name : "");
    setForm(typeof b.organisation_form === "string" ? b.organisation_form : "UNINCORPORATED_ORGANISATION");
    setClaim(typeof b.incorporation_claim === "string" ? b.incorporation_claim : "UNKNOWN");
    setLegalName(typeof b.legal_name === "string" ? b.legal_name : "");
    setIncorporationJurisdiction(typeof b.jurisdiction_of_incorporation === "string"
      ? b.jurisdiction_of_incorporation : "ZA");
    const identifiers = Array.isArray(b.registration_identifiers) ? b.registration_identifiers : [];
    const companyRegistration = identifiers.find((item: unknown) => item !== null &&
      typeof item === "object" && (item as Record<string, unknown>).type === "COMPANY_REGISTRATION") as
      Record<string, unknown> | undefined;
    setRegistration(typeof companyRegistration?.value === "string" ? companyRegistration.value : "");
    setMarket(typeof b.operating_country === "string" ? b.operating_country : "ZA");
    const requested = (app.requested_markets ?? []).map((item) => item.country_code)
      .filter((country) => /^[A-Z]{2}$/.test(country));
    setRequestedMarkets([...new Set(requested)]);
    const rep = b.authorised_representative as Record<string, unknown> | undefined;
    setRepresentative(typeof rep?.full_name === "string" ? rep.full_name : "");
    setRepresentativeRole(typeof rep?.role === "string" ? rep.role : "");
    setRequirementsSelected(requirements.flatMap(([key]) =>
      app.requirements?.[key] === true ? [key] : []));
  }

  function toggleMarket(country: string, enabled: boolean) {
    setRequestedMarkets((old) => enabled
      ? [...new Set([...old, country])]
      : old.filter((item) => item !== country));
  }

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSaving(true);
    setNotice("");
    try {
      if (claim === "INCORPORATED_CLAIMED" && (!legalName.trim() || !registration.trim())) {
        throw new Error("An incorporated-company claim needs its actual legal name and declared registration number.");
      }
      if (requestedMarkets.length === 0) {
        throw new Error("Select at least one requested market. Operating country is not market participation.");
      }
      const body = {
        business_identity: businessIdentity,
        requirements: {
          ...(draft?.requirements ?? {}),
          ...Object.fromEntries(requirements.map(([key]) => [key, requirementsSelected.includes(key)])),
        },
        requested_markets: requestedMarkets.map((country_code) =>
          draft?.requested_markets?.find((item) => item.country_code === country_code) ?? { country_code }),
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
          <label>Operating country
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
              <label>Jurisdiction of incorporation
                <select value={incorporationJurisdiction}
                  onChange={(e) => setIncorporationJurisdiction(e.target.value)}>
                  <option value="ZA">South Africa</option>
                  <option value="UG">Uganda</option>
                </select>
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
            <legend>Requested markets — separate from operating country and legal presence</legend>
            <div className={styles.checks}>
              {marketChoices.map(([code, label]) => (
                <label key={code}>
                  <input type="checkbox" checked={requestedMarkets.includes(code)}
                    onChange={(e) => toggleMarket(code, e.target.checked)} />
                  Request {label}
                </label>
              ))}
            </div>
            {requestedMarkets.some((country) => !marketChoices.some(([code]) => code === country)) ?
              <p className={styles.note}>Other markets in this saved application remain retained without being silently deleted. Their eligibility requires separate review.</p> : null}
          </fieldset>
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
