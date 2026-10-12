import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { legalPages } from "@/features/public-site/content";
import { SiteShell } from "@/features/public-site/SiteShell";

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
      <section className="bg-brand-forest py-16 text-white lg:py-24">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <p className="mb-4 text-xs font-extrabold uppercase tracking-widest text-brand-forest">PUBLICATION REVIEW REQUIRED</p>
          <h1>{item.title}</h1>
          <p>{item.introduction}</p>
        </div>
      </section>
      <section className="bg-white py-12 lg:py-20">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <div className="grid gap-4 md:grid-cols-2">
            <article className="rounded-xl border border-border p-6 [&_h2]:mb-4 [&_h2]:text-xl [&_h2]:font-semibold [&_p]:leading-relaxed [&_p]:text-muted-foreground [&_ul]:mt-4 [&_ul]:list-disc [&_ul]:pl-5">
              <h2>Before official publication</h2>
              <ul>{item.required.map((point) => <li key={point}>{point}</li>)}</ul>
            </article>
            <article className="rounded-xl border border-border p-6 [&_h2]:mb-4 [&_h2]:text-xl [&_h2]:font-semibold [&_p]:leading-relaxed [&_p]:text-muted-foreground [&_ul]:mt-4 [&_ul]:list-disc [&_ul]:pl-5">
              <h2>What this means</h2>
              <p>This page is an informational publication gate. It does not replace independently approved legal notices or any contractual terms that apply to a separately authorised service.</p>
            </article>
          </div>
          <div className="mt-10 flex flex-wrap items-center gap-5"><Link href="/" className="inline-flex min-h-11 items-center justify-center gap-2 rounded-md bg-brand-forest px-5 py-3 text-sm font-bold text-white hover:opacity-90">Return to Baobab <span aria-hidden="true">→</span></Link></div>
        </div>
      </section>
    </SiteShell>
  );
}
