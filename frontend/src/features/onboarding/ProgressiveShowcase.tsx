"use client";

import { useState } from "react";

import { Alert, PageHeader } from "@/components";

import {
  exampleForService,
  journeyStages,
  phaseSeparation,
  serviceIllustrations,
  type ServiceIllustration,
} from "./progressive-model";

import styles from "./ProgressiveShowcase.module.css";

/**
 * PEO-06A deliberately showcases the *shape* of progressive admission.
 * There is no public unauthenticated applicant submit or fake eligibility
 * calculation. The FE-03 confidential session/BFF and reviewed v2 routes
 * are prerequisites to a real workflow.
 */
export function ProgressiveShowcase() {
  const [interest, setInterest] = useState<ServiceIllustration>("presence");
  const example = exampleForService(interest) ?? serviceIllustrations[0];

  return (
    <main id="main" className="page">
      <PageHeader
        title="A clearer path to business onboarding"
        description="Start with your business goals. We only request supporting evidence when a verified rule or chosen activity requires it."
      />

      <Alert tone="informational" title="Journey preview — not a live application">
        This preview does not create, submit or approve an application. Authentication, tenant decisions,
        legal verification, subscriptions and production access remain under their separate authorities.
      </Alert>

      <section className={styles.hero} aria-labelledby="journey-heading">
        <div className={styles.heroCopy}>
          <p className={styles.eyebrow}>Progressive onboarding</p>
          <h2 id="journey-heading" className={styles.heroTitle}>A business can start small without pretending to be something it is not.</h2>
          <p>
            Your next requirement depends on what you want to do, where you operate, who is legally responsible
            and what a current provider or policy actually requires.
          </p>
          <p className={styles.note}>No automatic statutory exemptions. No shortcut to payments or cross-border execution.</p>
        </div>
        <div className={styles.heroSignal} aria-label="Illustrative progression">
          <span>Describe your business</span>
          <span aria-hidden="true">↓</span>
          <span>Independent review</span>
          <span aria-hidden="true">↓</span>
          <strong>Check each service before activation</strong>
        </div>
      </section>

      <section aria-labelledby="services-heading" className={styles.section}>
        <div className={styles.sectionIntro}>
          <p className={styles.eyebrow}>01 / Explore a service</p>
          <h2 id="services-heading">What would you like to do?</h2>
          <p>The examples below illustrate relevance, not a legal eligibility decision or a live provider offer.</p>
        </div>

        <div className={styles.selector} role="group" aria-label="Choose an illustrative service">
          {serviceIllustrations.map((service) => (
            <button
              type="button"
              key={service.id}
              className={styles.selectorButton}
              aria-pressed={interest === service.id}
              onClick={() => setInterest(service.id)}
            >
              {service.label}
            </button>
          ))}
        </div>

        <div className={styles.detail} aria-live="polite" aria-atomic="true">
          <div>
            <h3>{example.label}</h3>
            <p>{example.description}</p>
          </div>
          <div className={styles.detailColumns}>
            <div>
              <h4>Evidence that may matter</h4>
              <ul>{example.relevant.map((item) => <li key={item}>{item}</li>)}</ul>
            </div>
            <div>
              <h4>What still needs a separate check</h4>
              <ul>{example.restricted.map((item) => <li key={item}>{item}</li>)}</ul>
            </div>
          </div>
        </div>
      </section>

      <section aria-labelledby="stages-heading" className={styles.section}>
        <div className={styles.sectionIntro}>
          <p className={styles.eyebrow}>02 / Understand the journey</p>
          <h2 id="stages-heading">Seven steps, with the right decision at each one</h2>
          <p>Each transition stays governed by its own authority. There is no single all-powerful approval.</p>
        </div>
        <ol className={styles.stageGrid}>
          {journeyStages.map((stage, index) => (
            <li key={stage.id} className={styles.stage}>
              <span className={styles.number} aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
              <h3>{stage.title}</h3>
              <p>{stage.explanation}</p>
              <p className={styles.boundary}><strong>Boundary:</strong> {stage.boundary}</p>
            </li>
          ))}
        </ol>
      </section>

      <section aria-labelledby="status-heading" className={styles.section}>
        <div className={styles.sectionIntro}>
          <p className={styles.eyebrow}>03 / Follow real decisions</p>
          <h2 id="status-heading">Approved is not active</h2>
          <p>When the live journey is connected, each state must come from the Control Plane and its governed sources.</p>
        </div>
        <dl className={styles.statusList}>
          {phaseSeparation.map(([status, meaning]) => (
            <div key={status}>
              <dt>{status}</dt>
              <dd>{meaning}</dd>
            </div>
          ))}
        </dl>
      </section>

      <footer className={styles.footer}>
        <p><strong>Current availability:</strong> Showcase only. You cannot submit, verify or activate a business on this page.</p>
        <p>Every actual application will require an authenticated session, applicable terms, independent decisions and auditable evidence.</p>
      </footer>
    </main>
  );
}
