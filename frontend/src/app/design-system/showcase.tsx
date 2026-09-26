"use client";

import { useState } from "react";

import {
  Alert,
  Button,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  LoadingState,
  PageHeader,
  Select,
  SideNav,
  StatusBadge,
  statusTones,
  Table,
  Tabs,
  TextInput,
} from "@/components";

import styles from "./showcase.module.css";

// Illustrative rows for the catalogue only; no Control Plane data.
const sampleRows = [
  { id: "tor_example1", name: "Example Organisation A", status: "in-progress" as const, label: "Awaiting authorisation" },
  { id: "tor_example2", name: "Example Organisation B", status: "success" as const, label: "Fulfilled" },
];

export function DesignSystemShowcase() {
  const [confirming, setConfirming] = useState(false);
  const [confirmed, setConfirmed] = useState<string | null>(null);

  return (
    <div className={`page ${styles.layout}`}>
      <SideNav
        label="Design system sections"
        current="#statuses"
        items={[
          { href: "#statuses", label: "Statuses" },
          { href: "#actions", label: "Actions" },
          { href: "#forms", label: "Forms" },
          { href: "#feedback", label: "Feedback" },
          { href: "#tables", label: "Tables" },
        ]}
      />
      <main id="main">
        <PageHeader title="Design system" description="Owned CP Console components and their states. Illustrative data only." />

        <section id="statuses" aria-labelledby="statuses-heading">
          <h2 id="statuses-heading">Statuses</h2>
          <p className={styles.row}>
            {statusTones.map((tone) => (
              <StatusBadge key={tone} tone={tone} />
            ))}
          </p>
        </section>

        <section id="actions" aria-labelledby="actions-heading">
          <h2 id="actions-heading">Actions</h2>
          <p className={styles.row}>
            <Button variant="primary">Submit application</Button>
            <Button>Save draft</Button>
            <Button variant="ghost">View details</Button>
            <Button variant="danger" onClick={() => setConfirming(true)}>
              Suspend tenant
            </Button>
            <Button variant="primary" busy>
              Submitting…
            </Button>
          </p>
          {confirmed !== null ? (
            <Alert tone="success" title="Confirmed" announce="polite">
              <p>Reason recorded: {confirmed}</p>
            </Alert>
          ) : null}
          <ConfirmDialog
            open={confirming}
            onClose={() => setConfirming(false)}
            onConfirm={(reason) => {
              setConfirmed(reason ?? "");
              setConfirming(false);
            }}
            title="Suspend Example Organisation A?"
            consequence="Every workload of this tenant stops resolving context until it is activated again."
            confirmLabel="Suspend tenant"
            typedConfirmation="Example Organisation A"
            requireReason
          />
        </section>

        <section id="forms" aria-labelledby="forms-heading">
          <h2 id="forms-heading">Forms</h2>
          <Field id="legal-name" label="Legal name" hint="As registered with the company registry." required>
            {(control) => <TextInput {...control} />}
          </Field>
          <Field id="registration-number" label="Registration number" error="Enter the registration number shown on the certificate.">
            {(control) => <TextInput {...control} />}
          </Field>
          <Field id="residency" label="Residency region">
            {(control) => (
              <Select {...control} defaultValue="">
                <option value="" disabled>
                  Choose a region
                </option>
                <option value="af-south">Africa (South)</option>
                <option value="eu-west">Europe (West)</option>
              </Select>
            )}
          </Field>
        </section>

        <section id="feedback" aria-labelledby="feedback-heading">
          <h2 id="feedback-heading">Feedback</h2>
          <Alert tone="informational" title="Approval activates nothing">
            <p>Onboarding begins only when an operator requests it.</p>
          </Alert>
          <Alert tone="warning" title="The plan is stale">
            <p>Review the updated impact analysis before requesting approval again.</p>
          </Alert>
          <Tabs
            label="State examples"
            tabs={[
              { id: "loading", label: "Loading", content: <LoadingState message="Loading onboarding requests…" /> },
              {
                id: "empty",
                label: "Empty",
                content: (
                  <EmptyState
                    title="No onboarding requests yet"
                    explanation="An onboarding request is created from an approved admission decision. Approve an application to begin."
                  />
                ),
              },
              {
                id: "error",
                label: "Error",
                content: (
                  <ErrorState
                    title="The request could not be authorised"
                    detail="You requested this onboarding, and the same person cannot also authorise it."
                    nextStep="Ask another onboarding authoriser to review it."
                    reference="00000000-0000-4000-8000-000000000000"
                  />
                ),
              },
            ]}
          />
        </section>

        <section id="tables" aria-labelledby="tables-heading">
          <h2 id="tables-heading">Tables</h2>
          <Table
            caption="Onboarding requests"
            columns={[
              { key: "name", header: "Organisation", cell: (row) => row.name, sortHref: "?sort=name" },
              { key: "status", header: "Status", cell: (row) => <StatusBadge tone={row.status} label={row.label} /> },
              { key: "id", header: "Reference", cell: (row) => <code>{row.id}</code> },
            ]}
            rows={sampleRows}
            rowKey={(row) => row.id}
            sort={{ key: "name", direction: "ascending" }}
            empty={<EmptyState title="No onboarding requests" explanation="Requests appear here once created." />}
          />
        </section>
      </main>
    </div>
  );
}
