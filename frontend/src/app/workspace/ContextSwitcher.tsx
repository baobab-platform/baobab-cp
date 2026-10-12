"use client";

import { usePathname, useRouter } from "next/navigation";
import { Building2, ChevronDown, Globe2, Layers3 } from "lucide-react";

import { contextHref, type ConsoleContextOption } from "@/features/console/context-model";

/** A navigation switch, not a tenant context bearer or a persisted authority
 * selector. Destination independently rechecks grants and CP resource reads.
 */
export function ContextSwitcher({ options, verified }: { options: readonly ConsoleContextOption[]; verified: boolean }) {
  const router = useRouter();
  const pathname = usePathname();
  const active = options.find((option) => contextHref(option) === pathname);
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="hidden text-xs font-semibold text-muted-foreground lg:inline">Context</span>
      <div className="relative min-w-0">
        <Layers3 aria-hidden="true" className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-primary" />
        <label className="sr-only" htmlFor="workspace-context-select">Current governed resource context</label>
        <select id="workspace-context-select" value={active ? contextHref(active) : ""}
          onChange={(event) => {
            const next = options.find((option) => contextHref(option) === event.target.value);
            if (next) router.push(contextHref(next));
          }}
          disabled={!verified || options.length === 0}
          className="h-11 max-w-52 min-w-36 appearance-none truncate rounded-lg border border-border bg-card pr-9 pl-9 text-xs font-semibold text-foreground focus-visible:outline-2 focus-visible:outline-focus sm:max-w-64">
          <option value="">{verified ? "Select context" : "Context unavailable"}</option>
          {options.map((option) => (
            <option value={contextHref(option)} key={option.kind + ":" + option.id}>
              {option.kind === "organisation" ? "Organisation" : "Tenant"} · {option.label}
            </option>
          ))}
        </select>
        <ChevronDown aria-hidden="true" className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-muted-foreground" />
      </div>
      {active?.kind === "organisation" ? <Building2 aria-hidden="true" className="hidden size-4 text-primary xl:block" /> : null}
      {active?.kind === "tenant" ? <Globe2 aria-hidden="true" className="hidden size-4 text-primary xl:block" /> : null}
    </div>
  );
}
