import type { ReactNode } from "react";

import styles from "./Field.module.css";

/** Props a Field hands its control, so label, hint and error are always wired. */
export interface ControlProps {
  id: string;
  "aria-describedby"?: string;
  "aria-invalid"?: true;
  required?: boolean;
}

export interface FieldProps {
  id: string;
  label: string;
  hint?: string;
  /** A precise, actionable message; it is announced with the field. */
  error?: string;
  required?: boolean;
  children: (control: ControlProps) => ReactNode;
}

/** A labelled form field (WCAG 1.3.1, 3.3.1, 3.3.2). */
export function Field({ id, label, hint, error, required = false, children }: FieldProps) {
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ");
  const control: ControlProps = { id };
  if (describedBy) control["aria-describedby"] = describedBy;
  if (error) control["aria-invalid"] = true;
  if (required) control.required = true;
  return (
    <div className={styles.field}>
      <label className={styles.label} htmlFor={id}>
        {label}
        {required ? <span aria-hidden="true"> *</span> : null}
      </label>
      {hint ? (
        <p id={hintId} className={styles.hint}>
          {hint}
        </p>
      ) : null}
      {children(control)}
      {error ? (
        <p id={errorId} className={styles.error}>
          {error}
        </p>
      ) : null}
    </div>
  );
}
