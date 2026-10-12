import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { legalPages } from "@/features/public-site/content";
import { SiteShell, styles } from "@/features/public-site/SiteShell";

export function generateStaticParams() { return Object.keys(legalPages).map((slug) => ({ slug })); }
export const dynamicParams = false;
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params;
  const item = legalPages[slug];
  return item ? { title: item.title + " | Baobab Platform", robots: { index: false, follow: false } } : {};
}
export default async function LegalPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const item = legalPages[slug];
  if (!item) notFound();
  return (
    <SiteShell>
      <section className={styles.contentHero}>
        <div className={styles.container}>
          <p className={styles.eyebrow}>PUBLICATION REVIEW REQUIRED</p>
          <h1>{item.title}</h1>
          <p>{item.introduction}</p>
        </div>
      </section>
      <section className={styles.contentArea}>
        <div className={styles.container}>
          <div className={styles.contentGrid}>
            <article className={styles.contentCard}>
              <h2>Before official publication</h2>
              <ul>{item.required.map((point) => <li key={point}>{point}</li>)}</ul>
            </article>
            <article className={styles.contentCard}>
              <h2>What this means</h2>
              <p>This page is an informational publication gate. It does not replace independently approved legal notices or any contractual terms that apply to a separately authorised service.</p>
            </article>
          </div>
          <div className={styles.contentAction}><Link href="/" className={styles.darkButton}>Return to Baobab <span aria-hidden="true">→</span></Link></div>
        </div>
      </section>
    </SiteShell>
  );
}
