export type Entry = { label: string; href: string; description: string };
export const navigation: { name: string; links: Entry[] }[] = [
  { name: "Platform", links: [
    { label: "Platform overview", href: "/platform", description: "One connected enterprise foundation" },
    { label: "Architecture", href: "/architecture", description: "Control Plane and business engines" },
    { label: "Trust & governance", href: "/trust", description: "Identity, evidence and accountability" },
    { label: "Getting started", href: "/start", description: "Progressive business onboarding" },
  ] },
  { name: "Solutions", links: [
    { label: "Solutions", href: "/solutions", description: "Commerce, operations and intelligence" },
    { label: "Industries", href: "/industries", description: "Business-specific capabilities" },
    { label: "Markets", href: "/markets", description: "South Africa and Uganda first" },
  ] },
  { name: "Developers", links: [
    { label: "Developer centre", href: "/developers", description: "Contracts, APIs and integrations" },
    { label: "Documentation", href: "/documentation", description: "Architecture and code" },
    { label: "Status", href: "/status", description: "Operational transparency" },
  ] },
  { name: "Resources", links: [
    { label: "Resource centre", href: "/resources", description: "Guides and knowledge" },
    { label: "Questions", href: "/faq", description: "How Baobab works" },
    { label: "Company", href: "/company", description: "Our purpose" },
    { label: "Contact", href: "/contact", description: "Connect with the team" },
  ] },
];
export type Section = { heading: string; description: string; points?: string[] };
export type Page = { eyebrow: string; title: string; lead: string; sections: Section[]; action: string; href: string; note?: string };
export const pages: Record<string, Page> = {
  platform: {
    eyebrow: "The Baobab Platform", title: "One foundation. Independent businesses. Connected possibilities.",
    lead: "Baobab brings governed organisation identity, markets, entitlements and operational readiness together without taking control away from the businesses using it.",
    sections: [
      { heading: "Start with business needs", description: "Ask what the enterprise is trying to achieve, not which engine it should provision.", points: ["Organisation-first identity", "Purpose-specific evidence", "Independent operating choices"] },
      { heading: "Connect, do not centralise", description: "Control Plane owns governed control state; business engines retain their own domain authority.", points: ["Explicit capability entitlements", "Plans and independent approvals", "Readiness before activation"] },
      { heading: "Grow with context", description: "Expand into markets, digital estates and services through authorised change, not guessed business identities." },
    ],
    action: "Explore architecture", href: "/architecture",
  },
  architecture: {
    eyebrow: "Architecture", title: "A connected system of clear responsibilities.",
    lead: "The platform uses headless, capability-oriented architecture with authoritative contracts and independent runtime boundaries.",
    sections: [
      { heading: "Governance and identity", description: "Go Control Plane governs organisation and tenant control state. IAM owns authentication and federation. Shared owns canonical contracts.", points: ["Control Plane", "IAM with provider-neutral federation", "Shared contracts"] },
      { heading: "Business engines", description: "Trade, ERP, CMS, Pulse and other engines own the specialised business capabilities they provide.", points: ["Trade and commerce", "ERP and financial records", "Content and intelligence"] },
      { heading: "Operational confidence", description: "Immutable plans, governed approvals, provider observations and readiness evaluations are separate from a successful command." },
    ],
    action: "Developer resources", href: "/developers",
  },
  solutions: {
    eyebrow: "Solutions", title: "Build business experiences on shared enterprise foundations.",
    lead: "Connect commerce, operational records, digital publishing and decision support around the needs of an authorised enterprise.",
    sections: [
      { heading: "Commerce and cross-border trade", description: "B2B and B2C experiences with explicit markets, business identities and service capabilities." },
      { heading: "Enterprise operations", description: "Governed integration of accounting, operations and responsibilities without transferring ERP authority to Control Plane." },
      { heading: "Content and intelligence", description: "Compose digital experiences and evidence-informed insights through separately owned platform capabilities." },
    ],
    action: "Explore industries", href: "/industries",
    note: "Solution descriptions reflect platform architecture, not a guarantee of production availability for every capability.",
  },
  industries: {
    eyebrow: "Industries", title: "The business context matters.",
    lead: "Industry journeys can use different evidence, markets, legal actors and service providers. One universal checklist cannot serve them all.",
    sections: [
      { heading: "Agriculture and trade", description: "Support multi-market supplier, buyer and trade experiences with appropriate scope." },
      { heading: "Retail, logistics and services", description: "Compose commerce, delivery and operational capabilities as they become eligible." },
      { heading: "Property and enterprise services", description: "Model independently operated businesses with governed contracting relationships." },
      { heading: "Research and intelligence", description: "Connect information and insights without presenting unverified findings as regulatory authority." },
    ],
    action: "Explore the platform", href: "/platform",
  },
  markets: {
    eyebrow: "Markets", title: "Built for local realities. Ready to grow.",
    lead: "Initial market planning focuses on South Africa and Uganda. Participation and provider readiness are evaluated independently for each market and activity.",
    sections: [
      { heading: "South Africa", description: "Market-aware business identity, legal responsibility, operations and data residency constraints." },
      { heading: "Uganda", description: "Explicitly authorised market participation rather than assumptions transferred from another jurisdiction." },
      { heading: "Regional expansion", description: "Additional markets require reviewed policies, evidence and operational capability, not just a new dropdown value." },
    ],
    action: "Get started", href: "/start",
    note: "Market coverage is architectural scope, not proof of a live deployment, licence or operating right.",
  },
  developers: {
    eyebrow: "Developer centre", title: "Well-defined contracts. Connected applications.",
    lead: "Build experiences that use platform capabilities without letting browser code bypass governance or individual engine ownership.",
    sections: [
      { heading: "Contract-first integration", description: "Versioned Shared schemas, generated clients and explicit API boundaries.", points: ["No direct privileged browser tokens", "Scoped and authenticated requests", "Contract-drift validation"] },
      { heading: "Capability architecture", description: "Capabilities are resolved from authorised context, entitlements and provider eligibility rather than arbitrary engine selection." },
      { heading: "Source and documentation", description: "Public repository guides and accepted architecture decisions are the best starting point." },
    ],
    action: "Open documentation", href: "/documentation",
  },
  documentation: {
    eyebrow: "Documentation", title: "Explore the architecture and real implementation.",
    lead: "Follow the repository, accepted decisions and Shared contracts. Source code is not equivalent to a production certification.",
    sections: [
      { heading: "Control Plane", description: "Visit github.com/baobab-platform/baobab-cp for the authoritative Go API and protected Console implementation." },
      { heading: "Shared", description: "The baobab-platform/shared repository defines canonical cross-engine contracts." },
      { heading: "Related engines", description: "IAM, Trade, ERP, CMS, Pulse and Infrastructure each maintain independent ownership and release readiness." },
    ],
    action: "Browse Control Plane source", href: "https://github.com/baobab-platform/baobab-cp",
  },
  resources: {
    eyebrow: "Resources", title: "Understand the decisions behind the platform.",
    lead: "Start with business onboarding, architecture and trust principles. Editorial articles and testimonials should not be fabricated to fill a layout.",
    sections: [
      { heading: "Business-first onboarding", description: "Learn why services trigger relevant evidence requests rather than a uniform company-document gate." },
      { heading: "Enterprise architecture", description: "Understand tenant isolation, organisation autonomy, plans and authority boundaries." },
      { heading: "For developers", description: "Find real contracts, open source code and implementation guides." },
    ],
    action: "Frequently asked questions", href: "/faq",
  },
  faq: {
    eyebrow: "FAQ", title: "Questions worth asking.",
    lead: "Precise boundaries matter more than promising every capability immediately.",
    sections: [
      { heading: "Must my business already be incorporated?", description: "No universal incorporation requirement applies merely to starting an application. Specific activities and providers can still require an appropriate legal actor or evidence." },
      { heading: "Does approval mean my tenant is active?", description: "No. Admission, authorisation, provisioning, readiness and activation are separate decisions." },
      { heading: "Can a parent company access its subsidiaries?", description: "No automatic access follows from ownership, affiliation or sponsorship. Tenant permissions are governed independently." },
      { heading: "Can I choose engines directly?", description: "Ordinary applicants choose business needs and markets. The Control Plane derives eligible technical capability bindings." },
      { heading: "What does it cost?", description: "No public commercial plan or prices have been approved for publication. Founding-group internal treatment and external subscriptions are separately governed." },
    ],
    action: "Start an application", href: "/start",
  },
  trust: {
    eyebrow: "Trust and governance", title: "Accountability is a system, not a badge.",
    lead: "A claim, an independent verification, a legal mandate and operational readiness are different evidence-backed decisions.",
    sections: [
      { heading: "Identity and authority", description: "IAM authenticates. Control Plane enforces contextual scope and separation of duties." },
      { heading: "Evidence and legal responsibility", description: "Business declarations do not certify incorporation. Specific mandates are bounded by role, activity, market and time." },
      { heading: "Operations and readiness", description: "Approval, a completed operation and an active tenant are separate lifecycle states." },
      { heading: "Privacy and certification", description: "Public attestations require independently reviewed proof, policies and accountable sign-off." },
    ],
    action: "Understand architecture", href: "/architecture",
    note: "No ISO, SOC or PCI certification, production uptime or platform-wide regulatory compliance is asserted on this page.",
  },
  company: {
    eyebrow: "Company", title: "Built to help independent African enterprises thrive.",
    lead: "Baobab began as a shared foundation for Nabhold Group Africa and its independently operated businesses, with a broader ambition to support other enterprises.",
    sections: [
      { heading: "Purpose", description: "Reduce duplicated complexity while protecting business autonomy and accountable governance." },
      { heading: "Where we began", description: "The platform's founding-group model includes Nabhold, Thamani Global, ZuriBeans and Equator & Estate Co., each with its own truthful legal and operational representation." },
      { heading: "How we work", description: "Architecture decisions, canonical contracts and bounded engine ownership guide implementation." },
    ],
    action: "Explore Baobab", href: "/platform",
  },
  start: {
    eyebrow: "Getting started", title: "Describe your business. Discover the next step.",
    lead: "The progressive journey begins with your operating identity, chosen markets and business needs.",
    sections: [
      { heading: "1. Describe your business", description: "Provide an operating name, legal form and authorised representative. Pre-incorporation does not become fictitious verified incorporation." },
      { heading: "2. Choose your services", description: "Explain the activities you intend to conduct rather than selecting engine instances." },
      { heading: "3. Provide relevant evidence", description: "Requirements depend on activities, markets, legal responsibilities and providers. Review and authorisation remain independent." },
      { heading: "4. Track setup", description: "An approved application is not an active tenant. Follow provisioning, blockers and readiness separately." },
    ],
    action: "Open protected application workspace", href: "/workspace/apply",
    note: "The Console application is environment- and authorisation-gated. Access is not general availability or automatic tenant admission.",
  },
  status: {
    eyebrow: "Status", title: "Real operational status requires real evidence.",
    lead: "We will not substitute an all-green illustration for verified monitoring and incident history.",
    sections: [
      { heading: "Live monitoring not published", description: "A public status source, incident reporting process and service taxonomy must be approved before availability claims are made." },
      { heading: "What will appear here", description: "Service availability, incidents, maintenance windows and timestamped monitoring evidence." },
      { heading: "Private readiness", description: "Tenant and provider readiness records remain restricted to authorised Console users." },
    ],
    action: "Trust principles", href: "/trust",
    note: "No public uptime metrics or production acceptance are claimed.",
  },
  contact: {
    eyebrow: "Contact", title: "Connect with the Baobab team.",
    lead: "Enterprise and support contact routes are being established. We will not invent a mailbox or publish a form that discards submitted information.",
    sections: [
      { heading: "Enterprise enquiries", description: "A verified inbox, permission notice, retention policy and authorised owner are required before an enquiry form goes live." },
      { heading: "Development", description: "Technical source and architecture material are available through the Baobab GitHub organisation." },
      { heading: "Security disclosures", description: "Use only approved security-reporting routes. Do not post sensitive information in public repositories." },
    ],
    action: "Visit Baobab GitHub", href: "https://github.com/baobab-platform",
    note: "This version of the website does not collect contact submissions.",
  },
  accessibility: {
    eyebrow: "Accessibility", title: "A platform should be usable by everyone.",
    lead: "We target WCAG 2.2 AA in the public experience and Control Plane Console.",
    sections: [
      { heading: "Design principles", description: "Semantic navigation, visible focus, keyboard interaction, responsive reflow and reduced-motion support." },
      { heading: "Testing", description: "Automated accessibility checks are part of the frontend foundation; manual browser and assistive-technology acceptance remains necessary." },
      { heading: "Feedback", description: "An independently approved accessible contact channel will be published before general release." },
    ],
    action: "Explore Baobab", href: "/platform",
  },
};
export const legalPages: Record<string, { title: string; introduction: string; required: string[] }> = {
  privacy: {
    title: "Privacy notice", introduction: "Formal privacy terms await approval by the responsible legal entity. This is a publication placeholder, not an authoritative privacy notice.",
    required: ["Identify the controller and contact route", "Document analytics, storage and data retention", "Explain data-subject rights and international transfers"],
  },
  terms: {
    title: "Website terms", introduction: "Binding website terms await legal review. No contract, warranty or pricing is created by this placeholder.",
    required: ["Confirm legal publisher and applicable terms", "Approve intellectual property and acceptable-use wording", "Separate website terms from service agreements"],
  },
  cookies: {
    title: "Cookies and tracking", introduction: "This public information site includes no analytics or advertising-cookie integration. Protected Console session cookies are governed separately.",
    required: ["Review cookies and storage in the deployed environment", "Document purpose, duration and recipients", "Add consent controls before any non-essential tracking"],
  },
};
