import type { Metadata } from "next";
import Link from "next/link";
import { ArrowLeft, ArrowRight, LockKeyhole, ShieldCheck } from "lucide-react";

import { SiteShell } from "@/features/public-site/SiteShell";
import { readAuthSettings } from "@/server/auth/runtime";

export const metadata: Metadata = {
  title: "Log in to Control Plane | Baobab Platform",
  description: "Sign in to the secure Baobab Control Plane Console using an approved identity.",
};
export const dynamic = "force-dynamic";

export default function ConsoleLoginPage() {
  let enabled = false;
  try { readAuthSettings(); enabled = true; } catch { /* Do not present an unusable login. */ }
  return (
    <SiteShell>
      <section className="bg-brand-ivory py-16 lg:py-24">
        <div className="mx-auto max-w-3xl px-5 md:px-10">
          <div className="rounded-2xl border border-border bg-white p-7 shadow-xl sm:p-10">
            <span className="grid size-12 place-items-center rounded-xl bg-brand-forest text-brand-gold">
              <LockKeyhole aria-hidden="true" className="size-6" />
            </span>
            <p className="mt-6 text-xs font-bold uppercase tracking-widest text-brand-forest">Secure enterprise workspace</p>
            <h1 className="mt-3 text-3xl font-extrabold tracking-tight text-brand-ink sm:text-4xl">Log in to the Baobab Console</h1>
            <p className="mt-4 text-base leading-relaxed text-muted-foreground">
              Access Organisation 360°, governed applications, approvals, provisioning and readiness through your approved enterprise identity.
            </p>
            {enabled ? (
              <>
                <Link href="/api/auth/login" prefetch={false} className="mt-8 inline-flex min-h-12 w-full items-center justify-center gap-2 rounded-lg bg-brand-forest px-6 py-3 text-sm font-bold text-white hover:brightness-110 focus-visible:outline-2 focus-visible:outline-brand-gold">
                  Continue to secure sign in <ArrowRight aria-hidden="true" className="size-4" />
                </Link>
                <p className="mt-3 text-xs leading-relaxed text-muted-foreground">Authentication is handled by the approved identity provider. Access remains subject to organisation and tenant permissions.</p>
              </>
            ) : (
              <div role="status" className="mt-8 rounded-lg border border-border bg-brand-ivory p-5 text-sm leading-relaxed text-brand-ink">
                <strong>Console sign-in is not available in this environment.</strong>
                <p className="mt-2 text-muted-foreground">The protected Console has not been enabled with approved IAM configuration and session infrastructure. The public website remains available; no credentials are collected here.</p>
              </div>
            )}
            <div className="mt-8 flex flex-wrap items-center justify-between gap-4 border-t border-border pt-5">
              <Link href="/start" className="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-brand-forest underline-offset-4 hover:underline">
                <ArrowLeft aria-hidden="true" className="size-4" /> Getting started
              </Link>
              <Link href="/signup" className="inline-flex min-h-11 items-center gap-2 text-sm font-semibold text-brand-forest underline-offset-4 hover:underline">
                New to Baobab? Sign up / request access <ArrowRight aria-hidden="true" className="size-4" />
              </Link>
            </div>
          </div>
          <p className="mt-6 flex gap-2 text-xs leading-relaxed text-muted-foreground">
            <ShieldCheck aria-hidden="true" className="size-4 shrink-0" /> The Console is separate from the public website. Public browsing does not grant identity, membership or service access.
          </p>
        </div>
      </section>
    </SiteShell>
  );
}
