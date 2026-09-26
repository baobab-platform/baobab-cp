import type { ReactNode } from "react";

import { messages } from "@/lib/i18n/messages";

import styles from "./states.module.css";

/**
 * A contextual loading message (prompt section 85), such as "Loading
 * organisations…". Never an unexplained spinner.
 */
export function LoadingState({ message }: { message: string }) {
  return (
    <div className={styles.state} role="status">
      <span className={styles.spinner} aria-hidden="true" />
      <p className={styles.message}>{message}</p>
    </div>
  );
}

export interface EmptyStateProps {
  title: string;
  /** Teaches what would populate this view and how (prompt section 86). */
  explanation: string;
  action?: ReactNode;
}

export function EmptyState({ title, explanation, action }: EmptyStateProps) {
  return (
    <div className={styles.state}>
      <h2 className={styles.title}>{title}</h2>
      <p className={styles.message}>{explanation}</p>
      {action ? <div className={styles.action}>{action}</div> : null}
    </div>
  );
}

export interface ErrorStateProps {
  title: string;
  /** What happened, in business language. */
  detail: string;
  /** The actionable next step (prompt section 87). */
  nextStep?: string;
  /** The Control Plane correlation id, for support. Never a stack trace. */
  reference?: string;
  action?: ReactNode;
}

export function ErrorState({ title, detail, nextStep, reference, action }: ErrorStateProps) {
  return (
    <div className={styles.state} role="alert">
      <h2 className={styles.title}>{title}</h2>
      <p className={styles.message}>{detail}</p>
      {nextStep ? <p className={styles.message}>{nextStep}</p> : null}
      {reference ? (
        <p className={styles.reference}>
          {messages.errorReference}: <code>{reference}</code>
        </p>
      ) : null}
      {action ? <div className={styles.action}>{action}</div> : null}
    </div>
  );
}
