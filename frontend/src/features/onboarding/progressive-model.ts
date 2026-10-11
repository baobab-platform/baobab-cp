/**
 * PEO-06A: presentation-only semantics from ADR-BCP-026 sections 7 and 14.
 * Actual evidence applicability, IAM, admission, provider certification,
 * mandates and entitlement are decisions of authoritative backends.
 * Never interpret these illustrations as capability grants.
 */
export const journeyStages = [
  {
    id: "identity",
    title: "Identify yourself",
    explanation: "Sign in with a verified contact channel. Identity is not the same as business registration.",
    boundary: "Baobab IAM verifies the human; the Control Plane decides admission authority.",
  },
  {
    id: "business",
    title: "Tell us about your business",
    explanation: "Describe your business form, operating country, representative and responsible contracting actor.",
    boundary: "An emerging or unincorporated business is not silently converted into a verified company.",
  },
  {
    id: "interests",
    title: "Choose what you want to do",
    explanation: "Choose services and intended markets, without presuming that interest grants access.",
    boundary: "A subscription, an approved application and a provisioned capability are different records.",
  },
  {
    id: "requirements",
    title: "Review relevant requirements",
    explanation: "See why particular evidence matters to the chosen service, market and business situation.",
    boundary: "PAYE, UIF, VAT and corporate certificates are not universal admission requirements.",
  },
  {
    id: "admission",
    title: "Independent admission review",
    explanation: "A separate authorised reviewer and decider determine whether the application can progress.",
    boundary: "An applicant never approves their own application or authorises tenant creation.",
  },
  {
    id: "onboarding",
    title: "Authorise a separate tenant",
    explanation: "Independent operators request and authorise onboarding for the exact approved business.",
    boundary: "Parent and subsidiary Organisations keep separate tenant identities and permissions.",
  },
  {
    id: "readiness",
    title: "Verify service readiness",
    explanation: "Approved plans, eligible providers, security, subscriptions and observed health are checked.",
    boundary: "APPROVED does not mean READY. READY does not mean ACTIVE.",
  },
] as const;

export type JourneyStage = (typeof journeyStages)[number]["id"];

export const serviceIllustrations = [
  {
    id: "presence",
    label: "Business presence and content",
    description: "Draft content, an initial business presence and managed publication.",
    relevant: ["Authenticated business identity", "Rights to publish on a domain or estate when applicable"],
    restricted: ["Publication or domain-control actions need their own checks"],
  },
  {
    id: "intelligence",
    label: "Insights and market intelligence",
    description: "Explore permitted research reports and business intelligence.",
    relevant: ["Authenticated account", "Appropriate product subscription and data access rights"],
    restricted: ["Restricted source licences or classified data need separate permission"],
  },
  {
    id: "trade",
    label: "Trade and supplier operations",
    description: "Draft a catalogue before requesting actual market or transaction activity.",
    relevant: ["Business identity", "Requested product and intended market participation"],
    restricted: ["Selling, cross-border execution and regulated goods need activity-specific checks"],
  },
  {
    id: "finance",
    label: "ERP and bookkeeping",
    description: "Explore configurations before payroll or regulated financial operations.",
    relevant: ["Authenticated administrator", "Responsible contracting actor where applicable"],
    restricted: ["Payroll, invoicing, tax and settlement require separate applicable controls"],
  },
  {
    id: "payments",
    label: "Payment settlement",
    description: "Request settlement capability subject to independent provider decisions.",
    relevant: ["Verified applicable contracting/legal actor", "Provider-specific onboarding requirements"],
    restricted: ["Statutory and payment-provider restrictions cannot be waived by founding-group grace"],
  },
] as const;

export type ServiceIllustration = (typeof serviceIllustrations)[number]["id"];

/** Only known illustrative service IDs may be rendered; no approval decision. */
export function exampleForService(id: ServiceIllustration) {
  return serviceIllustrations.find((item) => item.id === id);
}

export const phaseSeparation = [
  ["Submitted", "A business claim is under review. No tenant authority."],
  ["Approved", "An admission decision exists; it does not create a tenant."],
  ["Authorised", "A separately approved onboarding request may be fulfilled."],
  ["Provisioning", "Desired state is being reconciled; not all services are ready."],
  ["Ready", "Required checks have passed; activation is still a separate governed decision."],
  ["Active", "Only the competent authoritative backend can report active status."],
] as const;
