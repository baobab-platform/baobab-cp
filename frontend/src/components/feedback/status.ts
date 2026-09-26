// The shared status vocabulary (prompt section 54). Features map their
// domain states onto these tones and keep their own precise wording; they
// never invent a new colour meaning.
export const statusTones = [
  "neutral",
  "informational",
  "in-progress",
  "success",
  "warning",
  "blocked",
  "failure",
  "expired",
  "revoked",
] as const;

export type StatusTone = (typeof statusTones)[number];

/** A distinct shape per tone, so status never relies on colour alone (WCAG 1.4.1). */
export const statusGlyph: Record<StatusTone, string> = {
  neutral: "○",
  informational: "ℹ",
  "in-progress": "◔",
  success: "✓",
  warning: "!",
  blocked: "⊘",
  failure: "✕",
  expired: "⌛",
  revoked: "⊗",
};
