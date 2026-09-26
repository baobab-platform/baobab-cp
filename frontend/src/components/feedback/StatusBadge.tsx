import { messages } from "@/lib/i18n/messages";

import { statusGlyph, type StatusTone } from "./status";
import styles from "./StatusBadge.module.css";

export interface StatusBadgeProps {
  tone: StatusTone;
  /** The domain's precise wording, e.g. "Awaiting authorisation". Defaults to the tone's name. */
  label?: string;
}

export function StatusBadge({ tone, label }: StatusBadgeProps) {
  return (
    <span className={styles.badge} data-tone={tone}>
      <span className={styles.glyph} aria-hidden="true">
        {statusGlyph[tone]}
      </span>
      {label ?? messages.status[tone]}
    </span>
  );
}
