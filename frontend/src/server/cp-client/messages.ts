// What the Console tells a user when the Control Plane refuses or fails a
// request (ADR-BCP-022 sections 95-131). Problem detail text is written for
// operators and never shown; these messages and the reference are.
export const controlPlaneMessages = {
  invalid: "The request was not accepted. Check the details and try again.",
  unauthenticated: "Your session has ended. Sign in again to continue.",
  forbidden: "You do not have permission to do this.",
  notFound: "This item does not exist, or you cannot see it.",
  conflict: "This conflicts with the item's current state. Reload it and try again.",
  stale: "This resource changed after you opened it. Reload the latest version before applying your changes.",
  preconditionRequired: "Reload this item before changing it.",
  rateLimited: "Too many requests. Wait a moment and try again.",
  unavailable: "The Control Plane is unavailable. Try again shortly.",
} as const;

export type ControlPlaneMessageKey = keyof typeof controlPlaneMessages;
