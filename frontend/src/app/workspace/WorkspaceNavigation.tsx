"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, Building2, ClipboardCheck, FilePenLine, LayoutDashboard, ShieldCheck, Info } from "lucide-react";
import { cn } from "@/lib/utils";
import { useWorkspaceAuthority } from "@/features/console/authority-context";
import { visibleConsoleRoutes } from "@/features/console/navigation-model";

const icons = {
  "/workspace": LayoutDashboard,
  "/workspace/apply": FilePenLine,
  "/workspace/review": ShieldCheck,
  "/workspace/operations": Activity,
  "/workspace/organisations": Building2,
} as const;

function NavigationItems() {
  const pathname = usePathname();
  const authority = useWorkspaceAuthority();
  const routes = visibleConsoleRoutes(authority);
  return (
    <ul className="grid gap-1">
      {routes.map(({ href, label, description }) => {
        const active = pathname === href;
        const Icon = icons[href];
        return (
          <li key={href}>
            <Link href={href} aria-current={active ? "page" : undefined}
              className={cn("group flex min-h-11 items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors",
                "hover:bg-muted hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-focus",
                active ? "bg-[var(--status-success-bg)] font-semibold text-primary" : "text-muted-foreground")}>
              <Icon aria-hidden="true" className="size-5 shrink-0" />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span>{label}</span><span className="text-xs font-normal text-muted-foreground">{description}</span>
              </span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

export function WorkspaceNavigation() {
  const authority = useWorkspaceAuthority();
  const note = authority.state === "unavailable"
    ? "Administrative navigation is hidden until current authority can be verified."
    : "Administrative links reflect your current grants. Actions are independently authorised.";
  return (
    <>
      <nav aria-label="Console workspace" className="hidden self-start rounded-xl border border-border bg-card p-3 shadow-sm md:block">
        <div className="mb-3 flex items-center gap-2 px-3 py-2">
          <ClipboardCheck aria-hidden="true" className="size-5 text-primary" />
          <span className="text-xs font-extrabold uppercase tracking-wider text-muted-foreground">Workspaces</span>
        </div>
        <NavigationItems />
        <p className="mt-4 flex gap-2 border-t border-border px-3 pt-4 text-xs leading-relaxed text-muted-foreground">
          <Info aria-hidden="true" className="mt-0.5 size-4 shrink-0" />{note}
        </p>
      </nav>
      <nav aria-label="Mobile Console workspace" className="rounded-xl border border-border bg-card p-3 shadow-sm md:hidden">
        <details className="group">
          <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between rounded-md px-3 text-sm font-semibold text-primary [&::-webkit-details-marker]:hidden">
            Workspace navigation <span aria-hidden="true">⌄</span>
          </summary>
          <div className="mt-2 border-t border-border pt-3">
            <NavigationItems />
            <p className="mt-4 text-xs leading-relaxed text-muted-foreground">{note}</p>
          </div>
        </details>
      </nav>
    </>
  );
}
