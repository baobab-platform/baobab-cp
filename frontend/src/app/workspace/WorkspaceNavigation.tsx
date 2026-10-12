"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, ClipboardCheck, FilePenLine, LayoutDashboard, ShieldCheck } from "lucide-react";
import { cn } from "@/lib/utils";

const entries = [
  { href: "/workspace", label: "Overview", icon: LayoutDashboard, detail: "Current work" },
  { href: "/workspace/apply", label: "My application", icon: FilePenLine, detail: "Your business" },
  { href: "/workspace/review", label: "Independent review", icon: ShieldCheck, detail: "Restricted" },
  { href: "/workspace/operations", label: "Provisioning & readiness", icon: Activity, detail: "Read-only evidence" },
] as const;

function NavigationItems() {
  const pathname = usePathname();
  return (
    <ul className="grid gap-1">
      {entries.map(({ href, label, detail, icon: Icon }) => {
        const active = pathname === href;
        return (
          <li key={href}>
            <Link
              href={href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "group flex min-h-11 items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors",
                "hover:bg-muted hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-focus",
                active ? "bg-[var(--status-success-bg)] font-semibold text-primary" : "text-muted-foreground",
              )}
            >
              <Icon aria-hidden="true" className="size-5 shrink-0" />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span>{label}</span>
                <span className="text-xs font-normal text-muted-foreground">{detail}</span>
              </span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

/** Navigational cues only; CP checks authority on every actual API command. */
export function WorkspaceNavigation() {
  return (
    <>
      <nav aria-label="Console workspace" className="hidden self-start rounded-xl border border-border bg-card p-3 shadow-sm md:block">
        <div className="mb-3 flex items-center gap-2 px-3 py-2">
          <ClipboardCheck aria-hidden="true" className="size-5 text-primary" />
          <span className="text-xs font-extrabold uppercase tracking-wider text-muted-foreground">Workspaces</span>
        </div>
        <NavigationItems />
        <p className="mt-4 border-t border-border px-3 pt-4 text-xs leading-relaxed text-muted-foreground">
          Menus do not grant access. Each activity is authorised independently by the Control Plane.
        </p>
      </nav>
      <nav aria-label="Mobile Console workspace" className="rounded-xl border border-border bg-card p-3 shadow-sm md:hidden">
        <details className="group">
          <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between rounded-md px-3 text-sm font-semibold text-primary [&::-webkit-details-marker]:hidden">
            Workspace navigation <span aria-hidden="true">⌄</span>
          </summary>
          <div className="mt-2 border-t border-border pt-3"><NavigationItems /></div>
        </details>
      </nav>
    </>
  );
}
