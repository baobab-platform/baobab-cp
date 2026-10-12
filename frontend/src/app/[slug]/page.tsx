import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { pages } from "@/features/public-site/content";
import { SiteShell } from "@/features/public-site/SiteShell";

export function generateStaticParams() { return Object.keys(pages).filter((slug) => slug !== "start").map((slug) => ({ slug })); }
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
      <section className="bg-brand-forest py-16 text-white lg:py-24">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <p className="mb-4 text-xs font-extrabold uppercase tracking-widest text-brand-forest">{page.eyebrow.toUpperCase()}</p>
          <h1>{page.title}</h1>
          <p>{page.lead}</p>
        </div>
      </section>
      <section className="bg-white py-12 lg:py-20">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <div className="grid gap-4 md:grid-cols-2">
            {page.sections.map((section) => (
              <article className="rounded-xl border border-border p-6 [&_h2]:mb-4 [&_h2]:text-xl [&_h2]:font-semibold [&_p]:leading-relaxed [&_p]:text-muted-foreground [&_ul]:mt-4 [&_ul]:list-disc [&_ul]:pl-5" key={section.heading}>
                <h2>{section.heading}</h2>
                <p>{section.description}</p>
                {section.points ? <ul>{section.points.map((point) => <li key={point}>{point}</li>)}</ul> : null}
              </article>
            ))}
          </div>
          {page.note ? <p role="note" className="mt-8 border-l-4 border-amber-600 bg-amber-50 p-5 text-amber-950">{page.note}</p> : null}
          <div className="mt-10 flex flex-wrap items-center gap-5">
            <Link className="inline-flex min-h-11 items-center justify-center gap-2 rounded-md bg-brand-forest px-5 py-3 text-sm font-bold text-white hover:opacity-90" href={page.href}>{page.action} <span aria-hidden="true">↗</span></Link>
            <Link className="inline-flex items-center gap-2 font-bold text-brand-forest underline-offset-4 hover:underline" href="/contact">Contact and enquiries <span aria-hidden="true">→</span></Link>
          </div>
        </div>
      </section>
    </SiteShell>
  );
}
