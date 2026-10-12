import type { Metadata } from "next";
import Link from "next/link";
import { ArrowLeft, ArrowRight, Building2, CircleCheck, FileCheck2, UserRoundPlus } from "lucide-react";

import { SiteShell } from "@/features/public-site/SiteShell";
import { readAuthSettings } from "@/server/auth/runtime";

export const metadata: Metadata = {
  title: "Sign up and request access | Baobab Platform",
  description: "Understand how to request Baobab Platform access through a governed business onboarding process.",
};
export const dynamic = "force-dynamic";

export default function SignupPage() {
  let enabled = false;
  try { readAuthSettings(); enabled = true; } catch { /* Access is not publicly provisioned. */ }
  return (
    <SiteShell>
      <section className="bg-brand-ivory py-16 lg:py-24">
        <div className="mx-auto max-w-4xl px-5 md:px-10">
          <div className="rounded-2xl border border-border bg-white p-7 shadow-xl sm:p-10">
            <span className="grid size-12 place-items-center rounded-xl bg-brand-forest text-brand-gold">
              <UserRoundPlus aria-hidden="true" className="size-6" />
            </span>
            <p className="mt-6 text-xs font-bold uppercase tracking-widest text-brand-forest">Sign up and business onboarding</p>
            <h1 className="mt-3 text-3xl font-extrabold tracking-tight text-brand-ink sm:text-4xl">Start your Baobab access journey</h1>
            <p className="mt-4 max-w-2xl text-base leading-relaxed text-muted-foreground">
              Access begins with an approved identity and a governed business application. This public page does not create accounts, collect passwords or grant tenant access.
            </p>
            <div className="mt-8 grid gap-4 md:grid-cols-2">
              <article className="rounded-xl border border-border p-5">
                <Building2 aria-hidden="true" className="size-6 text-brand-forest" />
                <h2 className="mt-3 font-bold text-brand-ink">Describe your organisation</h2>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  Declare business identity, markets and planned activities truthfully. Incorporation status and legal mandates are verified independently.
                </p>
              </article>
              <article className="rounded-xl border border-border p-5">
                <FileCheck2 aria-hidden="true" className="size-6 text-brand-forest" />
                <h2 className="mt-3 font-bold text-brand-ink">Apply and follow decisions</h2>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  Authorised reviewers evaluate evidence, admission and provisioning. Acceptance does not automatically mean activation.
                </p>
              </article>
            </div>
            {enabled ? (
              <div className="mt-8 rounded-lg border border-border bg-brand-ivory p-5">
                <p className="flex gap-2 text-sm font-semibold text-brand-ink"><CircleCheck aria-hidden="true" className="size-5 shrink-0 text-brand-forest" /> Protected applications are available in this environment.</p>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">Sign in through the approved identity provider to submit or continue an application. New-account enrolment depends on that provider and Baobab admission policies.</p>
                <Link href="/workspace/apply" className="mt-5 inline-flex min-h-11 items-center gap-2 rounded-lg bg-brand-forest px-5 text-sm font-bold text-white hover:brightness-110 focus-visible:outline-2 focus-visible:outline-brand-gold">
                  Continue to protected application <ArrowRight aria-hidden="true" className="size-4" />
                </Link>
              </div>
            ) : (
              <div role="status" className="mt-8 rounded-lg border border-border bg-brand-ivory p-5 text-sm leading-relaxed text-brand-ink">
                <strong>Self-service account registration and protected applications are not enabled here yet.</strong>
                <p className="mt-2 text-muted-foreground">The public onboarding guide is available, but no account is created and no application information is collected until the approved identity and intake configuration is active.</p>
                <Link href="/start" className="mt-4 inline-flex min-h-11 items-center gap-2 font-semibold text-brand-forest underline-offset-4 hover:underline">
                  Explore the onboarding process <ArrowRight aria-hidden="true" className="size-4" />
                </Link>
              </div>
            )}
            <div className="mt-8 flex flex-wrap justify-between gap-4 border-t border-border pt-5">
              <Link href="/start" className="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-brand-forest underline-offset-4 hover:underline">
                <ArrowLeft aria-hidden="true" className="size-4" /> How it works
              </Link>
              <Link href="/login" className="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-brand-forest underline-offset-4 hover:underline">
                Already have access? Log in <ArrowRight aria-hidden="true" className="size-4" />
              </Link>
            </div>
          </div>
        </div>
      </section>
    </SiteShell>
  );
}
