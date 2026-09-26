import type { ReactNode } from "react";

import { statusGlyph } from "./status";
import styles from "./Alert.module.css";

export type AlertTone = "informational" | "success" | "warning" | "failure";

export interface AlertProps {
  tone: AlertTone;
  title: string;
  children?: ReactNode;
  /** Announce when it appears: "polite" for outcomes, "assertive" only for failures that block the user. */
  announce?: "polite" | "assertive";
}

export function Alert({ tone, title, children, announce }: AlertProps) {
  const role = announce === "assertive" ? "alert" : announce === "polite" ? "status" : undefined;
  return (
    <div className={styles.alert} data-tone={tone} role={role}>
      <span className={styles.glyph} aria-hidden="true">
        {statusGlyph[tone]}
      </span>
      <div>
        <p className={styles.title}>{title}</p>
        {children ? <div className={styles.body}>{children}</div> : null}
      </div>
    </div>
  );
}
