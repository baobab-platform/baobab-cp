import Link from "next/link";
import type { ReactNode } from "react";

import { navigation } from "./content";
import styles from "./site.module.css";

export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <div className={styles.site}>
      <div className={styles.utility}>
        <span>Business infrastructure for an interconnected Africa</span>
        <Link href="/trust">Our approach to trust <span aria-hidden="true">↗</span></Link>
      </div>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <Link href="/" className={styles.brand} aria-label="Baobab Platform, homepage">
            <span className={styles.brandMark} aria-hidden="true">B</span>
            <span>BAOBAB<span className={styles.brandSub}> PLATFORM</span></span>
          </Link>
          <nav aria-label="Primary navigation" className={styles.desktopNavigation}>
            {navigation.map((group) => (
              <details key={group.name} className={styles.navGroup}>
                <summary>{group.name}<span aria-hidden="true" className={styles.chevron}>⌄</span></summary>
                <div className={styles.menu}>
                  <span className={styles.menuEyebrow}>EXPLORE {group.name.toUpperCase()}</span>
                  <div className={styles.menuGrid}>
                    {group.links.map((item) => (
                      <Link key={item.href} href={item.href} className={styles.menuItem}>
                        <strong>{item.label}</strong><span>{item.description}</span>
                      </Link>
                    ))}
                  </div>
                </div>
              </details>
            ))}
          </nav>
          <div className={styles.headerActions}>
            <Link className={styles.consoleLink} href="/workspace">Console <span aria-hidden="true">↗</span></Link>
            <Link className={styles.primaryButton} href="/start">Get started <span aria-hidden="true">→</span></Link>
          </div>
          <details className={styles.mobileNavigation}>
            <summary aria-label="Open navigation">Menu <span aria-hidden="true">☰</span></summary>
            <nav aria-label="Mobile navigation" className={styles.mobilePanel}>
              <Link href="/">Home</Link>
              {navigation.map((group) => (
                <div key={group.name}>
                  <strong>{group.name}</strong>
                  {group.links.map((item) => <Link key={item.href} href={item.href}>{item.label}</Link>)}
                </div>
              ))}
              <Link href="/workspace">Secure Console</Link>
              <Link href="/start">Get started</Link>
            </nav>
          </details>
        </div>
      </header>
      <main id="main">{children}</main>
      <SiteFooter />
    </div>
  );
}

function SiteFooter() {
  const footer = [
    { heading: "Platform", links: [["Overview", "/platform"], ["Architecture", "/architecture"], ["Trust", "/trust"], ["Markets", "/markets"]] },
    { heading: "Solutions", links: [["Solutions", "/solutions"], ["Industries", "/industries"], ["Getting started", "/start"], ["FAQs", "/faq"]] },
    { heading: "Developers", links: [["Developer centre", "/developers"], ["Documentation", "/documentation"], ["Status", "/status"], ["Console", "/workspace"]] },
    { heading: "Company", links: [["About", "/company"], ["Resources", "/resources"], ["Contact", "/contact"], ["Accessibility", "/accessibility"]] },
    { heading: "Legal", links: [["Privacy", "/legal/privacy"], ["Terms", "/legal/terms"], ["Cookies", "/legal/cookies"]] },
  ];
  return (
    <footer className={styles.footer}>
      <div className={styles.container}>
        <div className={styles.footerIntro}>
          <Link href="/" className={styles.footerBrand}>BAOBAB <span>PLATFORM</span></Link>
          <p>Independent enterprises. One connected foundation.</p>
          <p className={styles.footnote}>Designed for businesses across Africa, with initial market planning in South Africa and Uganda.</p>
        </div>
        <div className={styles.footerGrid}>
          {footer.map((group) => (
            <div key={group.heading}>
              <h2>{group.heading}</h2>
              {group.links.map(([label, href]) => <Link key={href} href={href}>{label}</Link>)}
            </div>
          ))}
        </div>
        <div className={styles.footerBottom}>
          <span>© {new Date().getUTCFullYear()} Baobab Platform. Publication and legal approvals pending.</span>
          <span>Public website information is not a statement of production service readiness.</span>
        </div>
      </div>
    </footer>
  );
}

export function SiteSection({ eyebrow, title, children, className = "" }: {
  eyebrow: string; title: string; children: ReactNode; className?: string
}) {
  return (
    <section className={className}>
      <div className={styles.container}>
        <p className={styles.eyebrow}>{eyebrow}</p>
        <h2 className={styles.sectionTitle}>{title}</h2>
        {children}
      </div>
    </section>
  );
}

export { styles };
