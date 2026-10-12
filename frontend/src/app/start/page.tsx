import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight, Building2, CircleCheck, LockKeyhole, ShieldCheck } from "lucide-react";

import { SiteShell } from "@/features/public-site/SiteShell";
import { pages } from "@/features/public-site/content";
import { readAuthSettings } from "@/server/auth/runtime";

export const metadata: Metadata = {
  title: "Get started | Baobab Platform",
  description: "Learn how to apply for access to Baobab Platform's governed enterprise workspace.",
};
export const dynamic = "force-dynamic";

export default function GetStartedPage() {
  const page = pages.start;
  if (!page) throw new Error("Missing public onboarding content");
  let enabled = false;
  try { readAuthSettings(); enabled = true; } catch { /* Unconfigured protected Console remains disabled. */ }
  return (
    <SiteShell>
      <section className="bg-brand-forest py-16 text-white lg:py-24">
        <div className="mx-auto max-w-7xl px-5 md:px-10 lg:px-12">
          <span className="inline-flex items-center gap-2 text-xs font-bold uppercase tracking-widest text-amber-200">
            <Building2 aria-hidden="true" className="size-5" /> Business-first onboarding
          </span>
          <h1 className="mt-5 max-w-4xl text-4xl font-extrabold tracking-tight sm:text-5xl">{page.title}</h1>
          <p className="mt-5 max-w-3xl text-lg leading-relaxed text-emerald-100">{page.lead}</p>
          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link href="/signup" className="inline-flex min-h-11 items-center gap-2 rounded-lg bg-brand-gold px-5 py-3 text-sm font-bold text-brand-deep hover:brightness-105 focus-visible:outline-2 focus-visible:outline-white">
              Request access <ArrowRight aria-hidden="true" className="size-4" />
            </Link>
            <Link href="/login" className="inline-flex min-h-11 items-center gap-2 rounded-lg border border-emerald-200 px-5 py-3 text-sm font-semibold text-white hover:bg-emerald-800 focus-visible:outline-2 focus-visible:outline-white">
              Already have access? Log in <LockKeyhole aria-hidden="true" className="size-4" />
            </Link>
          </div>
        </div>
      </section>
      <section className="bg-white py-14 lg:py-20">
        <div className="mx-auto max-w-7xl px-5 md:px-10 lg:px-12">
          <h2 className="text-2xl font-bold tracking-tight text-brand-ink sm:text-3xl">Your path into the platform</h2>
          <div className="mt-8 grid gap-5 md:grid-cols-2">
            {page.sections.map((section, index) => (
              <article className="rounded-2xl border border-border bg-card p-6 shadow-sm" key={section.heading}>
                <span className="grid size-9 place-items-center rounded-full bg-brand-ivory font-bold text-brand-forest">{index + 1}</span>
                <h3 className="mt-4 text-lg font-bold text-brand-ink">{section.heading.replace(/^\d+\.\s*/, "")}</h3>
                <p className="mt-3 text-sm leading-relaxed text-muted-foreground">{section.description}</p>
              </article>
            ))}
          </div>
          <div className="mt-8 rounded-xl border border-border bg-brand-ivory p-6">
            <div className="flex gap-3">
              {enabled ? <CircleCheck aria-hidden="true" className="size-6 shrink-0 text-brand-forest" /> :
                <ShieldCheck aria-hidden="true" className="size-6 shrink-0 text-brand-forest" />}
              <div>
                <h3 className="font-bold text-brand-ink">{enabled ? "Secure application workspace available" : "Protected application access is not enabled here"}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {enabled
                    ? "Sign in with an approved identity to begin or continue a governed application. An application does not automatically activate a tenant."
                    : "The public guide is available, but this environment is not configured for protected sign-in or account applications. No application data is collected on this page."}
                </p>
                {enabled ? <Link href="/workspace/apply" className="mt-3 inline-flex min-h-11 items-center gap-2 font-semibold text-brand-forest underline-offset-4 hover:underline">
                  Open protected application <ArrowRight aria-hidden="true" className="size-4" />
                </Link> : null}
              </div>
            </div>
          </div>
        </div>
      </section>
    </SiteShell>
  );
}
