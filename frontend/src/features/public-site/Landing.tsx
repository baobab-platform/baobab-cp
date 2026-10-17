import Link from "next/link";
import { ArrowRight, ArrowUpRight, Blocks, BrainCircuit, Building2, LogIn, ShieldCheck, ShoppingCart, Workflow, Check } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardTitle, CardDescription } from "@/components/ui/card";

import { SiteShell, SiteSection } from "./SiteShell";

const capabilities = [
  { icon: ShoppingCart, n: "01", title: "Commerce & trade", description: "Build B2B and consumer experiences around governed trade capabilities.", href: "/solutions" },
  { icon: Building2, n: "02", title: "Enterprise operations", description: "Connect operational and financial domains with clear ownership boundaries.", href: "/solutions" },
  { icon: Blocks, n: "03", title: "Digital experiences", description: "Create coherent websites, portals and content-led journeys.", href: "/architecture" },
  { icon: BrainCircuit, n: "04", title: "Market intelligence", description: "Turn evidence-informed insights into better business decisions.", href: "/solutions" },
  { icon: ShieldCheck, n: "05", title: "Identity & governance", description: "Maintain explicit accountability, access controls and operating mandates.", href: "/trust" },
  { icon: Workflow, n: "06", title: "Provisioning & readiness", description: "Follow plan, approval, execution and independent readiness evidence.", href: "/architecture" },
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
      <section className="overflow-hidden bg-[radial-gradient(ellipse_at_95%_20%,#235b43_0%,#103d2d_50%,#092a20_100%)] text-white">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <div className="grid items-center gap-12 py-16 lg:grid-cols-2 lg:py-28">
            <div className="space-y-5">
              <p className="flex items-center gap-2 text-xs font-bold uppercase tracking-widest text-amber-200"><span className="size-2 rounded-full bg-brand-gold" /> THE ENTERPRISE FOUNDATION FOR AFRICA</p>
              <h1 className="text-5xl font-extrabold leading-tight tracking-tight sm:text-6xl xl:text-7xl">One platform.<br /><em className="not-italic text-amber-200">Every business possibility.</em></h1>
              <p className="max-w-xl text-lg leading-relaxed text-emerald-100">Build, connect and manage business operations across markets through a governed, extensible enterprise foundation.</p>
              <div className="mt-8 flex flex-wrap items-center gap-3">
                <Link href="/start" className={buttonVariants({ variant: "gold" })}>Get started <ArrowUpRight aria-hidden="true" /></Link>
                <Link href="/login" className="inline-flex min-h-11 items-center gap-3 rounded-md border border-emerald-200 px-5 py-3 text-sm font-semibold text-white hover:bg-emerald-800 focus-visible:outline-2 focus-visible:outline-amber-200"><LogIn className="size-4" aria-hidden="true" /> Log in to Console</Link>
                <Link href="/platform" className="inline-flex min-h-11 items-center gap-2 px-2 text-sm font-semibold text-emerald-100 underline-offset-4 hover:underline">Explore platform <ArrowRight className="size-4" aria-hidden="true" /></Link>
              </div>
              <p className="max-w-lg text-xs leading-relaxed text-emerald-100">Platform capabilities are subject to authorisation and independently verified operational readiness.</p>
            </div>
            <div className="rounded-2xl border border-emerald-500/60 bg-[linear-gradient(145deg,#1b5840,#0b3025)] p-5 shadow-2xl sm:p-8" aria-label="Illustration of Control Plane connecting business services">
              <div className="flex flex-wrap justify-between gap-2 text-[10px] font-bold tracking-widest text-emerald-100">
                <span>PLATFORM INTELLIGENCE</span>
                <span>GOVERNED BY DESIGN</span>
              </div>
              <div className="mt-10 grid gap-2 rounded-xl bg-white p-6 text-center text-brand-ink shadow-xl">
                <span className="text-xs font-bold uppercase tracking-widest text-brand-forest">CENTRAL GOVERNANCE</span>
                <strong>BAOBAB CONTROL PLANE</strong>
                <span>Organisation · Tenants · Markets · Readiness</span>
              </div>
              <div className="mx-auto h-12 w-px bg-emerald-200" aria-hidden="true"><span /></div>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {["Trade", "ERP", "CMS", "Pulse"].map((label) => (
                  <div key={label} className="grid place-items-center gap-1 rounded-lg border border-emerald-300/70 bg-emerald-800/70 px-2 py-4 text-center text-white">
                    <Blocks className="size-5 text-amber-200" aria-hidden="true" /><strong>{label}</strong>
                    <span>Governed capability</span>
                  </div>
                ))}
              </div>
              <p className="mt-6 text-center text-xs text-emerald-100">Conceptual architecture · Individual engines retain their own authority</p>
            </div>
          </div>
          <div className="flex flex-wrap justify-between gap-4 border-t border-emerald-500/60 py-6 text-[11px] font-bold tracking-wider text-emerald-100">
            <span>BUSINESS-FIRST ONBOARDING</span><span>EXPLICIT TENANT AUTONOMY</span><span>MARKET-AWARE OPERATIONS</span>
          </div>
        </div>
      </section>

      <SiteSection eyebrow="THE PLATFORM" title="Complexity behind the scenes. Clarity for your business." className="bg-white py-16 lg:py-24">
        <div className="my-8 flex flex-col justify-between gap-6 md:flex-row md:items-end [&_p]:max-w-2xl [&_p]:leading-relaxed [&_p]:text-muted-foreground">
          <p>The platform&apos;s Control Plane coordinates authorised business intent while specialist engines remain responsible for their own work. You see one coherent experience—not a pile of unrelated software.</p>
          <Link href="/architecture" className="inline-flex items-center gap-2 font-bold text-brand-forest underline-offset-4 hover:underline">Discover the architecture <span aria-hidden="true">↗</span></Link>
        </div>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {capabilities.map((cap) => (
            <Card className="min-h-64 items-start" key={cap.n}>
              <span className="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-brand-forest"><cap.icon className="size-5" aria-hidden="true" />{cap.n} / CAPABILITY</span>
              <CardTitle>{cap.title}</CardTitle>
              <CardDescription>{cap.description}</CardDescription>
              <Link href={cap.href} aria-label={"Explore " + cap.title}>Explore <span aria-hidden="true">↗</span></Link>
            </Card>
          ))}
        </div>
      </SiteSection>

      <SiteSection eyebrow="PROGRESSIVE ONBOARDING" title="From business ambition to operational readiness." className="bg-brand-ivory py-16 lg:py-24">
        <p className="my-7 max-w-2xl text-lg leading-relaxed text-muted-foreground">An understandable business journey, with governed decisions and relevant evidence at the right stage.</p>
        <ol className="my-9 grid list-none gap-5 p-0 sm:grid-cols-2 lg:grid-cols-5 [&_li]:border-t-2 [&_li]:border-brand-forest [&_li]:pt-5 [&_li>span]:text-xl [&_li>span]:font-bold [&_li>span]:text-brand-forest [&_h3]:my-3 [&_h3]:text-lg [&_h3]:font-semibold [&_p]:text-sm [&_p]:leading-relaxed [&_p]:text-muted-foreground">
          {stages.map(([n, title, description]) => (
            <li key={n}><span>{n}</span><h3>{title}</h3><p>{description}</p></li>
          ))}
        </ol>
        <Link href="/start" className="inline-flex min-h-11 items-center justify-center gap-2 rounded-md bg-brand-forest px-5 py-3 text-sm font-bold text-white hover:opacity-90">See how onboarding works <span aria-hidden="true">↗</span></Link>
      </SiteSection>

      <SiteSection eyebrow="BUILT FOR INDEPENDENCE" title="One group can contain many autonomous businesses." className="bg-white py-16 lg:py-24">
        <div className="mt-8 grid items-start gap-10 lg:grid-cols-2">
          <div>
            <p className="my-7 max-w-2xl text-lg leading-relaxed text-muted-foreground">Corporate affiliation, operating identity, legal responsibility and tenant permission are not interchangeable. Baobab is designed to preserve those distinctions.</p>
            <Link href="/trust" className="inline-flex items-center gap-2 font-bold text-brand-forest underline-offset-4 hover:underline">Why governance matters <span aria-hidden="true">↗</span></Link>
          </div>
          <div className="grid gap-4 [&>div]:flex [&>div]:gap-4 [&>div]:rounded-xl [&>div]:border [&>div]:border-border [&>div]:p-5 [&>div>span]:font-extrabold [&>div>span]:text-brand-forest [&_p]:leading-relaxed [&_p]:text-muted-foreground [&_strong]:text-brand-ink">
            {[
              ["Organisation first", "Every tenant has a primary business identity."],
              ["Authority when needed", "Legal responsibility is governed by activity and market."],
              ["Boundaries that hold", "Parent-company affiliation does not automatically confer access."],
            ].map(([title, desc]) => <div key={title}><Check className="size-5 shrink-0" aria-hidden="true" /><p><strong>{title}</strong><br />{desc}</p></div>)}
          </div>
        </div>
      </SiteSection>

      <section className="bg-brand-forest py-16 text-white [&_h2]:my-4 [&_h2]:text-4xl [&_h2]:font-bold [&_p:not(.eyebrow)]:max-w-3xl [&_p:not(.eyebrow)]:leading-relaxed">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <div>
            <p className="mb-4 text-xs font-extrabold uppercase tracking-widest text-brand-forest">BUILT FOR AFRICAN MARKETS</p>
            <h2>Local context. Regional possibility.</h2>
            <p>South Africa and Uganda are the initial market focus. Expansion is grounded in explicit permissions, appropriate providers and the reality of each jurisdiction.</p>
          </div>
          <Link href="/markets" className="inline-flex min-h-11 items-center justify-center gap-3 rounded-md border border-emerald-200 px-5 py-3 text-sm font-semibold text-white no-underline hover:bg-white/10">Explore markets <span aria-hidden="true">↗</span></Link>
        </div>
      </section>

      <SiteSection eyebrow="BUILT FOR DEVELOPERS" title="Open architecture. Clear responsibility." className="bg-white py-16 lg:py-24">
        <div className="mt-8 grid items-start gap-10 lg:grid-cols-2">
          <p className="my-7 max-w-2xl text-lg leading-relaxed text-muted-foreground">Contract-first APIs, bounded domains, a separately authenticated Console and explicit capability resolution help product teams build without bypassing the Control Plane.</p>
          <div className="grid gap-5 rounded-xl bg-brand-deep p-7 text-emerald-50">
            <span className="text-xs font-bold uppercase tracking-wider text-amber-200">ARCHITECTURE AT A GLANCE</span>
            <code>Browser → Secure Console / BFF<br />Console → Control Plane API<br />Control Plane → authorised services</code>
            <Link href="/developers">Developer centre <span aria-hidden="true">↗</span></Link>
          </div>
        </div>
      </SiteSection>

      <SiteSection eyebrow="FREQUENTLY ASKED" title="Clarity before commitment." className="bg-brand-ivory py-16 lg:py-24">
        <div className="my-8 divide-y divide-border border-y border-border [&_details]:py-5 [&_p]:mt-4 [&_p]:leading-relaxed [&_p]:text-muted-foreground [&_summary]:flex [&_summary]:cursor-pointer [&_summary]:list-none [&_summary]:justify-between [&_summary]:gap-3 [&_summary]:font-semibold [&_summary::-webkit-details-marker]:hidden">
          {FAQs.map(([question, answer]) => (
            <details key={question}><summary>{question}<span aria-hidden="true">+</span></summary><p>{answer}</p></details>
          ))}
        </div>
        <Link href="/faq" className="inline-flex items-center gap-2 font-bold text-brand-forest underline-offset-4 hover:underline">All questions <span aria-hidden="true">↗</span></Link>
      </SiteSection>

      <section className="bg-brand-deep py-20 text-white [&_h2]:my-5 [&_h2]:max-w-4xl [&_h2]:text-4xl [&_h2]:font-bold [&_h2]:tracking-tight [&_p]:max-w-3xl [&_p]:leading-relaxed">
        <div className="mx-auto w-full max-w-7xl px-5 md:px-10 lg:px-12">
          <p className="flex items-center gap-2 text-xs font-bold uppercase tracking-widest text-amber-200">BUILD WITH PURPOSE</p>
          <h2>The next chapter of your business starts with a better foundation.</h2>
          <p>Explore Baobab, understand the operating model, and begin the governed onboarding journey when access is available.</p>
          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link href="/start" className={buttonVariants({ variant: "gold" })}>Get started <ArrowUpRight aria-hidden="true" /></Link>
            <Link href="/login" className="inline-flex min-h-11 items-center justify-center gap-3 rounded-md border border-emerald-200 px-5 py-3 text-sm font-semibold text-white no-underline hover:bg-white/10">Log in to secure Console <span aria-hidden="true">→</span></Link>
          </div>
          <p className="max-w-lg text-xs leading-relaxed text-emerald-100">Console sign-in requires authorised access and an enabled environment.</p>
        </div>
      </section>
    </SiteShell>
  );
}
