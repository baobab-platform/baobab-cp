"use client";
import Link from "next/link";
import { useWorkspaceAuthority } from "@/features/console/authority-context";
import { visibleConsoleRoutes } from "@/features/console/navigation-model";
import { Activity, ArrowRight, CheckCircle2, FilePenLine, Info, ShieldCheck } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";

const workspaces = [
  {
    href: "/workspace/apply", title: "Business application", icon: FilePenLine,
    description: "Describe your operating business, choose its markets and services, save a draft or submit an owned application.",
    action: "Open my application", detail: "Applicant",
  },
  {
    href: "/workspace/review", title: "Independent governance", icon: ShieldCheck,
    description: "Review submitted applications, record distinct decisions and make separately authorised onboarding requests.",
    action: "Open review workspace", detail: "Restricted authority",
  },
  {
    href: "/workspace/operations", title: "Provisioning & readiness", icon: Activity,
    description: "Read authoritative operation, plan and readiness records without confusing approval with active service.",
    action: "Inspect an operation", detail: "Authoritative readback",
  },
];

export default function WorkspaceIndex() {
  const authority = useWorkspaceAuthority();
  const available = new Set(visibleConsoleRoutes(authority).map((route) => route.href));
  const shownWorkspaces = workspaces.filter((item) => available.has(item.href as "/workspace/apply" | "/workspace/review" | "/workspace/operations"));
  return (
    <section aria-labelledby="workspace-title" className="space-y-8">
      <div className="rounded-2xl bg-brand-forest p-6 text-white sm:p-9">
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <span className="text-xs font-extrabold uppercase tracking-wider text-amber-200">Governed enterprise workspace</span>
          <span className="rounded-full border border-emerald-200 px-3 py-1 text-xs font-medium text-emerald-50">Access controlled</span>
        </div>
        <h1 id="workspace-title" className="max-w-2xl text-3xl font-bold tracking-tight sm:text-4xl">Where businesses become operational.</h1>
        <p className="mt-4 max-w-2xl text-base leading-relaxed text-emerald-50">
          Manage your business application and follow the governed decisions that connect intent to accountable operation.
        </p>
        <div className="mt-7"><Button asChild variant="gold"><Link href="/workspace/apply">Continue to my application <ArrowRight aria-hidden="true" /></Link></Button></div>
      </div>

      <div className="space-y-2">
        <p className="text-xs font-bold uppercase tracking-wider text-primary">Explore your workspace</p>
        <h2 className="text-2xl font-bold tracking-tight text-foreground">Choose the work you need to do.</h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          Only workspaces supported by your current effective grants are displayed. Every action is authorised again by the Control Plane.
        </p>
      </div>

      {authority.state === "unavailable" ? (
        <p role="status" className="rounded-lg border border-border bg-card p-4 text-sm leading-relaxed text-muted-foreground">
          Administrative access information is temporarily unavailable. Your own application remains accessible; privileged workspaces are hidden until their permissions can be confirmed.
        </p>
      ) : null}
      <div className="grid gap-4 lg:grid-cols-3">
        {shownWorkspaces.map(({ href, title, icon: Icon, description, action, detail }) => (
          <Card key={href} className="h-full justify-between border-border shadow-sm transition-shadow hover:shadow-md">
            <CardHeader>
              <div className="mb-4 flex items-center justify-between gap-3">
                <span className="grid size-11 place-items-center rounded-lg bg-[var(--status-success-bg)] text-primary">
                  <Icon aria-hidden="true" className="size-5" />
                </span>
                <Badge variant="neutral">{detail}</Badge>
              </div>
              <CardTitle>{title}</CardTitle>
              <CardDescription>{description}</CardDescription>
            </CardHeader>
            <CardContent className="sr-only">Access is controlled by IAM and Control Plane governance.</CardContent>
            <CardFooter>
              <Button asChild variant="outline" className="w-full justify-between">
                <Link href={href}>{action} <ArrowRight aria-hidden="true" /></Link>
              </Button>
            </CardFooter>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 rounded-xl border border-border bg-card p-6 md:grid-cols-[auto_1fr]">
        <Info aria-hidden="true" className="size-6 text-primary" />
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Understand the progression</h2>
          <p className="text-sm leading-relaxed text-muted-foreground">
            An application can be approved while tenant authorisation, provisioning, provider readiness and final activation remain outstanding.
          </p>
          <ol className="flex flex-wrap items-center gap-2 text-xs font-medium text-muted-foreground">
            {["Application", "Decision", "Authorisation", "Provisioning", "Readiness", "Activation"].map((step, i) => (
              <li key={step} className="flex items-center gap-2">
                {i > 0 && <ArrowRight aria-hidden="true" className="size-3" />}
                <span className="rounded-md bg-muted px-2 py-1">{step}</span>
              </li>
            ))}
          </ol>
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <CheckCircle2 aria-hidden="true" className="size-4 text-primary" />
            <span>This diagram illustrates lifecycle distinctions; it does not report live status.</span>
          </div>
        </div>
      </div>
    </section>
  );
}
