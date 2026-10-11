import Link from "next/link";

import styles from "./workspace.module.css";

export default function WorkspaceIndex() {
  return (
    <section className={styles.content}>
      <p className={styles.eyebrow}>Progressive onboarding</p>
      <h1>Governed enterprise workspace</h1>
      <p>Starting a business application does not create a Tenant, verify incorporation or grant a provider entitlement.</p>
      <div className={styles.cards}>
        <article>
          <h2>Applicant workspace</h2>
          <p>Describe your business, save an owned draft and submit for independent admission review.</p>
          <Link href="/workspace/apply">Open my application</Link>
        </article>
        <article>
          <h2>Governance workspace</h2>
          <p>Only Control Plane authorised reviewers and approvers can access or act on submitted applications.</p>
          <Link href="/workspace/review">Open independent review</Link>
        </article>
        <article>
          <h2>Operations</h2>
          <p>Inspect an authoritative provisioning operation. Approval and readiness never mean automatic activation.</p>
          <Link href="/workspace/operations">Inspect operation</Link>
        </article>
      </div>
    </section>
  );
}
