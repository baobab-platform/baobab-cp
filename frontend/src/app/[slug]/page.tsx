import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { pages } from "@/features/public-site/content";
import { SiteShell, styles } from "@/features/public-site/SiteShell";

export function generateStaticParams() { return Object.keys(pages).map((slug) => ({ slug })); }
export const dynamicParams = false;
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params;
  const page = pages[slug];
  if (!page) return {};
  return { title: page.title + " | Baobab Platform", description: page.lead };
}
export default async function PublicPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const page = pages[slug];
  if (!page) notFound();
  return (
    <SiteShell>
      <section className={styles.contentHero}>
        <div className={styles.container}>
          <p className={styles.eyebrow}>{page.eyebrow.toUpperCase()}</p>
          <h1>{page.title}</h1>
          <p>{page.lead}</p>
        </div>
      </section>
      <section className={styles.contentArea}>
        <div className={styles.container}>
          <div className={styles.contentGrid}>
            {page.sections.map((section) => (
              <article className={styles.contentCard} key={section.heading}>
                <h2>{section.heading}</h2>
                <p>{section.description}</p>
                {section.points ? <ul>{section.points.map((point) => <li key={point}>{point}</li>)}</ul> : null}
              </article>
            ))}
          </div>
          {page.note ? <p role="note" className={styles.contentNote}>{page.note}</p> : null}
          <div className={styles.contentAction}>
            <Link className={styles.darkButton} href={page.href}>{page.action} <span aria-hidden="true">↗</span></Link>
            <Link className={styles.textLink} href="/contact">Contact and enquiries <span aria-hidden="true">→</span></Link>
          </div>
        </div>
      </section>
    </SiteShell>
  );
}
