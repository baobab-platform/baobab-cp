import Link from "next/link";

import { SiteShell, SiteSection, styles } from "./SiteShell";

const capabilities = [
  { n: "01", title: "Commerce & trade", description: "Build B2B and consumer experiences around governed trade capabilities.", href: "/solutions" },
  { n: "02", title: "Enterprise operations", description: "Connect operational and financial domains with clear ownership boundaries.", href: "/solutions" },
  { n: "03", title: "Digital experiences", description: "Create coherent websites, portals and content-led journeys.", href: "/architecture" },
  { n: "04", title: "Market intelligence", description: "Turn evidence-informed insights into better business decisions.", href: "/solutions" },
  { n: "05", title: "Identity & governance", description: "Maintain explicit accountability, access controls and operating mandates.", href: "/trust" },
  { n: "06", title: "Provisioning & readiness", description: "Follow plan, approval, execution and independent readiness evidence.", href: "/architecture" },
];
const stages = [
  ["01", "Describe your business", "Start with your operating identity and accountable representative."],
  ["02", "Choose where and how", "Select markets and the services your business requires."],
  ["03", "Establish authority", "Supply relevant evidence and receive independently governed decisions."],
  ["04", "Prepare operations", "Follow an authorised plan and its provisioning work."],
  ["05", "Confirm readiness", "Inspect blockers and readiness before activation."],
];
const FAQs = [
  ["Can an emerging business apply?", "Yes, an application can describe an unincorporated business truthfully. Specific activities may still require an authorised legal actor or further evidence."],
  ["Will my enterprise retain its autonomy?", "The architecture treats organisations and tenants separately. A parent affiliation does not automatically grant access or remove operating autonomy."],
  ["Is every service available today?", "No. Backend implementation, provider eligibility, deployment readiness and authorised production activation are independently assessed."],
];
export function Landing() {
  return (
    <SiteShell>
      <section className={styles.hero}>
        <div className={styles.container}>
          <div className={styles.heroGrid}>
            <div className={styles.heroCopy}>
              <p className={styles.heroEyebrow}><span className={styles.goldDot} /> THE ENTERPRISE FOUNDATION FOR AFRICA</p>
              <h1>One platform.<br /><em>Every business possibility.</em></h1>
              <p className={styles.heroLead}>Build, connect and manage business operations across markets through a governed, extensible enterprise foundation.</p>
              <div className={styles.heroActions}>
                <Link href="/platform" className={styles.goldButton}>Explore the platform <span aria-hidden="true">↗</span></Link>
                <Link href="/start" className={styles.outlineButton}>How to get started <span aria-hidden="true">→</span></Link>
              </div>
              <p className={styles.heroDisclosure}>Platform capabilities are subject to authorisation and independently verified operational readiness.</p>
            </div>
            <div className={styles.heroVisual} aria-label="Illustration of Control Plane connecting business services">
              <div className={styles.visualTop}>
                <span>PLATFORM INTELLIGENCE</span>
                <span>GOVERNED BY DESIGN</span>
              </div>
              <div className={styles.controlNode}>
                <span className={styles.nodeLabel}>CENTRAL GOVERNANCE</span>
                <strong>BAOBAB CONTROL PLANE</strong>
                <span>Organisation · Tenants · Markets · Readiness</span>
              </div>
              <div className={styles.visualConnector} aria-hidden="true"><span /></div>
              <div className={styles.nodeGrid}>
                {["Trade", "ERP", "CMS", "Pulse"].map((label) => (
                  <div key={label} className={styles.engineNode}>
                    <span className={styles.engineIcon} aria-hidden="true">◈</span><strong>{label}</strong>
                    <span>Governed capability</span>
                  </div>
                ))}
              </div>
              <p className={styles.visualCaption}>Conceptual architecture · Individual engines retain their own authority</p>
            </div>
          </div>
          <div className={styles.heroFoot}>
            <span>BUSINESS-FIRST ONBOARDING</span><span>EXPLICIT TENANT AUTONOMY</span><span>MARKET-AWARE OPERATIONS</span>
          </div>
        </div>
      </section>

      <SiteSection eyebrow="THE PLATFORM" title="Complexity behind the scenes. Clarity for your business." className={styles.lightSection}>
        <div className={styles.introRow}>
          <p>The platform's Control Plane coordinates authorised business intent while specialist engines remain responsible for their own work. You see one coherent experience—not a pile of unrelated software.</p>
          <Link href="/architecture" className={styles.textLink}>Discover the architecture <span aria-hidden="true">↗</span></Link>
        </div>
        <div className={styles.featureGrid}>
          {capabilities.map((cap) => (
            <article className={styles.featureCard} key={cap.n}>
              <span className={styles.cardNumber}>{cap.n} / CAPABILITY</span>
              <h3>{cap.title}</h3>
              <p>{cap.description}</p>
              <Link href={cap.href} aria-label={"Explore " + cap.title}>Explore <span aria-hidden="true">↗</span></Link>
            </article>
          ))}
        </div>
      </SiteSection>

      <SiteSection eyebrow="PROGRESSIVE ONBOARDING" title="From business ambition to operational readiness." className={styles.warmSection}>
        <p className={styles.sectionLead}>An understandable business journey, with governed decisions and relevant evidence at the right stage.</p>
        <ol className={styles.stageGrid}>
          {stages.map(([n, title, description]) => (
            <li key={n}><span>{n}</span><h3>{title}</h3><p>{description}</p></li>
          ))}
        </ol>
        <Link href="/start" className={styles.darkButton}>See how onboarding works <span aria-hidden="true">↗</span></Link>
      </SiteSection>

      <SiteSection eyebrow="BUILT FOR INDEPENDENCE" title="One group can contain many autonomous businesses." className={styles.lightSection}>
        <div className={styles.splitLayout}>
          <div>
            <p className={styles.sectionLead}>Corporate affiliation, operating identity, legal responsibility and tenant permission are not interchangeable. Baobab is designed to preserve those distinctions.</p>
            <Link href="/trust" className={styles.textLink}>Why governance matters <span aria-hidden="true">↗</span></Link>
          </div>
          <div className={styles.principleList}>
            {[
              ["Organisation first", "Every tenant has a primary business identity."],
              ["Authority when needed", "Legal responsibility is governed by activity and market."],
              ["Boundaries that hold", "Parent-company affiliation does not automatically confer access."],
            ].map(([title, desc]) => <div key={title}><span aria-hidden="true">✓</span><p><strong>{title}</strong><br />{desc}</p></div>)}
          </div>
        </div>
      </SiteSection>

      <section className={styles.marketBanner}>
        <div className={styles.container}>
          <div>
            <p className={styles.eyebrow}>BUILT FOR AFRICAN MARKETS</p>
            <h2>Local context. Regional possibility.</h2>
            <p>South Africa and Uganda are the initial market focus. Expansion is grounded in explicit permissions, appropriate providers and the reality of each jurisdiction.</p>
          </div>
          <Link href="/markets" className={styles.outlineButton}>Explore markets <span aria-hidden="true">↗</span></Link>
        </div>
      </section>

      <SiteSection eyebrow="BUILT FOR DEVELOPERS" title="Open architecture. Clear responsibility." className={styles.lightSection}>
        <div className={styles.splitLayout}>
          <p className={styles.sectionLead}>Contract-first APIs, bounded domains, a separately authenticated Console and explicit capability resolution help product teams build without bypassing the Control Plane.</p>
          <div className={styles.developerPanel}>
            <span className={styles.codeLabel}>ARCHITECTURE AT A GLANCE</span>
            <code>Browser → Secure Console / BFF<br />Console → Control Plane API<br />Control Plane → authorised services</code>
            <Link href="/developers">Developer centre <span aria-hidden="true">↗</span></Link>
          </div>
        </div>
      </SiteSection>

      <SiteSection eyebrow="FREQUENTLY ASKED" title="Clarity before commitment." className={styles.warmSection}>
        <div className={styles.faqList}>
          {FAQs.map(([question, answer]) => (
            <details key={question}><summary>{question}<span aria-hidden="true">+</span></summary><p>{answer}</p></details>
          ))}
        </div>
        <Link href="/faq" className={styles.textLink}>All questions <span aria-hidden="true">↗</span></Link>
      </SiteSection>

      <section className={styles.finalCta}>
        <div className={styles.container}>
          <p className={styles.heroEyebrow}>BUILD WITH PURPOSE</p>
          <h2>The next chapter of your business starts with a better foundation.</h2>
          <p>Explore Baobab, understand the operating model, and begin the governed onboarding journey when access is available.</p>
          <div className={styles.heroActions}>
            <Link href="/start" className={styles.goldButton}>Get started <span aria-hidden="true">↗</span></Link>
            <Link href="/workspace" className={styles.outlineButton}>Open secure Console <span aria-hidden="true">→</span></Link>
          </div>
          <p className={styles.heroDisclosure}>Console sign-in requires authorised access and an enabled environment.</p>
        </div>
      </section>
    </SiteShell>
  );
}
