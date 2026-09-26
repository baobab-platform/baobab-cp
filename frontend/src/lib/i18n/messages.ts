// User-visible strings owned by shared components (prompt section 74).
// Features keep their own messages; nothing concatenates sentences.
export const messages = {
  skipToContent: "Skip to main content",
  loading: "Loading…",
  errorReference: "Reference",
  dismiss: "Close",
  cancel: "Cancel",
  typedConfirmationLabel: "Type {phrase} to confirm",
  reasonLabel: "Reason",
  reasonHint: "Recorded in the audit trail.",
  status: {
    neutral: "Neutral",
    informational: "Information",
    "in-progress": "In progress",
    success: "Succeeded",
    warning: "Needs attention",
    blocked: "Blocked",
    failure: "Failed",
    expired: "Expired",
    revoked: "Revoked",
  },
} as const;

/** Fills {placeholders} in a message template. */
export function format(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (match, name: string) => values[name] ?? match);
}
