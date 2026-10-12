/** Console route catalogue. Every href is a literal, implemented route.
 * Navigation visibility is a presentation hint, never authorisation.
 * No cross-tenant or subsidiary membership is inferred from an IAM role.
 */
export type AuthoritySummary = {
  state: "available" | "unavailable";
  permissions: string[];
  evaluatedAt?: string;
  elevatedUntil?: string;
};
export type ConsoleRoute = {
  href: "/workspace" | "/workspace/apply" | "/workspace/review" | "/workspace/operations";
  label: string;
  description: string;
  access: "public-to-applicant" | "administrative";
  permissions?: readonly string[];
};

export const consoleRoutes: readonly ConsoleRoute[] = [
  {
    href: "/workspace",
    label: "Overview",
    description: "Your current Console work",
    access: "public-to-applicant",
  },
  {
    href: "/workspace/apply",
    label: "My application",
    description: "Submit and follow your own business application",
    access: "public-to-applicant",
  },
  {
    href: "/workspace/review",
    label: "Independent review",
    description: "Governed admission and independent decisions",
    access: "administrative",
    permissions: ["application.review", "application.decide"],
  },
  {
    href: "/workspace/operations",
    label: "Provisioning & readiness",
    description: "Authoritative operation, plan and readiness readback",
    access: "administrative",
    permissions: ["operation.view"],
  },
] as const;

export function visibleConsoleRoutes(authority: AuthoritySummary): ConsoleRoute[] {
  const granted = new Set(authority.state === "available" ? authority.permissions : []);
  return consoleRoutes.filter((route) => route.access === "public-to-applicant" ||
    (route.permissions ?? []).some((permission) => granted.has(permission)));
}
