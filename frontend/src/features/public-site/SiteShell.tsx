import Link from "next/link";
import { ArrowUpRight, ChevronDown, Menu, Network } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ReactNode } from "react";

import { navigation } from "./content";

export function SiteShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-white font-sans text-brand-ink">
      <div className="flex items-center justify-between gap-4 bg-brand-deep px-5 py-2 text-xs text-emerald-50 md:px-10 [&_a]:text-amber-300">
        <span>Business infrastructure for an interconnected Africa</span>
        <Link href="/trust">Our approach to trust <ArrowUpRight aria-hidden="true" className="size-4" /></Link>
      </div>
      <header className="relative z-30 border-b border-border bg-white">
        <div className="mx-auto flex min-h-20 max-w-7xl items-center gap-5 px-5 md:px-10">
          <Link href="/" className="flex shrink-0 items-center gap-2 whitespace-nowrap text-base font-extrabold tracking-wider text-brand-forest no-underline" aria-label="Baobab Platform, homepage">
            <span className="grid size-9 place-items-center rounded-md bg-brand-forest text-brand-gold" aria-hidden="true"><Network className="size-5" /></span>
            <span>BAOBAB<span className="hidden font-medium tracking-normal lg:inline"> PLATFORM</span></span>
          </Link>
          <nav aria-label="Primary navigation" className="hidden flex-1 items-center lg:flex">
            {navigation.map((group) => (
              <details key={group.name} className="group relative">
                <summary className="flex min-h-16 cursor-pointer list-none items-center gap-2 px-3 text-sm font-semibold text-brand-ink [&::-webkit-details-marker]:hidden">{group.name}<ChevronDown aria-hidden="true" className="size-4 text-muted-foreground" /></summary>
                <div className="absolute top-full left-0 z-50 w-[min(36rem,90vw)] rounded-b-xl border border-border bg-white p-6 shadow-xl">
                  <span className="text-xs font-extrabold tracking-widest text-brand-forest">EXPLORE {group.name.toUpperCase()}</span>
                  <div className="mt-4 grid grid-cols-2 gap-2">
                    {group.links.map((item) => (
                      <Link key={item.href} href={item.href} className="block rounded-lg p-3 no-underline hover:bg-brand-ivory focus-visible:bg-brand-ivory [&_strong]:block [&_strong]:text-sm [&_strong]:text-brand-ink [&_span]:mt-1 [&_span]:block [&_span]:text-xs [&_span]:leading-relaxed [&_span]:text-muted-foreground">
                        <strong>{item.label}</strong><span>{item.description}</span>
                      </Link>
                    ))}
                  </div>
                </div>
              </details>
            ))}
          </nav>
          <div className="hidden shrink-0 items-center gap-4 lg:flex">
            <Link className="text-sm font-bold text-brand-forest" href="/workspace">Console <span aria-hidden="true">↗</span></Link>
            <Button asChild><Link href="/start">Get started <ArrowUpRight aria-hidden="true" /></Link></Button>
          </div>
          <details className="relative ml-auto lg:hidden">
            <summary aria-label="Open navigation" className="flex min-h-11 cursor-pointer list-none items-center gap-2 font-semibold [&::-webkit-details-marker]:hidden">Menu <Menu aria-hidden="true" className="size-5" /></summary>
            <nav aria-label="Mobile navigation" className="absolute top-full right-0 z-50 flex max-h-[75vh] w-[min(23rem,calc(100vw-2rem))] flex-col gap-2 overflow-y-auto rounded-lg border border-border bg-white p-4 shadow-xl [&_a]:block [&_a]:py-1 [&_strong]:text-brand-forest">
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
  const footer: { heading: string; links: [string, string][] }[] = [
    { heading: "Platform", links: [["Overview", "/platform"], ["Architecture", "/architecture"], ["Trust", "/trust"], ["Markets", "/markets"]] },
    { heading: "Solutions", links: [["Solutions", "/solutions"], ["Industries", "/industries"], ["Getting started", "/start"], ["FAQs", "/faq"]] },
    { heading: "Developers", links: [["Developer centre", "/developers"], ["Documentation", "/documentation"], ["Status", "/status"], ["Console", "/workspace"]] },
    { heading: "Company", links: [["About", "/company"], ["Resources", "/resources"], ["Contact", "/contact"], ["Accessibility", "/accessibility"]] },
    { heading: "Legal", links: [["Privacy", "/legal/privacy"], ["Terms", "/legal/terms"], ["Cookies", "/legal/cookies"]] },
  ];
  return (
    <footer className="bg-brand-deep py-16 text-emerald-50">
      <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
        <div className="mb-12 max-w-xl space-y-3">
          <Link href="/" className="text-lg font-extrabold tracking-widest text-white">BAOBAB <span>PLATFORM</span></Link>
          <p>Independent enterprises. One connected foundation.</p>
          <p className="text-sm leading-relaxed text-emerald-200">Designed for businesses across Africa, with initial market planning in South Africa and Uganda.</p>
        </div>
        <div className="grid grid-cols-2 gap-6 sm:grid-cols-3 lg:grid-cols-5 [&_a]:my-2 [&_a]:block [&_a]:text-sm [&_a]:text-emerald-100 [&_h2]:mb-4 [&_h2]:text-base [&_h2]:font-semibold">
          {footer.map((group) => (
            <div key={group.heading}>
              <h2>{group.heading}</h2>
              {group.links.map(([label, href]) => <Link key={href} href={href}>{label}</Link>)}
            </div>
          ))}
        </div>
        <div className="mt-12 flex flex-wrap justify-between gap-4 border-t border-emerald-700 pt-5 text-xs text-emerald-200">
          <span>© {new Date().getUTCFullYear()} Baobab Platform. Publication and legal approvals pending.</span>
          <span>Public website information is not a statement of production service readiness.</span>
        </div>
      </div>
    </footer>
  );
}

export function SiteSection({ eyebrow, title, children, className = "" }: {
  eyebrow: string; title: string; children: ReactNode; className?: string | undefined
}) {
  return (
    <section className={className}>
      <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
        <p className="mb-4 text-xs font-extrabold uppercase tracking-widest text-brand-forest">{eyebrow}</p>
        <h2 className="max-w-4xl text-3xl font-bold tracking-tight text-brand-ink sm:text-4xl lg:text-5xl">{title}</h2>
        {children}
      </div>
    </section>
  );
}

